//go:build tools
// +build tools

package tools

import (
	// e2e test deps pinned here so `go mod tidy` can be run without removing these dependencies
	// used in `make test-e2e-X.XX` and `make test-e2e-all` cmds
	_ "github.com/google/cadvisor/info/v1"
	_ "github.com/googleapis/gnostic"
	_ "github.com/prometheus/common/expfmt"
	_ "github.com/prometheus/prom2json"
	_ "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
)
