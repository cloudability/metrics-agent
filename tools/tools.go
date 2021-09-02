// +build tools

package tools

import (
	// linter pinned here so `go mod tidy` can be run without removing this dependency
	// used for `make lint` cmd
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"

	_ "github.com/googleapis/gnostic"
	_ "sigs.k8s.io/kind"
)
