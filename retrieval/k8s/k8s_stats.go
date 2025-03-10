package k8s

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	v1apps "k8s.io/api/apps/v1"
	v1batch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	KubernetesLastAppliedConfig = "kubectl.kubernetes.io/last-applied-configuration"
)

func StartUpInformers(clientset kubernetes.Interface, clusterVersion float64,
	resyncInterval int, parseMetricsData bool, stopCh chan struct{}) (map[string]*cache.SharedIndexInformer, error) {
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

	for _, informer := range clusterInformers {
		transform := GetTransformFunc(parseMetricsData)
		err := (*informer).SetTransform(transform)
		if err != nil {
			return nil, err
		}
	}

	// runs in background, starts all informers that are a part of the factory
	factory.Start(stopCh)
	// wait until all informers have successfully synced
	factory.WaitForCacheSync(stopCh)

	return clusterInformers, nil
}

// GetK8sMetricsFromInformer loops through all k8s resource informers in kubeAgentConfig writing each to the WSD
func GetK8sMetricsFromInformer(informers map[string]*cache.SharedIndexInformer,
	workDir *os.File) error {
	for resourceName, informer := range informers {
		// Cronjob informer will be nil if k8s version is less than 1.21, if so skip getting the list of cronjobs
		if *informer == nil {
			continue
		}
		resourceList := (*informer).GetIndexer().List()
		err := writeK8sResourceFile(workDir, resourceName, resourceList)

		if err != nil {
			return err
		}
	}
	return nil
}

// writeK8sResourceFile creates a new file in the upload sample directory for the resourceName passed in and writes data
func writeK8sResourceFile(workDir *os.File, resourceName string,
	resourceList []interface{}) (rerr error) {

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

// nolint: gocyclo
func sanitizeData(to interface{}, parseMetricsData bool) interface{} {
	switch to.(type) {
	case *corev1.Pod:
		return sanitizePod(to, parseMetricsData)
	case *v1apps.DaemonSet:
		cast := to.(*v1apps.DaemonSet)
		if parseMetricsData {
			cast.Spec.Template = corev1.PodTemplateSpec{}
			cast.Spec.RevisionHistoryLimit = nil
			cast.Spec.UpdateStrategy = v1apps.DaemonSetUpdateStrategy{}
			cast.Spec.MinReadySeconds = 0
			cast.Spec.RevisionHistoryLimit = nil
		}
		sanitizeMeta(&cast.ObjectMeta, parseMetricsData)
		return cast
	case *v1apps.ReplicaSet:
		cast := to.(*v1apps.ReplicaSet)
		if parseMetricsData {
			cast.Spec.Replicas = nil
			cast.Spec.Template = corev1.PodTemplateSpec{}
			cast.Spec.MinReadySeconds = 0
		}
		sanitizeMeta(&cast.ObjectMeta, parseMetricsData)
		return cast
	case *v1apps.Deployment:
		cast := to.(*v1apps.Deployment)
		if parseMetricsData {
			cast.Spec.Template = corev1.PodTemplateSpec{}
			cast.Spec.Replicas = nil
			cast.Spec.Strategy = v1apps.DeploymentStrategy{}
			cast.Spec.MinReadySeconds = 0
			cast.Spec.RevisionHistoryLimit = nil
			cast.Spec.ProgressDeadlineSeconds = nil
		}
		sanitizeMeta(&cast.ObjectMeta, parseMetricsData)
		return cast
	case *v1batch.Job:
		cast := to.(*v1batch.Job)
		if parseMetricsData {
			cast.Spec.Template = corev1.PodTemplateSpec{}
			cast.Spec.Parallelism = nil
			cast.Spec.Completions = nil
			cast.Spec.ActiveDeadlineSeconds = nil
			cast.Spec.BackoffLimit = nil
			cast.Spec.ManualSelector = nil
			cast.Spec.TTLSecondsAfterFinished = nil
			cast.Spec.CompletionMode = nil
			cast.Spec.Suspend = nil
		}
		sanitizeMeta(&cast.ObjectMeta, parseMetricsData)
		return cast
	case *v1batch.CronJob:
		cast := to.(*v1batch.CronJob)
		if parseMetricsData {
			// cronjobs have no Selector
			cast.Spec = v1batch.CronJobSpec{}
		}
		sanitizeMeta(&cast.ObjectMeta, parseMetricsData)
		return cast
	case *corev1.Service:
		cast := to.(*corev1.Service)
		if parseMetricsData {
			cast.Spec.Ports = nil
			cast.Spec.ClusterIP = ""
			cast.Spec.ClusterIPs = nil
			cast.Spec.Type = ""
			cast.Spec.ExternalIPs = nil
			cast.Spec.SessionAffinity = ""
			cast.Spec.LoadBalancerIP = ""
			cast.Spec.LoadBalancerSourceRanges = nil
			cast.Spec.ExternalName = ""
			cast.Spec.ExternalTrafficPolicy = ""
			cast.Spec.HealthCheckNodePort = 0
			cast.Spec.SessionAffinityConfig = nil
			cast.Spec.IPFamilies = nil
			cast.Spec.IPFamilyPolicy = nil
			cast.Spec.AllocateLoadBalancerNodePorts = nil
			cast.Spec.LoadBalancerClass = nil
			cast.Spec.InternalTrafficPolicy = nil
		}
		sanitizeMeta(&cast.ObjectMeta, parseMetricsData)
		return cast
	case *corev1.ReplicationController:
		cast := to.(*corev1.ReplicationController)
		if parseMetricsData {

			cast.Spec.Replicas = nil
			cast.Spec.Template = nil
			cast.Spec.MinReadySeconds = 0
		}
		sanitizeMeta(&cast.ObjectMeta, parseMetricsData)
		return cast
	case *corev1.Namespace:
		return sanitizeNamespace(to)
	case *corev1.PersistentVolume:
		cast := to.(*corev1.PersistentVolume)
		sanitizeMeta(&cast.ObjectMeta, parseMetricsData)
		return cast
	case *corev1.PersistentVolumeClaim:
		cast := to.(*corev1.PersistentVolumeClaim)
		sanitizeMeta(&cast.ObjectMeta, parseMetricsData)
		return cast
	case *corev1.Node:
		cast := to.(*corev1.Node)
		sanitizeMeta(&cast.ObjectMeta, parseMetricsData)
		return cast
	}
	return to
}

func sanitizeMeta(objectMeta *metav1.ObjectMeta, parseMetricsData bool) {
	objectMeta.ManagedFields = nil
	delete(objectMeta.Annotations, KubernetesLastAppliedConfig)
	if parseMetricsData {
		objectMeta.Finalizers = nil
	}
}

func sanitizePod(to interface{}, parseMetricsData bool) interface{} {
	cast := to.(*corev1.Pod)

	// stripping env var and related data from the object
	(*cast).ObjectMeta.ManagedFields = nil
	delete((*cast).ObjectMeta.Annotations, KubernetesLastAppliedConfig)

	for j, container := range (*cast).Spec.Containers {
		(*cast).Spec.Containers[j] = sanitizeContainer(container, parseMetricsData)
	}
	for j, container := range (*cast).Spec.InitContainers {
		(*cast).Spec.InitContainers[j] = sanitizeContainer(container, parseMetricsData)
	}
	return cast
}

func sanitizeContainer(container corev1.Container, parseMetricsData bool) corev1.Container {
	container.Env = nil
	if parseMetricsData {
		container.Command = nil
		container.Args = nil
		container.ImagePullPolicy = ""
		container.LivenessProbe = nil
		container.StartupProbe = nil
		container.ReadinessProbe = nil
		container.TerminationMessagePath = ""
		container.TerminationMessagePolicy = ""
		container.SecurityContext = nil
	}
	return container
}

func sanitizeNamespace(to interface{}) interface{} {
	cast := to.(*corev1.Namespace)
	(*cast).ObjectMeta.ManagedFields = nil
	return cast
}

func GetTransformFunc(parseMetricsData bool) func(resource interface{}) (interface{}, error) {
	return func(resource interface{}) (interface{}, error) {
		var err error
		resource = sanitizeData(resource, parseMetricsData)
		if err != nil {
			return nil, fmt.Errorf("resource is not valid type error: %v", err)
		}
		return resource, nil
	}
}
