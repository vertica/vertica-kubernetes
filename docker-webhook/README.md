# About

* Maintained by: The [vertica-kubernetes community](https://github.com/vertica/vertica-kubernetes)
* Docker Community: [Docker Forums](https://forums.docker.com/), [Stack Overflow](https://stackoverflow.com/questions/tagged/docker)

# Supported Tags
* [1.0.0, latest](https://github.com/vertica/vertica-kubernetes/blob/v1.0.0/docker-operator/Dockerfile)

# Quick Reference

* [Vertica-Kubernetes GitHub repository](https://github.com/vertica/vertica-kubernetes)
* [Vertica Helm chart repository](https://github.com/vertica/charts)
* [Official Vertica Documentation](https://www.vertica.com/docs/10.1.x/HTML/Content/Home.htm)
* [Using Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/)
* Supported architectures: `amd64`

# What is webhook?

Webhook is also called "admission controller", which is a piece of code that intercepts requests to the Kubernetes API server prior to persistence of the object, but after the request is authenticated and authorized.

https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/

# How to Use This Image

This image is used to deploy the webhook for VerticaDB operator. The webhook will be deployed at cluster scope. It will validate requests (and reject the invalid ones) to the VerticaDB CRD deployed into any namespaces.

See the official [Vertica GitHub repository](https://github.com/vertica/vertica-kubernetes) for a brief overview on how to install, configure, and uninstall the operator. See the [official Vertica documentation](https://www.vertica.com/docs/10.1.x/HTML/Content/Home.htm) for an in-depth look at the Vertica on Kubernetes architecture.

# License

View the [license information](https://www.vertica.com/end-user-license-agreement-ce-version/) for this image.
