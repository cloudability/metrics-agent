package k8s

import (
	"bufio"
	"encoding/json"
	"errors"
	"github.com/cloudability/metrics-agent/retrieval/raw"
	log "github.com/sirupsen/logrus"
	v1apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"net/http"
	"os"
	"time"
)

// GetK8sMetrics returns cloudabilty measurements retrieved from a given K8S Clientset
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
	ReplicationController  *cache.SharedIndexInformer
	Services               *cache.SharedIndexInformer
	Nodes                  *cache.SharedIndexInformer
	Pods                   *cache.SharedIndexInformer
	PersistentVolumes      *cache.SharedIndexInformer
	PersistentVolumeClaims *cache.SharedIndexInformer
	Replicasets            *cache.SharedIndexInformer
	Daemonsets             *cache.SharedIndexInformer
	Deployments            *cache.SharedIndexInformer
}

func StartUpInformers(clientset kubernetes.Interface) (ClusterInformers, error) {
	factory := informers.NewSharedInformerFactory(clientset, 3*time.Minute)

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
		ReplicationController:  &replicationControllerInformer,
		Services:               &servicesInformer,
		Nodes:                  &nodesInformer,
		Pods:                   &podsInformer,
		PersistentVolumes:      &persistentVolumesInformer,
		PersistentVolumeClaims: &persistentVolumeClaimsInformer,
		Replicasets:            &replicasetsInformer,
		Daemonsets:             &daemonsetsInformer,
		Deployments:            &deploymentsInformer,
	}
	return clusterInformers, nil
}

// nolint
func GetK8sMetricsFromInformer(informers ClusterInformers, workDir *os.File) error {
	//TODO return nil here so that make test will pass, w/o this we get nil dereference error from Ensure that a coll..
	if informers.Pods == nil {
		log.Infof("ReplicationController is nil this should only happen in unit testing")
		return nil
	}

	replicationControllers := (*informers.ReplicationController).GetIndexer().List()
	err := writeK8sResourceFile(workDir, "replicationcontrollers", replicationControllers)
	if err != nil {
		return err
	}
	services := (*informers.Services).GetIndexer().List()
	err = writeK8sResourceFile(workDir, "services", services)
	if err != nil {
		return err
	}
	nodes := (*informers.Nodes).GetIndexer().List()
	err = writeK8sResourceFile(workDir, "nodes", nodes)
	if err != nil {
		return err
	}
	pods := (*informers.Pods).GetIndexer().List()
	err = writeK8sResourceFile(workDir, "pods", pods)
	if err != nil {
		return err
	}
	persistentVolumes := (*informers.PersistentVolumes).GetIndexer().List()
	err = writeK8sResourceFile(workDir, "persistentvolumes", persistentVolumes)
	if err != nil {
		return err
	}
	persistentVolumeClaims := (*informers.PersistentVolumeClaims).GetIndexer().List()
	err = writeK8sResourceFile(workDir, "persistentvolumeclaims", persistentVolumeClaims)
	if err != nil {
		return err
	}
	replicaSets := (*informers.Replicasets).GetIndexer().List()
	err = writeK8sResourceFile(workDir, "replicasets", replicaSets)
	if err != nil {
		return err
	}
	daemonSets := (*informers.Daemonsets).GetIndexer().List()
	err = writeK8sResourceFile(workDir, "daemonsets", daemonSets)
	if err != nil {
		return err
	}
	deployments := (*informers.Deployments).GetIndexer().List()
	err = writeK8sResourceFile(workDir, "deployments", deployments)
	if err != nil {
		return err
	}
	return nil
}

//writeK8sResourceFile creates a new file in the upload sample directory for the resource name passed in
//nolint gocyclo
func writeK8sResourceFile(workDir *os.File, resourceName string, resourceList []interface{}) (rerr error) {

	file, err := os.OpenFile(workDir.Name()+"/"+resourceName+".jsonl",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.New("error: unable to create kubernetes metric file")
	}
	datawriter := bufio.NewWriter(file)

	switch resourceName {
	default:
		return errors.New("error: resource " + resourceName + " not supported")
	case "replicationcontrollers":
		for _, object := range resourceList {
			rc := object.(*v1.ReplicationController)
			data, err := json.Marshal(rc)
			if err != nil {
				return errors.New("error: unable to create marshal resource")
			}
			_, err = datawriter.WriteString(string(data) + "\n")
		}
	case "services":
		for _, object := range resourceList {
			s := object.(*v1.Service)
			data, err := json.Marshal(s)
			if err != nil {
				return errors.New("error: unable to create marshal resource")
			}
			datawriter.WriteString(string(data) + "\n")
		}
	case "nodes":
		for _, object := range resourceList {
			n := object.(*v1.Node)
			data, err := json.Marshal(n)
			if err != nil {
				return errors.New("error: unable to create marshal resource")
			}
			datawriter.WriteString(string(data) + "\n")
		}
	case "pods":
		for _, object := range resourceList {
			p := object.(*v1.Pod)
			data, err := json.Marshal(p)
			if err != nil {
				return errors.New("error: unable to create marshal resource")
			}
			datawriter.WriteString(string(data) + "\n")
		}
	case "persistentvolumes":
		for _, object := range resourceList {
			pv := object.(*v1.PersistentVolume)
			data, err := json.Marshal(pv)
			if err != nil {
				return errors.New("error: unable to create marshal resource")
			}
			datawriter.WriteString(string(data) + "\n")
		}
	case "persistentvolumeclaims":
		for _, object := range resourceList {
			pvc := object.(*v1.PersistentVolumeClaim)
			data, err := json.Marshal(pvc)
			if err != nil {
				return errors.New("error: unable to create marshal resource")
			}
			datawriter.WriteString(string(data) + "\n")
		}
	case "replicasets":
		for _, object := range resourceList {
			rs := object.(*v1apps.ReplicaSet)
			data, err := json.Marshal(rs)
			if err != nil {
				return errors.New("error: unable to create marshal resource")
			}
			datawriter.WriteString(string(data) + "\n")
		}
	case "daemonsets":
		for _, object := range resourceList {
			ds := object.(*v1apps.DaemonSet)
			data, err := json.Marshal(ds)
			if err != nil {
				return errors.New("error: unable to create marshal resource")
			}
			datawriter.WriteString(string(data) + "\n")
		}
	case "deployments":
		for _, object := range resourceList {
			d := object.(*v1apps.Deployment)
			data, err := json.Marshal(d)
			if err != nil {
				return errors.New("error: unable to create marshal resource")
			}
			datawriter.WriteString(string(data) + "\n")
		}
	}

	datawriter.Flush()
	file.Close()

	return err
}
