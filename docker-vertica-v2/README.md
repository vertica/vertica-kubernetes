# About

* Maintained by: The [vertica-kubernetes community](https://github.com/vertica/vertica-kubernetes)
* Docker Community: [Docker Forums](https://forums.docker.com/), [Stack Overflow](https://stackoverflow.com/questions/tagged/docker)

# Supported Tags

Vertica tags each container with the Vertica version. Each version is provided in two formats: a full version and a minimal version. The minimal version has some libraries stripped out to reduce its size. For a detailed description of the differences between the formats, see the [documentation](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Images.htm).

Each tag follows this format: _`<major>.<minor>.<patch>-<hotfix>[-minimal]`_. For example:
- `11.1.0-0` is the full image of 11.1, service pack 0, hotfix 0.
- `12.0.1-0-minimal` is the minimal image of 12.0, service pack 1, hotfix 0.

The `latest` tag always refers to the minimal image of the most recently released version.

For a comprehensive list, see [Tags](https://hub.docker.com/r/opentext/vertica-k8s/tags).

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

This image runs the Vertica server that is optimized for use with the [Vertica operator](https://hub.docker.com/r/opentext/verticadb-operator). The operator automates management and administrative tasks for an [Eon Mode](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Eon/Architecture.htm) database in Kubernetes. 

Vertica provides two versions of this image, depending on whether you require the TensorFlow package:
- If you do not require the TensorFlow package, Vertica recommends downloading the image with the *-minimal* supported tag suffix. Because it does not include the TensorFlow package, it has a reduced size. The *-minimal* image is the default image included in the [Vertica Helm chart](https://github.com/vertica/charts).
- If you do require the TensorFlow package, download an image without the *-minimal* suffix. For details about TensorFlow and Vertica, see [Setting up TensorFlow Support in Vertica](https://www.vertica.com/docs/latest/HTML/Content/Authoring/AnalyzingData/MachineLearning/UsingExternalModels/UsingTensorFlow/TensorFlowExample.htm).

For in-depth details about how to install and configure the VerticaDB operator, see the [Vertica documentation](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/Operator/Operator.htm).

# License

View the [license information](https://www.microfocus.com/en-us/legal/software-licensing) for this image.