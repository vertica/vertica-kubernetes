This repository contains the code for a Kubernetes operator that manages Vertica Analytic Database. The operator uses a custom resource definition (CRD) to automate administrative tasks for a Vertica database.

To deploy the operator and a Kubernetes cluster in a local test environment that requires minimal resources, see [DEVELOPER](https://github.com/vertica/vertica-kubernetes/blob/main/DEVELOPER.md).

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

# Installing the VerticaDB Operator

You have the following options when installing the VerticaDB operator:
- [OperatorHub.io](https://operatorhub.io). Download and install the operator using the Install instructions
- Install the Vertica Helm chart. The Helm chart includes the operator and admission controller.
 When you install the operator with the Helm charts, you must configure TLS for the admission controller webhook. For details, see Installing the [VerticaDB Operator](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/InstallOperator.htm).


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

For a detailed custom resource example, see [Creating a Custom Resource](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CreatingCustomResource.htm). For details about each parameter, see [Custom Resource Definition Parameters](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CustomResourceDefinitionParams.htm).



After this manifest is applied, the operator creates the necessary objects in Kubernetes, sets up the config directory in each pod, and creates an Eon Mode database in the communal path.


# Configuration Parameters

The VerticaDB CRD provides configuration parameters so that you can customize your Vertica on Kubernetes deployment. For a full list of parameters and their definitions, see [Custom Resource Definition Parameters](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CustomResourceDefinitionParams.htm).

# Vertica License

By default, we use the [Community Edition (CE)](https://www.vertica.com/download/vertica/trial-download/?) license if no license is provided. The CE license limits the number pods in a cluster to 3, and the dataset size to 1TB. Use your own license to extend the cluster past these limits.

Refer to the following resources for additional license information:
- [Creating a Custom Resource](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CreatingCustomResource.htm) for details about adding your own license as a Secret in the same namespace as the operator.
- [Upgrading Vertic on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/UpgradingWithOperator.htm) for detailsa about Vertica server version upgrades for a custom resource.


# Scale Up/Down

Vertica offers two scaling strategies, where each one improves different types of performance.  

- To increase the performance of complex, long-running queries, add nodes to an existing subcluster. 
- To increase the throughput of multiple short-term queries (often called "dashboard queries"), improve your cluster's parallelism by adding additional subclusters.

For complete instructions on scaling subclusters, client connections, and internal and external workloads, see [Subclusters on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/SubclustersOperator.htm).


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

# Additional Details

For additional details on the internals of Vertica, see the [Vertica Documentation](https://www.vertica.com/docs/11.0.x/HTML/Content/Home.htm).

# Developers
For details about setting up an environment to develop and run tests, see the [developer instructions](DEVELOPER.md).

# Licensing

vertica-kubernetes is open source code and is under the [Apache 2.0 license](https://github.com/vertica/vertica-kubernetes/blob/main/LICENSE), but it requires that you install the Vertica server RPM. If you do not have a Vertica server RPM, you can use the free [Vertica Community Edition (CE) server RPM](https://www.vertica.com/download/vertica/community-edition/community-edition-10-1-0/). The Vertica Community Edition server RPM is not an open source project, but it is free with certain limits on capacity. For more information about these limitations, see the [Vertica Community Edition End User License Agreement](https://www.vertica.com/end-user-license-agreement-ce-version/).
