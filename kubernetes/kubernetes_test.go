package kubernetes

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"path/filepath"
	"strings"

	"github.com/cloudability/metrics-agent/retrieval/raw"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/pkg/api/v1"
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
		clientSet := fake.NewSimpleClientset(&v1.Service{
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

		_, err := updateConfigurationForServices(clientSet, config)
		if err != nil {
			t.Errorf("Error getting services %v ", err)
		}
	})

	t.Run("ensure that an updated agentConfig with services if running without defined serviceport is returned",
		func(t *testing.T) {
			selfLink := "http://localhost"
			clientSet := fake.NewSimpleClientset(&v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "heapster",
					Namespace: v1.NamespaceDefault,
					Labels: map[string]string{
						"tag": "",
					},
					SelfLink: selfLink,
				},
			})

			_, err := updateConfigurationForServices(clientSet, config)
			if err != nil {
				t.Errorf("Error getting services %v ", err)
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
		if config.HeapsterURL != string(config.HeapsterProxyURL.Host+config.HeapsterProxyURL.Path) {
			t.Error(" HeapsterURL is not set to proxyPath ")
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

func TestCollectMetrics(t *testing.T) {

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResp, _ := json.Marshal(map[string]string{"test": "data", "time": time.Now().String()})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(jsonResp)

	}))
	defer ts.Close()

	cs := fake.NewSimpleClientset(
		&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node0", Namespace: v1.NamespaceDefault}},
		&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1", Namespace: v1.NamespaceDefault}},
		&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2", Namespace: v1.NamespaceDefault}},
	)

	sv, _ := cs.Discovery().ServerVersion()
	dir, _ := ioutil.TempDir("", "TestCollectMetrics")
	tDir, _ := os.Open(dir)

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
	}

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

		nodeBaselineFiles := []string{}
		nodeSummaryFiles := []string{}
		expectedBaselineFiles := []string{"node-baseline-node0.json", "node-baseline-node1.json", "node-baseline-node2.json"}
		expectedSummaryFiles := []string{"node-summary-node0.json", "node-summary-node1.json", "node-summary-node2.json"}

		filepath.Walk(ka.msExportDirectory.Name(), func(path string, info os.FileInfo, err error) error {

			if strings.HasPrefix(info.Name(), "node") {

				if strings.Contains(info.Name(), "baseline") {
					nodeBaselineFiles = append(nodeBaselineFiles, info.Name())
				}
				if strings.Contains(info.Name(), "summary") {
					nodeSummaryFiles = append(nodeSummaryFiles, info.Name())
				}
			}
			return nil
		})
		if len(nodeBaselineFiles) != len(expectedBaselineFiles) {
			t.Errorf("Expected %d baseline node metrics, instead got %d", len(expectedSummaryFiles), len(nodeBaselineFiles))
			return
		}
		if len(nodeSummaryFiles) != len(expectedSummaryFiles) {
			t.Errorf("Expected %d baseline node metrics, instead got %d", len(expectedSummaryFiles), len(nodeBaselineFiles))
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
