This repository contains the code for a Kubernetes operator that manages Vertica Analytic Database. The operator uses a custom resource definition (CRD) to automate administrative tasks for a Vertica database.

To deploy the operator and a Kubernetes cluster in a local test environment that requires minimal resources, see [DEVELOPER](https://github.com/vertica/vertica-kubernetes/blob/main/DEVELOPER.md).

# Prerequisites

- Resources to deploy Kubernetes objects
- Kubernetes (version 1.21.1+)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) (version 1.21.1+)  
- [helm](https://helm.sh/docs/intro/install/) (version 3.5.0+)

# Installing the CRD

Vertica extends the Kubernetes API with its [Custom Resource Definition](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/ContainerizedVerticaWithK8s.htm). Install the `CustomResourceDefinition` with a YAML manifest:

```shell
kubectl apply -f https://github.com/vertica/vertica-kubernetes/releases/download/v1.3.1/verticadbs.vertica.com-crd.yaml
```

# Installing the VerticaDB Operator

The [VerticaDB operator](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/Operator.htm) automates tasks and monitors the state of your Vertica on Kubernetes deployment. Vertica provides the following installation options:
- [OperatorHub.io](https://operatorhub.io/operator/verticadb-operator). OperatorHub.io is an operator registry for environments that use the Operator Lifecycle Manager (OLM). For installation instructions, click the **Install** button on the VerticaDB Operator page.
- [Install the Vertica Helm chart](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/HelmChartParams.htm). The Helm chart includes the operator and the admission controller.
 When you install the operator with the Helm charts, you must configure TLS for the admission controller webhook.
 The Vertica Helm chart [installs the CRD](#installing-the-crd) if it is not currently installed.  
 Helm chart installations can use a [sidecar container](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/ContainerizedVerticaWithK8s.htm) to send logs to a file in the Vertica server container for log aggregation. In addition, you can set logging levels with [Helm chart parameters](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/HelmChartParams.htm).
 
For complete installation instructions, see [Installing the VerticaDB Operator](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/InstallOperator.htm).


# Deploying Vertica

After the operator is installed and running, create a Vertica deployment by generating a custom resource (CR). The operator adds watches to the API server so that it is notified whenever a CR in the same namespace changes. 

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

After this manifest is applied, the operator creates the necessary objects in Kubernetes, sets up the config directory in each pod, and creates an Eon Mode database in the communal path.

For a comprehensive custom resource example, see [Creating a Custom Resource](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CreatingCustomResource.htm).


# Configuration Parameters

Use configuration parameters to customize your Vertica on Kubernetes deployment. For a full list of parameters and their definitions, see [Custom Resource Definition Parameters](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CustomResourceDefinitionParams.htm).

# Persistence

A pod is an ephemeral Kubernetes object that requires access to external storage to persist data between life cycles. Vertica on Kubernetes persists data with the following:
- Local volume mounts: Each pod uses a PersistentVolume (PV) to store local data. The PV is mounted in the container at `/home/dbadmin/local-data`, and has specific StorageClass and [storage format type](https://www.vertica.com/docs/latest/HTML/Content/Authoring/SupportedPlatforms/MCandServer.htm) requirements.
- Custom volume mounts: You can add additional volumes to the Vertica server or sidecar utility container filesystem. Custom volume mounts persist data for processes that require access to data that must persist between pod life cycles.

For details about local volume mounts, StorageClass requirements, and custom volume mounts, see [Containerized Vertica on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/ContainerizedVerticaWithK8s.htm).

# Scale Up and Down

Vertica on Kubernetes offers the following two scaling strategies to fine-tune your clusters by workload:  

- For complex, long-running queries, add nodes to an existing subcluster. 
- To increase the throughput of multiple short-term queries (often called "dashboard queries"), improve your cluster's parallelism by adding subclusters.

For complete instructions on scaling subclusters, see [Subclusters on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/SubclustersOperator.htm).


# Client Connections

External clients can target specific subclusters to handle their workload. Each subcluster has its own service object that you can configure to manage client connections. Use the `subclusters[i].serviceName` parameter to name a service object so that you can assign a single service object to one or more subclusters.

By default, the subcluster service object is set to `ClusterIP`, which load balances internal traffic across the pods in the subcluster. To allow connections from outside of the Kubernetes cluster, set the `subclusters[i].serviceType` parameter to `NodePort` or `LoadBalancer`.

For an overview about Vertica on Kubernetes and Service Objects, see [Containerized Vertica on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/ContainerizedVerticaWithK8s.htm).. 

For a detailed implementation example, see [Creating a Custom Resource](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CreatingCustomResource.htm).


# Migrating an Existing Database into Kubernetes
  
The operator can migrate an existing Eon Mode database into Kubernetes. The operator revives the existing database into a set of Kubernetes objects that mimics the setup of the database. Vertica provides `vdb-gen`, a standalone program that you can run against a live database to create the CR.

See [Generating a Custom Resource from an Existing Eon Mode Database](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/GeneratingCR.htm) for detailed steps.


# Vertica License

By default, the CR uses the [Community Edition (CE)](https://www.vertica.com/download/vertica/trial-download/?) license. The CE license limits the number pods in a cluster to 3, and the dataset size to 1TB.

Add your own license to extend your cluster past the CE limits. See [Creating a Custom Resource](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CreatingCustomResource.htm) for details about adding your own license as a Secret in the same namespace as the operator.

# Upgrading your License

Vertica recommends incremental upgrade paths. See [Upgrading Vertica on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/UpgradingWithOperator.htm) for details about Vertica server version upgrades for a custom resource.

# Vertica on Red Hat OpenShift

Vertica supports [Red Hat OpenShift](https://docs.openshift.com/container-platform/4.8/welcome/index.html), a hybrid cloud platform that adds security features and additional support to Kubernetes clusters.

The VerticaDB operator is available for download in the OpenShift OperatorHub. It is compatible with OpenShift versions 4.8 and higher.

OpenShift manages security with [Security Context Constraints](https://docs.openshift.com/container-platform/4.8/authentication/managing-security-context-constraints.html) (SCCs). Vertica provides the `anyuid-extra` SCC to manage Vertica security on OpenShift. In addition, Vertica is compatible with the default `privileged` SCC.


For details about Vertica on OpenShift, see [Red Hat OpenShift Overview](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/OpenShiftK8s.htm).

# Additional Details

For additional details on the internals of Vertica, see the [Vertica Documentation](https://www.vertica.com/docs/latest/HTML/Content/Home.htm).

# Developers
For details about setting up an environment to develop and run tests, see the [developer instructions](DEVELOPER.md).

# Licensing

vertica-kubernetes is open source code and is under the [Apache 2.0 license](https://github.com/vertica/vertica-kubernetes/blob/main/LICENSE), but it requires that you install the Vertica server RPM. If you do not have a Vertica server RPM, you can use the free [Vertica Community Edition (CE) server RPM](https://www.vertica.com/download/vertica/community-edition/community-edition-10-1-0/). The Vertica Community Edition server RPM is not an open source project, but it is free with certain limits on capacity. For more information about these limitations, see the [Vertica Community Edition End User License Agreement](https://www.vertica.com/end-user-license-agreement-ce-version/).
