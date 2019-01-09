package kubernetes

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

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

	failedNodeList := make(map[string]error)

	nodes, err := nodeSource.GetNodes()

	if err != nil {
		return nil, fmt.Errorf("cloudability metric agent is unable to get a list of nodes: %v", err)
	}

	containersRequest, err := buildContainersRequest()

	if err != nil {
		return nil, fmt.Errorf("Error occurred requesting container statistics: %v", err)
	}

	for _, n := range nodes.Items {
		// retrieve node summary directly from node
		if config.nodeRetrievalMethod == "direct" {

			ip, port, err := nodeSource.NodeAddress(&n)
			if err != nil {
				return nil, fmt.Errorf("error: %s", err)
			}
			nodeStatSum := fmt.Sprintf("https://%s:%v/stats/summary", ip, int64(port))
			_, err = config.NodeClient.GetRawEndPoint(http.MethodGet, prefix+"-summary-"+n.Name, workDir, nodeStatSum, nil, true)
			if err != nil {
				failedNodeList[n.Name] = err
			}
			containerStats := fmt.Sprintf("https://%s:%v/stats/container/", ip, int64(port))
			_, err = config.NodeClient.GetRawEndPoint(
				http.MethodPost, prefix+"-container-"+n.Name, workDir, containerStats, containersRequest, true)
			if err != nil {
				failedNodeList[n.Name] = err
			}
			continue
		}

		// retrieve node summary via kube-proxy
		nodeStatSum := fmt.Sprintf("%s/api/v1/nodes/%s:10255/proxy/stats/summary", config.ClusterHostURL, n.Name)
		_, err = config.InClusterClient.GetRawEndPoint(
			http.MethodGet, prefix+"-summary-"+n.Name, workDir, nodeStatSum, nil, true)
		if err != nil {
			failedNodeList[n.Name] = err
		}
		containerStats := fmt.Sprintf("%s/api/v1/nodes/%s:10255/proxy/stats/container/", config.ClusterHostURL, n.Name)
		_, err = config.InClusterClient.GetRawEndPoint(
			http.MethodPost, prefix+"-container-"+n.Name, workDir, containerStats, containersRequest, true)
		if err != nil {
			failedNodeList[n.Name] = err
		}
		continue
	}

	return failedNodeList, nil
}

//ensureNodeSource validates connectivity to the kubelet metrics endpoints.
// If unable to directly connect to the node summary & container stats endpoint, attempts to connect via kube-proxy
func ensureNodeSource(config KubeAgentConfig) (KubeAgentConfig, error) {

	nodeHTTPClient := http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		// nolint gosec
		InsecureSkipVerify: true,
	},
	}}

	clientSetNodeSource := NewClientsetNodeSource(config.Clientset)

	nodeClient := raw.NewClient(nodeHTTPClient, true, config.BearerToken, 0)

	config.NodeClient = nodeClient

	nodes, err := clientSetNodeSource.GetNodes()
	if err != nil {
		return config, fmt.Errorf("error retrieving nodes: %s", err)
	}

	ip, port, err := clientSetNodeSource.NodeAddress(&nodes.Items[0])
	if err != nil {
		return config, fmt.Errorf("error retrieving node addresses: %s", err)
	}

	// test node direct connectivity
	nodeStatSum := fmt.Sprintf("https://%s:%v/stats/summary", ip, int64(port))
	containerStats := fmt.Sprintf("https://%s:%v/stats/container/", ip, int64(port))
	ns, _, err := util.TestHTTPConnection(&nodeHTTPClient, nodeStatSum, http.MethodGet, config.BearerToken, 0, false)
	if err != nil {
		return config, err
	}
	cs, _, err := util.TestHTTPConnection(&nodeHTTPClient, containerStats, http.MethodPost, config.BearerToken, 0, false)
	if err != nil {
		return config, err
	}
	if ns && cs {
		config.nodeRetrievalMethod = "direct"
		return config, nil
	}

	// test node connectivity via kube-proxy
	nodeStatSum = fmt.Sprintf("%s/api/v1/nodes/%s/proxy/stats/summary", config.ClusterHostURL, nodes.Items[0].Name)
	containerStats = fmt.Sprintf("%s/api/v1/nodes/%s/proxy/stats/container/", config.ClusterHostURL, nodes.Items[0].Name)
	ns, _, err = util.TestHTTPConnection(&config.HTTPClient, nodeStatSum, http.MethodGet, config.BearerToken, 0, false)
	if err != nil {
		return config, err
	}
	cs, _, err = util.TestHTTPConnection(&config.HTTPClient, containerStats, http.MethodPost, config.BearerToken, 0, false)
	if err != nil {
		return config, err
	}
	if ns && cs {
		config.NodeClient = raw.Client{}
		config.nodeRetrievalMethod = "proxy"
		return config, nil
	}

	config.nodeRetrievalMethod = "unreachable"
	config.RetrieveNodeSummaries = false
	return config, fmt.Errorf("Unable to retrieve node metrics. Please verify RBAC roles: %v", err)
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
