# metrics-agent

The metrics-agent collects allocation metrics from a Kubernetes cluster system and sends the metrics to cloudability to help you gain visibility, reduce costs, and increase efficiency across your infrastructure.  The agent is designed to run as a container in each cluster inside your orchestration system.

[![Actions Status](https://github.com/cloudability/metrics-agent/workflows/Master/badge.svg)](https://github.com/cloudability/metrics-agent/actions)
[![Actions Status](https://github.com/cloudability/metrics-agent/workflows/Metrics-Agent/badge.svg)](https://github.com/cloudability/metrics-agent/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/cloudability/metrics-agent)](https://goreportcard.com/report/github.com/cloudability/metrics-agent)

## Kubernetes

By default, the agent runs in a namespace named "cloudability" (see options below).  Once deployed, the agent will pull metrics from the Kubernetes API and directly from each node in the cluster it is running in.  An example kubernetes deployment can be found [here](deploy/kubernetes/cloudability-metrics-agent.yaml).

Every 10 minutes the metrics agent creates a tarball of the gathered metrics and uploads to an Amazon Web Service S3 bucket. This process requires outbound connections to https://metrics-collector.cloudability.com/, to obtain a pre-signed URL, and https://apptio*.s3.amazonaws.com/ to upload the data. If the metrics agent is deployed behind a firewall, these addresses should be added to the outbound allow list.

## Supported Versions

### Kubernetes Versions

Kubernetes versions 1.32 and below are supported by the metrics agent on AWS cloud service (EKS), Google Cloud Platform (GKE), Azure cloud services (AKS), and Oracle Cloud (OKE).

### OpenShift Versions

OpenShift versions 4.17 to 4.12 are supported by the metrics agent on ROSA.

#### Architectures

On AWS, both AMD64 and ARM architectures are supported.

### Deploying with Helm

Instructions for deploying the metrics-agent using Helm can be found [here](https://cloudability.github.io/metrics-agent/). For helm versioning this repository follows the [simple 1-1 versioning](https://codefresh.io/docs/docs/new-helm/helm-best-practices/#simple-1-1-versioning) strategy where the chart version is in sync with the actual application.

### Unsupported Configurations

Cloudability Metrics Agent currently does not support Rancher or On Prem clusters.

### Configuration Options

| Environment Variable                               |                                                                                             Description                                                                                              |
|----------------------------------------------------|:----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------:|
| CLOUDABILITY_API_KEY                               |                                                                                    Required: Cloudability api key                                                                                    |
| CLOUDABILITY_CLUSTER_NAME                          |                                           Required: The cluster name to be used for the cluster the agent is running in. Cannot be exclusively whitespace.                                           |
| CLOUDABILITY_POLL_INTERVAL                         |                                                                    Optional: The interval (Seconds) to poll metrics. Default: 180                                                                    |
| CLOUDABILITY_OUTBOUND_PROXY                        |                    Optional: The URL of an outbound HTTP/HTTPS proxy for the agent to use (eg: http://x.x.x.x:8080). The URL must contain the scheme prefix (http:// or https://)                    |
| CLOUDABILITY_OUTBOUND_PROXY_AUTH                   | Optional: Basic Authentication credentials to be used with the defined outbound proxy. If your outbound proxy requires basic authentication credentials can be defined in the form username:password |
| CLOUDABILITY_OUTBOUND_PROXY_INSECURE               |                                                 Optional: When true, does not verify TLS certificates when using the outbound proxy. Default: False                                                  |
| CLOUDABILITY_USE_PROXY_FOR_GETTING_UPLOAD_URL_ONLY |                       Optional: When true, the specified proxy will be set for requests to get upload urls for metrics, but not for actually uploading metrics. Default: False                       |
| CLOUDABILITY_INSECURE                              |                                                    Optional: When true, does not verify certificates when making TLS connections. Default: False                                                     |
| CLOUDABILITY_FORCE_KUBE_PROXY                      |                                  Optional: When true, forces agent to use the proxy to connect to nodes rather than attempting a direct connection. Default: False                                   |
| CLOUDABILITY_COLLECTION_RETRY_LIMIT                |                                             Optional: Number of times agent should attempt to gather metrics from each source upon a failure Default: 1                                              |
| CLOUDABILITY_NAMESPACE                             |        Optional: Override the namespace that the agent runs in. It is not recommended to change this as it may negatively affect the agents ability to collect data. Default: `cloudability`         |
| CLOUDABILITY_LOG_FORMAT                            |                                                                     Optional: Format for log output (JSON,PLAIN) Default: PLAIN                                                                      |
| CLOUDABILITY_LOG_LEVEL                             |                                                           Optional: Log level to run the agent at (INFO,WARN,DEBUG,TRACE). Default: `INFO`                                                           |
| CLOUDABILITY_SCRATCH_DIR                           | Optional: Temporary directory that metrics will be written to. If set, must assure that the directory exists and that the user agent UID 10000 has read/write access to the folder. Default: `/tmp`  |
| CLOUDABILITY_NUMBER_OF_CONCURRENT_NODE_POLLERS     |                                                   Optional: Number of goroutines that are created to poll node metrics in parallel. Default: `100`                                                   |
| CLOUDABILITY_INFORMER_RESYNC_INTERVAL              |                      Optional: Period of time (in hours) that the informers will fully resync the list of running resources. Default: 24 hours. Can be set to 0 to never resync                      |
| CLOUDABILITY_PARSE_METRIC_DATA                     |                                        Optional: When true, core files will be parsed and non-relevant data will be removed prior to upload. Default: `false`                                        |
| CLOUDABILITY_HTTPS_CLIENT_TIMEOUT                  |                   Optional: Amount (in seconds) of time the http client has before timing out requests. Might need to be increased to clusters with large payloads. Default: `60`                    |
| CLOUDABILITY_UPLOAD_REGION                         |          Optional: The region the metrics-agent will upload data to. Default `us-west-2`. Supported values: `us-west-2`, `eu-central-1`, `ap-southeast-2`, `me-central-1`, `us-gov-west-1`           |

```sh

metrics-agent kubernetes --help
Command to collect Kubernetes Metrics

Usage:
  metrics-agent kubernetes [flags]

Flags:
      --api_key string                           Cloudability API Key - required
      --certificate_file string                  The path to a certificate file. - Optional
      --cluster_name string                      Kubernetes Cluster Name - required this must be unique to every cluster.
      --collection_retry_limit uint              Number of times agent should attempt to gather metrics from each source upon a failure (default 1)
  -h, --help                                     help for kubernetes
      --insecure                                 When true, does not verify certificates when making TLS connections. Default: False
      --key_file string                          The path to a key file. - Optional
      --outbound_proxy string                    Outbound HTTP/HTTPS proxy eg: http://x.x.x.x:8080. Must have a scheme prefix (http:// or https://) - Optional
      --outbound_proxy_auth string               Outbound proxy basic authentication credentials. Must defined in the form username:password - Optional
      --outbound_proxy_insecure                  When true, does not verify TLS certificates when using the outbound proxy. Default: False

      --force_kube_proxy                         When true, forces agent to use the proxy to connect to nodes rather than attempting a direct connection. Default: False
      --poll_interval int                        Time, in seconds, to poll the services infrastructure. Default: 180 (default 180)
      --namespace string                         The namespace which the agent runs in. Changing this is not recommended. (default `cloudability`)
      --informer_resync_interval int             The amount of time, in hours, between informer resyncs. (default 24)
      --number_of_concurrent_node_pollers int    The number of goroutines that are created to poll node metrics in parallel. (default `100`)
      --parse_metric_data bool                   When true, core files will be parsed and non-relevant data will be removed prior to upload. (default `false`)
      --https_client_timeout int                 Amount (in seconds) of time the https client has before timing out requests. (default `60`)
      --upload_region                            The region the metrics-agent will upload data to. (default `us-west-2`)
Global Flags:
      --log_format string   Format for log output (JSON,PLAIN) (default "PLAIN")
      --log_level string    Log level to run the agent at (INFO,WARN,DEBUG) (default "INFO")
```

## Deployment of Metrics-Agent

There are two ways to deploy Metrics-agent:

### Deployment using yaml

Cloudability customers can download the deployment yaml directly from Cloudability UI. The downloaded yaml contains default settings including the API key needed to enable the metrics-agent to upload metrics to Cloudability. The customer should change the default settings in the yaml according to their clusters' configuration and security requirements.

The API key is currently configured as an environment variable in the pod as plain text. It's highly recommended to integrate the API key with the customer's own secret manager solution. This could be the CSP's secret manager such as AWS secret manager, GCP secret manager, etc. Please refer to Kubernetes and CSP document for such integration.

### Deployment using helm

Cloudability customers can use helm to deploy the metrics-agent. Please refer to the [Helm Installation Guide](https://cloudability.github.io/metrics-agent/).

## Computing Resources for Metrics Agent

The following recommendation is based on number of nodes in the cluster. It's for references only. The actual required resources depends on a number of factors such as number of nodes, pods, workload, etc. Please adjust the resources depending on your actual usage. By default, the helm installation and manifest file configures the first row (nodes < 100) from the reference table.

| Number of Nodes | CPU Request | CPU Limit | Mem Request |  Mem Limit |
| --- | --- | --- | --- | --- |
| < 100 | 500m | 1000m | 2GBi | 4GBi |
| 100-200 | 1000m | 1500m | 4GBi | 8GBi |
| 200-500 | 1500m | 2000m | 8GBi | 16GBi |
| 500-1000 | 2000m | 3000m | 16GBi | 24GBi |
| 1000+ | 3000m |  | 24GBi |  |

## Networking Requirement for Metrics Agent
The container that hosts the metrics agent should allow HTTPS requests to following endpoints:
- metrics-collector.cloudability.com port 443

The container that hosts the metrics agent should have write access to following Apptio S3 buckets:
- apptio* (bucket prefixed with apptio)

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
