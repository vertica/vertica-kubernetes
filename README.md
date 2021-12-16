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

The VerticaDB CRD provides configuration parameters so that you can customize your Vertica on Kubernetes deployment.  

For a full list of parameters and their definitions, see [Custom Resource Definition Parameters](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CustomResourceDefinitionParams.htm).

# Persistence

Vertica on Kubernetes persists data using the following methods:
- Local volume mounts: Each pod uses a PersistentVolume (PV) to store local data. The PV is mounted in the container at `/home/dbadmin/local-data`, and has specific StorageClass and [storage format type](https://www.vertica.com/docs/11.0.x/HTML/Content/Authoring/SupportedPlatforms/MCandServer.htm) requirements.
- Custom volume mounts: You can add additional volumes to the Vertica server or sidecar utility container filesystem. This persists data for processes that require access to data that must persist between pod life cycles.

For details about local volume mounts, StorageClass requirements, and custom volume mounts, see [Containerized Vertica on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/ContainerizedVerticaWithK8s.htm).


# Vertica License and Upgrades

By default, we use the [Community Edition (CE)](https://www.vertica.com/download/vertica/trial-download/?) license if no license is provided. The CE license limits the number pods in a cluster to 3, and the dataset size to 1TB. Use your own license to extend the cluster past these limits.

Refer to the following resources for additional license information:
- [Creating a Custom Resource](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CreatingCustomResource.htm) for details about adding your own license as a Secret in the same namespace as the operator.
- [Upgrading Vertica on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/UpgradingWithOperator.htm) for details about Vertica server version upgrades for a custom resource.

# Scale Up and Down

Vertica offers two scaling strategies, where each one improves different types of performance.  

- To increase the performance of complex, long-running queries, add nodes to an existing subcluster. 
- To increase the throughput of multiple short-term queries (often called "dashboard queries"), improve your cluster's parallelism by adding additional subclusters.

For complete instructions on scaling subclusters, client connections, and internal and external workloads, see [Subclusters on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/SubclustersOperator.htm).


# Client Connections

Each subcluster has a `ClusterIP` service object for client connections. The service load balances traffic across the pods in the subcluster. Clients connect to the Vertica cluster through one of the subcluster service objects, depending on which subcluster the client is targeting. Use the `subclusters[i].serviceType` parameter to specify `NodePort` or `LoadBalancer` to allow connections from outside of the Kubernetes cluster.

For an overview about Vertica on Kubernetes and Service Objects, see [Containerized Vertica on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/ContainerizedVerticaWithK8s.htm).. 

For a detailed implementation example, see [Creating a Custom Resource](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CreatingCustomResource.htm).


# Migrating an Existing Database into Kubernetes
  
The operator can migrate an existing database into Kubernetes. The operator revives an existing database into a set of Kubernetes objects that mimics the setup of the database. We provide `vdb-gen`, a standalone program that you can run against a live database to create the CR.

For detailed steps, see [Generating a Custom Resource from an Existing Eon Mode Database](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/GeneratingCR.htm).


# Additional Details

For additional details on the internals of Vertica, see the [Vertica Documentation](https://www.vertica.com/docs/11.0.x/HTML/Content/Home.htm).

# Developers
For details about setting up an environment to develop and run tests, see the [developer instructions](DEVELOPER.md).

# Licensing

vertica-kubernetes is open source code and is under the [Apache 2.0 license](https://github.com/vertica/vertica-kubernetes/blob/main/LICENSE), but it requires that you install the Vertica server RPM. If you do not have a Vertica server RPM, you can use the free [Vertica Community Edition (CE) server RPM](https://www.vertica.com/download/vertica/community-edition/community-edition-10-1-0/). The Vertica Community Edition server RPM is not an open source project, but it is free with certain limits on capacity. For more information about these limitations, see the [Vertica Community Edition End User License Agreement](https://www.vertica.com/end-user-license-agreement-ce-version/).
