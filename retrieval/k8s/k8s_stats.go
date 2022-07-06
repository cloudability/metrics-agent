package k8s

import (
	"github.com/cloudability/metrics-agent/retrieval/raw"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"net/http"
	"os"
	"time"
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

type ClusterInformers struct {
	ReplicationController  cache.SharedIndexInformer
	Services               cache.SharedIndexInformer
	Nodes                  cache.SharedIndexInformer
	Pods                   cache.SharedIndexInformer
	PersistentVolumes      cache.SharedIndexInformer
	PersistentVolumeClaims cache.SharedIndexInformer
	Replicasets            cache.SharedIndexInformer
	Daemonsets             cache.SharedIndexInformer
	Deployments            cache.SharedIndexInformer
}

func StartUpInformers(clientset kubernetes.Interface) (ClusterInformers, error) {
	factory := informers.NewSharedInformerFactory(clientset, 10*time.Minute)

	// v1Sources
	replicationControllerInformer := factory.Core().V1().ReplicationControllers().Informer()
	servicesInformer := factory.Core().V1().Services().Informer()
	nodesInformer := factory.Core().V1().Nodes().Informer()
	podsInformer := factory.Core().V1().Pods().Informer()
	persistentVolumesInformer := factory.Core().V1().PersistentVolumes().Informer()
	persistentVolumeClaimsInformer := factory.Core().V1().PersistentVolumeClaims().Informer()
	// AppSources
	replicasetsInformer := factory.Apps().V1().ReplicaSets().Informer()
	daemonsetsInformer := factory.Apps().V1().DaemonSets().Informer()
	deploymentsInformer := factory.Apps().V1().Deployments().Informer()

	// closing this will kill all informers
	stopCh := make(chan struct{})
	// runs in background, starts all informers that are a part of the factory
	factory.Start(stopCh)
	// wait until all informers have successfully synced
	factory.WaitForCacheSync(stopCh)

	clusterInformers := ClusterInformers{
		ReplicationController:  replicationControllerInformer,
		Services:               servicesInformer,
		Nodes:                  nodesInformer,
		Pods:                   podsInformer,
		PersistentVolumes:      persistentVolumesInformer,
		PersistentVolumeClaims: persistentVolumeClaimsInformer,
		Replicasets:            replicasetsInformer,
		Daemonsets:             daemonsetsInformer,
		Deployments:            deploymentsInformer,
	}

	return clusterInformers, nil
}

func GetK8sMetricsFromInformer(clusterHostURL string, clusterVersion float64, workDir *os.File) error {

	return nil
}
