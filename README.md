# metrics-agent

The metrics-agent collects allocation metrics from a Kubernetes cluster system and sends the metrics to cloudability to help you gain visibility, reduce costs, and increase efficiency across your infrastructure.  The agent is designed to run as a container in each cluster inside your orchestration system.

[![Actions Status](https://github.com/cloudability/metrics-agent/workflows/Master/badge.svg)](https://github.com/cloudability/metrics-agent/actions)
[![Actions Status](https://github.com/cloudability/metrics-agent/workflows/Metrics-Agent/badge.svg)](https://github.com/cloudability/metrics-agent/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/cloudability/metrics-agent)](https://goreportcard.com/report/github.com/cloudability/metrics-agent)

## Kubernetes

By default, the agent runs in a namespace named "cloudability" (see options below).  Once deployed, the agent will pull metrics from the Kubernetes API and directly from each node in the cluster it is running in. Additionally it will pull metrics from [Heapster](https://github.com/kubernetes/heapster) if found running in the kube-system namespace.  An example kubernetes deployment can be found [here](deploy/kubernetes/cloudability-metrics-agent.yaml).

Every 10 minutes the metrics agent creates a tarball of the gathered metrics and uploads to an Amazon Web Service S3 bucket. This process requires outbound connections to https://metrics-collector.cloudability.com/, to obtain a pre-signed URL, and https://cldy-cake-pipeline.s3.amazonaws.com/ to upload the data. If the metrics agent is deployed behind a firewall, these addresses should be added to the outbound allow list.

### Supported Versions

#### 1.19 and below

Kubernetes versions 1.19 and below are supported by the metrics agent.

#### 1.18 

Metrics agent versions >= `1.5.0` support Kubernetes 1.18. It is fully backwards compatible with all previous supported versions as well.
Metrics agent versions < `1.5.0` require some manual tweaks on every node in the cluster in order to run on Kubernetes 1.18. 

##### Background

Kubernetes 1.18 [disabled by default the cadvisor endpoints](https://github.com/kubernetes/kubernetes/issues/68522) that the original metrics agent used to collect rich utilization data from the cluster. In order to run metrics agent versions < `1.5.0` on 1.18, you need to [manually enable the cadvisor endpoints on the kubelet](https://kubernetes.io/docs/reference/command-line-tools-reference/kubelet/) via the `--enable-cadvisor-json-endpoints` flag for every node in the cluster.


### Configuration Options

| Environment Variable                    | Description                                                                                                                          |
| --------------------------------------- |:------------------------------------------------------------------------------------------------------------------------------------:|
| CLOUDABILITY_API_KEY                    | Required: Cloudability api key                                                                                                       |
| CLOUDABILITY_CLUSTER_NAME               | Required: The cluster name to be used for the cluster the agent is running in.                                                       |
| CLOUDABILITY_POLL_INTERVAL              | Optional: The interval (Seconds) to poll metrics. Default: 180                                                                       |
| CLOUDABILITY_HEAPSTER_URL               | Optional: Only required if heapster is not deployed as a service in your cluster or is only accessable via a specific URL.           |
| CLOUDABILITY_OUTBOUND_PROXY             | Optional: The URL of an outbound HTTP/HTTPS proxy for the agent to use (eg: http://x.x.x.x:8080). The URL must contain the scheme prefix (http:// or https://)  |
| CLOUDABILITY_OUTBOUND_PROXY_AUTH        | Optional: Basic Authentication credentials to be used with the defined outbound proxy. If your outbound proxy requires basic authentication credentials can be defined in the form username:password |
| CLOUDABILITY_OUTBOUND_PROXY_INSECURE    | Optional: When true, does not verify TLS certificates when using the outbound proxy. Default: False |
| CLOUDABILITY_INSECURE                   | Optional: When true, does not verify certificates when making TLS connections. Default: False|
| CLOUDABILITY_RETRIEVE_NODE_SUMMARIES    | Optional: When true, collects metrics directly from each node in a cluster. When False, uses Heapster as the primary metrics source. Default: True|
| CLOUDABILITY_GET_ALL_CONTAINER_STATS    | Optional: When true, attempts to collect from both the stats/container and metrics/cadvisor endpoints, which may result in a larger metrics payload. When False, only collects first successful endpoint. Default: False|
| CLOUDABILITY_FORCE_KUBE_PROXY           | Optional: When true, forces agent to use the proxy to connect to nodes rather than attempting a direct connection. Default: False|
| CLOUDABILITY_COLLECT_HEAPSTER_EXPORT    | Optional: When true, attempts to collect metrics from Heapster if available. When False, does not collect Heapster metrics. Default: True|
| CLOUDABILITY_COLLECTION_RETRY_LIMIT     | Optional: Number of times agent should attempt to gather metrics from each source upon a failure Default: 1|
| CLOUDABILITY_NAMESPACE                  | Optional: Override the namespace that the agent runs in. It is not recommended to change this as it may negatively affect the agents ability to collect data. Default: `cloudability`|
| CLOUDABILITY_LOG_FORMAT                 | Optional: Format for log output (JSON,PLAIN) Default: PLAIN|
| CLOUDABILITY_LOG_LEVEL                  | Optional: Log level to run the agent at (INFO,WARN,DEBUG,TRACE). Default: `INFO`|
| CLOUDABILITY_SCRATCH_DIR                | Optional: Temporary directory that metrics will be written to. If set, must assure that the directory exists and that the user agent UID 1000 has read/write access to the folder. Default: `/tmp`|

```sh

metrics-agent kubernetes --help
Command to collect Kubernetes Metrics

Usage:
  metrics-agent kubernetes [flags]

Flags:
      --api_key string                           Cloudability API Key - required
      --certificate_file string                  The path to a certificate file. - Optional
      --cluster_name string                      Kubernetes Cluster Name - required this must be unique to every cluster.
      --heapster_override_url string             URL to connect to a running heapster instance. - optionally override the discovered Heapster URL.
      --collection_retry_limit uint              Number of times agent should attempt to gather metrics from each source upon a failure (default 1)
  -h, --help                                     help for kubernetes
      --insecure                                 When true, does not verify certificates when making TLS connections. Default: False
      --key_file string                          The path to a key file. - Optional
      --outbound_proxy string                    Outbound HTTP/HTTPS proxy eg: http://x.x.x.x:8080. Must have a scheme prefix (http:// or https://) - Optional
      --outbound_proxy_auth string               Outbound proxy basic authentication credentials. Must defined in the form username:password - Optional
      --outbound_proxy_insecure                  When true, does not verify TLS certificates when using the outbound proxy. Default: False
      --retrieve_node_summaries                  When true, collects metrics directly from each node in a cluster. When False, uses Heapster as the primary metrics source. Default: True
      --get_all_container_stats                  When true, attempts to collect from both the stats/container and metrics/cadvisor endpoints, which may result in a larger metrics payload. Default: False
      --force_kube_proxy                         When true, forces agent to use the proxy to connect to nodes rather than attempting a direct connection. Default: False
      --poll_interval int                        Time, in seconds, to poll the services infrastructure. Default: 180 (default 180)
      --namespace string                         The namespace which the agent runs in. Changing this is not recommended. (default `cloudability`)
Global Flags:
      --log_format string   Format for log output (JSON,PLAIN) (default "PLAIN")
      --log_level string    Log level to run the agent at (INFO,WARN,DEBUG) (default "INFO")
```

## Development

### Dependency management

We're using [go modules](https://github.com/golang/go/wiki/Modules) for Go dependencies.

### Source Code Analysis

We're using [golangci-lint](https://github.com/golangci/golangci-lint) for static source code analysis.

## Contributing code

You'll find information and help on how to contribute code in
[the CONTRIBUTING document](CONTRIBUTING.md) in this repo.


### To Run Locally

You must obtain a valid API Key and export it locally as an environment variable.

```sh
export CLOUDABILITY_API_KEY={your_api_key}
make deploy-local
```

## Local Development

The makefile target _deploy-local_ assumes that you have [docker](https://www.docker.com/community-edition) and kubernetes (with a context: docker-for-desktop) running locally. The target does the following:

- Builds a container with the local project codebase 
- Locally creates a deployment / pod with the local metrics agent container

### Testing
In addition to running all go tests via the make step `make test`,  `make test-e2e-all` runs end to end tests by spinning up a [kind](https://github.com/kubernetes-sigs/kind) cluster, building the metrics agent, deploying it to the reference clusters, then testing the collected data.  The use of kind requires a local docker daemon to be running.
