package kubernetes

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/cloudability/metrics-agent/retrieval/raw"
	"github.com/cloudability/metrics-agent/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/pkg/api/v1"
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

	failedNodeList := make(map[string]error)

	nodes, err := nodeSource.GetNodes()

	if err != nil {
		return nil, fmt.Errorf("cloudability metric agent is unable to get a list of nodes: %v", err)
	}

	for _, n := range nodes.Items {
		// retrieve node summary directly from node
		if config.nodeRetrievalMethod == "direct" {

			ip, port, err := nodeSource.NodeAddress(&n)
			if err != nil {
				return nil, fmt.Errorf("error: %s", err)
			}
			nodeStatSum := "https://" + ip + ":" + strconv.FormatInt(int64(port), 10) + "/stats/summary"
			_, err = config.NodeClient.GetRawEndPoint(prefix+n.Name, workDir, nodeStatSum)
			if err != nil {
				failedNodeList[n.Name] = err
				continue
			}
			continue
		}

		// retrieve node summary via kube-proxy
		nodeStatSum := config.ClusterHostURL + "/api/v1/nodes/" + n.Name + "/proxy/stats/summary"
		_, err = config.InClusterClient.GetRawEndPoint(prefix+n.Name, workDir, nodeStatSum)
		if err != nil {
			failedNodeList[n.Name] = err
			continue
		}
		continue
	}

	return failedNodeList, nil
}

//ensureNodeSource validates connectivity to the cadvisor summary endpoint.
// If unable to directly connect to the node summary endpoint, attempts to connect via kube-proxy
func ensureNodeSource(config KubeAgentConfig) (KubeAgentConfig, error) {

	nodeHTTPClient := http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		// nolint gas
		InsecureSkipVerify: true,
	},
	}}

	clientSetNodeSource := NewClientsetNodeSource(config.Clientset)

	nodeClient := raw.NewClient(nodeHTTPClient, true, "", 0)

	nodes, err := clientSetNodeSource.GetNodes()
	if err != nil {
		return config, fmt.Errorf("error retrieving nodes: %s", err)
	}

	ip, port, err := clientSetNodeSource.NodeAddress(&nodes.Items[0])
	if err != nil {
		return config, fmt.Errorf("error retrieving node addresses: %s", err)
	}

	// test node direct connectivity
	nodeStatSum := "https://" + ip + ":" + strconv.FormatInt(int64(port), 10) + "/stats/summary"
	s, _, _ := util.TestHTTPConnection(&nodeHTTPClient, nodeStatSum, config.BearerToken, 0, false)
	if s {
		config.NodeClient = nodeClient
		config.nodeRetrievalMethod = "direct"
		return config, nil
	}

	// test node connectivity via kube-proxy
	nodeStatSum = config.ClusterHostURL + "/api/v1/nodes/" + nodes.Items[0].Name + "/proxy/stats/summary"
	s, _, err = util.TestHTTPConnection(&config.HTTPClient, nodeStatSum, config.BearerToken, 0, false)
	if !s && err == nil {
		config.nodeRetrievalMethod = "proxy"
		return config, nil
	}

	config.NodeClient = nodeClient
	config.nodeRetrievalMethod = "unreachable"
	config.RetrieveNodeSummaries = false
	return config, fmt.Errorf("Unable to retrieve node metrics: %v", err)
}

func retrieveNodeSummaries(
	config KubeAgentConfig, msd string, metricSampleDir *os.File, nodeSource NodeSource) (err error) {

	config.failedNodeList = map[string]error{}

	// get node stats summaries
	config.failedNodeList, err = downloadNodeData("node-summary-", config, metricSampleDir, nodeSource)
	if err != nil {
		return fmt.Errorf("error downloading node metrics: %s", err)
	}

	if len(config.failedNodeList) > 0 {
		log.Printf("Warning: Failed to get node metrics: %+v", config.failedNodeList)
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
	return err
}
