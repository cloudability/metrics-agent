// +build tools

package tools

import (
	// linter pinned here so `go mod tidy` can be run without removing this dependency
	// used for `make lint` cmd
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"

	// e2e test deps pinned here so `go mod tidy` can be run without removing these dependencies
	// used in `make test-e2e-X.XX` and `make test-e2e-all` cmds
	_ "github.com/google/cadvisor/info/v1"
	_ "github.com/googleapis/gnostic"
	_ "github.com/prometheus/common/expfmt"
	_ "github.com/prometheus/prom2json"
	_ "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
	_ "sigs.k8s.io/kind"
)
