package k8s

import (
	"bufio"
	"encoding/json"
	"errors"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"os"
	"time"
)

func StartUpInformers(clientset kubernetes.Interface,
	resyncInterval int) (map[string]*cache.SharedIndexInformer, error) {
	factory := informers.NewSharedInformerFactory(clientset, time.Duration(resyncInterval)*time.Hour)

	// v1Sources
	replicationControllerInformer := factory.Core().V1().ReplicationControllers().Informer()
	servicesInformer := factory.Core().V1().Services().Informer()
	nodesInformer := factory.Core().V1().Nodes().Informer()
	podsInformer := factory.Core().V1().Pods().Informer()
	persistentVolumesInformer := factory.Core().V1().PersistentVolumes().Informer()
	persistentVolumeClaimsInformer := factory.Core().V1().PersistentVolumeClaims().Informer()
	namespacesInformer := factory.Core().V1().Namespaces().Informer()
	// AppSources
	replicasetsInformer := factory.Apps().V1().ReplicaSets().Informer()
	daemonsetsInformer := factory.Apps().V1().DaemonSets().Informer()
	deploymentsInformer := factory.Apps().V1().Deployments().Informer()
	// Jobs & Cronjobs
	jobsInformer := factory.Batch().V1().Jobs().Informer()
	cronJobsInformer := factory.Batch().V1().CronJobs().Informer()

	// closing this will kill all informers
	stopCh := make(chan struct{})
	// runs in background, starts all informers that are a part of the factory
	factory.Start(stopCh)
	// wait until all informers have successfully synced
	factory.WaitForCacheSync(stopCh)

	var clusterInformers = map[string]*cache.SharedIndexInformer{
		"replicationcontrollers": &replicationControllerInformer,
		"services":               &servicesInformer,
		"nodes":                  &nodesInformer,
		"pods":                   &podsInformer,
		"persistentvolumes":      &persistentVolumesInformer,
		"persistentvolumeclaims": &persistentVolumeClaimsInformer,
		"replicasets":            &replicasetsInformer,
		"daemonsets":             &daemonsetsInformer,
		"deployments":            &deploymentsInformer,
		"namespaces":             &namespacesInformer,
		"jobs":                   &jobsInformer,
		"cronjobs":               &cronJobsInformer,
	}
	return clusterInformers, nil
}

//GetK8sMetricsFromInformer loops through all k8s resource informers in kubeAgentConfig writing each to the WSD
func GetK8sMetricsFromInformer(informers map[string]*cache.SharedIndexInformer, workDir *os.File) error {
	for resourceName, informer := range informers {
		resourceList := (*informer).GetIndexer().List()
		err := writeK8sResourceFile(workDir, resourceName, resourceList)
		if err != nil {
			return err
		}
	}
	return nil
}

//writeK8sResourceFile creates a new file in the upload sample directory for the resource name passed in and writes data
func writeK8sResourceFile(workDir *os.File, resourceName string, resourceList []interface{}) (rerr error) {

	file, err := os.OpenFile(workDir.Name()+"/"+resourceName+".jsonl",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.New("error: unable to create kubernetes metric file")
	}
	datawriter := bufio.NewWriter(file)

	for _, k8Resource := range resourceList {
		data, err := json.Marshal(k8Resource)
		if err != nil {
			return errors.New("error: unable to marshal resource: " + resourceName)
		}
		_, err = datawriter.WriteString(string(data) + "\n")
		if err != nil {
			return errors.New("error: unable to write resource to file: " + resourceName)
		}
	}

	datawriter.Flush()
	file.Close()

	return err
}
