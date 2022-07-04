# About

* Maintained by: The [vertica-kubernetes community](https://github.com/vertica/vertica-kubernetes)
* Docker Community: [Docker Forums](https://forums.docker.com/), [Stack Overflow](https://stackoverflow.com/questions/tagged/docker)

# Supported Tags
* [12.0.0-0-minimal, latest](https://github.com/vertica/vertica-kubernetes/blob/v1.6.0/docker-vertica/Dockerfile)
* [12.0.0-0](https://github.com/vertica/vertica-kubernetes/blob/v1.6.0/docker-vertica/Dockerfile)
* [11.1.1-2-minimal](https://github.com/vertica/vertica-kubernetes/blob/v1.5.0/docker-vertica/Dockerfile)
* [11.1.1-2](https://github.com/vertica/vertica-kubernetes/blob/v1.5.0/docker-vertica/Dockerfile)
* [11.1.1-0-minimal](https://github.com/vertica/vertica-kubernetes/blob/v1.4.0/docker-vertica/Dockerfile)
* [11.1.1-0](https://github.com/vertica/vertica-kubernetes/blob/v1.4.0/docker-vertica/Dockerfile)
* [11.1.0-0-minimal](https://github.com/vertica/vertica-kubernetes/blob/v1.3.0/docker-vertica/Dockerfile)
* [11.1.0-0](https://github.com/vertica/vertica-kubernetes/blob/v1.3.0/docker-vertica/Dockerfile)
* [11.0.2-0-minimal](https://github.com/vertica/vertica-kubernetes/blob/v1.2.0/docker-vertica/Dockerfile)
* [11.0.2-0](https://github.com/vertica/vertica-kubernetes/blob/v1.2.0/docker-vertica/Dockerfile)
* [11.0.1-0-minimal](https://github.com/vertica/vertica-kubernetes/blob/v1.1.0/docker-vertica/Dockerfile)
* [11.0.1-0](https://github.com/vertica/vertica-kubernetes/blob/v1.1.0/docker-vertica/Dockerfile)
* [11.0.0-0-minimal](https://github.com/vertica/vertica-kubernetes/blob/v1.0.0/docker-vertica/Dockerfile)
* [11.0.0-0](https://github.com/vertica/vertica-kubernetes/blob/v1.0.0/docker-vertica/Dockerfile)
* [10.1.1-0](https://github.com/vertica/vertica-kubernetes/blob/v0.1.0/docker-vertica/Dockerfile)

# Quick Reference

* [Vertica-Kubernetes GitHub repository](https://github.com/vertica/vertica-kubernetes)
* [Vertica Helm chart repository](https://github.com/vertica/charts)
* [Official Vertica Documentation](https://www.vertica.com/docs/latest/HTML/Content/Home.htm)
* Supported architectures: `amd64`

# What is Vertica?

Vertica is a unified analytics platform, based on a massively scalable architecture with the broadest set of analytical functions spanning event and time series, pattern matching, geospatial and end-to-end in-database machine learning. Vertica enables you to easily apply these powerful functions to the largest and most demanding analytical workloads, arming you and your customers with predictive business insights faster than any analytics data warehouse in the market. Vertica provides a unified analytics platform across major public clouds and on-premises data centers and integrates data in cloud object storage and HDFS without forcing you to move any of your data.

https://www.vertica.com/

![](https://raw.githubusercontent.com/vertica/vertica-kubernetes/main/vertica-logo.png)

# How to Use This Image

This image runs the Vertica server that is optimized for use with the [Vertica operator](https://github.com/vertica/vertica-kubernetes/tree/main/docker-operator). The operator automates management and administrative tasks for an [Eon Mode](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Eon/Architecture.htm) database in Kubernetes. 

Vertica provides two versions of this image, depending on whether you require the TensorFlow package:
- If you do not require the TensorFlow package, Vertica recommends downloading the image with the *-minimal* supported tag suffix. Because it does not include the TensorFlow package, it has a reduced size. The *-minimal* image is the default image included in the [Vertica Helm chart](https://github.com/vertica/charts).
- If you do require the TensorFlow package, download an image without the *-minimal* suffix. For details about TensorFlow and Vertica, see [Setting up TensorFlow Support in Vertica](https://www.vertica.com/docs/latest/HTML/Content/Authoring/AnalyzingData/MachineLearning/UsingExternalModels/UsingTensorFlow/TensorFlowExample.htm).

For a brief overview on how to install and configure the operator, see the [Vertica GitHub repository](https://github.com/vertica/vertica-kubernetes). For an in-depth look at Vertica on Kubernetes, see the [Vertica documentation](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/ContainerizedVertica.htm).

# License

View the [license information](https://www.vertica.com/end-user-license-agreement-ce-version/) for this image.
