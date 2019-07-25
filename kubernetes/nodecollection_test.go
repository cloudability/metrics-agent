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

func NewTestClient(ts *httptest.Server) *fake.Clientset {
	s := strings.Split(ts.Listener.Addr().String(), ":")
	ip := s[0]
	port, _ := strconv.Atoi(s[1])
	return fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "proxyNode", Namespace: v1.NamespaceDefault},
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    "InternalIP",
						Address: ip,
					},
				},
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

	returnCodes := []int{200, 200, 400, 400, 200, 200, 400}
	ts := launchTLSTestServer(returnCodes)
	cs := NewTestClient(ts)
	ka := kubernetes.KubeAgentConfig{
		Clientset:  cs,
		HTTPClient: http.Client{},
	}

	defer ts.Close()

	t.Run("Ensure successful direct node source test", func(t *testing.T) {

		ka, err := kubernetes.EnsureNodeSource(ka)

		if ka.GetNodeRetrievalMethod() != "direct" || err != nil {
			t.Errorf("Expected direct node retrieval method but got %v: %v",
				ka.GetNodeRetrievalMethod(),
				err)
			return
		}

	})

	t.Run("Ensure successful proxy node source test", func(t *testing.T) {
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

		if ka.GetNodeRetrievalMethod() != "proxy" || err != nil {
			t.Errorf("Expected proxy node retrieval method but got %v: %v", ka.GetNodeRetrievalMethod(), err)
			return
		}
	})

	t.Run("Ensure unsuccessful node source test", func(t *testing.T) {
		ka, err := kubernetes.EnsureNodeSource(ka)
		if ka.GetNodeRetrievalMethod() != "unreachable" {
			t.Errorf("Expected unreachable node retrieval method but got %v: %v", ka.GetNodeRetrievalMethod(), err)
			return
		}
	})
}

func TestDownloadNodeData(t *testing.T) {
	returnCodes := []int{200, 200, 400, 400, 200, 200, 400}
	ts := launchTLSTestServer(returnCodes)
	cs := NewTestClient(ts)
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
}

type testNodeSource struct{}

func (tns testNodeSource) GetNodes() (*v1.NodeList, error) {
	returnCodes := []int{200, 200, 400, 400, 200, 200, 400}

	ts := launchTLSTestServer(returnCodes)

	s := strings.Split(ts.Listener.Addr().String(), ":")
	ip := s[0]
	port, _ := strconv.Atoi(s[1])
	nl := v1.NodeList{
		Items: []v1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "proxyNode", Namespace: v1.NamespaceDefault},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    "InternalIP",
							Address: ip,
						},
					},
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
		},
	}
	defer ts.Close()

	return &nl, nil
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
