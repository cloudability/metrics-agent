package kubernetes

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cloudability/metrics-agent/retrieval/raw"
	"github.com/cloudability/metrics-agent/util"
	"github.com/kubernetes/kubernetes/staging/src/k8s.io/client-go/util/retry"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/pkg/api/v1"
	v1Node "k8s.io/client-go/pkg/api/v1/node"
)

// NodeSource is an interface to get a list of Nodes
type NodeSource interface {
	GetNodes() (*v1.NodeList, error)
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

// GetNodes fetches the list of nodes from the clientSet
func (cns ClientsetNodeSource) GetNodes() (*v1.NodeList, error) {
	return cns.clientSet.CoreV1().Nodes().List(metav1.ListOptions{})
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

func downloadNodeData(prefix string,
	config KubeAgentConfig,
	workDir *os.File,
	nodeSource NodeSource) (map[string]error, error) {

	var nodes *v1.NodeList

	failedNodeList := make(map[string]error)

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		nodes, err = nodeSource.GetNodes()
		return
	})
	if err != nil {
		return nil, fmt.Errorf("cloudability metric agent is unable to get a list of nodes: %v", err)
	}

	containersRequest, err := buildContainersRequest()

	if err != nil {
		return nil, fmt.Errorf("error occurred requesting container statistics: %v", err)
	}

	anyNodeReady := false

	for _, n := range nodes.Items {
		if n.Spec.ProviderID == "" {
			failedNodeList[n.Name] = errors.New("Provider ID for node does not exist. " +
				"If this condition persists it will cause inconsistent cluster allocation")
		}

		// Nodes running in a cluster may not all be ready, check each node individually and if they are not ready
		// skip the node and create a log.
		nodeReady := v1Node.IsNodeReady(&n)
		if !nodeReady {
			log.Warnf("Node %s was not ready when attempting to get node summary", n.Name)
			continue
		}
		nd := nodeFetchData{
			nodeName:          n.Name,
			prefix:            prefix,
			workDir:           workDir,
			ClusterHostURL:    config.ClusterHostURL,
			containersRequest: containersRequest,
		}
		anyNodeReady = true
		// retrieve node summary directly from node if possible and allowed.
		// The config shouldn't allow direct connection if Fargate nodes were
		// found in the cluster at startup, but check again here to be safe.
		if config.nodeRetrievalMethod == direct && !isFargateNode(n) {
			err := directNodeFetch(nodeSource, config, &n, nd)
			// no error, no need to try proxy
			if err == nil {
				continue
			}
			// TODO: Do we want to consider the node failed if we can still try proxy?
			// make note of the error and fall through to proxy
			failedNodeList[n.Name] = fmt.Errorf("direct connect failed (will attempt proxy): %s", err)
		}
		// retrieve node summary via proxy
		err := proxyNodeFetch(nd, config)
		if err != nil {
			failedNodeList[n.Name] = fmt.Errorf("proxy connect failed: %s", err)
		}
	}

	if !anyNodeReady {
		log.Errorf("error retrieving node summaries: no nodes were ready in cluster %s", config.ClusterName)
	}

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

// directNodeFetch retrieves node stats directly from the node api
func directNodeFetch(nodeSource NodeSource, config KubeAgentConfig, n *v1.Node, nd nodeFetchData) error {
	ip, port, err := nodeSource.NodeAddress(n)
	if err != nil {
		return fmt.Errorf("problem getting node address: %s", err)
	}
	d := directNodeEndpoints(ip, port)
	return retrieveNodeData(nd, config.NodeClient, d.statsSummary(), d.statsContainer())
}

// proxyNodeFetch retrieves node data via the proxy api
func proxyNodeFetch(nd nodeFetchData, config KubeAgentConfig) error {
	proxy := proxyEndpoints(config.ClusterHostURL, nd.nodeName)
	return retrieveNodeData(nd, config.InClusterClient, proxy.statsSummary(), proxy.statsContainer())
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

func proxyEndpoints(clusterHostURL, nodeName string) proxyAPI {
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

// retrieveNodeData fetches summary and container data from the node
func retrieveNodeData(nd nodeFetchData, c raw.Client, nodeStatSum, containerStats string) error {
	source := sourceName{
		prefix:   nd.prefix,
		nodeName: nd.nodeName,
	}
	// fetch stats/summary data
	_, err := c.GetRawEndPoint(http.MethodGet, source.summary(), nd.workDir, nodeStatSum, nil, true)
	if err != nil {
		return err
	}
	// fetch container details
	_, err = c.GetRawEndPoint(
		http.MethodPost, source.container(), nd.workDir, containerStats, nd.containersRequest, true)
	return err

}

//ensureNodeSource validates connectivity to the kubelet metrics endpoints.
// Attempts direct connection to the node summary & container stats endpoint
// if possible and allowed, otherwise attempts to connect via kube-proxy
func ensureNodeSource(config KubeAgentConfig) (KubeAgentConfig, error) {

	nodeHTTPClient := http.Client{
		Timeout: time.Second * 30,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
		}}

	clientSetNodeSource := NewClientsetNodeSource(config.Clientset)

	nodeClient := raw.NewClient(nodeHTTPClient, true, config.BearerToken, 3)

	config.NodeClient = nodeClient

	nodes, err := clientSetNodeSource.GetNodes()
	if err != nil {
		return config, fmt.Errorf("error retrieving nodes: %s", err)
	}

	// Some nodes may not be ready, we want to find the first node that is ready so we can set the node source
	firstReadyNode, err := getFirstReadyNode(nodes)
	if err != nil {
		return config, err
	}

	ip, port, err := clientSetNodeSource.NodeAddress(firstReadyNode)
	if err != nil {
		return config, fmt.Errorf("error retrieving node addresses: %s", err)
	}

	if allowDirectConnect(config, nodes) {
		// test node direct connectivity
		d := directNodeEndpoints(ip, port)
		success, err := testNodeConn(&nodeHTTPClient, d.statsSummary(), d.statsContainer(), config.BearerToken)
		if err != nil {
			return config, err
		}
		if success {
			config.nodeRetrievalMethod = direct
			return config, nil
		}
	}

	// test node connectivity via kube-proxy
	p := proxyEndpoints(config.ClusterHostURL, firstReadyNode.Name)
	success, err := testNodeConn(&config.HTTPClient, p.statsSummary(), p.statsContainer(), config.BearerToken)
	if err != nil {
		return config, err
	}
	if success {
		config.NodeClient = raw.Client{}
		config.nodeRetrievalMethod = proxy
		return config, nil
	}

	config.nodeRetrievalMethod = unreachable
	config.RetrieveNodeSummaries = false
	return config, fmt.Errorf("unable to retrieve node metrics. Please verify RBAC roles: %v", err)
}

func testNodeConn(client *http.Client, nodeStatSum, containerStats, bearerToken string) (success bool, err error) {
	ns, _, err := util.TestHTTPConnection(client, nodeStatSum, http.MethodGet, bearerToken, 0, false)
	if err != nil {
		return false, err
	}
	cs, _, err := util.TestHTTPConnection(client, containerStats, http.MethodPost, bearerToken, 0, false)
	if err != nil {
		return false, err
	}
	return ns && cs, nil
}

func getFirstReadyNode(nodes *v1.NodeList) (*v1.Node, error) {
	for _, n := range nodes.Items {
		if v1Node.IsNodeReady(&n) {
			return &n, nil
		}
	}
	return nil, fmt.Errorf("error retrieving node addresses: no nodes were ready")
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
func allowDirectConnect(config KubeAgentConfig, nodes *v1.NodeList) bool {
	if config.ForceKubeProxy {
		log.Infof("ForceKubeProxy is set, direct node connection disabled")
		return false
	}
	// Clusters may be mixed Fargate and non-Fargate.
	// To simplify handling, we disallow direct connection
	// if any Fargate nodes are found.
	for _, n := range nodes.Items {
		if isFargateNode(n) {
			log.Infof("Fargate node found in cluster, direct node connection disabled. Learn more about Fargate support: %s", kbURL) //nolint: lll
			return false
		}
	}
	return true
}

func retrieveNodeSummaries(
	config KubeAgentConfig, msd string, metricSampleDir *os.File, nodeSource NodeSource) (err error) {

	config.failedNodeList = map[string]error{}

	// get node stats data
	config.failedNodeList, err = downloadNodeData("stats", config, metricSampleDir, nodeSource)
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
