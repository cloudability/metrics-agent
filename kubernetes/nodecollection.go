package kubernetes

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudability/metrics-agent/retrieval/raw"
	"github.com/cloudability/metrics-agent/util"
	log "github.com/sirupsen/logrus"

	v1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

type nodeError string

func (err nodeError) Error() string {
	return string(err)
}

const (
	FatalNodeError = nodeError("unable to retrieve required metrics from any node via direct or proxy connection")
)

// NodeSource is an interface to get a list of Nodes
type NodeSource interface {
	GetReadyNodes(ctx context.Context) ([]v1.Node, error)
	NodeAddress(node *v1.Node) (string, int32, error)
}

// ClientsetNodeSource implements NodeSource interface
type ClientsetNodeSource struct {
	clientSet kubernetes.Interface
}

type cadvisorStatsRequest struct {
	ContainerName string `json:"containerName,omitempty"`
	NumStats      int    `json:"num_stats,omitempty"`
	Subcontainers bool   `json:"subcontainers,omitempty"`
}

// NewClientsetNodeSource returns a ClientsetNodeSource with the given clientSet
func NewClientsetNodeSource(clientSet kubernetes.Interface) ClientsetNodeSource {
	return ClientsetNodeSource{
		clientSet: clientSet,
	}
}

// GetReadyNodes fetches the list of nodes from the clientSet and filters down to only ready nodes
func (cns ClientsetNodeSource) GetReadyNodes(ctx context.Context) ([]v1.Node, error) {
	allNodes, err := cns.clientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})

	if err != nil {
		return nil, err
	}

	var readyNodes []v1.Node
	for _, n := range allNodes.Items {
		i, nc := getNodeCondition(
			&n.Status,
			v1.NodeReady)
		if i >= 0 && nc.Type == v1.NodeReady {
			readyNodes = append(readyNodes, n)
		} else {
			log.Debugf("node, %s, is in a notready state. Node Condition: %+v", n.Name, nc)
		}
	}

	if len(readyNodes) == 0 {
		return nil, fmt.Errorf("there were 0 nodes in a ready state")
	}

	if len(readyNodes) != len(allNodes.Items) {
		log.Info("some nodes were in a not ready state when retrieving nodes")
	}

	return readyNodes, nil
}

// NodeAddress returns the internal IP address and kubelet port of a given node
func (cns ClientsetNodeSource) NodeAddress(node *v1.Node) (string, int32, error) {
	// adapted from k8s.io/kubernetes/pkg/util/node
	for _, addr := range node.Status.Addresses {
		if addr.Type == v1.NodeInternalIP {
			return addr.Address, node.Status.DaemonEndpoints.KubeletEndpoint.Port, nil
		}
	}
	return "", 0, fmt.Errorf("Could not find internal IP address for node %s ", node.Name)
}

// nolint: govet
func downloadNodeData(ctx context.Context, prefix string, config KubeAgentConfig,
	workDir *os.File, nodeSource NodeSource) (map[string]error, error) {
	var nodes []v1.Node
	failedNodeList := make(map[string]error)

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		nodes, err = nodeSource.GetReadyNodes(ctx)
		return
	})
	if err != nil {
		return nil, fmt.Errorf("cloudability metric agent is unable to get a list of nodes: %v", err)
	}

	containersRequest, err := buildContainersRequest()
	if err != nil {
		return nil, fmt.Errorf("error occurred requesting container statistics: %v", err)
	}

	log.Debugln("Starting node collection loop")

	var wg sync.WaitGroup
	var m sync.Mutex

	// creates a max number of concurrent goroutines that are allowed
	limiter := make(chan struct{}, config.ConcurrentPollers)

	for _, n := range nodes {
		// block if channel is full (limiting number of goroutines)
		limiter <- struct{}{}

		wg.Add(1)
		go func(currentNode v1.Node) {
			if currentNode.Spec.ProviderID == "" {
				errMessage := "Node ProviderID is not set which may be because the node is running in a " +
					"self managed environment, and this may cause inconsistent gathering of metrics data."
				log.Warnf(errMessage)
				m.Lock()
				failedNodeList[currentNode.Name] = errors.New("provider ID for node does not exist. " +
					"If this condition persists it will cause inconsistent cluster allocation")
				m.Unlock()
			}

			nd := nodeFetchData{
				nodeName:          currentNode.Name,
				prefix:            prefix,
				workDir:           workDir,
				ClusterHostURL:    config.ClusterHostURL,
				containersRequest: containersRequest,
			}
			err := retrieveNodeData(nd, config, nodeSource, currentNode)
			if err != nil {
				m.Lock()
				failedNodeList[currentNode.Name] = fmt.Errorf("node metrics retrieval problem occurred: %v", err)
				m.Unlock()
			}
			<-limiter
			wg.Done()
		}(n)
	}

	log.Debugln("Currently Waiting for all node data to be gathered")
	wg.Wait()
	log.Debugln("All nodes data has been gathered, no longer waiting")

	return failedNodeList, nil
}

// nodeFetchData is a convenience wrapper for
// information used to fetch node stats and store
// in the appropriate file location
type nodeFetchData struct {
	nodeName          string
	prefix            string
	workDir           *os.File
	ClusterHostURL    string
	containersRequest []byte
}

// setupDirectNodeAPI retrieves node stats directly from the node api
// nolint:revive
func setupDirectNodeAPI(ns NodeSource, config KubeAgentConfig, n *v1.Node, nd nodeFetchData) (directNode, error) {
	ip, port, err := ns.NodeAddress(n)
	if err != nil {
		return directNode{}, fmt.Errorf("problem getting node address: %s", err)
	}
	return directNodeEndpoints(ip, port), nil
}

type nodeAPI interface {
	statsSummary() string
	statsContainer() string
	mCAdvisor() string
}

type proxyAPI struct {
	clusterHostURL string
	nodeName       string
}

// statsSummary formats the proxy api stats/summary endpoint for the node
func (p proxyAPI) statsSummary() string {
	return fmt.Sprintf("%s/api/v1/nodes/%s/proxy/stats/summary", p.clusterHostURL, p.nodeName)
}

// statsContainer formats the proxy api stats/container endpoint for the node
func (p proxyAPI) statsContainer() string {
	return fmt.Sprintf("%s/api/v1/nodes/%s/proxy/stats/container/", p.clusterHostURL, p.nodeName)
}

// mCAdvisor formats the proxy api metrics/mCAdvisor endpoint, which outputs prometheus-format metrics
func (p proxyAPI) mCAdvisor() string {
	return fmt.Sprintf("%s/api/v1/nodes/%s/proxy/metrics/cadvisor", p.clusterHostURL, p.nodeName)
}

func setupProxyAPI(clusterHostURL, nodeName string) proxyAPI {
	return proxyAPI{
		clusterHostURL: clusterHostURL,
		nodeName:       nodeName,
	}
}

type directNode struct {
	ip   string
	port int64
}

// statsSummary formats the direct node stats/summary endpoint
func (d directNode) statsSummary() string {
	return fmt.Sprintf("https://%s:%v/stats/summary", d.ip, d.port)
}

// statsContainer formats the direct node stats/container endpoint
func (d directNode) statsContainer() string {
	return fmt.Sprintf("https://%s:%v/stats/container/", d.ip, d.port)
}

// mCAdvisor formats the direct node metrics/mCAdvisor endpoint
func (d directNode) mCAdvisor() string {
	return fmt.Sprintf("https://%s:%v/metrics/cadvisor", d.ip, d.port)
}

func directNodeEndpoints(ip string, port int32) directNode {
	return directNode{
		ip:   ip,
		port: int64(port),
	}
}

type sourceName struct {
	prefix   string
	nodeName string
}

func (s sourceName) summary() string {
	return fmt.Sprintf("%s-summary-%s", s.prefix, s.nodeName)
}

func (s sourceName) container() string {
	return fmt.Sprintf("%s-container-%s", s.prefix, s.nodeName)
}

func (s sourceName) cadvisorMetrics() string {
	return fmt.Sprintf("%s-cadvisor_metrics-%s", s.prefix, s.nodeName)
}

// retrieveNodeData fetches summary and container data for the node
func retrieveNodeData(nd nodeFetchData, config KubeAgentConfig, ns NodeSource, n v1.Node) error {
	connectionMethods := connectionOptions(config, n, nd, ns)
	source := sourceName{
		prefix:   nd.prefix,
		nodeName: nd.nodeName,
	}
	toFetch := map[Endpoint]bool{
		NodeStatsSummaryEndpoint: true,
	}
	// if we receive an error after the max number of retries when attempting to hit an endpoint that
	// we had previously verified to work, we fail and assume the node is unreachable at this time
	for _, cm := range connectionMethods {
		err := fetchEndpoint(toFetch, NodeStatsSummaryEndpoint, config, cm, func() (string, error) {
			return cm.client.GetRawEndPoint(http.MethodGet, source.summary(),
				nd.workDir, cm.API.statsSummary(), nil, true)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// fetchEndpoint is a convenience function to provide consistent logging, uniqueness,
// and error handling around fetching data from metrics endpoints
func fetchEndpoint(fetch map[Endpoint]bool, endpoint Endpoint, config KubeAgentConfig,
	cm ConnectionMethod, executeEndpointRequest func() (filename string, err error)) error {
	// Don't fetch if we already got it once
	if !fetch[endpoint] {
		return nil
	}
	if config.NodeMetrics.Available(endpoint, cm.ConnType) {
		// fetch metrics from the endpoint
		log.Debugf("Fetching data from %s endpoint via %s connection", endpoint, cm.FriendlyName)
		_, err := executeEndpointRequest()
		if err != nil {
			log.Debugf("Unable to fetch %s metrics: %v", endpoint, err)
			return err
		}
		delete(fetch, endpoint)
	}
	return nil
}

// connectionOptions returns the connection methods that are allowed for this node based on config
// settings and cluster composition
func connectionOptions(config KubeAgentConfig, n v1.Node, nd nodeFetchData, ns NodeSource) []ConnectionMethod {
	connectionMethods := make([]ConnectionMethod, 1)
	// The config shouldn't allow direct connection if Fargate nodes were
	// found in the cluster at startup, but check again here to be safe.
	if !config.ForceKubeProxy && !isFargateNode(n) {
		directAPI, err := setupDirectNodeAPI(ns, config, &n, nd)
		if err != nil {
			log.Debugf("Unable to attempt direct connection to node %s: %v", nd.nodeName, err)
		} else {
			connectionMethods = append(connectionMethods, ConnectionMethod{Direct, directAPI, config.NodeClient, direct})
		}
	}
	proxyAPI := setupProxyAPI(config.ClusterHostURL, nd.nodeName)
	connectionMethods = append(connectionMethods, ConnectionMethod{Proxy, proxyAPI, config.InClusterClient, proxy})
	return connectionMethods
}

// ensureNodeSource validates connectivity to the kubelet metrics endpoints.
// Attempts direct connection to the node summary & container stats endpoint
// if possible and allowed, otherwise attempts to connect via kube-proxy
func ensureNodeSource(ctx context.Context, config KubeAgentConfig) (KubeAgentConfig, error) {
	nodeHTTPClient := http.Client{
		Timeout: time.Second * 30,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
		}}

	clientSetNodeSource := NewClientsetNodeSource(config.Clientset)

	nodeClient := raw.NewClient(nodeHTTPClient, true, config.BearerToken, config.BearerTokenPath,
		config.CollectionRetryLimit, config.ParseMetricData)

	config.NodeClient = nodeClient

	nodes, err := clientSetNodeSource.GetReadyNodes(ctx)
	if err != nil {
		return config, fmt.Errorf("error retrieving nodes: %s", err)
	}

	directNodes := int32(0)
	proxyNodes := int32(0)
	failedDirect := int32(0)
	failedProxy := int32(0)
	directAllowed := allowDirectConnect(config, nodes)

	var wg sync.WaitGroup

	limiter := make(chan struct{}, config.ConcurrentPollers)

	for _, n := range nodes {
		// block if channel is full (limiting number of goroutines)
		limiter <- struct{}{}
		wg.Add(1)
		go func(currentNode v1.Node) {
			defer func() {
				<-limiter
				wg.Done()
			}()
			directlyConnected := false
			ip, port, err := clientSetNodeSource.NodeAddress(&currentNode)
			if err != nil {
				log.Warnf("error retrieving node addresses: %s", err)
				return
			}
			if directAllowed {
				// test node direct connectivity
				d := directNodeEndpoints(ip, port)
				success, err := checkEndpointConnections(config, &nodeHTTPClient, Direct, d.statsSummary())
				if err != nil {
					log.Warnf("Failed to connect to node [%s] directly with cause [%s]",
						d.statsSummary(), err.Error())
					atomic.AddInt32(&failedDirect, 1)
				}
				if success {
					directlyConnected = true
					atomic.AddInt32(&directNodes, 1)
				}
			}
			if !directlyConnected {
				p := setupProxyAPI(config.ClusterHostURL, currentNode.Name)
				success, err := checkEndpointConnections(config, &config.HTTPClient, Proxy, p.statsSummary())
				if err != nil {
					log.Warnf("Failed to connect to node [%s] via proxy with cause [%s]",
						p.statsSummary(), err.Error())
					atomic.AddInt32(&failedProxy, 1)
				}
				if success {
					atomic.AddInt32(&proxyNodes, 1)
				}
			}
		}(n)
	}
	log.Debugln("Currently Waiting for all node data to be gathered")
	wg.Wait()
	log.Infof("Of %d nodes, %d connected directly, %d connected via proxy, and %d could not be reached",
		len(nodes), directNodes, proxyNodes, failedProxy)

	if len(nodes) != int(directNodes+proxyNodes) {
		pct := int(directNodes+proxyNodes) * 100 / len(nodes)
		log.Warnf("Only %d percent of ready nodes could could be connected to, "+
			"agent will operate in a limited mode.", pct)
	}

	if (directNodes + proxyNodes) == 0 {
		return config, FatalNodeError
	}

	validateConfig(config, proxyNodes, directNodes)
	return config, nil
}

func validateConfig(config KubeAgentConfig, proxyNodes, directNodes int32) {
	if proxyNodes > 0 {
		config.NodeMetrics.SetAvailability(NodeStatsSummaryEndpoint, Proxy, true)
	} else if directNodes > 0 {
		config.NodeMetrics.SetAvailability(NodeStatsSummaryEndpoint, Direct, true)
	} else {
		config.NodeMetrics.SetAvailability(NodeStatsSummaryEndpoint, Proxy, false)
		config.NodeMetrics.SetAvailability(NodeStatsSummaryEndpoint, Direct, false)
	}
}

func checkEndpointConnections(config KubeAgentConfig, client *http.Client, method Connection,
	nodeStatSum string) (success bool, err error) {
	ns, _, err := util.TestHTTPConnection(client, nodeStatSum, http.MethodGet, config.BearerToken, 0, false)
	if err != nil {
		return false, err
	}
	log.Infof("Node [%s] available via %s connection? %v", nodeStatSum, method, ns)

	return ns, nil
}

// isFargateNode detects whether a node is a Fargate node, which affects
// how the agent will connect to it
func isFargateNode(n v1.Node) bool {
	v := n.Labels["eks.amazonaws.com/compute-type"]
	if v == "fargate" {
		log.Debugf("Fargate node found: %s", n.Name)
		return true
	}
	return false
}

// allowDirectConnect determines whether the client and the
// type of nodes in the cluster will allow retrieving data directly
// from the node
func allowDirectConnect(config KubeAgentConfig, nodes []v1.Node) bool {
	if config.ForceKubeProxy {
		log.Infof("ForceKubeProxy is set, direct node connection disabled")
		return false
	}
	// Clusters may be mixed Fargate and non-Fargate.
	// To simplify handling, we disallow direct connection
	// if any Fargate nodes are found.
	for _, n := range nodes {
		if isFargateNode(n) {
			//nolint: lll
			log.Infof(`Direct connection to an AWS Fargate node is not possible, so when a Fargate node is detected in the cluster, the agent switches to using a proxy connection for all node connections in the cluster. 
				The allocation of EKS Fargate tasks are not currently supported via the containers feature dashboard.
				Please contact your technical account manager for instructions on how to access EKS Fargate information via the reporting feature.`)
			return false
		}
	}
	return true
}

func retrieveNodeSummaries(ctx context.Context, config KubeAgentConfig, msd string, metricSampleDir *os.File,
	nodeSource NodeSource) (err error) {

	config.failedNodeList = map[string]error{}

	// get node stats data
	config.failedNodeList, err = downloadNodeData(ctx, "stats", config, metricSampleDir, nodeSource)
	if err != nil {
		return fmt.Errorf("error downloading node metrics: %s", err)
	}

	if len(config.failedNodeList) > 0 {
		log.Warnf("Warning failed to get node metrics: %+v", config.failedNodeList)
	}

	// move baseline metrics for each node into sample directory
	err = fetchNodeBaselines(msd, config.msExportDirectory.Name())
	if err != nil {
		return fmt.Errorf("error fetching node baseline files: %s", err)
	}

	// update node baselines with current sample
	err = updateNodeBaselines(msd, config.msExportDirectory.Name())
	if err != nil {
		return fmt.Errorf("error updating node baseline files: %s", err)
	}
	return nil
}

func buildContainersRequest() ([]byte, error) {
	// Request all containers.
	request := &cadvisorStatsRequest{
		Subcontainers: true,
		NumStats:      1,
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// getNodeCondition extracts the provided condition from the given status and returns that.
// Returns nil and -1 if the condition is not present, and the index of the located condition.
// Based on https://github.com/kubernetes/kubernetes/blob/v1.17.3/pkg/controller/util/node/controller_utils.go#L286
func getNodeCondition(status *v1.NodeStatus, conditionType v1.NodeConditionType) (int, *v1.NodeCondition) {
	if status == nil {
		return -1, nil
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditionType {
			return i, &status.Conditions[i]
		}
	}
	return -1, nil
}
