This repository has the code for a Kubernetes operator that manages Vertica Analytic Database.  It automates some of the administrative tasks of managing a Vertica database.  The operator manages this through a new custom resource definition (CRD) designed specifically for Vertica.

# Prerequisites

- Kubernetes (version 1.19.3+)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) (version 1.19.3+).  
- [helm](https://helm.sh/docs/intro/install/) (version 3.5.0+)

# Installing the Operator

***The instructions for installing the operator are intended for use when the image and helm chart are publicly hosted.  This will not happen until the end of July 2021.  Until then, refer to the [developer instructions](DEVELOPER.md) to compile the operator and package it in a container.***<br>

You need to install the CRD and deploy the operator before it can manage a Vertica database.  We provide a helm chart to handle this.  Run the following command to download and install the chart.

```
$ helm repo add vertica-charts https://vertica.github.io/charts
$ helm repo update
$ helm install vdb-op vertica-charts/verticadb-operator
```

Only one instance of the chart can be installed in a namespace.  The operator will only monitor CRs that were defined in the namespace it is deployed in.


# Installing the Webhook

***The instructions for installing the webhook are intended for use when the image and helm chart are publicly hosted.  This will not happen until the end of July 2021.  Until then, refer to the [developer instructions](DEVELOPER.md) to compile the webhook and package it in a container.***<br>

A separate install is needed to install a webhook for an admission controller.  An [admission controller](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) is a REST endpoint that you setup within Kubernetes that will verify proposed changes to the custom resource are allowed.  Running with the admission controller is optional, but it is highly encouraged as it will prevent simple errors being made when modifying the custom resource.

Kubernetes requires that the webhook accept TLS certificates.  So a certificate must be setup prior to installing the webhook.  We have choosen to use [cert-manager](https://cert-manager.io/docs/) for that.  You can install the cert-manager with a single `kubectl apply`.

```
$ kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.3.1/cert-manager.yaml
```

It can takes a few minutes to get the cert-manager ready.  There are some [documented steps](https://cert-manager.io/docs/installation/kubernetes/#verifying-the-installation) on how to verify the install is complete.

Then, install the webhook.

```
$ helm repo add vertica-charts https://vertica.github.io/charts
$ helm repo update
$ helm install vdb-webhook vertica-charts/verticadb-webhook
```

The webhook is cluster-scoped. It will be installed into only one namespace and will be used by operators installed in any namespaces.

# Deploying Vertica

Once the operator is installed and running, a Vertica deployment is created by generating a custom resource (CR).  The operator adds watches to the API server so that it gets notified whenever a CR in the same namespace changes. 

Here is the simplest configuration that the CR needs.

```
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  name: vertica-sample
spec:
  communal:
    path: "s3://nimbusdb/mspilchen"
    endpoint: http://minio
    credentialSecret: s3-creds    
  subclusters:
    - name: defaultsubcluster
      size: 3
```

The `communal.path` must be a bucket that already exists and is empty.  The `communal.endpoint` is the location that serves the bucket.  And the `communal.credentialSecre` is a secret in the same namespace that has the access key and secret to authenticate the endpoint.  The secret must have keys with names `accesskey` and `secretkey`.

You must specify at least one subcluster and it must be have a name.  If the size is omitted, it defaults to 3.

Once this manifest is applied, the operator will create the necessary objects in Kubernetes, setup the config directory in each pod and create an EON database in the communal path.

There are many parameters available to fine tune the deployment.  The spec above was intentionally minimal to illustrate how easy is it to setup a new Vertica deployment.  See the [Parameters](#Parameters) section below for a complete list.

# Vertica License

By default, we use the CE license if no license is given.  The CE license limits the number pods that you can have in a cluster to 3 and the dataset size to 1TB.  For enterprise customers, you can use your own license to extend the cluster past these limits.

To use your own license, it needs to be added to a secret.  The secret must be in the same namespace that the operator is deployed.  An example of copying the license into a secret is given with the command below.

```
$ kubectl create secret generic license –from-file=license.key=/path/to/license.key
```

You then specify the name of the secret in the CR by populating the `licenseSecret` field.

```
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  name: vertica-sample
spec:
  licenseSecret: license
  communal:
    path: "s3://nimbusdb/mspilchen"
    endpoint: http://minio
    credentialSecret: s3-creds    
  subclusters:
    - name: defaultsubcluster
      size: 3
```

The license will be automatically installed if it is set when the CR was initially created.  If it was added later, then you will need to manually install the license through admintools.  When a license secret is specified, the contents of the secret are mounted as files in `/home/dbadmin/licensing/mnt`.  For instance, the secret that we created above, when set in the CR will have the following directory in each of the pods.  

```
$ [dbadmin@demo-sc1-0 ~]$ ls /home/dbadmin/licensing/mnt
license.key
```

# Scale Up/Down

We offer two strategies for scaling, each one improves different types of performance.  

- To increase the performance of complex, long-running queries, add nodes to an existing subcluster. 
- To increase the throughput of multiple short-term queries (often called "dashboard queries"), improve your cluster's parallelism by adding additional subclusters.

All the subclusters are enumerated in the CR in a list format, each have their own size.  To scale up, you simply add a new subcluster to the CR or increase its size. 
Here is a sample CRD that defines three subclusters named 'main', 'analytics', and 'ml'.

```
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

Once the operator has finished reconciling this CR, we will have a statefulset for each subcluster, a service object for each subcluster and a combined total of 10 pods.

Scale down simply involves modifying the CR by removing a subclusters from the list element or shrinking their size.  We have an admission webhook to prevent invalid state transitions, such as 0 subclusters or only secondary subclusters.

The operator will automatically handle rebalancing of the shards whenever subclusters are added or removed.

# Client Connectivity

Each subcluster will have a service object for client connections.  The service will load balance across the pods in the subcluster.  Clients will connect to the Vertica cluster through one of the subcluster service objects, which one will depend on what subcluster they are targeting.
For instance, suppose we had the following CR:

```
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

We will have the following kubernetes objects:

```
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

Each service object has their own full qualified domain name.  The naming convention for each service object is: `<vdbName>-<subclusterName>`.  So, clients can direct their connections to: verticadb-sample-analytics, verticadb-sample-defaultsubcluster, or verticadb-sample-ml.  The actual pod that the client connects with is load balanced.

There is a headless service object listed above – its name matches the name of the VerticaDB.  That object is not intended for client connectivity.  It only exists to provide DNS name resolution for individual pods. .

All of the service objects listed above are of type ClusterIP.  This does load balancing for connections within the Kubernetes cluster.  This is the default service type.  You can specify NodePort or LoadBalancer with the `subclusters[i].serviceType` parameter if you want to allow connections from outside of the Kubernetes cluster.

# Existing Databases
  
We allow existing databases to be migrated into Kubernetes.  To do this the operator will revive an existing database into a set of Kubernetes objects that mimics the setup of the database.  To make this migration easier, we are providing a standalone program that you can run against a live database to create the CR.  Here are the steps you can follow to migrate your database with this tool.
  
1.	Run the program that will generate a CR based on the current database.  Below is a sample invocation of the command.  It will connect to the database named vertdb at 10.44.10.1 using the superuser password *secret*.  The output of the CR will be written to stdout, so we redirect it to *vdb.yaml* to preserve the output.  The *--name* parameter indicates that the new VerticaDB object is called *mydb*, but if this is omitted a name will be auto generated.<br><pre>$ vdb-gen --password secret --name mydb 10.44.10.1 vertdb > vdb.yaml</pre>
2.  Use `admintools -t stop_db` to stop the database that currently exists
3.	Apply the manifest that was generated by the CR generator.<br><pre>$ kubectl apply -f vdb.yaml<br>verticadb.vertica.com/mydb created<br></pre>
4.	Wait for the operator to do its work.  It will construct the statefulset's, install vertica at each pod and run revive.  Each of these steps will generate events in kubectl.  You can use the describe command to see the events for the verticadb.<br><pre>$ kubectl describe vdb mydb<br></pre>

# Upgrade

The [documented steps](https://www.vertica.com/docs/10.1.x/HTML/Content/Authoring/InstallationGuide/Upgrade/RunningUpgradeScript.htm) to upgrade Vertica are as follows:
1.	Stop the entire cluster.
2.	Update the RPM at each host.
3.	Start the cluster.
  
This model does not fit nicely in a Kubernetes environment – step 1 will conflict with the operator as its main job is to ensure Vertica is always running.  The idiomatic way to upgrade in Kubernetes is a rolling update model.  A new image container is *rolled* out to the cluster such that an any given time some pods are running the old version, and some are running the new version.  However, Vertica does not support a cluster running mixed releases.  

To upgrade a Vertica cluster in Kubernetes, we will follow the document steps above.  We have a special parameter in the CR will force the operator to skip monitoring of the Vertica process.  This will allow us to do cluster wide operations like stopping the cluster without the operator interfering.

The process to upgrade Vertica would then be the following steps:
1.	Set `autoRestartVertica` to false in the CRD – this tells the operator to avoid checking if the Vertica process is running.
```
$ kubectl patch verticadb vert-cluster --type=merge --patch '{"spec": {"autoRestartVertica": false}}'
```
2.	Wait for the operator to acknowledge this state change.
```
$ kubectl wait --for=condition=AutoRestartVertica=False vdb/vert-cluster –-timeout=180s
```

3.	Stop the entire cluster since we cannot allow for mixed Vertica versions:
```
$ kubectl exec vert-cluster-sc1-0 -- admintools -t stop_db -F -d vertdb
```
4.	Update the container tag in the CR. 
```
$ kubectl patch verticadb vert-cluster --type=merge --patch '{"spec": {"image": "verticadocker/vertica-k8s:11.1.1-0"}}'
```
5.	Delete the pods so that they pickup the new image.  We can simply delete the statefulset’s since the operator will regenerate those.
```
$ kubectl delete statefulset -l app.kubernetes.io/instance=vert-cluster -–cascade=forground
```
6.	Enable `autoRestartVertica` to give control back to the operator to restart.
```
$ kubectl patch verticadb vert-cluster --type=merge --patch '{"spec": {"autoRestartVertica": true}}'
```
7.	Wait for the operator to bring everything back up.
```
$ kubectl wait --for=condition=Ready=True pod -l app.kubernetes.io/instance=vert-cluster –-timeout=600s
```

To minimize the number of errors, we will validate that the image can only change when `autoRestartVertica` is false.  We have automated the above steps and included them in a script that you can run.  You can find the script in `scripts/upgrade-vertica.sh`.


# Persistence

Each pod uses a PV to store local data. The PV is mounted in the container at `/home/dbadmin/local-data`. You must set permissions on the PV mount to 0777, or you get a "Permissions Denied" error when the container starts. If the PV was dynamically provisioned, you might need to manually change permissions with `chmod` after it is created.

The local-data directory contains the following subdirectories:

* /home/dbadmin/local-data/*\<uid\>*/data/: Stores the local catalogs and any temporary files.  There is a symlink to this path to the `local.dataPat`h parameter (defaults to /data).
* /home/dbadmin/local-data/*\<uid\>*/depot/: Stores the depot for the local node.  There is a symlink to this path to the `local.depotPath` parameter (defaults to /depot). 
* /home/dbadmin/local-data/*\<uid\>*/config/: This is a symlink to `/opt/vertica/config`. This allows the contents of the configuration directory to persist between restarts.
* /home/dbadmin/local-data/*\<uid\>*/log/: This is a symlink to `/opt/vertica/log`.  This allows the log files to persist between restarts.

Where *\<uid\>* is the unique ID of the VerticaDB resource that is assigned by Kubernetes.
  
By default, the PV is selected using the default storage class. Use the `local.storageClass` parameter to select a specific storage class.

# Parameters
  
The following table describes each configurable parameter in the VerticaDB CRD and their defaults:

| Parameter Name | Description | Default Value |
|-------------|-------------|---------------|
| imagePullPolicy | Determines how often Kubernetes pulls the specified image. For details, see [Updating Images](https://kubernetes.io/docs/concepts/containers/images/#updating-images) in the Kubernetes documentation. | If the image tag ends with latest, we use Always.  Otherwise we use IfNotPresent
| imagePullSecrets | A list of secrets consisting of credentials for authentication to a private container repository. For details, see [Specifying imagePullSecrets](https://kubernetes.io/docs/concepts/containers/images/#specifying-imagepullsecrets-on-a-pod) in the Kubernetes documentation. | Not set |
| image | The name of the container that runs the server.  If hosting the containers in a private container repository this name must include the path to that repository.  Vertica doesn't allow communications between nodes running different versions.  So this will only be allowed to change if autoRestartVertica is disabled.| verticadocker/vertica-k8s:11.0.0-0-minimal |
| labels | Custom labels added to all of the objects that the operator creates. | Not set
| annotations | Custom annotations added to all of the objects that the operator creates. | Not set
| autoRestartVertica | State to indicate whether the operator will restart vertica if the process is not running.  Under normal circumstances this is set to true.  The purpose of this is to allow maintenance window, such as an upgrade, without the operator interfering. | true
| dbName | The name to use for the database.  When `initPolicy` is *Revive*, this must match the name of the database that used when it was originally created. | vertdb
| shardCount | The number of shards to create in the database.  This cannot be updated once the CR is created. | 12
| superuserPasswordSecret | A name of the secret that contains the password for the database's superuser.  The secret must be in the same namespace as the CR.  If this is not set, then we assume no such password is set for the database.  If this is set, it is up the user to create this secret before deployment.  The secret must have a key named password.<br><br> The following command creates the password: <br> ```kubectl create secret generic su-passwd --from-literal=password=sup3rs3cr3t```<br><br> The corresponding change in the CR is:<br> <pre>db:<br>  superuserSecretPassword: su-passwd<br> </pre>| Not set |
| licenseSecret | The name of a secret that contains the contents of license files.  The secret must be in the same namespace as the CR.  Each of the keys in the secret will be mounted as files in `/home/dbadmin/licensing/mnt`.  The operator automatically installs the first license, in alphabetical order, if it was set when the CR was created.  | Not set, which implies the CE license will be used |
| initPolicy | Specifies how to initialize the database in Kubernetes.  Available options are: *Create* or *Revive*.  *Create* will force creation of a new database.  *Revive* will initialize the database with the use of the revive command. | Create |
| ignoreClusterLease | Ignore the cluster lease when doing a revive or start_db.  Use this with caution, as ignoring the cluster lease when another system is using the same communal storage will cause corruption. | false
| kSafety | Sets the fault tolerance for the cluster. Allowable values are 0 or 1. 0 is only suitable for test environments because we have no fault tolerance and the cluster can only have between 1 and 3 pods. If set to 1, we have fault tolerance if nodes die and the cluster has a minimum of 3 pods.<br>This value cannot change after the initial creation of the VerticaDB.| 1 |
| reviveOrder | This specifies the order of nodes when doing a revive.  Each entry contains an index to a subcluster, which is an index in `subclusters[i]`, and a pod count of the number of pods include from the subcluster.<br><br>For example, suppose the database you want to revive has the following setup:<br>- v_db_node0001: subcluster A<br>- v_db_node0002: subcluster A<br>- v_db_node0003: subcluster B<br>- v_db_node0004: subcluster A<br>- v_db_node0005: subcluster B<br>- v_db_node0006: subcluster B<br><br>And the `subclusters[]` list is defined as {'A', 'B'}.  The revive order would be:<br>- {subclusterIndex:0, podCount:2}  # 2 pods from subcluster A<br>- {subclusterIndex:1, podCount:1}  # 1 pod from subcluster B<br>- {subclusterIndex:0, podCount:1}  # 1 pod from subcluster A<br>- {subclusterIndex:1, podCount:2}  # 2 pods from subcluster B<br><br>If InitPolicy is not Revive, this field can be ignored.|Not set
| local.storageClass | The local data stores the local catalog, depot and config files.  This defines the name of the storageClass to use for that volume.  This will be set when creating the PVC.  If this is not set, which is the default, means that that the PVC we create will use the default storage class set in Kubernetes.| Not set |
| local.requestSize | The minimum size of the local data volume when picking a PV.| 500Gi |
| local.dataPath | The path inside the container for the local data.  This path may need to be specified if initializing the database with a revive.  When doing a revive, the local paths must match the paths that were used when the database was first created. | /data |
| local.depotPath | The path inside the container that holds the depot.  Similar to local.dataPath, this path may need to be specified if initializing the database with a revive. | /depot |
| communal.path | The path to the communal storage. This must be a s3 bucket. You specify this using the s3:// bucket notation. For example: s3://bucket-name/key-name. The bucket must be created prior to creating the VerticaDB. This field is required and cannot change after creation.  If `initPolicy` is *Create*, then this path must be empty.  If the `initPolicy` is *Revive*, then this path must be non-empty. | Not set |
| communal.endpoint | The URL to the s3 endpoint. The endpoint must be prefaced with `http://` or `https://` to know what protocol to connect with. This field is required and cannot change after creation. | Not set |
| communal.credentialSecret | The name of a secret that contains the credentials to connect to the communal S3 endpoint.  The secret must have the following keys set: <br>- *accesskey*: The access key to use for any S3 request.<br>- *secretkey*: The secret that goes along with the access key.<br><br>For example, you can create your secret with the following command:<br><pre>kubectl create secret generic s3-creds <br>--from-literal=accesskey=accesskey --from-literal=secretkey=secretkey</pre><br>Then you set the the secret name in the CR.<br><pre>communal:<br>  credentialSecret: s3-creds<br></pre> | Not set |
| communal.includeUIDInPath | If true, the operator will include the VerticaDB's UID in the path.  This option exists if you reuse the communal path in the same endpoint as it forces each database path to be unique. | false
| subclusters[i].name | The name of the subcluster.  This is a required parameter.  | Not set |
| subclusters[i].size | The number of pods that the subcluster will have.  This determines the number of Vertica nodes that it will have.  Changing this number will either delete or schedule new pods. <br><br>The minimum size of any subcluster is 1.  If kSafety is 1 the actual minimum may be higher – as you need at least 3 nodes from primary subclusters to satisfy k-safety.<br><br>Note, you must have a valid license to pick a value that causes the size of all subclusters combined to be bigger than 3.  The default license that comes in the vertica container is for the community edition, which can only have up to 3 nodes.  The license can be set with the `licenseSecret` parameter.| 3 |
| subclusters[i].isPrimary | Indicates whether the subcluster is a primary or a secondary.  You must have at least one primary subcluster in the database. | true |
| subclusters[i].nodeSelector | This gives control of what nodes are used to schedule each pod.  If it is not set, the [node selector](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector) is left off the pod that is created by the subcluster.  To set this parameter, provide a list of key/value pairs.<br><br>For example, to schedule the server pods only at nodes that have specific key/value pairs, include the following: <br><pre>subclusters:<br>  - name: sc1<br>    nodeSelector:<br>      disktype: ssd<br>      region: us-east</pre> | Not set |
| subclusters[i].affinity | Like nodeSelector, [affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity) allows you to constrain the pod only to certain pods.  It is more expressive than just using node selectors.  If not set, then no affinity setting will be used with the pods.<br><br> The following example uses affinity to ensure a node does not serve two Vertica pods:<br><pre>subclusters:<br>  - name: sc1<br>    affinity:<br>      podAntiAffinity:<br>        requiredDuringSchedulingIgnoredDuringExecution:<br>        - labelSelector:<br>            matchExpressions:<br>            - key: app.kubernetes.io/name<br>            operator: In<br>            values:<br>            - vertica<br>          topologyKey: "kubernetes.io/hostname"<br>| Not set |
| subclusters[i].priorityClassName | The [priority class name](https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass) assigned to pods in the subclusters StatefulSet.  This affects where the pod gets scheduled. | Not set. |
| subclusters[i].tolerations | Any [tolerations and taints](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) used to influence where a pod is scheduled. | Not set |
| subclusters[i].resources.\* | This defines the [resource requests and limits](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/) for pods in the subcluster. Users can set the limit if there is a maximum amount of CPU and memory that each server pod can consume.  If the request is set, that dictates the minimum amount of CPU and memory the node must have when scheduling the pod.  <br><br>Refer to the [Recommendations for Sizing Vertica Nodes and Clusters](https://www.vertica.com/kb/Recommendations-for-Sizing-Vertica-Nodes-and-Clusters/Content/Hardware/Recommendations-for-Sizing-Vertica-Nodes-and-Clusters.htm) in the Vertica knowledge base to figure out what values you need to set based on your workload.<br><br>It is advisable that the request and limits match as this ensures the pods are assigned to the [guaranteed QoS class](https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/).  This will reduces the chance that pods are chosen by the OOM killer.<br><br>Here is a sample config for this setting.  It limits each Vertica host to be scheduled at Kubernetes nodes that have 96GB of free ram and at least 32 CPUs.  Both the limits and requests are the same to ensure the pods are given the guaranteed QoS class.<br><br><pre>subclusters:<br>  - name: defaultsubcluster<br>    resources:<br>      requests:<br>        memory: "96Gi"<br>        cpu: 32<br>      limits:<br>        memory: "96Gi"<br>        cpu: 32<br></pre>| Not set|
| subclusters[i].serviceType | Identifies the [type of Kubernetes service](https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types) to use for external client connectivity.  The default is type is `ClusterIP`, which sets a stable IP and port that is accessible only from within the Kubernetes cluster. Depending on the service type, you might need to set additional parameters, including `nodePort` or `externalIPs`. | ClusterIP |
| subclusters[i].nodePort | When `subclusters[i].serviceType` is set to `NodePort`, this parameter enables you to define the port that is opened at each node. The port must be within the defined range allocated by the control plane (typically ports 30000-32767).  If you are using `NodePort` and omit the port number, Kubernetes assigns the port automatically. | Not set |
| subclusters[i].externalIPs | Enables the service object to attach to a specified [external IP](https://kubernetes.io/docs/concepts/services-networking/service/#external-ips).  If not set, the external IP is empty in the service object. | Not set |

# Additional Details

For additional details on the internals of Vertica, see the official [Vertica Documentation](https://www.vertica.com/docs/10.1.x/HTML/Content/Home.htm).

# Developers
For details on setting up an environment to develop and run tests, see the [developer instructions](DEVELOPER.md) and the [Vertica Integrator's Guide](https://verticaintegratorsguide.org/wiki/index.php?title=Main_Page).

# Licensing

vertica-kubernetes is open source code and is under the [Apache 2.0 license](https://github.com/vertica/vertica-kubernetes/blob/main/LICENSE), but it requires that you install the Vertica server RPM. If you do not have a Vertica server RPM, you can use the free [Vertica Community Edition (CE) server RPM](https://www.vertica.com/download/vertica/community-edition/community-edition-10-1-0/). The Vertica Community Edition server RPM is not an open source project, but it is free with certain limits on capacity. For more information about these limitations, see the [Vertica Community Edition End User License Agreement](https://www.vertica.com/end-user-license-agreement-ce-version/).



