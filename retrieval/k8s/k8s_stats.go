package k8s

import (
	"net/http"
	"os"

	"github.com/cloudability/metrics-agent/v2/retrieval/raw"
	log "github.com/sirupsen/logrus"
)

//GetK8sMetrics returns cloudabilty measurements retrieved from a given K8S Clientset
func GetK8sMetrics(clusterHostURL string, clusterVersion float64, workDir *os.File, rawClient raw.Client) (err error) {

	v1Sources, v1beta1Sources, v1AppSources := getk8sSourcePaths(clusterVersion)

	// get v1 sources
	for _, v1s := range v1Sources {
		_, err := rawClient.GetRawEndPoint(http.MethodGet, v1s, workDir, clusterHostURL+"/api/v1/"+v1s, nil, true)
		if err != nil {
			log.Errorf("Error retrieving "+v1s+" metric endpoint: %s", err)
			return err
		}
	}

	// get v1beta1 sources
	for _, v1b1s := range v1beta1Sources {
		_, err := rawClient.GetRawEndPoint(
			http.MethodGet, v1b1s, workDir, clusterHostURL+"/apis/extensions/v1beta1/"+v1b1s, nil, true)
		if err != nil {
			log.Errorf("Error retrieving "+v1b1s+" metric endpoint: %s", err)
			return err
		}
	}

	// get v1 App sources
	for _, v1Apps := range v1AppSources {
		_, err := rawClient.GetRawEndPoint(http.MethodGet, v1Apps, workDir, clusterHostURL+"/apis/apps/v1/"+v1Apps, nil, true)
		if err != nil {
			log.Errorf("Error retrieving "+v1Apps+" metric endpoint: %s", err)
			return err
		}
	}

	// get jobs
	_, err = rawClient.GetRawEndPoint(http.MethodGet, "jobs", workDir, clusterHostURL+"/apis/batch/v1/jobs", nil, true)
	if err != nil {
		log.Errorf("Error retrieving jobs metric endpoint: %s", err)
		return err
	}

	return err
}

func getk8sSourcePaths(clusterVersion float64) (v1Sources []string, v1beta1Sources []string, v1AppSources []string) {
	commonSrcs := []string{
		"replicasets",
		"daemonsets",
		"deployments",
	}
	v1Sources = []string{
		"namespaces",
		"replicationcontrollers",
		"services",
		"nodes",
		"pods",
		"persistentvolumes",
		"persistentvolumeclaims",
	}

	v1beta1Sources = []string{}
	v1AppSources = []string{}

	// common sources [deployments, replicasets, daemonsets] moved from beta to apps v1.16 onward
	if clusterVersion < 1.16 {
		v1beta1Sources = append(v1beta1Sources, commonSrcs...)
	} else {
		v1AppSources = append(v1AppSources, commonSrcs...)
	}

	return v1Sources, v1beta1Sources, v1AppSources

}
