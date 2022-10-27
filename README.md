## Deploying the metrics-agent With Helm

[Helm](https://helm.sh) must be installed to use the charts.  Please refer to
Helm's [documentation](https://helm.sh/docs) to get started.

Once Helm has been set up correctly, add the repo as follows:

    helm repo add metrics-agent https://cloudability.github.io/metrics-agent/

If you had already added this repo earlier, run 
    
    helm repo update

to retrieve the latest versions of the packages. You can then run

    helm search repo metrics-agent

to see the charts.

To install the metrics-agent chart:

    helm install metrics-agent --set apiKey=<yourApiKey> --set clusterName=<yourClusterName> metrics-agent/metrics-agent

To install the metrics-agent chart where the api key is stored in a kubernetes secret
    
    kubectl create secret generic metrics-agent-secret --from-literal=CLOUDABILITY_API_KEY=<YourApiKey> -n cloudability
    helm install metrics-agent --set secretName=metrics-agent-secret --set clusterName=<yourClusterName> metrics-agent/metrics-agent

To uninstall the chart:

    helm delete metrics-agent