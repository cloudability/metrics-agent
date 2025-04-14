package kubernetes

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/cloudability/metrics-agent/retrieval/raw"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFetchEndpoint(t *testing.T) {
	t.Run("Verify that endpoint is removed from map upon successful fetch", func(t *testing.T) {
		endpointsToFetch := map[Endpoint]bool{
			NodeStatsSummaryEndpoint: true,
		}
		mask := EndpointMask{}
		// Direct method is enabled
		mask.SetAvailability(NodeStatsSummaryEndpoint, Direct, true)
		config := KubeAgentConfig{
			NodeMetrics:       mask,
			ConcurrentPollers: 10,
		}
		// Use Direct method for connection
		cm := ConnectionMethod{
			ConnType: Direct,
		}
		// returns no error, so endpoint call should "succeed"
		endpointFetcherMock := func() (filename string, err error) { return "file-name", nil }

		err := fetchEndpoint(endpointsToFetch, NodeStatsSummaryEndpoint, config, cm, endpointFetcherMock)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if endpointsToFetch[NodeStatsSummaryEndpoint] {
			t.Errorf("%s should have been removed from map upon successful fetch, got %+v",
				NodeStatsSummaryEndpoint, endpointsToFetch)
		}

	})

	t.Run("Verify that endpoint is not removed from map upon unsuccessful fetch", func(t *testing.T) {
		endpointsToFetch := map[Endpoint]bool{
			NodeStatsSummaryEndpoint: true,
		}
		mask := EndpointMask{}
		mask.SetAvailability(NodeStatsSummaryEndpoint, Direct, true)
		config := KubeAgentConfig{
			NodeMetrics:       mask,
			ConcurrentPollers: 10,
		}
		cm := ConnectionMethod{
			ConnType: Direct,
		}
		// fetcher returns an error, so we should not mark endpoint as fetched
		endpointFetcherFunc := func() (filename string, err error) {
			return "", fmt.Errorf("whoa there buddy you can't fetch that there endpoint pardner")
		}

		err := fetchEndpoint(endpointsToFetch, NodeStatsSummaryEndpoint, config, cm, endpointFetcherFunc)
		if err == nil {
			t.Error("expected error to occur when endpointFetcherFunc returns an error")
		}
		if !endpointsToFetch[NodeStatsSummaryEndpoint] {
			t.Errorf("%s should not have been removed from map upon failed fetch, got %+v",
				NodeStatsSummaryEndpoint, endpointsToFetch)
		}
	})

	t.Run("Verify that endpoint is not fetched if method is not available", func(t *testing.T) {
		endpointsToFetch := map[Endpoint]bool{
			NodeStatsSummaryEndpoint: true,
		}
		mask := EndpointMask{}
		// only proxy is available
		mask.SetAvailability(NodeStatsSummaryEndpoint, Proxy, true)
		config := KubeAgentConfig{
			NodeMetrics:       mask,
			ConcurrentPollers: 10,
		}
		// try to fetch via Direct
		cm := ConnectionMethod{
			ConnType: Direct,
		}
		// this returns an error but we shouldn't call it because this endpoint doesn't have Direct conn as an option
		endpointFetcherMock := func() (filename string, err error) {
			return "", fmt.Errorf("whoa there buddy you can't fetch that there endpoint pardner")
		}

		err := fetchEndpoint(endpointsToFetch, NodeStatsSummaryEndpoint, config, cm, endpointFetcherMock)
		if err != nil {
			t.Errorf("should not have error because endpointFetcherMock shouldn't have been called: %v", err)
		}
		if !endpointsToFetch[NodeStatsSummaryEndpoint] {
			t.Errorf("%s should not have been removed from map upon failed fetch, got %+v",
				NodeStatsSummaryEndpoint, endpointsToFetch)
		}
	})
}

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
	return NewTestClientWithNodes(ts, labels, 1)
}

func NewTestClientWithNodes(ts *httptest.Server, labels map[string]string, numNodes int) *fake.Clientset {
	s := strings.Split(ts.Listener.Addr().String(), ":")
	ip := s[0]
	port, _ := strconv.Atoi(s[1])
	nodes := make([]runtime.Object, numNodes)
	for i := 0; i < numNodes; i++ {
		nodes[i] = &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("proxyNode.%d", i),
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
		}
	}
	return fake.NewSimpleClientset(nodes...)
}

func TestEnsureNodeSource(t *testing.T) {
	// Cause test server to return success for the first two test queries, enabling
	// the first test to succeed on the "direct" step.
	// The second (proxy) test encounters two 400s when attempting a direct connection,
	// which triggers a fallback to proxy testing, which receives the next two 200s.
	// The final 400 fails the direct connection test, and as no proxy client is provided
	// for the "unsuccessful" node test, it falls through to unreachable status.
	// If no status code is provided, the default response is 200.

	t.Run("Ensure successful direct node source test", func(t *testing.T) {
		returnCodes := []int{200, 200}
		ts := launchTLSTestServer(returnCodes)
		cs := NewTestClient(ts, nodeSampleLabels)
		defer ts.Close()
		ka := KubeAgentConfig{
			Clientset:            cs,
			HTTPClient:           http.Client{},
			CollectionRetryLimit: 0,
			ConcurrentPollers:    10,
			NodeMetrics:          EndpointMask{},
		}
		ka, err := ensureNodeSource(context.TODO(), ka)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !ka.NodeMetrics.DirectAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("Expected direct node retrieval method but got %v: %v",
				ka.NodeMetrics.Options(NodeStatsSummaryEndpoint),
				err)
			return
		}
	})

	t.Run("Ensure successful on mix of node failures and success", func(t *testing.T) {
		returnCodes := []int{200, 400, 400}
		ts := launchTLSTestServer(returnCodes)
		cs := NewTestClientWithNodes(ts, nodeSampleLabels, 2)
		defer ts.Close()
		ka := KubeAgentConfig{
			Clientset:            cs,
			HTTPClient:           http.Client{},
			CollectionRetryLimit: 0,
			ConcurrentPollers:    10,
			NodeMetrics:          EndpointMask{},
		}
		ka, err := ensureNodeSource(context.TODO(), ka)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !ka.NodeMetrics.DirectAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("Expected direct node retrieval method but got %v: %v",
				ka.NodeMetrics.Options(NodeStatsSummaryEndpoint),
				err)
			return
		}
	})

	t.Run("Ensure proxy on mix of node direct and proxy", func(t *testing.T) {
		returnCodes := []int{200, 400, 200}
		ts := launchTLSTestServer(returnCodes)
		cs := NewTestClientWithNodes(ts, nodeSampleLabels, 2)
		defer ts.Close()
		ka := KubeAgentConfig{
			Clientset:            cs,
			CollectionRetryLimit: 0,
			ConcurrentPollers:    10,
			NodeMetrics:          EndpointMask{},
			ClusterHostURL:       "https://" + ts.Listener.Addr().String(),
			// The proxy connection method uses the config http client
			HTTPClient: http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
			}},
		}
		ka, err := ensureNodeSource(context.TODO(), ka)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if ka.NodeMetrics.DirectAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("Expected proxy node retrieval method but got %v: %v",
				ka.NodeMetrics.Options(NodeStatsSummaryEndpoint),
				err)
			return
		}

		if !ka.NodeMetrics.ProxyAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("Expected proxy node retrieval method but got %v: %v",
				ka.NodeMetrics.Options(NodeStatsSummaryEndpoint),
				err)
			return
		}
	})

	t.Run("Ensure all needed clients function when multiple methods are set", func(t *testing.T) {
		// Two endpoints will succeed both times, but stats summary will fail on direct
		directConnectionAttempts := []int{200}
		proxyConnectionAttempts := []int{200}
		directConnectionReturnCodes := append(directConnectionAttempts, proxyConnectionAttempts...)
		ts := launchTLSTestServer(directConnectionReturnCodes)
		defer ts.Close()
		cs := NewTestClient(ts, nodeSampleLabels)
		ka := KubeAgentConfig{
			Clientset: cs,
			// The proxy connection method uses the config http client
			HTTPClient: http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
			}},
			ClusterHostURL:    "https://" + ts.Listener.Addr().String(),
			ConcurrentPollers: 10,
			NodeMetrics:       EndpointMask{},
			// just populate some dummy fields here to ensure neither client gets unset
			InClusterClient: raw.NewClient(http.Client{}, true, "token", "", 0, false),
			NodeClient:      raw.NewClient(http.Client{}, true, "token", "", 0, false),
		}

		ka, err := ensureNodeSource(context.TODO(), ka)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if ka.NodeMetrics.ProxyAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("Expected /stats/summary to use direct, but proxy was allowed %v: %v",
				ka.NodeMetrics.Options(NodeStatsSummaryEndpoint), err)
			return
		}
		if !ka.NodeMetrics.DirectAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("Expected Direct method but got %v: %v",
				ka.NodeMetrics.Options(NodeStatsSummaryEndpoint), err)
			return
		}
		// ensure that both clients are populated
		if ka.NodeClient.HTTPClient == nil {
			t.Errorf("Direct connection client should be populated")
		}
		if ka.InClusterClient.HTTPClient == nil {
			t.Errorf("Proxy client should be populated")
		}
	})

	t.Run("Ensure successful proxy node source test", func(t *testing.T) {
		returnCodes := []int{404, 200}
		ts := launchTLSTestServer(returnCodes)
		cs := NewTestClient(ts, nodeSampleLabels)
		defer ts.Close()
		ka := KubeAgentConfig{
			Clientset: cs,
			// The proxy connection method uses the config http client
			HTTPClient: http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
			}},
			ClusterHostURL:    "https://" + ts.Listener.Addr().String(),
			ConcurrentPollers: 10,
			NodeMetrics:       EndpointMask{},
		}

		ka, err := ensureNodeSource(context.TODO(), ka)

		if !ka.NodeMetrics.ProxyAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("Expected stats/summary to proxy direct method but got %v: %v",
				ka.NodeMetrics.Options(NodeStatsSummaryEndpoint), err)
			return
		}
	})

	t.Run("Ensure unsuccessful node source test", func(t *testing.T) {
		returnCodes := []int{400, 400}
		ts := launchTLSTestServer(returnCodes)
		cs := NewTestClient(ts, nodeSampleLabels)
		defer ts.Close()
		ka := KubeAgentConfig{
			Clientset:            cs,
			HTTPClient:           http.Client{},
			CollectionRetryLimit: 0,
			ConcurrentPollers:    10,
			NodeMetrics:          EndpointMask{},
		}
		ka, err := ensureNodeSource(context.TODO(), ka)

		if !ka.NodeMetrics.Unreachable(NodeStatsSummaryEndpoint) {
			t.Errorf("Expected Unreachable but got %v: %v",
				ka.NodeMetrics.Options(NodeStatsSummaryEndpoint), err)
			return
		}
	})

	t.Run("Ensure Fargate node forces proxy connection", func(t *testing.T) {
		returnCodes := []int{200, 200}
		ts := launchTLSTestServer(returnCodes)
		cs := NewTestClient(ts, fargateLabels)
		ka := KubeAgentConfig{
			Clientset: cs,
			HTTPClient: http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
			}},
			ClusterHostURL:    "https://" + ts.Listener.Addr().String(),
			ConcurrentPollers: 10,
			NodeMetrics:       EndpointMask{},
		}
		ka, err := ensureNodeSource(context.TODO(), ka)

		if ka.NodeMetrics.DirectAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("Direct connection should not be enabled with fargate nodes present")
		}
		if !ka.NodeMetrics.ProxyAllowed(NodeStatsSummaryEndpoint) || err != nil {
			t.Errorf("Expected proxy node retrieval method for Fargate node but got %v. Error: %v, Config: %+v",
				ka.NodeMetrics.Options(NodeStatsSummaryEndpoint),
				err,
				ka)
			return
		}
	})

	t.Run("Ensure config flag forces proxy connection", func(t *testing.T) {
		returnCodes := []int{200, 200}
		ts := launchTLSTestServer(returnCodes)
		cs := NewTestClient(ts, nodeSampleLabels)
		ka := KubeAgentConfig{
			Clientset: cs,
			HTTPClient: http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
				// nolint gosec
				InsecureSkipVerify: true,
			},
			}},
			ClusterHostURL:    "https://" + ts.Listener.Addr().String(),
			ForceKubeProxy:    true,
			ConcurrentPollers: 10,
			NodeMetrics:       EndpointMask{},
		}
		ka, err := ensureNodeSource(context.TODO(), ka)
		if ka.NodeMetrics.DirectAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("Direct connection should not be enabled with force proxy flag set")
		}
		if !ka.NodeMetrics.ProxyAllowed(NodeStatsSummaryEndpoint) || err != nil {
			t.Errorf("Expected proxy node retrieval method with force_kube_proxy flag set, but got %v. Error: %v, Config: %+v",
				ka.NodeMetrics.Options(NodeStatsSummaryEndpoint),
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
		if isFargateNode(n) {
			t.Errorf("Incorrectly identified a node as Fargate")
		}
	})

	t.Run("Fargate node returns true", func(t *testing.T) {
		// add Fargate-identifying labels
		n.ObjectMeta.Labels = fargateLabels
		if !isFargateNode(n) {
			t.Errorf("Should have identified node as Fargate")
		}
	})

}

func TestDownloadNodeData(t *testing.T) {
	returnCodes := []int{200, 200, 200, 400, 400, 400, 200, 200, 200, 400}
	ts := launchTLSTestServer(returnCodes)
	cs := NewTestClient(ts, nodeSampleLabels)
	defer ts.Close()

	t.Run("Ensure node added to fail list when providerID doesn't exist", func(t *testing.T) {
		ed, ns, ka := setupTestNodeDownloaderClients(ts, cs, 1)
		failedNodeList, _ := downloadNodeData(
			context.TODO(),
			"baseline",
			ka,
			ed,
			ns,
		)

		errFromList, ok := failedNodeList["proxyNode"]
		if !ok {
			t.Error("Expected error for nodename \"proxyNode\"")
		}

		if errFromList.Error() != "provider ID for node does not exist. "+
			"If this condition persists it will cause inconsistent cluster allocation" {
			t.Error("unexpected error")
		}
	})

	t.Run("Ensure error is returned when GetReadyNodes returns error", func(t *testing.T) {
		ed, _, ka := setupTestNodeDownloaderClients(ts, cs, 1)
		ns := testNodeSource{}

		_, err := downloadNodeData(
			context.TODO(),
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

// nolint:revive
func TestDownloadNodeDataRetries(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	var callCount uint
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var code int
		if callCount > 0 {
			code = 403
		} else {
			code = 500
		}
		callCount++
		w.WriteHeader(code)
	}))
	cs := NewTestClient(ts, nodeSampleLabels)
	defer ts.Close()

	t.Run("should honor max collection retry limit", func(t *testing.T) {
		var maxRetry uint = 1
		ed, ns, ka := setupTestNodeDownloaderClients(ts, cs, maxRetry)
		failedNodeList, err := downloadNodeData(
			context.TODO(),
			"baseline",
			ka,
			ed,
			ns,
		)
		g.Expect(err).To(gomega.BeNil())
		// just one node in the list to attempt fetch from
		g.Expect(failedNodeList).To(gomega.HaveLen(1), "the node passed in is unreachable")
		// only a single endpoint connection will be attempted (and retried) before failing
		maxAttempts := maxRetry + 1
		g.Expect(callCount).To(gomega.Equal(maxAttempts), "should fail up to maxRetry + 1 times")
	})

}

type testNodeSource struct {
	Nodes []v1.Node
}

func (tns testNodeSource) GetReadyNodes(ctx context.Context) ([]v1.Node, error) {
	returnCodes := []int{200, 200, 200, 400, 400, 400, 200, 200, 200, 400}

	ts := launchTLSTestServer(returnCodes)
	nodes := tns.Nodes
	defer ts.Close()

	if len(nodes) == 0 {
		return nil, fmt.Errorf("0 nodes were ready")
	}

	return nodes, nil
}

// nolint:revive
func (tns testNodeSource) NodeAddress(node *v1.Node) (string, int32, error) {
	return "", int32(0), nil
}

// launchTLSTestServer takes a slice of http status codes (int) to return
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

// setupTestNodeDownloaderClients returns commonly-needed configs and clients
// for testing node downloads
func setupTestNodeDownloaderClients(ts *httptest.Server,
	cs *fake.Clientset,
	retries uint) (*os.File, testNodeSource, KubeAgentConfig) {
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
		"",
		retries,
		false,
	)
	ka := KubeAgentConfig{
		Clientset:            cs,
		HTTPClient:           c,
		InClusterClient:      rc,
		ClusterHostURL:       "https://" + ts.Listener.Addr().String(),
		ConcurrentPollers:    10,
		CollectionRetryLimit: retries,
	}
	ka.NodeMetrics = EndpointMask{}
	ka.NodeMetrics.SetAvailability(NodeStatsSummaryEndpoint, Proxy, true)

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
				PodCIDR: "",
			},
		},
	}
	return ed, ns, ka
}
