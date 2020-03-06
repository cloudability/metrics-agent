module github.com/cloudability/metrics-agent

go 1.14

require (
	github.com/golangci/golangci-lint v1.23.7
	github.com/google/cadvisor v0.36.0
	github.com/sirupsen/logrus v1.4.3-0.20190701143506-07a84ee7412e
	github.com/spf13/cobra v0.0.5
	github.com/spf13/viper v1.6.2
	k8s.io/api v0.17.3
	k8s.io/apimachinery v0.17.3
	k8s.io/client-go v0.17.3
	k8s.io/kubernetes v1.13.0
	sigs.k8s.io/kind v0.7.0
)
