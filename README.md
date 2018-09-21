# metrics-agent

The metrics-agent collects allocation metrics from a container orchestration system (currently Kubernetes) and sends them to cloudability to help you gain visibility, reduce costs and increase efficiency across your infrastructure.  The agent is designed to run as a docker container in each cluster inside your orchestration system.

[![CircleCI](https://circleci.com/gh/cloudability/metrics-agent/tree/master.svg?style=svg)](https://circleci.com/gh/cloudability/metrics-agent/tree/master)
[![Go Report Card](https://goreportcard.com/badge/github.com/cloudability/metrics-agent)](https://goreportcard.com/report/github.com/cloudability/metrics-agent)

## Kubernetes

The agent requires that it runs in a namespace named "cloudability" with a service account, pulling metrics from the Kubernetes API and [Heapster](https://github.com/kubernetes/heapster).  The agent will attempt to connect to heapster, and if unable to communicate with it, the agent will launch Heapster as a service in the cloudability namespace. An example kubernetes deployment can be found [here](deploy/kubernetes/cloudability-metrics-agent.yaml).

### Configuration Options

| Environment Variable                    | Description                                                                                                                          |
| --------------------------------------- |:------------------------------------------------------------------------------------------------------------------------------------:|
| CLOUDABILITY_API_KEY                    | Required: Cloudability api key                                                                                                       |
| CLOUDABILITY_CLUSTER_NAME               | Required: The cluster name to be used for the cluster the agent is running in.                                                       |
| CLOUDABILITY_POLL_INTERVAL              | Optional: The interval (Seconds) to poll metrics. Default: 300                                                                       |
| CLOUDABILITY_HEAPSTER_URL               | Optional: Only required if heapster is not deployed as a service in your cluster or is only accessable via a specific URL.           |
| CLOUDABILITY_OUTBOUND_PROXY             | Optional: The URL of an outbound HTTP/HTTPS proxy for the agent to use (eg: http://x.x.x.x:8080). The URL must contain the scheme prefix (http:// or https://)  |
| CLOUDABILITY_OUTBOUND_PROXY_AUTH        | Optional: Basic Authentication credentials to be used with the defined outbound proxy. If your outbound proxy requires basic authentication credentials can be defined in the form username:password |
| CLOUDABILITY_OUTBOUND_PROXY_INSECURE        | Optional: When true, does not verify TLS certificates when using the outbound proxy. Default: False |
| CLOUDABILITY_INSECURE        | Optional: When true, does not verify certificates when making TLS connections. Default: False|
| CLOUDABILITY_RETRIEVE_NODE_SUMMARIES        | Optional: When true, collects metrics directly from each node in a cluster. Default: False|

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
  -h, --help                                     help for kubernetes
      --insecure                                 When true, does not verify certificates when making TLS connections. Default: False
      --key_file string                          The path to a key file. - Optional
      --outbound_proxy string                    Outbound HTTP/HTTPS proxy eg: http://x.x.x.x:8080. Must have a scheme prefix (http:// or https://) - Optional
      --outbound_proxy_auth string               Outbound proxy basic authentication credentials. Must defined in the form username:password - Optional
      --outbound_proxy_insecure                  When true, does not verify TLS certificates when using the outbound proxy. Default: False
      --poll_interval int                        Time, in seconds, to poll the services infrastructure. Default: 180 (default 180)
```

## Development

### Dependency management

We're using [dep](https://github.com/golang/dep) to manage our Go dependencies.

### Source Code Analysis

We're using [Go Meta Linter](https://github.com/alecthomas/gometalinter) for static source code analysis.

## Contributing code

You'll find information and help on how to contribute code see
[the contributing doc](CONTRIBUTING.md) of this repo.


### To Run Locally

You must obtain a valid API Key and export it locally as an environment variable.

```sh
export CLOUDABILITY_API_KEY={your_api_key}
make deploy-local
```

## Local Development

The makefile step deploy-local assumes that you have [docker](https://www.docker.com/community-edition) and kubernetes (with a context: docker-for-desktop) running locally. The step does the following:

- Build a container with the local project codebase

- locally deploy a deployment / pod with the local metrics agent container
