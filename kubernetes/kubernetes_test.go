package kubernetes

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudability/metrics-agent/retrieval/raw"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// nolint: dupl
func TestCreateClusterConfig(t *testing.T) {
	config := KubeAgentConfig{
		APIKey:       "1234-456-789",
		PollInterval: 600,
		Insecure:     false,
	}
	t.Run("ensure that a clientset and agentConfig are returned", func(t *testing.T) {
		config, err := createClusterConfig(config)
		if config.Clientset == nil || config.UseInClusterConfig || err != nil {
			t.Errorf("Expected clientset and agentConfig to successfully create / update %v ", err)
		}
	})
}

// nolint: dupl
func TestUpdateConfigurationForServices(t *testing.T) {

	config := KubeAgentConfig{
		APIKey:         "1234-456-789",
		ClusterHostURL: "http://localhost:8088",
		PollInterval:   600,
		Insecure:       false,
	}

	t.Run("ensure that an updated agentConfig with services if running is returned", func(t *testing.T) {
		selfLink := "http://localhost"
		servicePort := 8080
		config.Clientset = fake.NewSimpleClientset(&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "heapster",
				Namespace: v1.NamespaceDefault,
				Labels: map[string]string{
					"tag": "",
				},
				SelfLink: selfLink,
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Port: int32(servicePort),
					},
				},
			},
		})

		_, err := updateConfigurationForServices(config)
		if err != nil {
			t.Errorf("Error getting services %v ", err)
		}
	})

	t.Run("ensure that an updated agentConfig with services if running without defined serviceport is returned",
		func(t *testing.T) {
			selfLink := "http://localhost"
			config.Clientset = fake.NewSimpleClientset(&v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "heapster",
					Namespace: v1.NamespaceDefault,
					Labels: map[string]string{
						"tag": "",
					},
					SelfLink: selfLink,
				},
			})

			_, err := updateConfigurationForServices(config)
			if err != nil {
				t.Errorf("Error getting services %v ", err)
			}

		})

}

func TestEnsureMetricServicesAvailable(t *testing.T) {
	t.Parallel()
	t.Run("should return error if can't get node summaries", func(t *testing.T) {
		cs := fake.NewSimpleClientset(
			&v1.Node{
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{{Type: v1.NodeInternalIP}},
					Conditions: []v1.NodeCondition{{
						Type:   v1.NodeReady,
						Status: v1.ConditionTrue,
					}},
				},
				ObjectMeta: metav1.ObjectMeta{Name: "node0", Namespace: v1.NamespaceDefault}},
		)
		config := KubeAgentConfig{
			RetrieveNodeSummaries: true,
			CollectHeapsterExport: false,
			Clientset:             cs,
			NodeMetrics:           EndpointMask{},
		}
		config, err := ensureMetricServicesAvailable(config)
		if err == nil {
			t.Errorf("expected an error for ensureMetricServicesAvailable")
			return
		}
		if !config.NodeMetrics.Unreachable(NodeStatsSummaryEndpoint) {
			t.Errorf("expected connection to be unreachable, instead was %s",
				config.NodeMetrics.Options(NodeStatsSummaryEndpoint))
		}
	})

	t.Run("shouldn't return error if successfully fetch node summaries", func(t *testing.T) {
		cs := fake.NewSimpleClientset(
			&v1.Node{Status: v1.NodeStatus{Addresses: []v1.NodeAddress{{Type: v1.NodeInternalIP}}},
				ObjectMeta: metav1.ObjectMeta{Name: "node0", Namespace: v1.NamespaceDefault}},
		)
		client := http.Client{}
		ts := NewTestServer()
		defer ts.Close()
		config := KubeAgentConfig{
			RetrieveNodeSummaries: true,
			CollectHeapsterExport: false,
			Clientset:             cs,
			ClusterHostURL:        ts.URL,
			HeapsterURL:           ts.URL,
			HTTPClient:            client,
			InClusterClient:       raw.NewClient(client, true, "", 0),
		}

		var err error
		_, err = ensureMetricServicesAvailable(config)
		if err != nil {
			t.Errorf("Unexpected error fetching node summaries: %s", err)
		}
	})
}

// nolint: dupl
func TestUpdateConfigWithOverrideURLs(t *testing.T) {

	t.Parallel()

	t.Run("ensure that heapsterURL is set when Overridden Heapster URL argument is defined", func(t *testing.T) {
		config := updateConfigWithOverrideURLs(KubeAgentConfig{
			HeapsterOverrideURL: "https://heapster.availabile.here:443/",
			Insecure:            false,
		})
		if config.HeapsterURL != config.HeapsterOverrideURL || config.Insecure {
			t.Error(" HeapsterURL override URL set but not validated and set to heapsterURL ")
		}

	})

	t.Run("ensure that heapsterURL is set to proxyPath when overridden Heapster URL is not defined", func(t *testing.T) {
		config := updateConfigWithOverrideURLs(KubeAgentConfig{
			HeapsterOverrideURL: "",
			HeapsterProxyURL: url.URL{
				Host: "http://localhost:8888/",
				Path: "/api/v1/namespaces/default/services/heapster:8888/proxy/metrics"},
		})
		if config.HeapsterURL != config.HeapsterProxyURL.Host+config.HeapsterProxyURL.Path {
			t.Error(" HeapsterURL is not set to proxyPath ")
		}

	})

}

func TestUpdateConfigWithOverrideNamespace(t *testing.T) {
	t.Parallel()

	config := KubeAgentConfig{
		APIKey:       "1234-456-789",
		PollInterval: 600,
		Insecure:     false,
		Namespace:    "testing-namespace",
	}
	t.Run("ensure that namespace is set correctly", func(t *testing.T) {
		config, _ := createClusterConfig(config)
		if config.Namespace != "testing-namespace" {
			t.Errorf("Expected Namespace to be \"testing-namespace\" but received \"%v\" ", config.Namespace)
		}
	})
}

func TestCreateAgentStatusMetric(t *testing.T) {

	AgentStartTime := time.Now()
	exportDir := os.TempDir() + "/" + strconv.FormatInt(time.Now().Unix(), 10)
	_ = os.MkdirAll(exportDir, os.ModePerm)
	tD, _ := os.Open(exportDir)

	cs := fake.NewSimpleClientset()
	sv, _ := cs.Discovery().ServerVersion()

	config := KubeAgentConfig{
		AgentStartTime: AgentStartTime,
		ClusterVersion: ClusterVersion{
			version:     1.1,
			versionInfo: sv,
		},
		UseInClusterConfig: false,
		HeapsterURL:        "https://heapster.url",
	}

	t.Run("Ensure that a cloudability Status metric is created", func(t *testing.T) {
		err := createAgentStatusMetric(tD, config, AgentStartTime)

		if err != nil {
			t.Errorf("Error creating agent Status Metric: %v", err)
		}
		os.RemoveAll(tD.Name())
	})
}

//nolint gocyclo
func TestCollectMetrics(t *testing.T) {

	ts := NewTestServer()
	defer ts.Close()

	cs := fake.NewSimpleClientset(
		&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node0", Namespace: v1.NamespaceDefault}, Status: v1.NodeStatus{Conditions: []v1.NodeCondition{{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		}}}},
		&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1", Namespace: v1.NamespaceDefault}, Status: v1.NodeStatus{Conditions: []v1.NodeCondition{{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		}}}},
		&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2", Namespace: v1.NamespaceDefault}, Status: v1.NodeStatus{Conditions: []v1.NodeCondition{{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		}}}},
	)

	sv, err := cs.Discovery().ServerVersion()
	if err != nil {
		t.Errorf("Error getting server version: %v", err)
	}
	dir, err := ioutil.TempDir("", "TestCollectMetrics")
	if err != nil {
		t.Errorf("error creating temp dir: %v", err)
	}
	tDir, err := os.Open(dir)
	if err != nil {
		t.Errorf("Error opening temp dir: %v", err)
	}

	ka := KubeAgentConfig{
		ClusterVersion: ClusterVersion{
			version:     1.1,
			versionInfo: sv,
		},
		Clientset:             cs,
		HTTPClient:            http.Client{},
		msExportDirectory:     tDir,
		UseInClusterConfig:    false,
		ClusterHostURL:        ts.URL,
		HeapsterURL:           ts.URL,
		Insecure:              true,
		BearerToken:           "",
		RetrieveNodeSummaries: true,
		ForceKubeProxy:        false,
		GetAllConStats:        true,
	}
	ka.NodeMetrics = EndpointMask{}
	// set Proxy method available
	ka.NodeMetrics.SetAvailable(NodeStatsSummaryEndpoint, Proxy, true)
	ka.NodeMetrics.SetAvailable(NodeContainerEndpoint, Proxy, true)
	ka.NodeMetrics.SetAvailable(NodeCadvisorEndpoint, Proxy, true)
	// set Direct as option as well
	ka.NodeMetrics.SetAvailable(NodeStatsSummaryEndpoint, Direct, true)
	ka.NodeMetrics.SetAvailable(NodeContainerEndpoint, Direct, true)

	ka.InClusterClient = raw.NewClient(ka.HTTPClient, ka.Insecure, ka.BearerToken, 0)
	fns := NewClientsetNodeSource(cs)

	t.Run("Ensure that a collection occurs", func(t *testing.T) {
		// download the initial baseline...like a typical CollectKubeMetrics would
		err := downloadBaselineMetricExport(ka, fns)
		if err != nil {
			t.Error(err)
		}
		err = ka.collectMetrics(ka, cs, fns)
		if err != nil {
			t.Error(err)
		}

		// TODO: add cprom files to this list
		nodeBaselineFiles := []string{}
		nodeSummaryFiles := []string{}
		expectedBaselineFiles := []string{
			"baseline-container-node0.json",
			"baseline-container-node1.json",
			"baseline-container-node2.json",
			"baseline-summary-node0.json",
			"baseline-summary-node1.json",
			"baseline-summary-node2.json",
		}
		expectedSummaryFiles := []string{
			"stats-container-node0.json",
			"stats-container-node1.json",
			"stats-container-node2.json",
			"stats-summary-node0.json",
			"stats-summary-node1.json",
			"stats-summary-node2.json",
		}

		filepath.Walk(ka.msExportDirectory.Name(), func(path string, info os.FileInfo, err error) error {

			if strings.HasPrefix(info.Name(), "stats-") || strings.HasPrefix(info.Name(), "baseline-") {

				if strings.Contains(info.Name(), "baseline-summary") || strings.Contains(info.Name(), "baseline-container-") {
					nodeBaselineFiles = append(nodeBaselineFiles, info.Name())
				}
				if strings.Contains(info.Name(), "stats-summary") || strings.Contains(info.Name(), "stats-container-") {
					nodeSummaryFiles = append(nodeSummaryFiles, info.Name())
				}
			}
			return nil
		})
		if len(nodeBaselineFiles) != len(expectedBaselineFiles) {
			t.Errorf("Expected %d baseline metrics, instead got %d", len(expectedSummaryFiles), len(nodeBaselineFiles))
			return
		}
		if len(nodeSummaryFiles) != len(expectedSummaryFiles) {
			t.Errorf("Expected %d summary metrics, instead got %d", len(expectedSummaryFiles), len(nodeBaselineFiles))
			return
		}
		for i, n := range expectedBaselineFiles {
			if n != nodeBaselineFiles[i] {
				t.Errorf("Expected file name %s instead got %s", n, nodeBaselineFiles[i])
			}
		}
		for i, n := range expectedSummaryFiles {
			if n != nodeSummaryFiles[i] {
				t.Errorf("Expected file name %s instead got %s", n, nodeBaselineFiles[i])
			}
		}
	})

}

func TestExtractNodeNameAndExtension(t *testing.T) {
	t.Run("should return node name and json extension", func(t *testing.T) {
		wantedNodeName := "-container-ip-10-110-217-3.ec2.internal"
		wantedExtension := ".json"

		filename := "stats-container-ip-10-110-217-3.ec2.internal.json"

		nodeName, extension := extractNodeNameAndExtension("stats", filename)

		if nodeName != wantedNodeName {
			t.Errorf("expected %s but got %s", wantedNodeName, nodeName)
		}

		if extension != wantedExtension {
			t.Errorf("expected %s but got %s", wantedExtension, extension)
		}
	})

	t.Run("should return node name and txt extension", func(t *testing.T) {
		wantedNodeName := "-cadvisor_metrics-ip-10-110-214-235.ec2.internal"
		wantedExtension := ".txt"

		filename := "stats-cadvisor_metrics-ip-10-110-214-235.ec2.internal.txt"

		nodeName, extension := extractNodeNameAndExtension("stats", filename)

		if nodeName != wantedNodeName {
			t.Errorf("expected %s but got %s", wantedNodeName, nodeName)
		}

		if extension != wantedExtension {
			t.Errorf("expected %s but got %s", wantedExtension, extension)
		}
	})
}

func TestSetProxyURL(t *testing.T) {
	t.Run("Ensure that proxyURL without correct schema prefix raises an error", func(t *testing.T) {
		_, err := setProxyURL("iforgottoaddaschema.com:1234")
		if err == nil {
			t.Errorf("Proxy URL without correct schema prefix should raise an error: %v", err)
		}
	})

	t.Run("Ensure that proxyURL with schema prefix does not raise an error", func(t *testing.T) {
		_, err := setProxyURL("https://iforgottoaddaschema.com:1234")
		if err != nil {
			t.Errorf("Proxy URL with schema prefix should not raise an error: %v", err)
		}
	})

}

func NewTestServer() *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResp, _ := json.Marshal(map[string]string{"test": "data", "time": time.Now().String()})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(jsonResp)

	}))
	return ts
}
