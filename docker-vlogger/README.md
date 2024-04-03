# About

* Maintained by: The [vertica-kubernetes community](https://github.com/vertica/vertica-kubernetes)
* Docker Community: [Docker Forums](https://forums.docker.com/), [Stack Overflow](https://stackoverflow.com/questions/tagged/docker)

# Supported Tags

* [1.0.1, latest](https://github.com/vertica/vertica-kubernetes/blob/v2.1.2/docker-vlogger/Dockerfile)
* [1.0.0](https://github.com/vertica/vertica-kubernetes/blob/v1.2.0/docker-vlogger/Dockerfile)

# Quick Reference

* [Vertica-Kubernetes GitHub repository](https://github.com/vertica/vertica-kubernetes)
* [Vertica Helm chart repository](https://github.com/vertica/charts)
* [Vertica Documentation](https://www.vertica.com/docs/11.0.x/HTML/Content/Home.htm)
* Supported architectures: `amd64`

# What is Vertica?

Vertica is a unified analytics platform, based on a massively scalable architecture with the broadest set of analytical functions spanning event and time series, pattern matching, geospatial and end-to-end in-database machine learning. Vertica enables you to easily apply these powerful functions to the largest and most demanding analytical workloads, arming you and your customers with predictive business insights faster than any analytics data warehouse in the market. Vertica provides a unified analytics platform across major public clouds and on-premises data centers and integrates data in cloud object storage and HDFS without forcing you to move any of your data.

https://www.vertica.com/

![](https://raw.githubusercontent.com/vertica/vertica-kubernetes/main/vertica-logo.png)

# How to Use This Image

This image is used to deploy a sidecar utility container that assists with logging for the [opentext/vertica-k8s](https://hub.docker.com/r/opentext/vertica-k8s/tags?page=1&ordering=last_updated) server image. The sidecar sends logs from vertica.log in the Vertica server to stdout on the host node to simplify log aggregation.

For an overview of the sidecar container, see [Containerized Vertica on Kubernetes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/ContainerizedVerticaWithK8s.htm). For sidecar implementation details, see [Creating a Custom Resource](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/CreatingCustomResource.htm).

# License

View the [license information](https://www.microfocus.com/en-us/legal/software-licensing) for this image.
