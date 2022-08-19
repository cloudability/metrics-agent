package test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/expfmt"

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
	NodeContainers             map[string]map[string]cadvisor.ContainerInfo
	BaselineNodeContainers     map[string]map[string]cadvisor.ContainerInfo
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
	"stats-container-":           AsContainerNodeSummary(false),
	"baseline-container-":        AsContainerNodeSummary(true),
	"stats-cadvisor_metrics-":    AsCadvisorMetrics(false),
	"baseline-cadvisor_metrics-": AsCadvisorMetrics(true),
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

		// assign storage to the resource list (ex: &p.Pods) that was passed in from the knownFileTypes. And get the
		// k8s object type from knownFileTypes so the json decoder knows what type of object fields to decode
		storage, k8sObject := refFn(parsedK8sList)

		// loop reading 1 line at a time until EOF
		for {

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
			default:
				return errors.New("unknown type from file: " + fname)
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

func AsCadvisorMetrics(baseline bool) UnmarshalForK8sListFn {
	return func(fname string, fdata []byte, parsedK8sList *ParsedK8sLists) error {
		cPromMetrics, err := parseRawCadvisorPrometheusFile(fdata)
		if err != nil {
			return err
		}
		cPromMetricsCI := parsedK8sList.CadvisorPrometheus
		if baseline {
			cPromMetricsCI = parsedK8sList.BaselineCadvisorPrometheus
		}
		cPromMetricsCI[parseHostFromFileName(fname)] = cPromToContainerInfo(cPromMetrics)
		return nil
	}
}

func parseRawCadvisorPrometheusFile(fdata []byte) (map[string]*prom2json.Family, error) {
	cPromMetrics := make(map[string]*prom2json.Family)
	in := bytes.NewReader(fdata)
	var parser expfmt.TextParser
	metricFamilies, err := parser.TextToMetricFamilies(in)
	if err != nil {
		return nil, fmt.Errorf("reading cadvisor prometheus text format failed: %v", err)
	}
	for name, mf := range metricFamilies {
		cPromMetrics[name] = prom2json.NewFamily(mf)
	}
	return cPromMetrics, nil
}

func cPromToContainerInfo(metricFamilies map[string]*prom2json.Family) map[string]cadvisor.ContainerInfo {
	containerInfos := make(map[string]cadvisor.ContainerInfo)
	missingMetrics := make(map[string]bool)
	for metricName, metricsFamily := range metricFamilies {
		for _, containerMetric := range metricsFamily.Metrics {
			metric, ok := containerMetric.(prom2json.Metric)
			if !ok {
				continue
			}
			container := metric.Labels["id"]
			if container == "" {
				container = "node-level-metric"
			}
			var ci cadvisor.ContainerInfo
			var err error
			if ci, ok = containerInfos[container]; !ok {
				ci, err = newContainerInfoFromCProm(metric, container)
				if err != nil {
					continue
				}
			}
			ci = containerMetricsFromCProm(ci, metricName, metric, missingMetrics)
			containerInfos[container] = ci
		}
	}
	return containerInfos
}

func containerMetricsFromCProm(
	ci cadvisor.ContainerInfo,
	metricName string,
	metric prom2json.Metric,
	tmp map[string]bool) cadvisor.ContainerInfo {
	converterFunc, ok := cPromToCIConversions[metricName]
	if !ok {
		tmp[metricName] = true
		return ci
	}
	converterFunc(metric, &ci)
	return ci
}

var cPromToCIConversions = map[string]metricConverter{
	"container_fs_io_time_seconds_total": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		updateFileSystemEntry(metric, ci, func(fs cadvisor.FsStats) cadvisor.FsStats {
			fs.IoTime = (strToUint(metric.Value) * 1000) // convert to milliseconds
			return fs
		})
	},
	// DiskIO
	"container_fs_writes_bytes_total": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		updateDiskIOEntry(metric, ci, func(disk cadvisor.PerDiskStats) cadvisor.PerDiskStats {
			metricVal := strToUint(metric.Value)
			disk.Stats["Write"] = metricVal
			disk.Stats["Total"] += metricVal
			disk.Stats["Sync"] = metricVal
			return disk
		})
	},
	"container_fs_reads_bytes_total": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		updateDiskIOEntry(metric, ci, func(disk cadvisor.PerDiskStats) cadvisor.PerDiskStats {
			bytesRead := strToUint(metric.Value)
			disk.Stats["Read"] = bytesRead
			disk.Stats["Total"] += bytesRead
			return disk
		})
	},
	// Filesystem
	"container_fs_sector_reads_total": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		updateFileSystemEntry(metric, ci, func(fs cadvisor.FsStats) cadvisor.FsStats {
			// Cumulative count of sector reads completed
			fs.ReadsCompleted = strToUint(metric.Value)
			return fs
		})
	},
	"container_fs_limit_bytes": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		updateFileSystemEntry(metric, ci, func(fs cadvisor.FsStats) cadvisor.FsStats {
			fs.Limit = strToUint(metric.Value)
			return fs
		})
	},
	"container_fs_usage_bytes": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		updateFileSystemEntry(metric, ci, func(fs cadvisor.FsStats) cadvisor.FsStats {
			fs.Usage = strToUint(metric.Value)
			var writeBytes uint64
			updateDiskIOEntry(metric, ci, func(disk cadvisor.PerDiskStats) cadvisor.PerDiskStats {
				writeBytes = disk.Stats["Write"]
				return disk
			})
			if writeBytes >= fs.Usage {
				fs.Available = writeBytes - fs.Usage
			}
			return fs
		})
	},
	"container_start_time_seconds": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		tm := time.Unix(strToInt(metric.Value, 64), 0)
		ci.Spec.CreationTime = tm.UTC()
	},
	"container_network_receive_errors_total": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		addNetworkEntries(metric, ci, func(net cadvisor.InterfaceStats) cadvisor.InterfaceStats {
			net.RxErrors = strToUint(metric.Value)
			return net
		})
	},
	"container_spec_memory_limit_bytes": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		ci.Spec.Memory.Limit = strToUint(metric.Value)
		ci.Spec.HasMemory = true
	},
	"container_memory_rss": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		ci.Stats[0].Memory.RSS = strToUint(metric.Value)
	},
	"container_memory_swap": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		ci.Stats[0].Memory.Swap = strToUint(metric.Value)

	},
	"container_cpu_cfs_periods_total": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		ci.Stats[0].Cpu.CFS.Periods = strToUint(metric.Value)
	},
	"container_cpu_load_average_10s": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		ci.Stats[0].Cpu.LoadAverage = int32(strToInt(metric.Value, 32))
	},
	"container_cpu_cfs_throttled_seconds_total": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		ci.Stats[0].Cpu.CFS.ThrottledTime = (strToUint(metric.Value) * 1e+9) // convert to nanoseconds
	},
	"container_cpu_usage_seconds_total": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		ci.Stats[0].Cpu.Usage.Total = strToUint(metric.Value)
	},
	"container_memory_failures_total": func(metric prom2json.Metric, ci *cadvisor.ContainerInfo) {
		ci.Stats[0].Memory.Failcnt = strToUint(metric.Value)
	},
}

func strToUint(str string) uint64 {
	bitSize := 64
	fl, err := strconv.ParseFloat(str, bitSize)
	if err != nil {
		return 0
	}
	rounded := math.Round(fl)
	return uint64(rounded)
}

func strToInt(str string, bitSize int) int64 {
	fl, err := strconv.ParseFloat(str, bitSize)
	if err != nil {
		return 0
	}
	rounded := math.Round(fl)
	return int64(rounded)
}

func updateFileSystemEntry(m prom2json.Metric, ci *cadvisor.ContainerInfo, fn FileSystemMetricAdder) {
	var fs cadvisor.FsStats
	ci.Spec.HasFilesystem = true
	for i, entry := range ci.Stats[0].Filesystem {
		if entry.Device == m.Labels["device"] {
			fs = fn(entry)
			ci.Stats[0].Filesystem[i] = fs
			return
		}
	}
	fs.Device = m.Labels["device"]
	fs = fn(fs)
	ci.Stats[0].Filesystem = append(ci.Stats[0].Filesystem, fs)
}

func updateDiskIOEntry(m prom2json.Metric, ci *cadvisor.ContainerInfo, fn DiskIOMetricAdder) {
	var fs cadvisor.PerDiskStats
	ci.Spec.HasDiskIo = true
	for i, entry := range ci.Stats[0].DiskIo.IoServiceBytes {
		if entry.Device == m.Labels["device"] {
			if entry.Stats == nil {
				entry.Stats = make(map[string]uint64)
			}
			fs = fn(entry)
			ci.Stats[0].DiskIo.IoServiceBytes[i] = fs
			return
		}
	}
	fs.Device = m.Labels["device"]
	fs.Stats = make(map[string]uint64)
	fs = fn(fs)
	ci.Stats[0].DiskIo.IoServiceBytes = append(ci.Stats[0].DiskIo.IoServiceBytes, fs)
}

func addNetworkEntries(m prom2json.Metric, ci *cadvisor.ContainerInfo, fn NetworkMetricAdder) {
	var net cadvisor.InterfaceStats
	for i, entry := range ci.Stats[0].Network.Interfaces {
		if entry.Name == m.Labels["interface"] {
			net = fn(entry)
			ci.Stats[0].Network.Interfaces[i] = net
			return
		}
	}
	net.Name = m.Labels["interface"]
	net = fn(net)
	ci.Stats[0].Network.Interfaces = append(ci.Stats[0].Network.Interfaces, net)
}

func newContainerInfoFromCProm(metric prom2json.Metric, container string) (cadvisor.ContainerInfo, error) {
	if metric.TimestampMs == "" {
		metric.TimestampMs = "0"
	}
	i, err := strconv.ParseInt(metric.TimestampMs, 10, 64)
	if err != nil {
		return cadvisor.ContainerInfo{}, fmt.Errorf("could not parse timestamp: %v", err)
	}
	tm := time.Unix(i, 0)
	ci := cadvisor.ContainerInfo{
		ContainerReference: cadvisor.ContainerReference{
			Id:        metric.Labels["name"],
			Name:      container,
			Namespace: metric.Labels["namespace"],
		},
		Subcontainers: nil,
		Spec: cadvisor.ContainerSpec{
			Labels: map[string]string{
				"io.kubernetes.container.name": metric.Labels["container"],
				"io.kubernetes.pod.namespace":  metric.Labels["namespace"],
				"io.kubernetes.pod.name":       metric.Labels["pod"],
			},
			Image: metric.Labels["image"],
		},
		Stats: []*cadvisor.ContainerStats{
			{
				Timestamp:     tm,
				Cpu:           cadvisor.CpuStats{},
				DiskIo:        cadvisor.DiskIoStats{},
				Memory:        cadvisor.MemoryStats{},
				Network:       cadvisor.NetworkStats{},
				Filesystem:    nil,
				TaskStats:     cadvisor.LoadStats{},
				Accelerators:  nil,
				Processes:     cadvisor.ProcessStats{},
				CustomMetrics: nil,
			},
		},
	}
	return ci, nil
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
