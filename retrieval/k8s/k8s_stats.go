package k8s

import (
	"bufio"
	"encoding/json"
	"errors"
	"github.com/cloudability/metrics-agent/retrieval/raw"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"os"
	"time"
)

const (
	KubernetesLastAppliedConfig = "kubectl.kubernetes.io/last-applied-configuration"
)

func StartUpInformers(clientset kubernetes.Interface, clusterVersion float64,
	resyncInterval int, stopCh chan struct{}) (map[string]*cache.SharedIndexInformer, error) {
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
	// Jobs
	jobsInformer := factory.Batch().V1().Jobs().Informer()
	// Cronjobs were introduced in k8s 1.21 so for older versions do not attempt to create an informer
	var cronJobsInformer cache.SharedIndexInformer
	if clusterVersion > 1.20 {
		cronJobsInformer = factory.Batch().V1().CronJobs().Informer()
	}

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
func GetK8sMetricsFromInformer(informers map[string]*cache.SharedIndexInformer,
	workDir *os.File, parseMetricData bool) error {
	for resourceName, informer := range informers {
		// Cronjob informer will be nil if k8s version is less than 1.21, if so skip getting the list of cronjobs
		if *informer == nil {
			continue
		}
		resourceList := (*informer).GetIndexer().List()
		err := writeK8sResourceFile(workDir, resourceName, resourceList, parseMetricData)

		if err != nil {
			return err
		}
	}
	return nil
}

//writeK8sResourceFile creates a new file in the upload sample directory for the resource name passed in and writes data
func writeK8sResourceFile(workDir *os.File, resourceName string,
	resourceList []interface{}, parseMetricData bool) (rerr error) {

	file, err := os.OpenFile(workDir.Name()+"/"+resourceName+".jsonl",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.New("error: unable to create kubernetes metric file")
	}
	datawriter := bufio.NewWriter(file)

	for _, k8Resource := range resourceList {

		if parseMetricData {
			k8Resource = getSanitizedK8sResource(k8Resource)
		}

		data, err := json.Marshal(k8Resource)

		if err != nil {
			return errors.New("error: unable to marshal resource: " + resourceName)
		}
		_, err = datawriter.WriteString(string(data) + "\n")
		if err != nil {
			return errors.New("error: unable to write resource to file: " + resourceName)
		}
	}

	err = datawriter.Flush()
	if err != nil {
		return err
	}
	err = file.Close()
	if err != nil {
		return err
	}

	return err
}

func getSanitizedK8sResource(k8Resource interface{}) interface{} {

	return sanitizeData(k8Resource)
}

func sanitizeData(to interface{}) interface{} {
	switch to.(type) {
	case *raw.LabelSelectorMatchedResource:
		return sanitizeSelectorMatchedResource(to)
	case *corev1.Pod:
		return sanitizePod(to)
	case *raw.LabelMapMatchedResource:
		return sanitizeMapMatchedResource(to)
	case *corev1.Namespace:
		return sanitizeNamespace(to)
	}
	return to
}

func sanitizeSelectorMatchedResource(to interface{}) interface{} {
	cast := to.(*raw.LabelSelectorMatchedResource)

	// stripping env var and related data from the object
	(*cast).ObjectMeta.ManagedFields = nil
	if _, ok := (*cast).ObjectMeta.Annotations[KubernetesLastAppliedConfig]; ok {
		delete((*cast).ObjectMeta.Annotations, KubernetesLastAppliedConfig)
	}

	return cast
}

func sanitizePod(to interface{}) interface{} {
	cast := to.(*corev1.Pod)

	// stripping env var and related data from the object
	(*cast).ObjectMeta.ManagedFields = nil
	if _, ok := (*cast).ObjectMeta.Annotations[KubernetesLastAppliedConfig]; ok {
		delete((*cast).ObjectMeta.Annotations, KubernetesLastAppliedConfig)
	}
	for j, container := range (*cast).Spec.Containers {
		(*cast).Spec.Containers[j] = sanitizeContainer(container)
	}
	for j, container := range (*cast).Spec.InitContainers {
		(*cast).Spec.InitContainers[j] = sanitizeContainer(container)
	}
	return cast
}

func sanitizeContainer(container corev1.Container) corev1.Container {
	container.Env = nil
	container.Command = nil
	container.Args = nil
	container.ImagePullPolicy = ""
	container.LivenessProbe = nil
	container.StartupProbe = nil
	container.ReadinessProbe = nil
	container.TerminationMessagePath = ""
	container.TerminationMessagePolicy = ""
	container.SecurityContext = nil
	return container
}

func sanitizeMapMatchedResource(to interface{}) interface{} {
	cast := to.(*raw.LabelMapMatchedResource)
	// stripping env var and related data from the object
	(*cast).ObjectMeta.ManagedFields = nil
	if _, ok := (*cast).ObjectMeta.Annotations[KubernetesLastAppliedConfig]; ok {
		delete((*cast).ObjectMeta.Annotations, KubernetesLastAppliedConfig)
	}
	(*cast).Finalizers = nil
	return cast
}

func sanitizeNamespace(to interface{}) interface{} {
	cast := to.(*corev1.Namespace)
	(*cast).ObjectMeta.ManagedFields = nil
	return cast
}
