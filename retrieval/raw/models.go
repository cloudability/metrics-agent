package raw

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	AgentMeasurement       = "agent-measurement"
	Namespaces             = "namespaces"
	Pods                   = "pods"
	Deployments            = "deployments"
	ReplicaSets            = "replicasets"
	ReplicationControllers = "replicationcontrollers"
	DaemonSets             = "daemonsets"
	Services               = "services"
	Jobs                   = "jobs"
	Nodes                  = "nodes"
	PersistentVolumes      = "persistentvolumes"
	PersistentVolumeClaims = "persistentvolumeclaims"
)

// ParsableFileSet contains file names that can be minimized via de/re serialization
var ParsableFileSet = map[string]struct{}{
	AgentMeasurement:       {},
	Namespaces:             {},
	Pods:                   {},
	Deployments:            {},
	ReplicaSets:            {},
	ReplicationControllers: {},
	DaemonSets:             {},
	Services:               {},
	Jobs:                   {},
	Nodes:                  {},
	PersistentVolumes:      {},
	PersistentVolumeClaims: {},
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

// LabelMapMatchedResource is a k8s resource that "points" to a pod by a label map. This struct
// gathers the minimal necessary fields for adding the relevant labels to the heapster metric.
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
	Items []corev1.Namespace `json:"items"`
}

// PodList represents the list of pods unmarshalled from the pods api.
type PodList struct {
	ListResponse
	Items []corev1.Pod `json:"items"`
}

// NodeList represents the list of nodes unmarshalled from the nodes api.
type NodeList struct {
	ListResponse
	Items []corev1.Node `json:"items"`
}

// PersistentVolumeList represents the list of persistent volumes unmarshalled from the persistent volumes api.
type PersistentVolumeList struct {
	ListResponse
	Items []corev1.PersistentVolume `json:"items"`
}

// PersistentVolumeClaimList represents the list of persistent volume claims unmarshalled from the persistent
// volume claims api.
type PersistentVolumeClaimList struct {
	ListResponse
	Items []corev1.PersistentVolumeClaim `json:"items"`
}

// CldyAgent has information from the agent JSON file.
type CldyAgent struct {
	Name      string            `json:"name,omitempty"`
	Metrics   map[string]uint64 `json:"metrics,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	Timestamp int64             `json:"ts,omitempty"`
	Value     float64           `json:"value,omitempty"`
	Values    map[string]string `json:"values,omitempty"`
}
