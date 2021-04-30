# Introduction

This chart uses the Helm package manager to create a Vertica deployment on a Kubernetes cluster.  

# Prerequisites

* Kubernetes 1.19.3+
* Helm 3+

# Installing the Chart

The following commands download and deploy Vertica on the Kubernetes cluster in the default configuration, with the release name `my-release`:
```
$ helm repo add vertica-charts https://vertica.github.io/charts
$ helm repo update
$ helm install my-release vertica-charts/vertica
```
See [Parameters](#parameters) for a list of configuration options that are available during installation.  

# Installing Vertica

Use `kubectl` to set up the configuration directory and install Vertica with the [install_vertica](https://www.vertica.com/docs/10.1.x/HTML/Content/Authoring/InstallationGuide/InstallingVertica/InstallVerticaScript.htm) script.  
    
1. Store your release name, selectors, namespace, hosts, and pod names in shell variables to make the `install_vertica` script more readable:
    ```
    $ RELEASE=my-release
    $ SELECTOR=vertica.com/usage=server,app.kubernetes.io/name=vertica,app.kubernetes.io/instance=$RELEASE
    $ NAMESPACE=my-namespace
    $ ALL_HOSTS=$(kubectl get pods -n $NAMESPACE --selector=$SELECTOR -o=jsonpath='{range .items[*]}{.metadata.name}.{.spec.subdomain},{end}' | sed 's/.$//')
    $ POD_NAME=$(kubectl get pods -n $NAMESPACE --selector=$SELECTOR -o jsonpath="{.items[0].metadata.name}")
    ```
2. Run the `install_vertica` script to install Vertica, and add all of the pods to the Vertica cluster in the `my-release` instance: 
    ```
    $ kubectl exec $POD_NAME -i -n $NAMESPACE -- sudo /opt/vertica/sbin/install_vertica \
        --license /path/to/license.dat \
        --accept-eula \
        --hosts $ALL_HOSTS \
        --dba-user-password-disabled \
        --failure-threshold NONE \
        --no-system-configuration \
        --point-to-point \
        --data-dir /home/dbadmin/local-data/data
    ```
# Creating an Eon Mode Database

Use `kubectl` to create a database in the `my-release` instance using [admintools](https://www.vertica.com/docs/10.1.x/HTML/Content/Authoring/AdministratorsGuide/AdminTools/WritingAdministrationToolsScripts.htm) and the `create_db` option.

1. Store your release name, selectors, namespace, hosts, and pod names in shell variables to make the `install_vertica` script more readable:
    ```
    $ RELEASE=my-release
    $ SELECTOR=vertica.com/usage=server,app.kubernetes.io/name=vertica,app.kubernetes.io/instance=$RELEASE
    $ NAMESPACE=my-namespace
    $ ALL_HOSTS=$(kubectl get pods -n $NAMESPACE --selector=$SELECTOR -o=jsonpath='{range .items[*]}{.metadata.name}.{.spec.subdomain},{end}' | sed 's/.$//')
    $ POD_NAME=$(kubectl get pods -n $NAMESPACE --selector=$SELECTOR -o jsonpath="{.items[0].metadata.name}")
    ```

2. Create a configuration file called `auth_params.conf`. This file contains your S3 credentials, including your access key, secret key, and S3 endpoint. In the following command, replace values enclosed in angle brackets (<>) with values for your environment:
    ```
    $ cat <<EOF | tee auth_params.conf
    awsauth = <access-key>:<secret-key>
    awsendpoint = <endpoint-ip>:<port-number>
    awsenablehttps = <1 for https and 0 for http>
    EOF
    ```
3. Copy auth_params.conf to the ```/home/dbadmin``` directory in a pod:
    ```
    $ kubectl -n $NAMESPACE cp auth_params.conf $POD_NAME:/home/dbadmin/auth_params.conf
    ```

4. Use admintools with the create_db option to create a database on all of the pods. For this command to execute successfully, the S3 endpoint must be up and running, and the S3 bucket must exist in the endpoint. Replace values enclosed in angle brackets (<>) with values for your environment:
    ```
    $ kubectl -n $NAMESPACE exec -i $POD_NAME -- /opt/vertica/bin/admintools \
      -t create_db \
      --hosts=$ALL_HOSTS \
      --communal-storage-location=s3://<bucket-name> \
      -x /home/dbadmin/auth_params.conf \
      --shard-count=12 \
      --depot-path=/home/dbadmin/local-data/depot \
      --database <database-name>
    ```


# Uninstalling the Chart

The following command removes all of the Kubernetes components associated with the chart and deletes the `my-release` deployment:

```
$ helm delete my-release
```

# Parameters

The following table describes the Vertica helm chart's configurable parameters and their defaults:

| Parameter Name | Description | Default Value |
|-------------|-------------|---------------|
| image.pullPolicy | Determines how often Kubernetes pulls the specified image. For details, see [Updating Images](https://kubernetes.io/docs/concepts/containers/images/#updating-images) in the Kubernetes documentation. | IfNotPresent
| image.pullSecrets | A list of secrets consisting of credentials for authentication to a private container repository. For details, see [Specifying imagePullSecrets](https://kubernetes.io/docs/concepts/containers/images/#specifying-imagepullsecrets-on-a-pod) in the Kubernetes documentation. | Not set |
| image.server.name | The container that runs the Vertica server. If the container is hosted in a private container repository, this name must include the path to that repository. | vertica-k8s |
| image.server.tag | The tag associated with the server container. | 10.1.1-0 |
| labels | Custom labels added to all of the objects that the helm chart creates. | Not set
| annotations | Custom annotations added to all of the objects that the helm chart creates. | Not set
| db.superuserPasswordSecret | The secret that contains the database superuser password. To use a password, you must create this secret before deployment. <br><br> The following command creates the password: <br> ```kubectl create secret generic su-passwd --from-literal=password=sup3rs3cr3t```<br><br> The corresponding change in the YAML file is:<br> <pre>db:<br>  superuserSecretPassword:<br>    name: su-passwd<br>    key: password</pre>**If you do not create this secret before deployment, there is no password authentication for the database.** | Not set |
| db.licenseSecret | The name of a secret that contains the contents your Vertica license file. The secret must share a namespace with the StatefulSet. Each of the keys in the secret is mounted as a file in `/home/dbadmin/licensing/mnt`. Because the install is manual, you must reference these mounted files during the install.<br>The following command creates a secret named vertica-license:<br>```kubectl create secret generic vertica-license --from-file=license.dat=/path/to/license.dat```<br><br>The corresponding change in the YAML file is:<br><pre>db:<br>  licenseSecret:<br>    name: licenses</pre> | Not set |
| subclusters.defaultsubcluster.replicaCount | The number of replicas in the server StatefulSet. This parameter determines the number of Vertica hosts in the cluster. Changing this number either deletes or schedules new pods.<br><br>**If the number of replicas exceeds 120, you must manually configure Vertica for large cluster support.**  | 1 |
| subclusters.defaultsubcluster.nodeSelector | The [node selector](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector) provides control over which nodes are used to schedule a pod. If this parameter is not set, the node selector is omitted from the pod that is created by the StatefulSet. To set this parameter, provide a list of key/value pairs.<br><br>For example, to schedule the server pods only at nodes that have two key/value pairs, include the following: <br><pre>subclusters:<br>  defaultsubcluster:<br>    nodeSelector:<br>      disktype: ssd<br>      region: us-east</pre> | Not set |
| subclusters.defaultsubcluster.affinity | Like nodeSelector, [affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity) allows you to constrain the pod only to specific nodes. The affinity setting is more expressive than nodeSelector. If this parameter is not set, then no affinity setting is used with the pods.<br><br> The following example uses affinity to ensure a node does not serve two server pods:<br><pre>subcluster:<br>  defaultsubcluster:<br>    affinity:<br>      podAntiAffinity:<br>        requiredDuringSchedulingIgnoredDuringExecution:<br>        - labelSelector:<br>            matchExpressions:<br>            - key: app.kubernetes.io/name<br>            operator: In<br>            values:<br>            - vertica<br>          topologyKey: "kubernetes.io/hostname"<br>| Not set |
| subclusters.defaultsubcluster.priorityClassName | The [priority class name](https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass) assigned to pods in the StatefulSet.  This affects where the pod gets scheduled. | Not set. |
| subclusters.defaultsubcluster.tolerations | Any [tolerations and taints](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) used to influence where a pod is scheduled. | Not set |
| db.storage.local.storageClass | Defines the name of the storageClass used for the local data volume that stores the local catalog, depot, and configuration files. By default, this parameter is not set.<br><br>Set this parameter when creating the persistent volume claim (PVC). The PVC in the Vertica default configuration uses the default StoragClass set in Kubernetes. | Not set |
| db.storage.local.size | The minimum size of the local data volume when selecting a persistent volume (PV). | 500Gi |
| subclusters.defaultsubcluster.resources.\* | This defines the [resource requests and limits](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/) for pods in the server StatefulSet. <br><br> - `resources.limits` sets the maximum amount of CPU and memory that each server pod can consume. <br> - `resources.requests` defines the minimum CPU and memory available on a node to schedule a pod. <br><br>As a best practice, set the `requests` and `limits` to equal values to ensure the pods are assigned to the [guaranteed QoS class](https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/). This reduces the possibility that pods are chosen by the OOM killer.<br><br> Vertica selects defaults based recommendations described in [Recommendations for Sizing Vertica Nodes and Clusters](https://www.vertica.com/kb/Recommendations-for-Sizing-Vertica-Nodes-and-Clusters/Content/Hardware/Recommendations-for-Sizing-Vertica-Nodes-and-Clusters.htm) in the Vertica knowledge base. These limits are for testing environments - they are not suitable for production workloads. We will flow the resource limits down the pods with the downward API so that the request pools are sized accordingly. <br><br> Below is a sample configuration for this setting. It ensures that each Vertica host is scheduled on Kubernetes nodes that have 96GB of free RAM, and at least 32 CPUs. `limits` and `requests` are equal to ensure that the pods are assigned to the guaranteed QoS class.<br><pre>subclusters:<br>  defaultsubcluster:<br>    resources:<br>      requests:<br>        memory: "96Gi"<br>        cpu: 32<br>      limits:<br>        memory: "96Gi"<br>        cpu: 32</pre> | <pre>requests:<br>  cpu: 4<br>limits:<br>  memory: 16Gi</pre><br>_This limit was picked based on the published specs.  It is meant to be a bare minimum to try out Vertica and by no means suitable for production._ |
| subclusters.defaultsubcluster.service.type | Identifies the [type of Kubernetes service](https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types) to use for external client connectivity.  The default is type is `ClusterIP`, which sets a stable IP and port that is accessible only from within the Kubernetes cluster. Depending on the service type, you might need to set additional parameters, including `service.nodePort` or `service.externalIPs`. | ClusterIP |
| subclusters.defaultsubcluster.service.nodePort | When `subclusters.defaultsubcluster.service.type` is set to `NodePort`, this parameter enables you to define the port that is opened at each node. The port must be within the defined range allocated by the control plane (ports 30000-32767).If you are using `NodePort` and omit the port number, Kubernetes assigns the port automatically. | Not set |
| subclusters.defaultsubcluster.service.externalIPs | Enables the service object to attach to a specified [external IP](https://kubernetes.io/docs/concepts/services-networking/service/#external-ips).  If not set, the external IP is empty in the service object. | Not set |

# Accessing from Outside the Cluster

There is a load balanced service object that provides client access to Vertica. By default, it is created as type `ClusterIP`, which allows access from within the Kubernetes cluster only. To change the type and enable external access to the cluster, change the `subclusters.defaultsubcluster.service.type` parameter.

The following command returns the name of the service object:

```
$ kubectl get svc --selector=vertica.com/svc-type=external,app.kubernetes.io/name=vertica,app.kubernetes.io/instance=my-release
```

# Pod Restart

After the database is created, we attempt to restart Vertica each time a pod is restarted. Vertica is successfully restarted if the cluster does not lose quorum (> 50% of the Vertica nodes). The database is updated with the pod's new IP address automatically. If quorum is lost, then you must manually restart the database.

# Persistence

Each pod uses a PV to store local data. This is mounted in the container at `/home/dbadmin/local-data`. This directory contains the following subdirectories:

* **/home/dbadmin/local-data/data/**: Stores the local catalogs. When creating the database, use this as the data directory.
* **/home/dbadmin/local-data/depot/**: When creating the database, use this as the depot directory.  
* **/home/dbadmin/local-data/config/**: This is a symlink to `/opt/vertica/config`. This allows the contents of the configuration directory to persist between restarts.
* **/home/dbadmin/local-data/log/**: This is a symlink to `/opt/vertica/log`.  This allows the log files to persist between restarts.

By default, the PV is selected using the default storage class. Use the `db.storage.local.storageClass` parameter to select a specific storage class.

# Additional Details

For additional details on the internals of Vertica, see the official [Vertica Documentation](https://www.vertica.com/docs/10.1.x/HTML/Content/Home.htm).

# Developers
For details on setting up an environment to develop and run tests, see the [developer instructions](DEVELOPER.md) and the [Vertica Integrator's Guide](https://verticaintegratorsguide.org/wiki/index.php?title=Main_Page).

# Licensing

vertica-kubernetes is open source code and is under the [Apache 2.0 license](https://github.com/vertica/vertica-kubernetes/blob/main/LICENSE), but it requires that you install the Vertica server RPM. If you do not have a Vertica server RPM, you can use the free [Vertica Community Edition (CE) server RPM](https://www.vertica.com/download/vertica/community-edition/community-edition-10-1-0/). The Vertica Community Edition server RPM is not an open source project, but it is free with certain limits on capacity. For more information about these limitations, see the [Vertica Community Edition End User License Agreement](https://www.vertica.com/end-user-license-agreement-ce-version/).
