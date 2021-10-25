This repository contains the code for a Kubernetes operator that manages Vertica Analytic Database. The operator uses a custom resource definition (CRD) to automate administrative tasks for a Vertica database.

To deploy the operator and a Kubernetes cluster in a local test environment that requires minimal resources, see [DEVELOPER](https://github.com/vertica/vertica-kubernetes/blob/readme-11.0SP1/DEVELOPER.md).

# Prerequisites

- Resources to deploy Kubernetes objects
- Kubernetes (version 1.21.1+)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) (version 1.21.1+)  
- [helm](https://helm.sh/docs/intro/install/) (version 3.5.0+)

# Installing the CRD

Install the `CustomResourceDefinition` with a YAML manifest:

```shell
kubectl apply -f https://github.com/vertica/vertica-kubernetes/releases/download/v1.1.0/verticadbs.vertica.com-crd.yaml
```

The operator Helm chart will install the CRD if it is not currently installed.

# TLS Requirements

The operator includes an [admission controller](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/), a REST endpoint within Kubernetes that uses a webhook to verify that proposed changes to the custom resource are allowed. Kubernetes requires that all webhooks accept TLS certificates. Choose one of the following options to manage TLS certificates for your custom resource:

- [cert-manager](https://cert-manager.io/docs/)
- Custom certificates

## cert-manager

cert-manager has built-in support in Kubernetes, and generates and manages your certificates. Install the cert-manager with `kubectl apply`.

```shell
kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.5.3/cert-manager.yaml
```

Installation might take a few minutes. For steps on how to verify that the install is complete, see the [cert-manager documentation](https://cert-manager.io/docs/installation/verify/).

## Custom Certificates

Use custom certificates for more control over your data encryption if your custom resource requires multiple certificates for different client or storage connections. 

For detailed configuration instructions, see [Installing the Helm Charts](http://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/InstallHelmCharts.htm) in the Vertica documentation. For details about the available configuration options, see the [helm parameters](https://github.com/vertica/vertica-kubernetes/blob/main/helm-charts/verticadb-operator/README.md).


# Installing the Operator

Vertica bundles the operator and admission controller in a Helm chart. Run the following commands to download and install the chart:

```shell
helm repo add vertica-charts https://vertica.github.io/charts
helm repo update
helm install vdb-op vertica-charts/verticadb-operator
```

You can install only one instance of the chart in a namespace. The operator monitors CRs that were defined in its namespace only.

The feature gate NamespaceDefaultLabelName is required for the webhook to work. It is enabled by default on Kubernetes 1.21.0. This feature will add a label 'kubernetes.io/metadata.name=nsName' to each namespace, which is needed in order to use the namespaceSelector in the webhook config.  If you are on an older version of Kubernetes, you must manually add the label in order for the webhook to work.


# Deploying Vertica

After the operator is installed and running, create a Vertica deployment by generating a custom resource (CR). The operator adds watches to the API server so that it gets notified whenever a CR in the same namespace changes. 

Launching a new Vertica deployment with the operator is simple. Below is the minimal required CR configuration:

```shell
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  name: vertica-sample
spec:
  communal:
    path: "s3://<bucket-name>/<key-name>"
    endpoint: http://path/to/endpoint
    credentialSecret: s3-creds    
  subclusters:
    - name: defaultsubcluster
      size: 3
```

In the previous example configuration:
- `communal.path` must be a bucket that already exists and is empty.
- `communal.endpoint` is the location that serves the bucket.
- `communal.credentialSecret` is a [Secret](https://kubernetes.io/docs/concepts/configuration/secret/) in the same namespace that has the access key and secret to authenticate the endpoint. The Secret must use keys with names `accesskey` and `secretkey`.
- You must specify at least one subcluster and it must have a `name` value. If `size` is omitted, it is set to 3.

After this manifest is applied, the operator creates the necessary objects in Kubernetes, sets up the config directory in each pod, and creates an Eon Mode database in the communal path.

There are many parameters available to fine-tune the deployment. For a complete list, see [Parameters](#Parameters). For a detailed custom resource example, see [Creating a Custom Resource](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CreatingCustomResource.htm).

# Vertica License

By default, we use the [Community Edition (CE)](https://www.vertica.com/download/vertica/trial-download/?) license if no license is provided. The CE license limits the number pods in a cluster to 3, and the dataset size to 1TB. Use your own license to extend the cluster past these limits.

To use your own license, add it to a Secret in the same namespace as the operator. The following command copies the license into a Secret named `license`:

```shell
kubectl create secret generic license --from-file=license.key=/path/to/license.key
```

Next, specify the name of the Secret in the CR by populating the `licenseSecret` field:

```shell
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  name: vertica-sample
spec:
  licenseSecret: license
  communal:
    path: "s3://<bucket-name>/<key-name>"
    endpoint: http://path/to/endpoint
    credentialSecret: s3-creds   
  subclusters:
    - name: defaultsubcluster
      size: 3
```

The license is installed automatically if it is set when the CR was initially created. If it is added at a later time, then you must install the license manually with admintools.  When a license Secret is specified, the contents of the Secret are mounted as files in `/home/dbadmin/licensing/mnt`. For example, the Secret created in the previous commands has the following directory in each pod created by the CR:

```shell
[dbadmin@demo-sc1-0 ~]$ ls /home/dbadmin/licensing/mnt
license.key
```

# Scale Up/Down

We offer two strategies for scaling, each one improves different types of performance.  

- To increase the performance of complex, long-running queries, add nodes to an existing subcluster. 
- To increase the throughput of multiple short-term queries (often called "dashboard queries"), improve your cluster's parallelism by adding additional subclusters.

Each subcluster is enumerated in the CR in a list format, and each has a `size` property. To scale up, add a new subcluster to the CR, or increase an existing subcluster's size.

Below is a sample CRD that defines three subclusters named 'main', 'analytics', and 'ml':

```shell
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  name: vertica-sample
spec:
  communal:
    path: "s3://nimbusdb/mspilchen"
    credentialSecret: s3-creds
    endpoint: http://minio
  subclusters:
    - name: main
      size: 3
      isPrimary: true
    - name: analytics
      size: 5
      isPrimary: false
    - name: ml
      size: 2
      isPrimary: false
```

After the operator reconciles these changes to the CR, the deployment has a combined total of 10 pods. Each subcluster is deployed as a [StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) with its own [service object](https://kubernetes.io/docs/concepts/services-networking/service/).

To scale down, modify the CR by removing a subcluster from the list element, or decreasing the `size` property. The admission webhook prevents invalid state transitions, such as 0 subclusters or only secondary subclusters.

The operator automatically rebalances database shards whenever subclusters are added or removed.

# Client Connectivity

Each subcluster has a service object for client connections. The service load balances traffic across the pods in the subcluster. Clients connect to the Vertica cluster through one of the subcluster service objects, depending on which subcluster the client is targeting.

For example, suppose we have the following CR:

```shell
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  name: verticadb-sample
  namespace: default
spec:
  communal:
    credentialSecret: s3-auth
    path: s3://nimbusdb/db
    endpoint: http://minio
  subclusters:
  - name: defaultsubcluster
    serviceType: ClusterIP
    size: 3
  - name: ml
    serviceType: ClusterIP
    size: 2
  - name: analytics
    serviceType: ClusterIP
    size: 2
```

This CR uses the following Kubernetes objects:

```shell
NAME                                            TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)             AGE
service/verticadb-sample-analytics              ClusterIP   10.96.21.26     <none>        5433/TCP,5444/TCP   21m
service/verticadb-sample                        ClusterIP   None            <none>        22/TCP              21m
service/verticadb-sample-defaultsubcluster      ClusterIP   10.96.162.159   <none>        5433/TCP,5444/TCP   12h
service/verticadb-sample-ml                     ClusterIP   10.96.71.15     <none>        5433/TCP,5444/TCP   27m
 
NAME                                                  READY   AGE
statefulset.apps/verticadb-sample-analytics           2/2     21m
statefulset.apps/verticadb-sample-defaultsubcluster   3/3     12h
statefulset.apps/verticadb-sample-ml                  2/2     27m
```

Each service object has its own full qualified domain name (FQDN). The naming convention for each service object is `<vdbName>-<subclusterName>`. Clients can direct their connections to: verticadb-sample-analytics, verticadb-sample-defaultsubcluster, or verticadb-sample-ml.  The actual pod that the client connects with is load balanced.

The previous example includes a headless service object named `service/verticadb-sample`, which matches the name of the Vertica database. This object exists to provide DNS name resolution for individual pods, and is not intended for client connectivity.

All of the service objects listed above are of type `ClusterIP`. This is the default service type, and it provides load balancing for connections within the Kubernetes cluster. Use the `subclusters[i].serviceType` parameter to specify NodePort or LoadBalancer to allow connections from outside of the Kubernetes cluster.

# Migrating an Existing Database into Kubernetes
  
The operator can migrate an existing database into Kubernetes. The operator revives an existing database into a set of Kubernetes objects that mimics the setup of the database. We provide `vdb-gen`, a standalone program that you can run against a live database to create the CR.
  
1. The following command runs `vdb-gen` and generates a CR based on the existing database. The output is written to stdout and redirected to a YAML-formatted file:
   ```shell
   $ vdb-gen --password secret --name mydb 10.44.10.1 vertdb > vdb.yaml
   ```
   The previous command uses the following flags and values:
   - `password`: The existing database superuser secret password.
   - `name`: The new VerticaDB CR object,
   - `10.44.10.1`: The IP address of the existing database.
   - `vertdb`: The name of the existing Eon Mode database.
   - `vdb.yaml`: The YAML-formatted file that contains the generated custom resource definition.

2. Use `admintools -t stop_db` to stop the database that currently exists.
3. Apply the manifest that was generated by the CR generator:
   ```shell
   $ kubectl apply -f vdb.yaml
   verticadb.vertica.com/mydb created
   ```

4. Wait for the operator to construct the StatefulSet, install Vertica in each pod, and run revive. Each of these steps generate events in kubectl. You can use the describe command to see the events for the verticadb:
   ```shell
   $ kubectl describe vdb mydb
   ```

# Upgrading Vertica

The idiomatic way to upgrade in Kubernetes is a rolling update model, which means the new image container is *rolled* out to the cluster. At any given time, some pods are running the old version, and some are running the new version.

However, Vertica does not support a cluster running mixed releases. The documented steps to [upgrade Vertica](https://www.vertica.com/docs/11.0.x/HTML/Content/Authoring/InstallationGuide/Upgrade/RunningUpgradeScript.htm) are summarized below:

1.	Stop the entire cluster.
2.	Update the RPM at each host.
3.	Start the cluster.

Vertica on Kubernetes uses the workflow described in the preceding steps. This is triggered whenever the `.spec.image` changes in the CR.  When the operator detects this, it will enter the upgrade mode, logging events of its progress for monitoring purposes.

Upgrade the Vertica server version and use the kubectl command line tool to monitor the progress. The operator indicates when an upgrade is in progress or complete with the UpgradeInProgress status condition.

1. Update the `.spec.image` in the CR.  This can be driven by a helm upgrade if you have the CR in a chart, or a simple patch of the CR:
   ```shell
   $ kubectl patch verticadb vert-cluster --type=merge --patch '{"spec": {"image": "vertica/vertica-k8s:11.1.1-0"}}'
   ```
   
2. Wait for the operator to acknowledge this change and enter the upgrade mode:
   ```shell
   $ kubectl wait --for=condition=UpgradeInProgress=True vdb/vert-cluster –-timeout=180s
   ```
   
3.  Wait for the operator to leave the upgrade mode:
    ```shell
    $ kubectl wait --for=condition=UpgradeInProgress=True vdb/cluster-name –-timeout=180s
    ```
   
You can monitor what part of the upgrade the operator is in by looking at the events it generates.

```shell
$ kubectl describe vdb cluster-name
...<snip>...
Events:
  Type    Reason                   Age    From                Message
  ----    ------                   ----   ----                -------
  Normal  ImageChangeStart         5m10s  verticadb-operator  Vertica server image change has been initiated to 'vertica-k8s:11.0.1-0'
  Normal  ClusterShutdownStarted   5m12s  verticadb-operator  Calling 'admintools -t stop_db'
  Normal  ClusterShutdownSucceeded 4m08s  verticadb-operator  Successfully called 'admintools -t stop_db' and it took 56.22132s
  Normal  ClusterRestartStarted    4m25s  verticadb-operator  Calling 'admintools -t start_db' to restart the cluster
  Normal  ClusterRestartSucceeded  25s    verticadb-operator  Successfully called 'admintools -t start_db' and it took 240s
  Normal  ImageChangeSucceeded     5s     verticadb-operator  Vertica server image change has completed successfully.
```

Vertica recommends that [upgrade paths](https://www.vertica.com/docs/11.0.x/HTML/Content/Authoring/InstallationGuide/Upgrade/UpgradePaths.htm?zoom_highlight=upgrade%20path) be incremental – meaning you upgrade to each intermediate major and minor release.  The operator assumes the images chosen are following this path and doesn't try to validate it.

# Persistence

Each pod uses a PV to store local data. The PV is mounted in the container at `/home/dbadmin/local-data`. 

Select a [recommended storage format type](https://www.vertica.com/docs/11.0.x/HTML/Content/Authoring/SupportedPlatforms/MCandServer.htm) as the `fsType` value for your StorageClass.

The `local-data` directory contains the following subdirectories:

* /home/dbadmin/local-data/*\<uid\>*/data/: Stores the local catalogs and any temporary files. There is a symlink to this path to the `local.dataPath` parameter. **Default**: `/data`.
* /home/dbadmin/local-data/*\<uid\>*/depot/: Stores the depot for the local node. There is a symlink to this path to the `local.depotPath` parameter. **Default**:`/depot`. 
* /home/dbadmin/local-data/*\<uid\>*/config/: This is a symlink to `/opt/vertica/config`. This allows the contents of the configuration directory to persist between restarts.
* /home/dbadmin/local-data/*\<uid\>*/log/: This is a symlink to `/opt/vertica/log`. This allows the log files to persist between restarts.

**NOTE**: *\<uid\>* is the unique ID of the VerticaDB resource that is assigned by Kubernetes.
  
By default, the PV is selected using the default storage class. Use the `local.storageClass` parameter to select a specific storage class.

# Parameters
  
The following table describes each configurable parameter in the VerticaDB CRD and their defaults:

| Parameter Name | Description | Default Value |
|-------------|-------------|---------------|
| annotations | Custom annotations added to all of the objects that the operator creates. | 
| autoRestartVertica | State to indicate whether the operator will restart vertica if the process is not running.<br>Under normal circumstances this is set to true. Set this parameter to false when performing manual maintenance that requires a DOWN database. This prevents the operator from interfering with the database state. | true
| certSecrets | A list of Secrets for custom TLS certificates. The Secrets are mounted in the Vertica container to make custom certificates available.<br>The full path is `/certs/<secret-name>/<key_i>`, where `<secret-name>` is the name provided when you created the Secret, and `<key_i>` is one of the keys in the Secret.<br>If you update the certificate after you add it to a custom resource, the operator updates the value automatically. If you add or delete a certificate, the operator reschedules the pod with the new configuration. |  
| communal.caFile | The mount path in the container filesystem to a CA certificate file that validates HTTPS connections to an S3-compatible endpoint. Typically, the certificate is stored in a Secret and included in `certSecrets`. | |
| communal.credentialSecret | The name of a Secret that contains the credentials to connect to the communal S3 endpoint. The Secret must have the following keys set: <br>- *accesskey*: The access key to use for any S3 request.<br>- *secretkey*: The secret that goes along with the access key.<br><br>For example, you can create your secret with the following command:<br><pre>kubectl create secret generic s3-creds <br>--from-literal=accesskey=accesskey --from-literal=secretkey=secretkey</pre><br>Then you set the the Secret name in the CR:<br><pre>communal:<br>  credentialSecret: s3-creds<br></pre> |  |
| communal.endpoint | The URL to the S3 endpoint. The endpoint must begin with either `http://` or `https://`. This field is required and cannot change after creation. |  |
| communal.includeUIDInPath | When set to true, the operator includes the VerticaDB's UID in the path. This option exists if you reuse the communal path in the same endpoint as it forces each database path to be unique. | false |
| communal.path | The path to the communal storage. This must be a s3 bucket. You specify this using the `s3://` bucket notation. For example: `s3://bucket-name/key-name`. You must create this bucket before creating the VerticaDB. This field is required and cannot change after creation.  If `initPolicy` is *Create*, then this path must be empty.  If the `initPolicy` is *Revive*, then this path must be non-empty. |  |
| communal.region | The geographic region where the S3 bucket is located.  If you do not set the correct region, you might experience a delay before the bootstrap fails because Vertica retries several times before giving up. | us-east-1 |
| dbName | The name to use for the database.  When `initPolicy` is `Revive`, this must match the name of the database that used when it was originally created. | vertdb
| ignoreClusterLease | Ignore the cluster lease when doing a revive or start_db. Use this with caution, as ignoring the cluster lease when another system is using the same communal storage will cause corruption. | false
| image | The name of the container that runs the server.  If hosting the containers in a private container repository, this name must include the path to that repository.  Whenever this changes, the operator treats this as an upgrade and will stop the entire cluster and restart it with the new image. | vertica/vertica-k8s:11.0.0-0-minimal |
| imagePullPolicy | Determines how often Kubernetes pulls the specified image. For details, see [Updating Images](https://kubernetes.io/docs/concepts/containers/images/#updating-images) in the Kubernetes documentation. | If the image tag ends with `latest`, we use `Always`.  Otherwise we use `IfNotPresent`.
| imagePullSecrets | A list of secrets consisting of credentials for authentication to a private container repository. For details, see [Specifying imagePullSecrets](https://kubernetes.io/docs/concepts/containers/images/#specifying-imagepullsecrets-on-a-pod) in the Kubernetes documentation. |  |
| initPolicy | Specifies how to initialize the database in Kubernetes. Available options are: *Create* or *Revive*.  *Create* forces the creation of a new database. *Revive* initializes the database with the use of the revive command. | Create |
| kSafety | Sets the fault tolerance for the cluster. Allowable values are 0 or 1. 0 is only suitable for test environments because we have no fault tolerance and the cluster can only have between 1 and 3 pods. If set to 1, we have fault tolerance if nodes die and the cluster has a minimum of 3 pods.<br>This value cannot change after the initial creation of the VerticaDB.| 1 |
| labels | Custom labels added to all of the objects that the operator creates. | 
| licenseSecret | The name of a Secret that contains the contents of license files. The Secret must be in the same namespace as the CR. Each of the keys in the Secret are mounted as files in `/home/dbadmin/licensing/mnt`. The operator automatically installs the first license, in alphabetical order, if it was set when the CR was created.  | Not set, which implies the CE license will be used |
| local.dataPath | The path inside the container filesystem for the local data. This path might need to be specified if initializing the database with a revive. When doing a revive, the local paths must match the paths that were used when the database was first created. | `/data` |
| local.depotPath | The path inside the container filesystem that holds the depot. This path might need to be specified if initializing the database with a revive. | `/depot` |
| local.requestSize | The minimum size of the local data volume when picking a PV.| 500Gi |
| local.storageClass | The local data stores the local catalog, depot, and config files. This defines the name of the storageClass to use for that volume. This is set when creating the PVC. By default, this parameter is not set. The PVC in the default Vertica configuration uses the default storage class set in Kubernetes.|  |
| reviveOrder | This specifies the order of nodes when doing a revive. Each entry contains an index to a subcluster, which is an index in `subclusters[i]`, and a pod count of the number of pods include from the subcluster.<br><br>For example, suppose the database you want to revive has the following setup:<br>- v_db_node0001: subcluster A<br>- v_db_node0002: subcluster A<br>- v_db_node0003: subcluster B<br>- v_db_node0004: subcluster A<br>- v_db_node0005: subcluster B<br>- v_db_node0006: subcluster B<br><br>And the `subclusters[]` list is defined as {'A', 'B'}.  The revive order would be:<br>- {subclusterIndex:0, podCount:2}  # 2 pods from subcluster A<br>- {subclusterIndex:1, podCount:1}  # 1 pod from subcluster B<br>- {subclusterIndex:0, podCount:1}  # 1 pod from subcluster A<br>- {subclusterIndex:1, podCount:2}  # 2 pods from subcluster B<br><br>If InitPolicy is not Revive, this field can be ignored.|
| restartTimeout | This specifies the timeout, in seconds, to use when calling admintools to restart pods.  If not specified, it defaults 0, which means we will use the admintools default of 20 minutes. | 0 |
| shardCount | The number of shards to create in the database.  This cannot be updated once the CR is created. | 12
| sidecars[] | One or more optional utility containers that complete tasks for the Vertica server container. Each sidecar entry is a fully-formed container spec, similar to the container that you add to a Pod spec.<br>The following example adds a sidecar named vlogger to the custom resource:<br><pre>sidecars:<br>  - name: vlogger<br>    image: image:tag<br>    volumeMounts:<br>      - name: my-custom-vol<br>        mountPath: /path/to/custom-volume</pre><br>`volumeMounts.name` is the name of a custom volume. This value must match `volumes.name` to mount the custom volume in the sidecar container filesystem. See the `volumes` parameter description for additional details.| empty |
| subclusters[i].affinity | Allows you to constrain the pod only to certain pods. It is more expressive than just using node selectors. If not set, then no [affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity) setting will be used with the pods.<br><br> The following example uses affinity to ensure a node does not serve two Vertica pods:<br><pre>subclusters:<br>  - name: sc1<br>    affinity:<br>      podAntiAffinity:<br>        requiredDuringSchedulingIgnoredDuringExecution:<br>        - labelSelector:<br>            matchExpressions:<br>            - key: app.kubernetes.io/name<br>            operator: In<br>            values:<br>            - vertica<br>          topologyKey: "kubernetes.io/hostname"<br>|  |
| subclusters[i].externalIPs | Enables the service object to attach to a specified [external IP](https://kubernetes.io/docs/concepts/services-networking/service/#external-ips).  If not set, the external IP is empty in the service object. |  |
| subclusters[i].isPrimary | Indicates whether the subcluster is a primary or a secondary. Each database must have at least one primary subcluster. | true |
| subclusters[i].name | The name of the subcluster.  This is a required parameter.  |  |
| subclusters[i].nodePort | When `subclusters[i].serviceType` is set to `NodePort`, this parameter enables you to define the port that is opened at each node. The port must be within the defined range allocated by the control plane (typically ports 30000-32767). If you are using `NodePort` and omit the port number, Kubernetes assigns the port automatically. |  |
| subclusters[i].nodeSelector | Provides control over which nodes are used to schedule each pod. If it is not set, the [node selector](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector) is left off the pod that is created by the subcluster. To set this parameter, provide a list of key/value pairs.<br><br>For example, to schedule the server pods only at nodes that have specific key/value pairs, include the following: <br><pre>subclusters:<br>  - name: sc1<br>    nodeSelector:<br>      disktype: ssd<br>      region: us-east</pre> |  |
| subclusters[i].priorityClassName | The [priority class name](https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass) assigned to pods in the subclusters StatefulSet. This affects where the pod gets scheduled. |  |
| subclusters[i].resources.\* | This defines the [resource requests and limits](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/) for pods in the subcluster. Users can set the limit if there is a maximum amount of CPU and memory that each server pod can consume. If the request is set, that dictates the minimum amount of CPU and memory the node must have when scheduling the pod.  <br><br>Refer to the [Recommendations for Sizing Vertica Nodes and Clusters](https://www.vertica.com/kb/Recommendations-for-Sizing-Vertica-Nodes-and-Clusters/Content/Hardware/Recommendations-for-Sizing-Vertica-Nodes-and-Clusters.htm) in the Vertica knowledge base to figure out what values you need to set based on your workload.<br><br>It is advisable that the request and limits match as this ensures the pods are assigned to the [guaranteed QoS class](https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/).  This will reduces the chance that pods are chosen by the OOM killer.<br><br>Here is a sample config for this setting.  It limits each Vertica host to be scheduled at Kubernetes nodes that have 96GB of free ram and at least 32 CPUs.  Both the limits and requests are the same to ensure the pods are given the guaranteed QoS class.<br><br><pre>subclusters:<br>  - name: defaultsubcluster<br>    resources:<br>      requests:<br>        memory: "96Gi"<br>        cpu: 32<br>      limits:<br>        memory: "96Gi"<br>        cpu: 32<br></pre>| |
| subclusters[i].serviceType | Identifies the [type of Kubernetes service](https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types) to use for external client connectivity. The default is type is `ClusterIP`, which sets a stable IP and port that is accessible only from within the Kubernetes cluster. Depending on the service type, you might need to set additional parameters, including `nodePort` or `externalIPs`. | ClusterIP |
| subclusters[i].size | The number of pods in the subcluster. This determines the number of Vertica nodes in the subcluster. Changing this number either deletes or schedules new pods. <br><br>The minimum size of any subcluster is 1. If `kSafety` is 1, the actual minimum may be higher, as you need at least 3 nodes from primary subclusters to satisfy k-safety.<br><br>**NOTE**: You must have a valid license to pick a value that causes the size of all subclusters combined to be bigger than 3. The default license that comes in the Vertica container is for the Cmmunity Edition (CE), which only allows up to 3 nodes. The license can be set with the `licenseSecret` parameter.| 3 |
| subclusters[i].tolerations | Any [tolerations and taints](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) used to influence where a pod is scheduled. |  |
| superuserPasswordSecret | The Secret that contains the database superuser password. The Secret must be in the same namespace as the CR. If this is not set, then we assume no such password is set for the database.  If this is set, it is up the user to create this Secret before deployment. The Secret must have a key named `password`.<br><br> The following command creates the password: <br> ```kubectl create secret generic su-passwd --from-literal=password=sup3rs3cr3t```<br><br> The corresponding change in the CR is:<br> <pre>db:<br>  superuserSecretPassword: su-passwd<br> </pre>|  |
| volumes | List of custom volumes that persist sidecar container data. Each volume element requires a name value and a volume type. `volumes` accepts any Kubernetes volume type.<br>To mount a volume in a sidecar filesystem, `volumes.name` must match the `sidecars[i].volumeMounts.name` value for the associated sidecar element. | |

# Additional Details

For additional details on the internals of Vertica, see the [Vertica Documentation](https://www.vertica.com/docs/11.0.x/HTML/Content/Home.htm).

# Developers
For details about setting up an environment to develop and run tests, see the [developer instructions](DEVELOPER.md).

# Licensing

vertica-kubernetes is open source code and is under the [Apache 2.0 license](https://github.com/vertica/vertica-kubernetes/blob/main/LICENSE), but it requires that you install the Vertica server RPM. If you do not have a Vertica server RPM, you can use the free [Vertica Community Edition (CE) server RPM](https://www.vertica.com/download/vertica/community-edition/community-edition-10-1-0/). The Vertica Community Edition server RPM is not an open source project, but it is free with certain limits on capacity. For more information about these limitations, see the [Vertica Community Edition End User License Agreement](https://www.vertica.com/end-user-license-agreement-ce-version/).
