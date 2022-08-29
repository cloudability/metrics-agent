package test

import (
	"encoding/json"
	"fmt"
	cadvisor "github.com/google/cadvisor/info/v1"
	"github.com/prometheus/prom2json"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	statsapi "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
	"strings"
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

type k8sRefFn func(lists *ParsedK8sLists) interface{}

var knownFileTypes = map[string]UnmarshalForK8sListFn{
	"agent-measurement.json":      ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.CldyAgent }),
	"namespaces.json":             ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.Namespaces }),
	"pods.json":                   ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.Pods }),
	"deployments.json":            ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.Deployments }),
	"replicasets.json":            ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.ReplicaSets }),
	"replicationcontrollers.json": ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.ReplicationControllers }),
	"daemonsets.json":             ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.DaemonSets }),
	"services.json":               ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.Services }),
	"jobs.json":                   ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.Jobs }),
	"nodes.json":                  ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.Nodes }),
	"persistentvolumes.json":      ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.PersistentVolumes }),
	"persistentvolumeclaims.json": ByRefFn(func(p *ParsedK8sLists) interface{} { return &p.PersistentVolumeClaims }),
	"stats-summary-":              AsNodeSummary(false),
	"baseline-summary-":           AsNodeSummary(true),
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

// ByRefFn returns a UnmarshalForK8sListFn that will unmarshal into the ref returned by the given refFn
func ByRefFn(refFn k8sRefFn) UnmarshalForK8sListFn {
	return func(fname string, fdata []byte, parsedK8sList *ParsedK8sLists) error {
		if len(fdata) <= 0 {
			return fmt.Errorf("File: %v appears to be empty", fname)
		}
		return json.Unmarshal(fdata, refFn(parsedK8sList))
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
