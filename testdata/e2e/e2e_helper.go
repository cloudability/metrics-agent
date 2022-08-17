package test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	cadvisor "github.com/google/cadvisor/info/v1"
	"github.com/prometheus/prom2json"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	statsapi "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
)

type ParsedK8sLists struct {
	Namespaces                 NamespaceList
	Pods                       PodList
	Deployments                LabelSelectorMatchedResourceList
	ReplicaSets                LabelSelectorMatchedResourceList
	Services                   LabelMapMatchedResourceList
	Jobs                       LabelSelectorMatchedResourceList
	DaemonSets                 LabelSelectorMatchedResourceList
	Nodes                      NodeList
	PersistentVolumes          PersistentVolumeList
	PersistentVolumeClaims     PersistentVolumeClaimList
	ReplicationControllers     LabelMapMatchedResourceList
	NodeSummaries              map[string]statsapi.Summary
	BaselineNodeSummaries      map[string]statsapi.Summary
	CadvisorPrometheus         map[string]map[string]cadvisor.ContainerInfo
	BaselineCadvisorPrometheus map[string]map[string]cadvisor.ContainerInfo
	CldyAgent                  CldyAgent
}

// ListResponse is a base object for unmarshaling k8s objects from the JSON files containing them. It captures
// the general fields present on all the responses.
type ListResponse struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   map[string]string `json:"metadata"`
	Code       int               `json:"code"`
	Details    map[string]string `json:"details"`
	Message    string            `json:"message"`
	Reason     string            `json:"reason"`
	Status     string            `json:"status"`
}

// LabelSelectorMatchedResource is a k8s resource that "points" to a pod by a label selector. This struct
// gathers the minimal necessary fields for adding the relevant labels to the heapster metric.
type LabelSelectorMatchedResource struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              struct {
		LabelSelector metav1.LabelSelector `json:"selector,omitempty"`
	} `json:"spec,omitempty"`
}

// LabelSelectorMatchedResourceList is a slice of LabelSelectorMatchedResource, one for each entry in the json.
type LabelSelectorMatchedResourceList struct {
	ListResponse
	Items []LabelSelectorMatchedResource `json:"items"`
}

// LabelMapMatchedResource is a k8s resource that "points" to a pod by a label map.
type LabelMapMatchedResource struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              struct {
		LabelSelector map[string]string `json:"selector,omitempty"`
	} `json:"spec,omitempty"`
	Status struct {
		LoadBalancer LoadBalancer `json:"loadBalancer"`
	}
}

// LoadBalancer represents ingress for ELB resources
type LoadBalancer struct {
	Ingress []struct {
		Hostname string `json:"hostname"`
	} `json:"ingress"`
}

// LabelMapMatchedResourceList is a slice of LabelMapMatchedResource, one for each entry in the json.
type LabelMapMatchedResourceList struct {
	ListResponse
	Items []LabelMapMatchedResource `json:"items"`
}

// NamespaceList represents the list of namespaces unmarshalled from the namespaces api.
type NamespaceList struct {
	ListResponse
	Items []v1.Namespace `json:"items"`
}

// PodList represents the list of pods unmarshalled from the pods api.
type PodList struct {
	ListResponse
	Items []v1.Pod `json:"items"`
}

// NodeList represents the list of nodes unmarshalled from the nodes api.
type NodeList struct {
	ListResponse
	Items []v1.Node `json:"items"`
}

// PersistentVolumeList represents the list of persistent volumes unmarshalled from the persistent volumes api.
type PersistentVolumeList struct {
	ListResponse
	Items []v1.PersistentVolume `json:"items"`
}

// PersistentVolumeClaimList represents the list of persistent volume claims unmarshalled from the persistent
// volume claims api.
type PersistentVolumeClaimList struct {
	ListResponse
	Items []v1.PersistentVolumeClaim `json:"items"`
}

type CldyAgent struct {
	Name      string            `json:"name,omitempty"`
	Metrics   map[string]uint64 `json:"metrics,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	Timestamp int64             `json:"ts,omitempty"`
	Value     float64           `json:"value,omitempty"`
	Values    map[string]string `json:"values,omitempty"`
}

type FileSystemMetricAdder func(cadvisor.FsStats) cadvisor.FsStats

type NetworkMetricAdder func(cadvisor.InterfaceStats) cadvisor.InterfaceStats

type DiskIOMetricAdder func(stats cadvisor.PerDiskStats) cadvisor.PerDiskStats

type metricConverter func(metric prom2json.Metric, info *cadvisor.ContainerInfo)

// UnmarshalForK8sListFn function alias type for a function that unmarshals data into a ParsedK8sLists ref
type UnmarshalForK8sListFn func(fname string, fdata []byte, parsedK8sList *ParsedK8sLists) error

type k8sRefFn func(lists *ParsedK8sLists) (interface{}, interface{})

var knownFileTypes = map[string]UnmarshalForK8sListFn{
	"agent-measurement.json":      ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.CldyAgent, nil }),
	"namespaces.json":             ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.Namespaces, nil }),
	"pods.json":                   ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.Pods, nil }),
	"deployments.json":            ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.Deployments, nil }),
	"replicasets.json":            ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.ReplicaSets, nil }),
	"replicationcontrollers.json": ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.ReplicationControllers, nil }),
	"daemonsets.json":             ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.DaemonSets, nil }),
	"services.json":               ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.Services, nil }),
	"jobs.json":                   ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.Jobs, nil }),
	"nodes.json":                  ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.Nodes, nil }),
	"persistentvolumes.json":      ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.PersistentVolumes, nil }),
	"persistentvolumeclaims.json": ByRefFn(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.PersistentVolumeClaims, nil }),
	// file formats for Informer data
	"namespaces.jsonl": ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.Namespaces, &corev1.Namespace{} }),
	"pods.jsonl":       ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) { return &p.Pods, &corev1.Pod{} }),
	"deployments.jsonl": ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) {
		return &p.Deployments, &LabelSelectorMatchedResource{}
	}),
	"replicasets.jsonl": ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) {
		return &p.ReplicaSets, &LabelSelectorMatchedResource{}
	}),
	"replicationcontrollers.jsonl": ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) {
		return &p.ReplicationControllers, &LabelMapMatchedResource{}
	}),
	"daemonsets.jsonl": ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) {
		return &p.DaemonSets, &LabelSelectorMatchedResource{}
	}),
	"services.jsonl": ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) {
		return &p.Services, &LabelMapMatchedResource{}
	}),
	"jobs.jsonl": ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) {
		return &p.Jobs, &LabelSelectorMatchedResource{}
	}),
	"nodes.jsonl": ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) {
		return &p.Nodes, &corev1.Node{}
	}),
	"persistentvolumes.jsonl": ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) {
		return &p.PersistentVolumes, &corev1.PersistentVolume{}
	}),
	"persistentvolumeclaims.jsonl": ByRefFnInformer(func(p *ParsedK8sLists) (interface{}, interface{}) {
		return &p.PersistentVolumeClaims, &corev1.PersistentVolumeClaim{}
	}),
	"stats-summary-":             AsNodeSummary(false),
	"baseline-summary-":          AsNodeSummary(true),
}

var agentFileTypes = map[string]bool{
	"stats-summary-":             true,
	"baseline-summary-":          true,
	"stats-container-":           true,
	"baseline-container-":        true,
	"stats-cadvisor_metrics-":    true,
	"baseline-cadvisor_metrics-": true,
}

func shouldSkipFileCheck(fileType string, k8sMinorVersion int) bool {
	if k8sMinorVersion >= 18 && (fileType == "baseline-container-" || fileType == "stats-container-") {
		return true
	}
	return false
}

// ByRefFn returns a UnmarshalForK8sListFn (so this function returns a function with signature like fname, fdata,
// parsedK8sList that returns an error). ByRefFn is the unmarshalling process for k8s resources coming from .json files.
// Examples of these files include (pods.json and nodes.json). ByRefFn's parameter 'refFn' is of type k8sRefFn, this
// is a function that takes in *ParsedK8sList and returns 2 interfaces. The first interface is the part of the
// ParsedK8sList that the data is being unmarshalled to. For example, if unmarshalling pods.json, the first interface
// will be &p.Pods which is the 'Pods' of the ParsedK8sLists struct. The second interface is not needed for this
// unmarshalling structure and will always be nil.
func ByRefFn(refFn k8sRefFn) UnmarshalForK8sListFn {
	return func(fname string, fdata []byte, parsedK8sList *ParsedK8sLists) error {
		// assign storage to the resource list (ex: &p.Pods) that was passed in from the knownFileTypes.
		storage, _ := refFn(parsedK8sList)
		// unmarshal the resource file (ex: pods.json) into the storage object
		return json.Unmarshal(fdata, storage)
	}
}

// ByRefFnInformer returns a UnmarshalForK8sListFn (so this function returns a function with signature like fname,
// fdata, parsedK8sList that returns an error). ByRefFnInformer is the unmarshalling process for k8s resources coming
// from .jsonl files. Examples of these files include (pods.jsonl and nodes.jsonl). ByRefFn's parameter 'refFn'
// is of type k8sRefFn, this is a function that takes in *ParsedK8sList and returns 2 interfaces. The first interface
// is the part of the ParsedK8sList that the data is being unmarshalled to. For example, if unmarshalling pods.jsonl,
// the first interface will be &p.Pods which is the 'Pods' of the ParsedK8sLists struct. The second interface that is
// returned is a typed (but not casted) singular kubernetes object (examples would be: (pods.jsonl) *corev1.Pod,
// (nodes.jsonl) *corev1.Node, and (services.jsonl/replicationcontrollers.jsonl) *LabelMapMatchedResource). The informer
// data is stored in jsonl format with each line representing one kubernetes object and this function decodes the
// jsonl line by line.
// nolint: funlen, gocyclo
func ByRefFnInformer(refFn k8sRefFn) UnmarshalForK8sListFn {
	return func(fname string, fdata []byte, parsedK8sList *ParsedK8sLists) error {
		reader := bytes.NewReader(fdata)
		dec := json.NewDecoder(reader)
		// loop reading 1 line at a time until EOF
		for {
			// assign storage to the resource list (ex: &p.Pods) that was passed in from the knownFileTypes. And get the
			// k8s object type from knownFileTypes so the json decoder knows what type of object fields to decode
			storage, k8sObject := refFn(parsedK8sList)
			if err := dec.Decode(k8sObject); err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
			// since k8sObject is technically an interface, we need to type switch (thus casting) the k8sObject and
			// append the object to the correct ParsedK8sLists field (ex: Pods.Items, Deployments.Items,
			// Namespaces.Items). storage is also technically an interface, so we need to cast this correctly based
			// on the parsedK8sList field that is passed in (ex: &p.Pods, &p.Services)
			switch typedK8sObject := k8sObject.(type) {
			case *corev1.Namespace:
				typedStorage, ok := storage.(*NamespaceList)
				if !ok {
					return errors.New("cannot cast NamespaceList")
				}
				typedStorage.Items = append(typedStorage.Items, *typedK8sObject)
			case *corev1.Pod:
				typedStorage, ok := storage.(*PodList)
				if !ok {
					return errors.New("cannot cast PodList")
				}
				typedStorage.Items = append(typedStorage.Items, *typedK8sObject)
			case *corev1.Node:
				typeStorage, ok := storage.(*NodeList)
				if !ok {
					return errors.New("cannot cast NodeList")
				}
				typeStorage.Items = append(typeStorage.Items, *typedK8sObject)
			case *corev1.PersistentVolume:
				typeStorage, ok := storage.(*PersistentVolumeList)
				if !ok {
					return errors.New("cannot cast PVList")
				}
				typeStorage.Items = append(typeStorage.Items, *typedK8sObject)
			case *corev1.PersistentVolumeClaim:
				typeStorage, ok := storage.(*PersistentVolumeClaimList)
				if !ok {
					return errors.New("cannot cast PVCList")
				}
				typeStorage.Items = append(typeStorage.Items, *typedK8sObject)
			case *LabelSelectorMatchedResource:
				typeStorage, ok := storage.(*LabelSelectorMatchedResourceList)
				if !ok {
					return errors.New("cannot cast LabelSelectorMatchedResourceList")
				}
				typeStorage.Items = append(typeStorage.Items, *typedK8sObject)
			case *LabelMapMatchedResource:
				typeStorage, ok := storage.(*LabelMapMatchedResourceList)
				if !ok {
					return errors.New("cannot cast LabelMapMatchedResourceList")
				}
				typeStorage.Items = append(typeStorage.Items, *typedK8sObject)
			}
		}
		return nil
	}
}

// AsNodeSummary returns a UnmarshalForK8sListFn that will parse the data as a Node Summary
func AsNodeSummary(baseline bool) UnmarshalForK8sListFn {
	return func(fname string, fdata []byte, parsedK8sList *ParsedK8sLists) error {
		s := statsapi.Summary{}
		err := json.Unmarshal(fdata, &s)
		if err != nil {
			return err
		}
		summaries := parsedK8sList.NodeSummaries
		if baseline {
			summaries = parsedK8sList.BaselineNodeSummaries
		}
		summaries[s.Node.NodeName] = s
		return nil
	}
}

func toAgentFileType(n string) string {
	for at := range agentFileTypes {
		if strings.HasPrefix(n, at) {
			return at
		}
	}

	return n
}

func checkForRequiredFiles(seen map[string]bool, k8sMinorVersion int) error {
	for f := range knownFileTypes {
		if shouldSkipFileCheck(f, k8sMinorVersion) {
			continue
		}
		if seen[f] {
			continue
		}

		return fmt.Errorf("missing expected files: %#v, SEEN: %#v", f, seen)
	}
	return nil
}
