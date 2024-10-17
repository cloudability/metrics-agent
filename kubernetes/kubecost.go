package kubernetes

import (
	"bytes"
	"encoding/json"
	"github.com/cloudability/metrics-agent/util"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"os"
	"strings"
)

const networkIngress = "kubecost_pod_network_ingress_bytes_total"
const networkEgress = "kubecost_pod_network_egress_bytes_total"
const gpuMetric = "DCGM_FI_DEV_GPU_UTIL"

type kcClient struct {
	url     string
	metrics []string
}

func newKubecostClient(url string, gpuEnabled, networkEnabled bool) *kcClient {
	metrics := []string{}
	if gpuEnabled {
		metrics = append(metrics, gpuMetric)
	}
	if networkEnabled {
		metrics = append(metrics, networkEgress)
		metrics = append(metrics, networkIngress)
	}
	return &kcClient{
		url:     url,
		metrics: metrics,
	}
}

func (kc kcClient) downloadSample(workDir *os.File, prefix string) (err error) {
	data, err := kc.getMetrics()
	if err != nil {
		return err
	}
	fileName := prefix + "-kubecost.json"
	respFile, err := os.Create(workDir.Name() + "/" + fileName)
	if err != nil {
		return err
	}
	log.Infof("writing sample to %s", respFile.Name())
	defer util.SafeClose(respFile.Close, &err)
	marshalled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = io.Copy(respFile, bytes.NewReader(marshalled))
	return err
}

func (kc kcClient) getMetrics() (_ promData, err error) {
	metrics := strings.Join(kc.metrics, "|")
	url := kc.url + "/api/v1/query?query={__name__=~\"" + metrics + "\"}"
	log.Infof("Downloading sample from %s", url)
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return promData{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return promData{}, err
	}
	defer util.SafeClose(response.Body.Close, &err)
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return promData{}, err
	}
	prom := promData{}
	err = json.Unmarshal(data, &prom)
	return prom, err
}

type promData struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric struct {
				Name       string `json:"__name__"`
				Instance   string `json:"instance"`
				Namespace  string `json:"namespace"`
				Pod        string `json:"pod"`
				PodName    string `json:"pod_name"`
				Container  string `json:"container"`
				Internet   string `json:"internet"`
				SameRegion string `json:"same_region"`
				SameZone   string `json:"same_zone"`
			} `json:"metric"`
			Value []interface{} `json:"value"`
		} `json:"result"`
	} `json:"data"`
}
