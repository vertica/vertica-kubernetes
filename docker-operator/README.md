# About

* Maintained by: The [vertica-kubernetes community](https://github.com/vertica/vertica-kubernetes)
* Docker Community: [Docker Forums](https://forums.docker.com/), [Stack Overflow](https://stackoverflow.com/questions/tagged/docker)

# Supported Tags

The [Vertica Helm chart](https://github.com/vertica/vertica-kubernetes/tree/main/helm-charts/verticadb-operator) installs the VerticaDB operator with a tag for the most recently released version. For a comprehensive list, see [Tags](https://hub.docker.com/r/opentext/verticadb-operator/tags).

# Quick Reference

* [Vertica-Kubernetes GitHub repository](https://github.com/vertica/vertica-kubernetes)
* [Vertica Helm chart repository](https://github.com/vertica/charts)
* [Vertica Documentation](https://www.vertica.com/docs/latest/HTML/Content/Home.htm)
* Supported architectures: `amd64`

# What is Vertica?

Vertica is a unified analytics platform, based on a massively scalable architecture with the broadest set of analytical functions spanning event and time series, pattern matching, geospatial and end-to-end in-database machine learning. Vertica enables you to easily apply these powerful functions to the largest and most demanding analytical workloads, arming you and your customers with predictive business insights faster than any analytics data warehouse in the market. Vertica provides a unified analytics platform across major public clouds and on-premises data centers and integrates data in cloud object storage and HDFS without forcing you to move any of your data.

https://www.vertica.com/

![](https://raw.githubusercontent.com/vertica/vertica-kubernetes/main/vertica-logo.png)

# How to Use This Image

This image is used to deploy the VerticaDB operator. The operator manages a Vertica [Eon Mode](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Eon/Architecture.htm) database in Kubernetes, and automates the following administrative tasks:
- Installing Vertica
- Upgrading Vertica
- Creating and reviving a Vertica database
- Restarting and rescheduling DOWN pods to maintain quorum
- Subcluster scaling
- Service management and health monitoring for pods
- Load balancing for internal and external traffic

The VerticaDB operator image includes an [admission controller](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/). The admission controller uses a webhook to verify changes to mutable states in a custom resource instance.

For in-depth details about how to install and configure the VerticaDB operator and admission controller, see the [Vertica documentation](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/Operator.htm).

# License

View the [license information](https://www.microfocus.com/en-us/legal/software-licensing) for this image.
