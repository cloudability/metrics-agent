package test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cadvisor "github.com/google/cadvisor/info/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	statsapi "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	// . "github.com/onsi/ginkgo"
	// . "github.com/onsi/gomega"
)

// func TestMetricsSample(t *testing.T) {
// 	RegisterFailHandler(Fail)
// 	RunSpecs(t, "[MetricsAgent]")
// }
func TestMetricSample(t *testing.T) {

	wd := os.Getenv("WORKING_DIR")

	parsedK8sLists := &ParsedK8sLists{}
	//load the metric samples
	err := filepath.Walk(wd, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}

		// check if it is a regular file (not dir)
		if info.Mode().IsRegular() {
			fmt.Println("Proccessing:", info.Name())
			if unmarshalFn, ok := knownFileTypes[info.Name()]; ok {
				f, err := ioutil.ReadFile(path)
				if err != nil {
					return err
				}

				if err := unmarshalFn(path, f, parsedK8sLists); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Errorf("bad news %v", err)
	}

	// t.Parallel()
	t.Run("ensure that a metrics sample has cloudability namespace", func(t *testing.T) {
		f := false
		for _, ns := range parsedK8sLists.Namespaces.Items {
			if ns.Name == "cloudability" {
				f = true
			}
		}
		if !f {
			t.Error("Namespace cloudability not found in metric sample")
		}
	})

	// t.Run("ensure that a metrics sample has cloudability namespace", func(t *testing.T) {
	// 	f := false
	// 	for _, ns := range parsedK8sLists. Namespaces.Items {
	// 		if ns.Name == "cloudability" {
	// 			f = true
	// 		}
	// 	}
	// 	if !f {
	// 		t.Error("Namespace cloudability not found in metric sample")
	// 	}
	// })

}

type ParsedK8sLists struct {
	Namespaces             NamespaceList
	Pods                   PodList
	Deployments            LabelSelectorMatchedResourceList
	ReplicaSets            LabelSelectorMatchedResourceList
	Services               LabelMapMatchedResourceList
	Jobs                   LabelSelectorMatchedResourceList
	DaemonSets             LabelSelectorMatchedResourceList
	Nodes                  NodeList
	PersistentVolumes      PersistentVolumeList
	PersistentVolumeClaims PersistentVolumeClaimList
	ReplicationControllers LabelMapMatchedResourceList
	NodeSummaries          map[string]statsapi.Summary
	BaselineNodeSummaries  map[string]statsapi.Summary
	NodeContainers         map[string]map[string]cadvisor.ContainerInfo
	BaselineNodeContainers map[string]map[string]cadvisor.ContainerInfo
	CldyAgent              CldyAgent
}

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
	"stats-container-":            AsContainerNodeSummary(false),
	"baseline-container-":         AsContainerNodeSummary(true),
}

// UnmarshalForK8sListFn function alias type for a function that unmarshals data into a ParsedK8sLists ref
type UnmarshalForK8sListFn func(fname string, fdata []byte, parsedK8sList *ParsedK8sLists) error

type k8sRefFn func(lists *ParsedK8sLists) interface{}

// ByRefFn returns a UnmarshalForK8sListFn that will unmarshal into the ref returned by the given refFn
func ByRefFn(refFn k8sRefFn) UnmarshalForK8sListFn {
	return func(fname string, fdata []byte, parsedK8sList *ParsedK8sLists) error {
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

// AsContainerNodeSummary returns a UnmarshalForK8sListFn that will parse the data as a Container Summary
func AsContainerNodeSummary(baseline bool) UnmarshalForK8sListFn {
	return func(fname string, fdata []byte, parsedK8sList *ParsedK8sLists) error {
		ci := map[string]cadvisor.ContainerInfo{}
		err := json.Unmarshal(fdata, &ci)
		if err != nil {
			return err
		}
		nodeContainers := parsedK8sList.NodeContainers
		if baseline {
			nodeContainers = parsedK8sList.BaselineNodeContainers
		}
		nodeContainers[parseHostFromFileName(fname)] = ci
		return nil
	}
}

func parseHostFromFileName(fileName string) string {
	s := strings.SplitAfterN(fileName, "-", 3)
	return strings.TrimSuffix(s[2], filepath.Ext(s[2]))
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
