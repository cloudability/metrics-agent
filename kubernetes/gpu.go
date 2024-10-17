package kubernetes

import (
	"fmt"
	"github.com/cloudability/metrics-agent/util"
	log "github.com/sirupsen/logrus"
	"io"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type gpuClient struct {
	labelKey       string
	labelValue     string
	endpointLister v1.EndpointsLister
}

func newGPUClient(servicesInformer *cache.SharedIndexInformer, labelKey, labelValue string) *gpuClient {
	endpointLister := v1.NewEndpointsLister((*servicesInformer).GetIndexer())
	return &gpuClient{
		endpointLister: endpointLister,
		labelKey:       labelKey,
		labelValue:     labelValue,
	}
}

func (gc gpuClient) downloadSample(workDir *os.File, prefix string) (err error) {
	endpoints, err := gc.listEndpoints()
	if err != nil {
		return err
	}
	for i, endpoint := range endpoints {
		log.Infof("pulling data for endpoint %v at %v", i, endpoint)
		err = downloadToFile(workDir, prefix, endpoint)
		if err != nil {
			return err
		}
	}
	return nil
}

func downloadToFile(workDir *os.File, prefix string, endpoint string) (err error) {
	endpointIP := strings.Split(endpoint, ":")[0]
	fileName := fmt.Sprintf("%s-%s-gpu.txt", prefix, endpointIP)
	req, err := http.NewRequest(http.MethodGet, "http://"+endpoint+"/metrics", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer util.SafeClose(resp.Body.Close, &err)
	respFile, err := os.Create(workDir.Name() + "/" + fileName)
	if err != nil {
		return err
	}
	log.Infof("writing sample to %s", respFile.Name())
	defer util.SafeClose(respFile.Close, &err)

	_, err = io.Copy(respFile, resp.Body)
	return err
}

func (gc gpuClient) listEndpoints() ([]string, error) {
	// "app.kubernetes.io/name" / "dcgm-exporter"
	req, err := labels.NewRequirement(gc.labelKey, selection.In, []string{gc.labelValue})
	if err != nil {
		return []string{}, err
	}
	selector := labels.NewSelector().Add(*req)
	list, err := gc.endpointLister.List(selector)
	if err != nil {
		return []string{}, err
	}
	addresses := map[string]struct{}{}
	for _, endpoint := range list {
		for _, subset := range endpoint.Subsets {
			for _, address := range subset.Addresses {
				for _, port := range subset.Ports {
					addr := address.IP + ":" + strconv.Itoa(int(port.Port))
					addresses[addr] = struct{}{}
				}
			}
		}
	}
	addrs := []string{}
	for address := range addresses {
		addrs = append(addrs, address)
		log.Infof("address %v", address)
	}
	return addrs, nil
}
