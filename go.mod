module github.com/cloudability/metrics-agent

go 1.16

require (
	github.com/Azure/azure-sdk-for-go v43.0.0+incompatible // indirect
	github.com/GoogleCloudPlatform/k8s-cloud-provider v0.0.0-20200415212048-7901bc822317 // indirect
	github.com/Microsoft/hcsshim v0.8.10-0.20200715222032-5eafd1556990 // indirect
	github.com/containernetworking/cni v0.8.0 // indirect
	github.com/coredns/corefile-migration v1.0.10 // indirect
	github.com/docker/docker v1.4.2-0.20200309214505-aa6a9891b09c // indirect
	github.com/evanphx/json-patch v4.9.0+incompatible // indirect
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/golangci/golangci-lint v1.38.0
	github.com/google/cadvisor v0.36.1-0.20200323171535-8af10c683a96
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/googleapis/gnostic v0.4.0 // indirect
	github.com/ishidawataru/sctp v0.0.0-20190723014705-7c296d48a2b5 // indirect
	github.com/json-iterator/go v1.1.10 // indirect
	github.com/kr/pretty v0.2.0 // indirect
	github.com/moby/ipvs v1.0.1 // indirect
	github.com/onsi/ginkgo v1.14.2
	github.com/onsi/gomega v1.10.4
	github.com/opencontainers/runc v1.0.0-rc91.0.20200707015106-819fcc687efb // indirect
	github.com/prometheus/common v0.10.0
	github.com/prometheus/prom2json v1.3.0
	github.com/sirupsen/logrus v1.8.0
	github.com/spf13/cobra v1.1.3
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/viper v1.7.1
	github.com/stretchr/objx v0.2.0 // indirect
	go.etcd.io/etcd v0.5.0-alpha.5.0.20200819165624-17cef6e3e9d5 // indirect
	golang.org/x/oauth2 v0.0.0-20191202225959-858c2ad4c8b6 // indirect
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0 // indirect
	google.golang.org/api v0.15.1 // indirect
	google.golang.org/appengine v1.6.5 // indirect
	google.golang.org/protobuf v1.24.0 // indirect
	k8s.io/api v0.17.3
	k8s.io/apimachinery v0.17.3
	k8s.io/client-go v0.17.3
	k8s.io/gengo v0.0.0-20200428234225-8167cfdcfc14 // indirect
	k8s.io/klog/v2 v2.2.0 // indirect
	k8s.io/kube-openapi v0.0.0-20200121204235-bf4fb3bd569c // indirect
	k8s.io/kubernetes v1.19.0-alpha.1
	k8s.io/system-validators v1.1.2 // indirect
	k8s.io/utils v0.0.0-20200729134348-d5654de09c73 // indirect
	sigs.k8s.io/kind v0.7.0
	sigs.k8s.io/structured-merge-diff/v4 v4.0.1 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)

replace (
	k8s.io/api v0.0.0 => k8s.io/api v0.17.3
	k8s.io/apiextensions-apiserver v0.0.0 => k8s.io/apiextensions-apiserver v0.17.3
	k8s.io/apimachinery v0.0.0 => k8s.io/apimachinery v0.17.3
	k8s.io/apiserver v0.0.0 => k8s.io/apiserver v0.17.3
	k8s.io/cli-runtime v0.0.0 => k8s.io/cli-runtime v0.17.3
	k8s.io/client-go v0.0.0 => k8s.io/client-go v0.17.3
	k8s.io/cloud-provider v0.0.0 => k8s.io/cloud-provider v0.17.3
	k8s.io/cluster-bootstrap v0.0.0 => k8s.io/cluster-bootstrap v0.17.3
	k8s.io/code-generator v0.0.0 => k8s.io/code-generator v0.17.3
	k8s.io/component-base v0.0.0 => k8s.io/component-base v0.17.3
	k8s.io/cri-api v0.0.0 => k8s.io/cri-api v0.17.3
	k8s.io/csi-translation-lib v0.0.0 => k8s.io/csi-translation-lib v0.17.3
	k8s.io/kube-aggregator v0.0.0 => k8s.io/kube-aggregator v0.17.3
	k8s.io/kube-controller-manager v0.0.0 => k8s.io/kube-controller-manager v0.17.3
	k8s.io/kube-proxy v0.0.0 => k8s.io/kube-proxy v0.17.3
	k8s.io/kube-scheduler v0.0.0 => k8s.io/kube-scheduler v0.17.3
	k8s.io/kubectl v0.0.0 => k8s.io/kubectl v0.17.3
	k8s.io/kubelet v0.0.0 => k8s.io/kubelet v0.17.3
	k8s.io/legacy-cloud-providers v0.0.0 => k8s.io/legacy-cloud-providers v0.17.3
	k8s.io/metrics v0.0.0 => k8s.io/metrics v0.17.3
	k8s.io/sample-apiserver v0.0.0 => k8s.io/sample-apiserver v0.17.3
)
