
## Cloudability Deployment

### Chart Details

This chart will deploy cloudability metrics agent in the cluster. You can either  deploy manually or use a CI tool.  
The following overrides were performed.


```
 override_values:
        key: CLOUDABILITY_API_KEY
        value: ((api-key-in-vault-location))

        key: cluster
        value: <cluster-name>
```
**Important note on override values**

Our strategy was to store the value of the CLOUDABILITY_API_KEY in vault , then pass it as an environment variable stored in a secret. A example of the secret is shown below.  

```
apiVersion: v1
kind: Secret
metadata:
  name: {{ .Values.configuration.appname }}
  labels:
    app: {{ .Values.configuration.appname }}
    chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
    release: {{ .Release.Name }}
    heritage: {{ .Release.Service }}
  namespace: {{ .Values.configuration.name }}
type: Opaque
data:
  CLOUDABILITY_API_KEY: {{ .Values.secrets.CLOUDABILITY_API_KEY | b64enc | quote }}
```

The value of the secret is referenced in the values.yaml file.

```
secrets:
  CLOUDABILITY_API_KEY: ''
```  
This value is deliberately left blank in order not to expose the credentials  and will be passed in at deployment time through a CI tool (or manually). Your deployment yaml file should look like this ;

```
env:
  - name: "CLOUDABILITY_API_KEY"
    valueFrom:
      secretKeyRef:
        name: {{ .Values.configuration.appname }}
        key: CLOUDABILITY_API_KEY
```

The steps above relate to vanilla kubernetes clusters.  However , for GKE clusters , there are additional instructions.

### GKE Additional Instructions.

If you are deploying in a GKE cluster , in addition to the steps above , you  also  have to add a [cluster label](https://cloud.google.com/kubernetes-engine/docs/how-to/creating-managing-labels) with key: gke-cluster and value: the cluster name(s) you set in the form/YAML. This will allow a mapping from the  GKE clusters to line items in the GCP billing file, and allocate costs to the clusters.

The command to label the GKE cluster is shown below.

```
gcloud container clusters update <cluster-name> --update-labels gke-cluster=<cluster-name> --zone=${cluster-zone} --project=${project-containing-clusters}                                                                         
```
For clusters with RBAC installed (1.8.x and greater): Make sure  your account has cluster-admin role before deploying the Metrics Agent. By default, a user account does not have the cluster-admin role.

The  following command can be used to grant a user the cluster-admin role:
```
kubectl create clusterrolebinding username-cluster-admin-binding --clusterrole=cluster-admin --user=username@emailaddress.com"
```








## Configuration

| Parameter                     | Description                                          | Default                           |
| ---------------------------   | -----------------------------------------------------| ----------------------------------|        
|`configuration.replicaNumber`  | Number of replicas for the Deployment                | `1`                               | |`configuration.appname`        | The name of the application                          |`cloudability-metrics-agent`       |                                                                               |`configuration.name`           | default name of objects                              |`cloudability`                     |
| `configuration.pullPolicy`    | The pull policy                                      | `Always`                          |
| `configuration.image`         | The image to pull                                    |`cloudability/metrics-agent:latest`|                                   
| `resources.memoryRequest`     | Memory Request                                       |`128Mi`                            |
| `resources.memoryLimit `      | Memory Limit                                         |`512Mi`                            |
| `resources.cpuRequest`        | Cpu Request                                          |`.1`                               |              | `resources.cpuLimit`          | CPU limit                                            |`.5`                               |
| `cluster.name`                | The name of the cluster                              | `<your-cluster-name>`                  |         
