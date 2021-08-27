_This container is deprecated.  The webhook is now include in the VerticaDB operator image._

# About

* Maintained by: The [vertica-kubernetes community](https://github.com/vertica/vertica-kubernetes)
* Docker Community: [Docker Forums](https://forums.docker.com/), [Stack Overflow](https://stackoverflow.com/questions/tagged/docker)

# Supported Tags
* [1.0.0, latest](https://github.com/vertica/vertica-kubernetes/blob/v1.0.0/docker-webhook/Dockerfile)

# Quick Reference

* [Vertica-Kubernetes GitHub repository](https://github.com/vertica/vertica-kubernetes)
* [Vertica Helm chart repository](https://github.com/vertica/charts)
* [Official Vertica Documentation](https://www.vertica.com/docs/11.0.x/HTML/Content/Home.htm)
* [Using Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/)
* Supported architectures: `amd64`

# What is webhook?

Webhook is also called "admission controller", which is a piece of code that intercepts requests to the Kubernetes API server prior to persistence of the object, but after the request is authenticated and authorized.

https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/

# How to Use This Image

This image is used to deploy the [admission controller](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) for the Vertica operator. The admission controller is a cluster-scoped webhook that prevents invalid state changes to the custom resource instance. When you save a change to a custom resource instance, the admission controller webhook queries a REST endpoint that provides state rules for custom resource objects. If a change violates the state rules, the admission controller prevents the change and returns a error.

See the [Vertica GitHub repository](https://github.com/vertica/vertica-kubernetes) for a brief overview on how to install and configure the operator. See the [Vertica documentation](https://www.vertica.com/docs/11.0.x/HTML/Content/Authoring/Containers/ContainerizedVertica.htm) for an in-depth look at the Vertica on Kubernetes architecture.

# License

View the [license information](https://www.vertica.com/end-user-license-agreement-ce-version/) for this image.
