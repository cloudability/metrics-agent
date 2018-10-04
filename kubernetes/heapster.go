package kubernetes

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cloudability/metrics-agent/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	apiv1 "k8s.io/client-go/pkg/api/v1"
	appsv1beta1 "k8s.io/client-go/pkg/apis/apps/v1beta1"
	"k8s.io/client-go/rest"
)

type heapsterMetricExport []struct {
	Metrics struct {
		CPUUsage []struct {
			Start time.Time `json:"start,omitempty"`
			End   time.Time `json:"end,omitempty"`
			Value int       `json:"value,omitempty"`
		} `json:"cpu/usage,omitempty"`
		MemoryCache []struct {
			Start time.Time `json:"start,omitempty"`
			End   time.Time `json:"end,omitempty"`
			Value int       `json:"value,omitempty"`
		} `json:"memory/cache,omitempty"`
		MemoryMajorPageFaults []struct {
			Start time.Time `json:"start,omitempty"`
			End   time.Time `json:"end,omitempty"`
			Value int       `json:"value,omitempty"`
		} `json:"memory/major_page_faults,omitempty"`
		MemoryPageFaults []struct {
			Start time.Time `json:"start,omitempty"`
			End   time.Time `json:"end,omitempty"`
			Value int       `json:"value,omitempty"`
		} `json:"memory/page_faults,omitempty"`
		MemoryRss []struct {
			Start time.Time `json:"start,omitempty"`
			End   time.Time `json:"end,omitempty"`
			Value int       `json:"value,omitempty"`
		} `json:"memory/rss,omitempty"`
		MemoryUsage []struct {
			Start time.Time `json:"start,omitempty"`
			End   time.Time `json:"end,omitempty"`
			Value int       `json:"value,omitempty"`
		} `json:"memory/usage,omitempty"`
		MemoryWorkingSet []struct {
			Start time.Time `json:"start,omitempty"`
			End   time.Time `json:"end,omitempty"`
			Value int       `json:"value,omitempty"`
		} `json:"memory/working_set,omitempty"`
		Uptime []struct {
			Start time.Time `json:"start,omitempty"`
			End   time.Time `json:"end,omitempty"`
			Value int       `json:"value,omitempty"`
		} `json:"uptime,omitempty"`
	} `json:"metrics,omitempty"`
	Labels struct {
		ContainerName string `json:"container_name,omitempty"`
		HostID        string `json:"host_id,omitempty"`
		Hostname      string `json:"hostname,omitempty"`
		Nodename      string `json:"nodename,omitempty"`
	} `json:"labels,omitempty"`
}

// returns the proxy url of heapster in the cluster (returns last found based on match)
func getHeapsterURL(clientset kubernetes.Interface, clusterHostURL string) (URL url.URL, err error) {
	pods, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{})

	if err != nil {
		log.Fatalf("cloudability metric agent is unable to get a list of pods: %v", err)
	}

	services, err := clientset.CoreV1().Services("").List(metav1.ListOptions{})
	if err != nil {
		log.Fatalf("cloudability metric agent is unable to get a list of services: %v", err)
	}

	for _, pod := range pods.Items {
		if strings.Contains(pod.Name, "heapster") {
			URL.Host = clusterHostURL
			URL.Path = pod.SelfLink + ":8082/proxy/api/v1/metric-export"
			log.Printf("Found Heapster at:  %v%v", URL.Host, URL.Path)
		}
	}

	// prefer accessing via service if present
	// nolint dupl
	for _, service := range services.Items {
		if service.Name == "heapster" && service.Namespace == "cloudability" {
			URL.Host = "http://heapster.cloudability:8082"
			URL.Path = "/api/v1/metric-export"
			log.Printf("Found Heapster service running in the cloudability namespace at:  %v%v", URL.Host, URL.Path)
			return URL, nil
		} else if service.Name == "heapster" {
			URL.Host = clusterHostURL
			if len(service.Spec.Ports) > 0 {
				URL.Path = service.SelfLink + ":" + strconv.Itoa(
					int(service.Spec.Ports[0].Port)) + "/proxy/api/v1/metric-export"
			} else {
				URL.Path = service.SelfLink + "/proxy/api/v1/metric-export"
			}
			log.Printf("Found Heapster at:  %v%v", URL.Host, URL.Path)
		}
	}

	return URL, err

}

//ensure Heapster is accessible or launch a copy
func ensureValidHeapster(config KubeAgentConfig) (KubeAgentConfig, error) {

	client := &config.HTTPClient
	var err error

	// if Heapster is found in the cluster, test to make sure we can connect to it
	// or launch an instance in the cloudability namespace. Make sure and close the returned response body.
	if config.HeapsterURL != "" {
		return validateHeapster(config, client, config.Namespace)
	}
	log.Printf("Could not find heapster running in the cluster, launching a copy in the %s namespace.", config.Namespace)

	config.HeapsterURL, err = launchHeapster(config.Clientset, config.UseInClusterConfig, config.Namespace)

	if err != nil {
		return config, fmt.Errorf("Error launching heapster in the %s namespace: %+v", err, config.Namespace)
	}
	innerTest, _, err := util.TestHTTPConnection(
		client, config.HeapsterURL, http.MethodGet, config.BearerToken, retryCount, true)
	if innerTest || err == nil {
		log.Printf("Connected to heapster at: %v", config.HeapsterURL)
	}

	return config, err
}

func validateHeapster(config KubeAgentConfig, client rest.HTTPClient, namespace string) (KubeAgentConfig, error) {
	outerTest, body, err := util.TestHTTPConnection(
		client, config.HeapsterURL, http.MethodGet, config.BearerToken, retryCount, true)

	var me heapsterMetricExport

	if err := json.Unmarshal(*body, &me); err != nil {
		log.Printf("malformed response from heapster running at: %v", config.HeapsterURL)
	}

	if !outerTest {
		log.Printf("Could not connect to heapster at: %v, launching a copy in the Cloudability namespace.",
			config.HeapsterURL)

		config.HeapsterURL, err = launchHeapster(config.Clientset, config.UseInClusterConfig, namespace)

		if err != nil {
			return config, fmt.Errorf("Error launching heapster in the %s namespace: %+v", err, namespace)
		}
		innerTest, _, err := util.TestHTTPConnection(
			client, config.HeapsterURL, http.MethodGet, config.BearerToken, retryCount, true)
		if innerTest {
			log.Printf("Connected to heapster at: %v", config.HeapsterURL)
		}

		return config, err

	} else if len(me) < 10 || err != nil {
		log.Printf("Received empty or malformed response from heapster running at: %v launching a copy in the %s namespace.",
			config.HeapsterURL, namespace)
		config.HeapsterURL, err = launchHeapster(config.Clientset, config.UseInClusterConfig, namespace)
		if err != nil {
			return config, fmt.Errorf("Error launching heapster in the Cloudability namespace: %+v", err)
		}
		innerTest, _, err := util.TestHTTPConnection(
			client, config.HeapsterURL, http.MethodGet, config.BearerToken, retryCount, true)
		if innerTest || err == nil {
			log.Printf("Connected to heapster at: %v", config.HeapsterURL)
		}
		return config, err
	}

	log.Printf("Connected to heapster at: %v", config.HeapsterURL)
	return config, err
}

func launchHeapster(clientset kubernetes.Interface, inClusterConfig bool,
	namespace string) (heapsterURL string, err error) {

	deploymentsClient := clientset.AppsV1beta1().Deployments(namespace)
	servicesClient := clientset.CoreV1().Services(namespace)
	deployment := getDeployment(namespace)
	service := getService(namespace)

	//Create Deployment / update if already present
	_, err = deploymentsClient.Get(deployment.Name, metav1.GetOptions{})
	if err != nil {
		dResult, err := deploymentsClient.Create(deployment)
		if err != nil {
			log.Printf("deployment create err: %+v", err)
			return "", err
		}
		log.Printf("Created deployment %q in the %s namespace.\n", dResult.GetObjectMeta().GetName(), namespace)
	} else {
		dResult, err := deploymentsClient.Update(deployment)
		if err != nil {
			log.Printf("deployment update err: %+v", err)
			return "", err
		}
		log.Printf("Updated deployment %q in the %s namespace.\n", dResult.GetObjectMeta().GetName(), namespace)
	}

	// Create Service / update if already present
	s, err := servicesClient.Get(service.Name, metav1.GetOptions{})
	if err != nil {
		sResult, err := servicesClient.Create(service)
		if err != nil {
			log.Printf("service create err: %+v", err)
			return "", err
		}
		log.Printf("Created service %q in the %s namespace.\n", sResult.GetObjectMeta().GetName(), namespace)

		if inClusterConfig {
			heapsterURL = buildHeapsterURL(namespace)
			return heapsterURL, err
		}

		heapsterURL = buildLocalHeapsterURL(strconv.Itoa(int(sResult.Spec.Ports[0].NodePort)))
		return heapsterURL, err
	}
	// make sure we set immutable fields before updating
	service.ObjectMeta.ResourceVersion = s.ObjectMeta.ResourceVersion
	service.Spec.Selector = s.Spec.Selector
	service.Spec.ClusterIP = s.Spec.ClusterIP

	sResult, err := servicesClient.Update(service)
	if err != nil {
		log.Printf("service update err: %+v", err)
		return "", err
	}
	log.Printf("Updated service %q in the cloudability namespace.\n", sResult.GetObjectMeta().GetName())

	if inClusterConfig {
		heapsterURL = buildHeapsterURL(namespace)
		return heapsterURL, err
	}

	heapsterURL = buildLocalHeapsterURL(strconv.Itoa(int(sResult.Spec.Ports[0].NodePort)))
	return heapsterURL, err
}

func buildHeapsterURL(namespace string) string {
	return "http://heapster." + namespace + ":8082/api/v1/metric-export"
}

func buildLocalHeapsterURL(port string) string {
	return "http://localhost:" + port + "/api/v1/metric-export"
}

func getDeployment(namespace string) *appsv1beta1.Deployment {
	replicaCount := int32(1)
	deployment := &appsv1beta1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "heapster",
			Namespace: namespace,
		},
		Spec: appsv1beta1.DeploymentSpec{
			Replicas: &replicaCount,
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     "heapster",
						"k8s-app": "heapster",
					},
				},
				Spec: apiv1.PodSpec{
					ServiceAccountName: namespace,
					Containers: []apiv1.Container{
						{
							Name:            "heapster",
							Image:           "k8s.gcr.io/heapster-amd64:v1.5.3",
							ImagePullPolicy: apiv1.PullAlways,
							Ports: []apiv1.ContainerPort{
								{
									Name:          "http",
									Protocol:      apiv1.ProtocolTCP,
									ContainerPort: int32(8082),
								},
							},
							Command: []string{
								"/heapster",
								"--source=kubernetes:https://kubernetes.default",
							},
						},
					},
				},
			},
		},
	}
	return deployment
}

func getService(namespace string) *apiv1.Service {
	service := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "heapster",
			Namespace: namespace,
		},
		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeNodePort,
			Ports: []apiv1.ServicePort{
				{
					Protocol: apiv1.ProtocolTCP,
					Port:     int32(8082),
					TargetPort: intstr.IntOrString{
						IntVal: 8082,
					},
				},
			},
			Selector: map[string]string{
				"app": "heapster",
			},
		},
	}
	return service
}

func handleBaselineHeapsterMetrics(msExportDirectory, msd, baselineMetricSample, heapsterMetricExport string) error {
	// copy into the current sample directory the most recent baseline metric export
	err := util.CopyFileContents(msd+"/"+filepath.Base(baselineMetricSample), baselineMetricSample)
	if err != nil {
		log.Println("Warning: Previous baseline not found or incomplete")
	}

	// remove the baseline metric if it is not json
	if baselineMetricSample != "" && filepath.Base(baselineMetricSample) != "baseline-metrics-export.json" {
		if err = os.Remove(baselineMetricSample); err != nil {
			return fmt.Errorf("error cleaning up invalid baseline metric export: %s", err)
		}
	}
	// update the baseline metric export with the most recent sample from this collection
	err = util.CopyFileContents(
		filepath.Dir(msExportDirectory)+"/"+"baseline-metrics-export"+filepath.Ext(
			heapsterMetricExport), heapsterMetricExport)
	if err != nil {
		return fmt.Errorf("error updating baseline metric export: %s", err)
	}

	return nil
}
