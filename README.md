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

### Kubernetes

To install the metrics-agent chart:

    helm install metrics-agent --set apiKey=<yourApiKey> --set clusterName=<yourClusterName> --set uploadRegion=<regionToUploadTo> metrics-agent/metrics-agent -n cloudability --create-namespace

Or to install the metrics-agent chart into an existing cloudability namespace where the api key is stored in an existing kubernetes secret

    helm install metrics-agent --set secretName=<NameOfSecret> --set clusterName=<yourClusterName> --set uploadRegion=<regionToUploadTo> metrics-agent/metrics-agent -n cloudability

### OpenShift

To install the metrics-agent chart:

    helm install metrics-agent --set apiKey=<yourApiKey> --set clusterName=<yourClusterName> --set openShift=true --set uploadRegion=<regionToUploadTo> metrics-agent/metrics-agent -n cloudability --create-namespace

Or to install the metrics-agent chart into an existing cloudability namespace where the api key is stored in an existing kubernetes secret

    helm install metrics-agent --set secretName=<NameOfSecret> --set clusterName=<yourClusterName> --set openShift=true --set uploadRegion=<regionToUploadTo> metrics-agent/metrics-agent -n cloudability



To uninstall the chart:

    helm delete metrics-agent -n cloudability

Notes: 
* `<yourApiKey>` and `<yourUploadRegion>` are obtained through the Containers Insights page in the Cloudability UI.
* Deploying the metrics-agent with Helm creates a kubernetes secret that stores the api-key value. The metrics-agent
deployment then pulls the apikey value from this secret.