package kubernetes

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/pkg/api/v1"
)

func TestGetHeapsterURL(t *testing.T) {
	cs := fake.NewSimpleClientset()
	// nolint dupl
	t.Run("Ensure that heapster pod is found in the kube-system namespace", func(t *testing.T) {

		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "heapster",
				Namespace: "kube-system",
				SelfLink:  "/api/v1/namespaces/kube-system/pods/heapster-6d9d49d496-5scrb",
			},
		}
		_, _ = cs.CoreV1().Pods("kube-system").Create(pod)

		clusterHostURL := "http://locahost"

		url, err := getHeapsterURL(cs, clusterHostURL)
		if err != nil {
			t.Error(err)
		}
		if url.Host != "http://localhost" &&
			url.Path != "/api/v1/namespaces/kube-system/pods/heapster-6d9d49d496-5scrb:8082/proxy/api/v1/metric-export" {
			t.Errorf("Error getting heapster pod url: %v", err)
		}
	})
	// nolint dupl
	t.Run("Ensure that heapster service is found in the kube-system namespace", func(t *testing.T) {

		service := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "heapster",
				Namespace: "kube-system",
				SelfLink:  "/api/v1/namespaces/kube-system/services/heapster",
			},
		}
		_, _ = cs.CoreV1().Services("kube-system").Create(service)

		clusterHostURL := "http://locahost"

		url, err := getHeapsterURL(cs, clusterHostURL)
		if err != nil {
			t.Error(err)
		}
		if url.Host != "http://localhost" &&
			url.Path != "/api/v1/namespaces/kube-system/services/heapster/proxy/api/v1/metric-export" {
			t.Errorf("Error getting heapster service url: %v", err)
		}
	})
}
func TestEnsureValidHeapster(t *testing.T) {

	t.Run("Ensure that a valid heapster service is found and responds with data", func(t *testing.T) {

		testData := "../util/testdata/test-cluster-metrics-sample/sample-1510159016/heapster-metrics-export.json"
		body, _ := ioutil.ReadFile(testData)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)

		}))
		defer ts.Close()

		cs := fake.NewSimpleClientset()

		kac := KubeAgentConfig{
			HTTPClient:         http.Client{},
			UseInClusterConfig: true,
			ClusterHostURL:     ts.URL,
			Clientset:          cs,
			HeapsterURL:        ts.URL,
			Insecure:           true,
			BearerToken:        "",
		}

		_, err := ensureValidHeapster(kac)

		if err != nil {
			t.Error(err)
		}
	})

}

func TestLauchHeapster(t *testing.T) {

	t.Parallel()

	cs := fake.NewSimpleClientset()

	t.Run("Ensure that heapster is launched locally", func(t *testing.T) {
		heapsterURL, err := launchHeapster(cs, false, "cloudability")

		if err != nil {
			t.Error(err)
		}
		if heapsterURL != "http://localhost:0/api/v1/metric-export" {
			t.Errorf("Error launching heapster without incluster config: %v", err)
		}
	})

	t.Run("Ensure that heapster is launched locally (incluster config)", func(t *testing.T) {
		heapsterURL, err := launchHeapster(cs, true, "cloudability")

		if err != nil {
			t.Error(err)
		}
		if heapsterURL != "http://heapster.cloudability:8082/api/v1/metric-export" {
			t.Errorf("Error launching heapster with incluster config: %v", err)
		}
	})

	t.Run("Ensure that heapster is launched in the kube-system namespace (incluster config)", func(t *testing.T) {
		heapsterURL, err := launchHeapster(cs, true, "kube-system")

		if err != nil {
			t.Error(err)
		}
		if heapsterURL != "http://heapster.kube-system:8082/api/v1/metric-export" {
			t.Errorf("Error launching heapster with incluster config: %v", err)
		}
	})

}
