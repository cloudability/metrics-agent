package kubernetes

import (
	"fmt"
	"os"

	"github.com/cloudability/metrics-agent/retrieval/raw"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeSource is an interface to get a list of Nodes
type NodeSource interface {
	GetNodes() ([]string, error)
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
func (cns ClientsetNodeSource) GetNodes() ([]string, error) {
	nodeObjs, err := cns.clientSet.CoreV1().Nodes().List(metav1.ListOptions{})
	nodeNames := make([]string, 0, len(nodeObjs.Items))
	for _, node := range nodeObjs.Items {
		nodeNames = append(nodeNames, node.Name)
	}
	return nodeNames, err
}

func downloadNodeData(prefix string,
	config KubeAgentConfig,
	workDir *os.File,
	rawClient raw.Client,
	nodeSource NodeSource) error {

	nodes, err := nodeSource.GetNodes()

	if err != nil {
		return fmt.Errorf("cloudability metric agent is unable to get a list of nodes: %v", err)
	}

	for _, n := range nodes {

		nodeStatSum := config.ClusterHostURL + "/api/v1/nodes/" + n + "/proxy/stats/summary"
		// download node summary
		_, err := rawClient.GetRawEndPoint(prefix+n, workDir, nodeStatSum)
		if err != nil {
			return fmt.Errorf("unable to retrive node: %s %s summary metrics: %s", n, prefix, err)
		}
	}
	return nil
}
