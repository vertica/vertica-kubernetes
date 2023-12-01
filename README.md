This repository contains the code for a Kubernetes operator that manages Vertica Analytic Database. The operator uses a custom resource definition (CRD) to automate administrative tasks for a Vertica database.

To deploy the operator and a Kubernetes cluster in a local test environment that requires minimal resources, see [DEVELOPER](https://github.com/vertica/vertica-kubernetes/blob/main/DEVELOPER.md).

# Supported Platforms 

See [Containerized Environments](https://www.vertica.com/docs/latest/HTML/Content/Authoring/SupportedPlatforms/Containers.htm) for supported plaform information, including version, communal storage, and managed services support.

# Prerequisites

- Resources to deploy Kubernetes objects
- Kubernetes (version 1.21.1+)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) (version 1.21.1+)  
- [helm](https://helm.sh/docs/intro/install/) (version 3.5.0+)

# Installing the VerticaDB Operator

The [VerticaDB operator](https://docs.vertica.com/latest/en/containerized/db-operator/) automates tasks and monitors the state of your Vertica on Kubernetes deployment. It uses an admission controller webhook to verify any state changes to resource objects. Install the operator with [OperatorHub.io](#operatorhubio) or [Helm charts](#helm-charts).

## OperatorHub.io

[OperatorHub.io](https://operatorhub.io/) is an operator registry for environments that use the Operator Lifecycle Manager (OLM). For installation instructions, go to the [VerticaDB Operator page](https://operatorhub.io/operator/verticadb-operator) and select the **Install** button.

## Helm Charts

The Vertica [Helm chart](https://docs.vertica.com/latest/en/containerized/db-operator/installing-db-operator/) includes the operator and the admission controller. Additionally, the Helm chart [installs the CRD](#installing-the-crd) if it is not currently installed.  

The Helm chart provides the following customization options:
- Choose how you want to manage TLS for the admission controller webhook. You can have the operator create a self-signed certificate internally (the default), use [cert-manager](https://cert-manager.io/docs/) to create a self-signed certificates, or define custom certificates.
- Leverage [Kubernetes RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) to grant install privileges to service accounts. By default, installing the Helm chart requires cluster administrator privileges.
- Install the operator without the admission controller webhook. Note that not deploying the admission controller might result in invalid state transistions.
- Use a [sidecar container](https://docs.vertica.com/latest/en/containerized/containerized-on-k8s/) to send logs to a file in the Vertica server container for log aggregation. In addition, you can set logging levels with [Helm chart parameters](https://docs.vertica.com/latest/en/containerized/db-operator/helm-chart-parameters/).
 
For complete installation instructions, see [Installing the VerticaDB Operator](https://docs.vertica.com/latest/en/containerized/db-operator/installing-db-operator/).


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

For a comprehensive custom resource example, see [VerticaDB CR](https://docs.vertica.com/latest/en/containerized/custom-resource-definitions/verticadb/).


# Configuration Parameters

Use configuration parameters to customize your Vertica on Kubernetes deployment. For a full list of parameters and their definitions, see [Custom Resource Definition Parameters](https://docs.vertica.com/latest/en/containerized/custom-resource-definition-parameters/).

# Persistence

A pod is an ephemeral Kubernetes object that requires access to external storage to persist data between life cycles. Vertica on Kubernetes persists data with the following:
- Local volume mounts: Each pod uses a PersistentVolume (PV) to store local data. The PV is mounted in the container at `/home/dbadmin/local-data`, and has specific StorageClass and [storage format type](https://docs.vertica.com/latest/en/supported-platforms/server-and-mc/) requirements.
- Custom volume mounts: You can add additional volumes to the Vertica server or sidecar utility container filesystem. Custom volume mounts persist data for processes that require access to data that must persist between pod life cycles.

For details about local volume mounts, StorageClass requirements, and custom volume mounts, see [Containerized Vertica on Kubernetes](https://docs.vertica.com/latest/en/containerized/).

# Scaling Subclusters

Vertica on Kubernetes manages subclusters by workload, which allows you to fine-tune your workload with one of the following scaling strategies:

- Subcluster: Increase the throughput of multiple short-term queries (often called "dashboard queries") to improve your cluster's parallelism.
- Pod: Increase the number of pods in an existing subcluster for complex, long-running queries. 

For details about manually sizing subclusters for new workloads, see [Subclusters on Kubernetes](https://docs.vertica.com/latest/en/containerized/subclusters-on-k8s/).


After you create a subcluster, you can use the VerticaAutoscaler to automatically scale subclusters or pods when resource metrics reach specified triggers. For instructions on automatically scaling existing subclusters with VerticaAutoscaler, see [VerticaAutoscaler Custom Resource](https://docs.vertica.com/latest/en/containerized/custom-resource-definitions/verticaautoscaler-custom-resource/).


# Client Connections

External clients can target specific subclusters to handle their workload. Each subcluster has its own service object that you can configure to manage client connections. Use the `subclusters[i].serviceName` parameter to name a service object so that you can assign a single service object to one or more subclusters.

By default, the subcluster service object is set to `ClusterIP`, which load balances internal traffic across the pods in the subcluster. To allow connections from outside of the Kubernetes cluster, set the `subclusters[i].serviceType` parameter to `NodePort` or `LoadBalancer`.

For an overview about Vertica on Kubernetes and Service Objects, see [Containerized Vertica on Kubernetes](https://docs.vertica.com/latest/en/containerized/containerized-on-k8s/). 

For a detailed implementation example, see [VerticaDB CR](https://docs.vertica.com/latest/en/containerized/custom-resource-definitions/verticadb/).

# Migrating an Existing Database into Kubernetes
  
The operator can migrate an existing Eon Mode database into Kubernetes. The operator revives the existing database into a set of Kubernetes objects that mimics the setup of the database. Vertica provides `vdb-gen`, a standalone program that you can run against a live database to create the CR.

See [Generating a Custom Resource from an Existing Eon Mode Database](https://docs.vertica.com/latest/en/containerized/generating-custom-resource-from-an-existing-eon-db/) for detailed steps.


# Vertica License

By default, the CR uses the [Community Edition (CE)](https://docs.vertica.com/latest/en/getting-started/community-edition-ce/) license. The CE license limits the number pods in a cluster to 3, and the dataset size to 1TB.

Add your own license to extend your cluster past the CE limits. See [VerticaDB CR](https://docs.vertica.com/latest/en/containerized/custom-resource-definitions/verticadb/) for details about adding your own license as a Secret in the same namespace as the operator.

# Upgrading your License

Vertica recommends incremental upgrade paths. See [Upgrading Vertica on Kubernetes](https://docs.vertica.com/latest/en/containerized/upgrading-on-k8s/) for details about Vertica server version upgrades for a custom resource.

# Vertica on Red Hat OpenShift

Vertica supports [Red Hat OpenShift](https://docs.openshift.com/container-platform/4.8/welcome/index.html), a hybrid cloud platform that adds security features and additional support to Kubernetes clusters.

The VerticaDB operator is available for download in the OpenShift OperatorHub. It is compatible with OpenShift versions 4.8 and higher.

OpenShift manages security with [Security Context Constraints](https://docs.openshift.com/container-platform/4.8/authentication/managing-security-context-constraints.html) (SCCs). You need to use the 'anyuid' SCC to manage the security of the Veriica pods.

For details about Vertica on OpenShift, see [Red Hat OpenShift integration](https://docs.vertica.com/latest/en/containerized/db-operator/red-hat-openshift-integration/).

# Prometheus Integration

Vertica integrates with [Prometheus](https://prometheus.io) to collect time series metrics on the VerticaDB operator. Configure Prometheus to authorize connections using a role-based access (RBAC) sidecar proxy, or expose the metrics to external clients with an HTTP endpoint.

For more information, see [Prometheus Integration](https://docs.vertica.com/latest/en/containerized/db-operator/prometheus-integration/).

# Additional Details

For additional details on the internals of Vertica, see the [Vertica Documentation](https://docs.vertica.com/latest/en/).

# Developers
For details about setting up an environment to develop and run tests, see the [developer instructions](DEVELOPER.md).

# Licensing

vertica-kubernetes is open source code and is under the [Apache 2.0 license](https://github.com/vertica/vertica-kubernetes/blob/main/LICENSE), but it requires that you install the Vertica server RPM. If you do not have a Vertica server RPM, you can use the free [Vertica Community Edition (CE) server RPM](https://www.vertica.com/download/vertica/community-edition/community-edition-10-1-0/). The Vertica Community Edition server RPM is not an open source project, but it is free with certain limits on capacity. For more information about these limitations, see the [Vertica Community Edition End User License Agreement](https://www.vertica.com/end-user-license-agreement-ce-version/).
