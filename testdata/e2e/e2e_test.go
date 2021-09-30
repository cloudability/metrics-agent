package test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	"strings"
	"testing"

	cadvisor "github.com/google/cadvisor/info/v1"
	v1 "k8s.io/api/core/v1"
	statsapi "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
)

func TestMetricSample(t *testing.T) {
	const stress = "stress"
	wd := os.Getenv("WORKING_DIR")
	kv := os.Getenv("KUBERNETES_VERSION")
	versionParts := strings.Split(kv, ".")
	minorVersion, err := strconv.Atoi(versionParts[1])
	if err != nil {
		t.Errorf("Unable to determine kubernetes minor version: %s", err)
	}

	parsedK8sLists := &ParsedK8sLists{
		NodeSummaries:              make(map[string]statsapi.Summary),
		BaselineNodeSummaries:      make(map[string]statsapi.Summary),
		NodeContainers:             make(map[string]map[string]cadvisor.ContainerInfo),
		BaselineNodeContainers:     make(map[string]map[string]cadvisor.ContainerInfo),
		CadvisorPrometheus:         make(map[string]map[string]cadvisor.ContainerInfo),
		BaselineCadvisorPrometheus: make(map[string]map[string]cadvisor.ContainerInfo),
	}
	t.Parallel()

	t.Run("ensure that a metrics sample has expected files for cluster version", func(t *testing.T) {
		seen := make(map[string]bool, len(knownFileTypes))

		err := filepath.Walk(wd, func(path string, info os.FileInfo, e error) error {
			if e != nil {
				return e
			}

			// check if it is a regular file (not dir)
			if info.Mode().IsRegular() {
				n := info.Name()
				ft := toAgentFileType(n)
				seen[toAgentFileType(ft)] = true
				if unmarshalFn, ok := knownFileTypes[ft]; ok {
					t.Logf("Processing: %v", n)
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
			t.Fatalf("Failed: %v", err)
		}
		err = checkForRequiredFiles(seen, minorVersion)
		if err != nil {
			t.Fatalf("Failed: %v", err)
		}
	})

	t.Run("ensure that a metrics sample contains the cloudability namespace", func(t *testing.T) {
		for _, ns := range parsedK8sLists.Namespaces.Items {
			if ns.Name == "cloudability" {
				return
			}
		}
		t.Error("Namespace cloudability not found in metric sample")
	})

	t.Run("ensure that a metrics sample has expected pod data", func(t *testing.T) {
		for _, po := range parsedK8sLists.Pods.Items {
			if strings.HasPrefix(po.Name, stress) && po.Status.QOSClass == v1.PodQOSBestEffort {
				return
			}

		}
		t.Error("pod stress not found in metric sample")
	})

	t.Run("ensure that a metrics sample has expected containers summary data", func(t *testing.T) {
		for _, ns := range parsedK8sLists.NodeSummaries {
			for _, pf := range ns.Pods {
				if strings.HasPrefix(pf.PodRef.Name, stress) && pf.PodRef.Namespace == stress && pf.CPU.UsageNanoCores != nil {
					return
				}
			}
		}
		t.Error("pod summary data not found in metric sample")
	})

	// 2020.9.10 - TODO: Remove this test once we stop supporting minor versions below 18
	t.Run("ensure that a metrics sample has expected containers stat data", func(t *testing.T) {
		if minorVersion < 18 {
			for _, nc := range parsedK8sLists.NodeContainers {

				for _, s := range nc {
					if strings.HasPrefix(s.Name, "/kubepods/besteffort/pod") && s.Namespace == "containerd" && strings.HasPrefix(
						s.Spec.Labels["io.kubernetes.pod.name"], stress) {
						return
					}
				}
			}
			t.Error("pod container stat data not found in metric sample")
		}
		return
	})

	t.Run("ensure that a metrics sample has expected cadvisor prometheus data", func(t *testing.T) {
		for _, containerInfos := range parsedK8sLists.CadvisorPrometheus {
			for _, containerInfo := range containerInfos {
				if minorVersion >= 21 {
					if strings.HasPrefix(containerInfo.Name, "/kubelet/kubepods/besteffort/pod") &&
						containerInfo.Namespace == stress &&
						strings.HasPrefix(containerInfo.Spec.Labels["io.kubernetes.pod.name"], stress) {
						return
					}
				} else {
					if strings.HasPrefix(containerInfo.Name, "/kubepods/besteffort/pod") &&
						containerInfo.Namespace == stress &&
						strings.HasPrefix(containerInfo.Spec.Labels["io.kubernetes.pod.name"], stress) {
						return
					}
				}
			}
		}
		t.Error("pod cadvisor prometheus data not found in metric sample")
	})

}
