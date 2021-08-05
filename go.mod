module github.com/cloudability/metrics-agent

go 1.16

require (
	github.com/evanphx/json-patch v4.9.0+incompatible // indirect
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/google/go-cmp v0.5.4 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/googleapis/gnostic v0.4.0 // indirect
	github.com/imdario/mergo v0.3.8 // indirect
	github.com/json-iterator/go v1.1.10 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/onsi/ginkgo v1.16.1
	github.com/onsi/gomega v1.11.0
	github.com/pelletier/go-toml v1.6.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.1.3
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/viper v1.7.1
	github.com/stretchr/testify v1.7.0 // indirect
	golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad // indirect
	golang.org/x/net v0.0.0-20210405180319-a5a99cb37ef4 // indirect
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/sys v0.0.0-20210510120138-977fb7262007 // indirect
	golang.org/x/text v0.3.5 // indirect
	golang.org/x/time v0.0.0-20200416051211-89c76fbcd5d1 // indirect
	google.golang.org/appengine v1.6.6 // indirect
	google.golang.org/protobuf v1.25.0 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/api v0.17.3
	k8s.io/apimachinery v0.17.3
	k8s.io/client-go v0.17.3
	k8s.io/kube-openapi v0.0.0-20200121204235-bf4fb3bd569c // indirect
	k8s.io/utils v0.0.0-20200729134348-d5654de09c73 // indirect
	sigs.k8s.io/kind v0.7.0 // indirect
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
