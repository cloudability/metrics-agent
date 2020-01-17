package kubernetes_test

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudability/metrics-agent/kubernetes"
	"github.com/cloudability/metrics-agent/retrieval/raw"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	v1 "k8s.io/client-go/pkg/api/v1"
)

// labels found on an amazon EKS fargate node
var fargateLabels = map[string]string{
	"eks.amazonaws.com/compute-type": "fargate",
	"beta.kubernetes.io/os":          "linux",
}

// labels found on a generic node
var nodeSampleLabels = map[string]string{
	"beta.kubernetes.io/os":          "linux",
	"kubernetes.io/arch":             "amd64",
	"eks.amazonaws.com/compute-type": "not-fargate",
}

func NewTestClient(ts *httptest.Server, labels map[string]string) *fake.Clientset {
	s := strings.Split(ts.Listener.Addr().String(), ":")
	ip := s[0]
	port, _ := strconv.Atoi(s[1])
	return fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "proxyNode",
				Namespace: v1.NamespaceDefault,
				Labels:    labels,
			},
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    "InternalIP",
						Address: ip,
					},
				},
				Conditions: []v1.NodeCondition{{
					Type:   v1.NodeReady,
					Status: v1.ConditionTrue,
				}},
				DaemonEndpoints: v1.NodeDaemonEndpoints{
					KubeletEndpoint: v1.DaemonEndpoint{
						Port: int32(port),
					},
				},
			},
		},
	)
}

func TestEnsureNodeSource(t *testing.T) {
	// Cause test server to return success for the first two test queries, enabling
	// the first test to succeed on the "direct" step.
	// The second (proxy) test encounters two 400s when attempting a direct connection,
	// which triggers a fallback to proxy testing, which receives the next two 200s.
	// The final 400 fails the direct connection test, and as no proxy client is provided
	// for the "unsuccessful" node test, it falls through to unreachable status.
	// If no status code is provided, the default response is 200.
	returnCodes := []int{200, 200, 400, 400, 200, 200, 400}
	ts := launchTLSTestServer(returnCodes)
	cs := NewTestClient(ts, nodeSampleLabels)
	ka := kubernetes.KubeAgentConfig{
		Clientset:  cs,
		HTTPClient: http.Client{},
	}

	defer ts.Close()

	t.Run("Ensure successful direct node source test", func(t *testing.T) {

		ka, err := kubernetes.EnsureNodeSource(ka)

		if ka.GetNodeRetrievalMethod() != kubernetes.Direct || err != nil {
			t.Errorf("Expected direct node retrieval method but got %v: %v",
				ka.GetNodeRetrievalMethod(),
				err)
			return
		}

	})

	t.Run("Ensure successful proxy node source test", func(t *testing.T) {
		ka := kubernetes.KubeAgentConfig{
			Clientset: cs,
			// The proxy connection method uses the config http client
			HTTPClient: http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
			}},
			ClusterHostURL: "https://" + ts.Listener.Addr().String(),
		}

		ka, err := kubernetes.EnsureNodeSource(ka)

		if ka.GetNodeRetrievalMethod() != kubernetes.Proxy || err != nil {
			t.Errorf("Expected proxy node retrieval method but got %v: %v", ka.GetNodeRetrievalMethod(), err)
			return
		}
	})

	t.Run("Ensure unsuccessful node source test", func(t *testing.T) {
		ka, err := kubernetes.EnsureNodeSource(ka)

		if ka.GetNodeRetrievalMethod() != kubernetes.Unreachable {
			t.Errorf("Expected unreachable node retrieval method but got %v: %v", ka.GetNodeRetrievalMethod(), err)
			return
		}
	})

	t.Run("Ensure Fargate node forces proxy connection", func(t *testing.T) {
		cs := NewTestClient(ts, fargateLabels)
		ka := kubernetes.KubeAgentConfig{
			Clientset: cs,
			HTTPClient: http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
			}},
			ClusterHostURL: "https://" + ts.Listener.Addr().String(),
		}
		ka, err := kubernetes.EnsureNodeSource(ka)

		if ka.GetNodeRetrievalMethod() != kubernetes.Proxy || err != nil {
			t.Errorf("Expected proxy node retrieval method for Fargate node but got %v. Error: %v, Config: %+v",
				ka.GetNodeRetrievalMethod(),
				err,
				ka)
			return
		}

	})

	t.Run("Ensure config flag forces proxy connection", func(t *testing.T) {
		cs := NewTestClient(ts, nodeSampleLabels)
		ka := kubernetes.KubeAgentConfig{
			Clientset: cs,
			HTTPClient: http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
			}},
			ClusterHostURL: "https://" + ts.Listener.Addr().String(),
			ForceKubeProxy: true,
		}
		ka, err := kubernetes.EnsureNodeSource(ka)

		if ka.GetNodeRetrievalMethod() != kubernetes.Proxy || err != nil {
			t.Errorf("Expected proxy node retrieval method with force_kube_proxy flag set, but got %v. Error: %v, Config: %+v",
				ka.GetNodeRetrievalMethod(),
				err,
				ka)
			return
		}

	})
}

func TestFargateNodeDetection(t *testing.T) {
	n := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proxyNode",
			Namespace: v1.NamespaceDefault,
			Labels:    nodeSampleLabels,
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    "InternalIP",
					Address: "1.110.235.222",
				},
			},
			Conditions: []v1.NodeCondition{{
				Type:   v1.NodeReady,
				Status: v1.ConditionTrue,
			}},
			DaemonEndpoints: v1.NodeDaemonEndpoints{
				KubeletEndpoint: v1.DaemonEndpoint{
					Port: 80,
				},
			},
		},
	}

	t.Run("non-Fargate node returns false", func(t *testing.T) {
		if kubernetes.IsFargateNode(n) {
			t.Errorf("Incorrectly identified a node as Fargate")
		}
	})

	t.Run("Fargate node returns true", func(t *testing.T) {
		// add Fargate-identifying labels
		n.ObjectMeta.Labels = fargateLabels
		if !kubernetes.IsFargateNode(n) {
			t.Errorf("Should have identified node as Fargate")
		}
	})

}

func TestDownloadNodeData(t *testing.T) {
	returnCodes := []int{200, 200, 400, 400, 200, 200, 400}
	ts := launchTLSTestServer(returnCodes)
	cs := NewTestClient(ts, nodeSampleLabels)
	defer ts.Close()

	t.Run("Ensure node added to fail list when providerID doesn't exist", func(t *testing.T) {
		c := http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					// nolint gosec
					InsecureSkipVerify: true,
				},
			}}
		rc := raw.NewClient(
			c,
			true,
			"",
			uint(1),
		)
		ka := kubernetes.KubeAgentConfig{
			Clientset:       cs,
			HTTPClient:      c,
			InClusterClient: rc,
			ClusterHostURL:  "https://" + ts.Listener.Addr().String(),
		}
		wd, _ := os.Getwd()
		ed, _ := os.Open(fmt.Sprintf("%s/testdata", wd))

		ns := testNodeSource{}

		s := strings.Split(ts.Listener.Addr().String(), ":")
		ip := s[0]
		port, _ := strconv.Atoi(s[1])
		ns.Nodes = []v1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "proxyNode", Namespace: v1.NamespaceDefault},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    "InternalIP",
							Address: ip,
						},
					},
					Conditions: []v1.NodeCondition{{
						Type:   v1.NodeReady,
						Status: v1.ConditionTrue,
					}},
					DaemonEndpoints: v1.NodeDaemonEndpoints{
						KubeletEndpoint: v1.DaemonEndpoint{
							Port: int32(port),
						},
					},
				},
				Spec: v1.NodeSpec{
					PodCIDR:    "",
					ExternalID: "",
				},
			},
		}

		failedNodeList, _ := kubernetes.DownloadNodeData(
			"baseline",
			ka,
			ed,
			ns,
		)

		errFromList, ok := failedNodeList["proxyNode"]
		if !ok {
			t.Error("Expected error for nodename \"proxyNode\"")
		}

		if errFromList.Error() != "Provider ID for node does not exist. "+
			"If this condition persists it will cause inconsistent cluster allocation" {
			t.Error("unexpected error")
		}
	})

	t.Run("Ensure error is returned when GetReadyNodes returns error", func(t *testing.T) {
		c := http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					// nolint gosec
					InsecureSkipVerify: true,
				},
			}}
		rc := raw.NewClient(
			c,
			true,
			"",
			uint(1),
		)
		ka := kubernetes.KubeAgentConfig{
			Clientset:       cs,
			HTTPClient:      c,
			InClusterClient: rc,
			ClusterHostURL:  "https://" + ts.Listener.Addr().String(),
		}
		wd, _ := os.Getwd()
		ed, _ := os.Open(fmt.Sprintf("%s/testdata", wd))

		ns := testNodeSource{}

		_, err := kubernetes.DownloadNodeData(
			"baseline",
			ka,
			ed,
			ns,
		)

		if err == nil {
			t.Error("Expected no nodes found error")
		}

		if err.Error() != "cloudability metric agent is unable to get a list of nodes: 0 nodes were ready" {
			t.Error("unexpected error")
		}
	})
}

type testNodeSource struct {
	Nodes []v1.Node
}

func (tns testNodeSource) GetReadyNodes() ([]v1.Node, error) {
	returnCodes := []int{200, 200, 400, 400, 200, 200, 400}

	ts := launchTLSTestServer(returnCodes)
	nodes := tns.Nodes
	defer ts.Close()

	if len(nodes) == 0 {
		return nil, fmt.Errorf("0 nodes were ready")
	}

	return nodes, nil
}

func (tns testNodeSource) NodeAddress(node *v1.Node) (string, int32, error) {
	return "", int32(0), nil
}

//launchTLSTestServer takes a slice of http status codes (int) to return
func launchTLSTestServer(responseCodes []int) *httptest.Server {
	callCount := 0
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount < len(responseCodes) {
			w.WriteHeader(responseCodes[callCount])
			callCount++
		}
	}))

	return ts
}
