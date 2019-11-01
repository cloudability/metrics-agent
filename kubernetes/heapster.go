package kubernetes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/cloudability/metrics-agent/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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
		}
	}

	// prefer accessing via service if present
	// nolint dupl
	for _, service := range services.Items {
		if service.Name == "heapster" && service.Namespace == "cloudability" {
			URL.Host = "http://heapster.cloudability:8082"
			URL.Path = "/api/v1/metric-export"
			return URL, nil
		} else if service.Name == "heapster" {
			URL.Host = clusterHostURL
			if len(service.Spec.Ports) > 0 {
				URL.Path = service.SelfLink + ":" + strconv.Itoa(
					int(service.Spec.Ports[0].Port)) + "/proxy/api/v1/metric-export"
			} else {
				URL.Path = service.SelfLink + "/proxy/api/v1/metric-export"
			}
		}
	}

	return URL, err

}

func validateHeapster(config KubeAgentConfig, client rest.HTTPClient) error {
	outerTest, body, err := util.TestHTTPConnection(
		client, config.HeapsterURL, http.MethodGet, config.BearerToken, retryCount, true)
	if err != nil {
		return err
	}
	if !outerTest {
		return fmt.Errorf("no heapster found")
	}
	var me heapsterMetricExport
	if err := json.Unmarshal(*body, &me); err != nil {
		return fmt.Errorf("malformed response from heapster running at: %v", config.HeapsterURL)
	}
	if len(me) < 10 {
		return fmt.Errorf("received empty or malformed response from heapster running at: %v",
			config.HeapsterURL)
	}
	log.Debugf("Connected to heapster at: %v", config.HeapsterURL)
	return err
}

func handleBaselineHeapsterMetrics(msExportDirectory, msd, baselineMetricSample, heapsterMetricExport string) error {
	// copy into the current sample directory the most recent baseline metric export
	err := util.CopyFileContents(msd+"/"+filepath.Base(baselineMetricSample), baselineMetricSample)
	if err != nil {
		log.Warn("Warning previous baseline not found or incomplete")
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
