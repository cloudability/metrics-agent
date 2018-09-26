package kubernetes

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	v1 "k8s.io/client-go/pkg/api/v1"
)

// func TestEnsureNodeSource(t *testing.T) {

// 	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		w.WriteHeader(200)
// 	}))

// 	s := strings.Split(ts.Listener.Addr().String(), ":")
// 	ip := s[0]
// 	port, _ := strconv.Atoi(s[1])

// 	cs := fake.NewSimpleClientset(
// 		&v1.Node{
// 			ObjectMeta: metav1.ObjectMeta{Name: "directNode", Namespace: v1.NamespaceDefault},
// 			Status: v1.NodeStatus{
// 				Addresses: []v1.NodeAddress{
// 					{
// 						Type:    "InternalIP",
// 						Address: ip,
// 					},
// 				},
// 				DaemonEndpoints: v1.NodeDaemonEndpoints{
// 					KubeletEndpoint: v1.DaemonEndpoint{
// 						Port: int32(port),
// 					},
// 				},
// 			},
// 		},
// 	)

// 	tsp := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		if strings.Contains(r.URL.Path, "/proxy/stats") && r.Method == http.MethodGet {
// 			w.WriteHeader(200)
// 		} else if strings.Contains(r.URL.Path, "/proxy/stats/") && r.Method == http.MethodPost {
// 			w.WriteHeader(200)
// 		}
// 		w.WriteHeader(403)

// 	}))

// 	defer ts.Close()

// 	s = strings.Split(tsp.Listener.Addr().String(), ":")
// 	ip = s[0]
// 	port, _ = strconv.Atoi(s[1])
// 	tspcs := fake.NewSimpleClientset(
// 		&v1.Node{
// 			ObjectMeta: metav1.ObjectMeta{Name: "proxyNode", Namespace: v1.NamespaceDefault},
// 			Status: v1.NodeStatus{
// 				Addresses: []v1.NodeAddress{
// 					{
// 						Type:    "InternalIP",
// 						Address: ip,
// 					},
// 				},
// 				DaemonEndpoints: v1.NodeDaemonEndpoints{
// 					KubeletEndpoint: v1.DaemonEndpoint{
// 						Port: int32(port),
// 					},
// 				},
// 			},
// 		},
// 	)

// 	t.Run("Ensure successful direct node source test", func(t *testing.T) {

// 		ka := KubeAgentConfig{
// 			Clientset:  cs,
// 			HTTPClient: http.Client{},
// 		}

// 		ka, err := ensureNodeSource(ka)

// 		if ka.nodeRetrievalMethod != "direct" || err != nil {
// 			t.Errorf("Expected direct node retrieval method but got %v: %v", ka.nodeRetrievalMethod, err)
// 			return
// 		}

// 		ts.Close()
// 	})

// 	t.Run("Ensure successful proxy node source test", func(t *testing.T) {
// 		ka := KubeAgentConfig{
// 			Clientset: tspcs,
// 			HTTPClient: http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
// 				// nolint gosec
// 				InsecureSkipVerify: true,
// 			},
// 			}},
// 			ClusterHostURL: "https://" + tsp.Listener.Addr().String(),
// 		}

// 		ka, err := ensureNodeSource(ka)

// 		if ka.nodeRetrievalMethod != "proxy" || err != nil {
// 			t.Errorf("Expected proxy node retrieval method but got %v: %v", ka.nodeRetrievalMethod, err)
// 			return
// 		}
// 	})

// 	t.Run("Ensure unsuccessful node source test", func(t *testing.T) {
// 		ka := KubeAgentConfig{
// 			Clientset:  tspcs,
// 			HTTPClient: http.Client{},
// 		}

// 		ka, err := ensureNodeSource(ka)

// 		if ka.nodeRetrievalMethod != "unreachable" || err == nil {
// 			t.Errorf("Expected unreachable node retrieval method but got %v: %v", ka.nodeRetrievalMethod, err)
// 			return
// 		}
// 		tsp.Close()
// 	})
// }

func TestEnsureNodeSource(t *testing.T) {

	returnCodes := []int{200, 200, 400, 400, 200, 200}
	ts := launchTLSTestServer(returnCodes)

	s := strings.Split(ts.Listener.Addr().String(), ":")
	ip := s[0]
	port, _ := strconv.Atoi(s[1])
	cs := fake.NewSimpleClientset(
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

	ka := KubeAgentConfig{
		Clientset:  cs,
		HTTPClient: http.Client{},
	}

	defer ts.Close()

	t.Run("Ensure successful direct node source test", func(t *testing.T) {

		ka, err := ensureNodeSource(ka)

		if ka.nodeRetrievalMethod != "direct" || err != nil {
			t.Errorf("Expected direct node retrieval method but got %v: %v", ka.nodeRetrievalMethod, err)
			return
		}

	})

	t.Run("Ensure successful proxy node source test", func(t *testing.T) {
		ka := KubeAgentConfig{
			Clientset: cs,
			HTTPClient: http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
			}},
			ClusterHostURL: "https://" + ts.Listener.Addr().String(),
		}

		ka, err := ensureNodeSource(ka)

		if ka.nodeRetrievalMethod != "proxy" || err != nil {
			t.Errorf("Expected proxy node retrieval method but got %v: %v", ka.nodeRetrievalMethod, err)
			return
		}
	})

	t.Run("Ensure unsuccessful node source test", func(t *testing.T) {

		ka, err := ensureNodeSource(ka)
		if ka.nodeRetrievalMethod != "unreachable" {
			t.Errorf("Expected unreachable node retrieval method but got %v: %v", ka.nodeRetrievalMethod, err)
			return
		}
	})
}

//launchTLSTestServer takes a slice of http status codes (int) to return
func launchTLSTestServer(responseCodes []int) *httptest.Server {
	callCount := 0
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.WriteHeader(responseCodes[callCount])
		callCount++
	}))

	return ts
}
