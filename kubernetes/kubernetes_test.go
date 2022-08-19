package kubernetes

import (
	"context"
	"encoding/json"
	"k8s.io/client-go/tools/cache"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	fcache "k8s.io/client-go/tools/cache/testing"

	"github.com/cloudability/metrics-agent/retrieval/raw"
	v1apps "k8s.io/api/apps/v1"
	v1batch "k8s.io/api/batch/v1"
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

		_, err := updateConfigurationForServices(context.TODO(), config)
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

			_, err := updateConfigurationForServices(context.TODO(), config)
			if err != nil {
				t.Errorf("Error getting services %v ", err)
			}

		})

}

func TestEnsureMetricServicesAvailable(t *testing.T) {
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
			Clientset:         cs,
			NodeMetrics:       EndpointMask{},
			ConcurrentPollers: 10,
		}
		config, err := ensureMetricServicesAvailable(context.TODO(), config)
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
			Clientset:         cs,
			ClusterHostURL:    ts.URL,
			HeapsterURL:       ts.URL,
			HTTPClient:        client,
			ConcurrentPollers: 10,
			InClusterClient:   raw.NewClient(client, true, "", "", 0, false),
		}

		var err error
		_, err = ensureMetricServicesAvailable(context.TODO(), config)
		if err != nil {
			t.Errorf("Unexpected error fetching node summaries: %s", err)
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

// nolint gocyclo
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
	dir, err := os.MkdirTemp("", "TestCollectMetrics")
	if err != nil {
		t.Errorf("error creating temp dir: %v", err)
	}
	tDir, err := os.Open(dir)
	if err != nil {
		t.Errorf("Error opening temp dir: %v", err)
	}

	mockInformers := getMockInformers()

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
		BearerTokenPath:       "",
		ForceKubeProxy:        false,
		Informers:             mockInformers,
		ConcurrentPollers:     10,
	}
	ka.NodeMetrics = EndpointMask{}
	// set Proxy method available
	ka.NodeMetrics.SetAvailability(NodeStatsSummaryEndpoint, Proxy, true)
	// set Direct as option as well
	ka.NodeMetrics.SetAvailability(NodeStatsSummaryEndpoint, Direct, true)
	wd, err := os.Getwd()
	if err != nil {
		t.Error(err)
	}
	ka.BearerTokenPath = wd + "/testdata/mockToken"

	ka.InClusterClient = raw.NewClient(ka.HTTPClient, ka.Insecure, ka.BearerToken, ka.BearerTokenPath, 0)
	fns := NewClientsetNodeSource(cs)

	t.Run("Ensure that a collection occurs", func(t *testing.T) {
		// download the initial baseline...like a typical CollectKubeMetrics would
		err := downloadBaselineMetricExport(context.TODO(), ka, fns)
		if err != nil {
			t.Error(err)
		}
		err = ka.collectMetrics(context.TODO(), ka, cs, fns)
		if err != nil {
			t.Error(err)
		}

		nodeBaselineFiles := []string{}
		nodeSummaryFiles := []string{}
		expectedBaselineFiles := []string{
			"baseline-summary-node0.json",
			"baseline-summary-node1.json",
			"baseline-summary-node2.json",
		}
		expectedSummaryFiles := []string{
			"stats-summary-node0.json",
			"stats-summary-node1.json",
			"stats-summary-node2.json",
		}

		filepath.Walk(ka.msExportDirectory.Name(), func(path string, info os.FileInfo, err error) error {

			if strings.HasPrefix(info.Name(), "stats-") || strings.HasPrefix(info.Name(), "baseline-") {

				if isRequiredFile(info.Name(), "baseline-") {
					nodeBaselineFiles = append(nodeBaselineFiles, info.Name())
				}
				if isRequiredFile(info.Name(), "stats-") {
					nodeSummaryFiles = append(nodeSummaryFiles, info.Name())
				}
			}
			return nil
		})
		if len(nodeBaselineFiles) != len(expectedBaselineFiles) {
			t.Errorf("Expected %d baseline metrics, instead got %d", len(expectedBaselineFiles), len(nodeBaselineFiles))
			return
		}
		if len(nodeSummaryFiles) != len(expectedSummaryFiles) {
			t.Errorf("Expected %d summary metrics, instead got %d", len(expectedSummaryFiles), len(nodeSummaryFiles))
			return
		}
		for i, n := range expectedBaselineFiles {
			if n != nodeBaselineFiles[i] {
				t.Errorf("Expected file name %s instead got %s", n, nodeBaselineFiles[i])
			}
		}
		for i, n := range expectedSummaryFiles {
			if n != nodeSummaryFiles[i] {
				t.Errorf("Expected file name %s instead got %s", n, nodeSummaryFiles[i])
			}
		}
	})
	t.Run("Ensure collection occurs with parseMetrics enabled"+
		" ensure sensitive data is stripped", func(t *testing.T) {
		// download the initial baseline...like a typical CollectKubeMetrics would
		err := downloadBaselineMetricExport(context.TODO(), ka, fns)
		if err != nil {
			t.Error(err)
		}
		err = ka.collectMetrics(context.TODO(), ka, cs, fns)
		if err != nil {
			t.Error(err)
		}

		// i think delete the original test and walk through looking for k8s files and check before and after for removal.
		filepath.Walk(ka.msExportDirectory.Name(), func(path string, info os.FileInfo, err error) error {
			// if suffix is jsonl check if the data has been parsed
			if strings.HasSuffix(info.Name(), "jsonl") {
				if strings.Contains(info.Name(), "pods") {
					// check if secrets were not stripped from pods if parseMetrics is false
					in, _ := os.ReadFile(path)
					if !strings.Contains(string(in), "superSecret") {
						t.Error("Source file should have contained secret, but did not")
					}
				}
			}
			return nil
		})
	})

}

// isRequiredFile checks if the filename matches one of the filenames
// we require to be in a metrics payload
// ex: baseline-summary
func isRequiredFile(filename string, fileType string) bool {
	if strings.Contains(filename, fileType+"summary") {
		return true
	}
	if strings.Contains(filename, fileType+"container-") {
		return true
	}
	if strings.Contains(filename, fileType+"cadvisor_metrics-") {
		return true
	}
	return false
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

func getMockInformers() map[string]*cache.SharedIndexInformer {
	replicationControllers := fcache.NewFakeControllerSource()
	replicationControllers.Add(&v1.ReplicationController{ObjectMeta: metav1.ObjectMeta{Name: "rc1"}})
	rcinformer := cache.NewSharedInformer(replicationControllers,
		&v1.ReplicationController{}, 1*time.Second).(cache.SharedIndexInformer)
	services := fcache.NewFakeControllerSource()
	services.Add(&v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "s1"}})
	sinformer := cache.NewSharedInformer(services, &v1.Service{}, 1*time.Second).(cache.SharedIndexInformer)
	nodes := fcache.NewFakeControllerSource()
	nodes.Add(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}})
	ninformer := cache.NewSharedInformer(nodes, &v1.Node{}, 1*time.Second).(cache.SharedIndexInformer)
	pods := fcache.NewFakeControllerSource()
	pods.Add(&v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1"}})
	pinformer := cache.NewSharedInformer(pods, &v1.Pod{}, 1*time.Second).(cache.SharedIndexInformer)
	persistentVolumes := fcache.NewFakeControllerSource()
	persistentVolumes.Add(&v1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv1"}})
	pvinformer := cache.NewSharedInformer(persistentVolumes,
		&v1.PersistentVolume{}, 1*time.Second).(cache.SharedIndexInformer)
	persistentVolumeClaims := fcache.NewFakeControllerSource()
	persistentVolumeClaims.Add(&v1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "pvc1"}})
	pvcinformer := cache.NewSharedInformer(persistentVolumeClaims,
		&v1.PersistentVolumeClaim{}, 1*time.Second).(cache.SharedIndexInformer)
	replicaSets := fcache.NewFakeControllerSource()
	replicaSets.Add(&v1apps.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "rs1"}})
	rsinformer := cache.NewSharedInformer(replicaSets, &v1apps.ReplicaSet{},
		1*time.Second).(cache.SharedIndexInformer)
	daemonSets := fcache.NewFakeControllerSource()
	daemonSets.Add(&v1apps.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "ds1"}})
	dsinformer := cache.NewSharedInformer(daemonSets,
		&v1apps.DaemonSet{}, 1*time.Second).(cache.SharedIndexInformer)
	deployments := fcache.NewFakeControllerSource()
	deployments.Add(&v1apps.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "d1"}})
	dinformer := cache.NewSharedInformer(deployments, &v1apps.Deployment{}, 1*time.Second).(cache.SharedIndexInformer)
	namespaces := fcache.NewFakeControllerSource()
	namespaces.Add(&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}})
	nainformer := cache.NewSharedInformer(namespaces, &v1.Namespace{}, 1*time.Second).(cache.SharedIndexInformer)
	jobs := fcache.NewFakeControllerSource()
	jobs.Add(&v1batch.Job{ObjectMeta: metav1.ObjectMeta{Name: "job1"}})
	jinformer := cache.NewSharedInformer(jobs, &v1batch.Job{}, 1*time.Second).(cache.SharedIndexInformer)
	cronJobs := fcache.NewFakeControllerSource()
	cronJobs.Add(&v1batch.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "cj1"}})
	cjinformer := cache.NewSharedInformer(cronJobs, &v1batch.CronJob{}, 1*time.Second).(cache.SharedIndexInformer)

	mockInformers := map[string]*cache.SharedIndexInformer{
		"replicationcontrollers": &rcinformer,
		"services":               &sinformer,
		"nodes":                  &ninformer,
		"pods":                   &pinformer,
		"persistentvolumes":      &pvinformer,
		"persistentvolumeclaims": &pvcinformer,
		"replicasets":            &rsinformer,
		"daemonsets":             &dsinformer,
		"deployments":            &dinformer,
		"namespaces":             &nainformer,
		"jobs":                   &jinformer,
		"cronjobs":               &cjinformer,
	}
	return mockInformers
}
