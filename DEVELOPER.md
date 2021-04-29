# Introduction

This guide explains out to setup a environment to develop and test Vertica in Kubernetes.

# Software Setup
Use of this repo obviously requires a working Kubernetes cluster.  In addition to that, we require the following software to be installed in order to run the integration tests:

- [go](https://golang.org/doc/install) (version 1.13.8)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) (version 1.20.1).  If you are using a real Kubernetes cluster this will already be installed.
- [helm](https://helm.sh/docs/intro/install/) (version 3.5.0)
- [kubectx](https://github.com/ahmetb/kubectx/releases/download/v0.9.1/kubectx) (version 0.9.1)
- [kubens](https://github.com/ahmetb/kubectx/releases/download/v0.9.1/kubens) (version 0.9.1)
- [daemonize](https://software.clapper.org/daemonize/)
- [stern](https://github.com/wercker/stern) (version 1.11.0)

# Kind Setup
[Kind](https://kind.sigs.k8s.io/) is a way to setup a multi-node Kubernetes cluster using Docker.  It mimics a multi-node setup by starting a separate container for each node.  The requirements for running Kind are quite low - it is possible to set this up on your own laptop.  This is the intended deployment to run the tests in an automated fashion.

We have a wrapper that you can use that will setup kind and create a cluster suitable for testing Vertica. The following command creates a cluster named cluster1 that has one master node and two worker nodes. It takes only a few minutes to complete:  

```
scripts/kind.sh init cluster1
```

After it returns, change the context to use the cluster. The cluster has its own kubectl context named kind-cluster1:

```
kubectx kind-cluster1
```

Test the container out by checking the status of the nodes:

```
kubectl get nodes
```

After kind is up, you need to configure it to run the integration tests.  The `setup-int-tests.sh` script encompasses all of the setup:

```
scripts/setup-int-tests.sh
```



# Kind Cleanup

After you are done with the cluster, you can delete it with our helper script. Substitute `cluster1` with the name of your cluster:

```
scripts/kind.sh term cluster1
```

If you forgot your name, run kind directly to get the clusters installed:

```
$ PATH=$PATH:$HOME/go/bin
$ kind get clusters
cluster1
```

# Developer Workflow

## Make Changes

The structure of the repo is as follows:
- **docker-\***: Contains the Dockerfile that we have dependencies on.
- **helm-charts/vertica**: Contains the helm charts to deploy Vertica.  This chart manages all of the required Kubernetes objects.
- **helm-charts/vertica-int-tests**: Contains the helm charts to run integration tests against Vertica.

## Build and Push Containers

We currently make use of a few containers:
- **vertica**: This container is used as an initContainer to bootstrap the config directory (/opt/vertica/config), as well as the long-running container that runs the vertica daemon. The files for this container are in the `docker-vertica/` directory.
- **python-tools**: This is a container we use for integration tests.  It is a minimal python base with vertica_python installed.  It has a helper class that creates a connection object using information from Kubernetes. We use this to write integration tests.

In order to run Vertica in Kubernetes, we need to package Vertica inside a container.  This container is then referenced in the YAML file when we install the helm chart.

Run this make target to build the necessary containers:

```
make docker-build
```

By default, this creates a container that is stored in the local daemon. The tag is `<namespace>-1`.

You need to make these containers available to the Kubernetes cluster. With kind, you need push them into the cluster so they appear as local containers. Use the `kind load docker-image` command for this. The following script handles this for all images:

```
scripts/push-to-kind.sh -t <your-tag> <cluster-name>
```

Due to the size of the vertica image, this step can take in excess of 10 minutes when run on a laptop.

If your image builds fail silently, confirm that there is enough disk space in your Docker repository to store the built images.

## Run Unit Tests

Unit testing verifies the YAML files we create in `helm install` are in a valid format.  Due to the various config knobs we provide, there are a lot of variations to the actual YAML files that helm installs. We have two flavors of unit testing:  

1. **Helm lint**: This uses the chart verification test that is built into Helm. You can run this with the following make target:

```
make lint
```

2. **Helm unittest**: 

```
make run-unit-tests
```

Unit tests are stored in `helm-charts/vertica/tests`. They use the [unittest plugin for helm](https://github.com/quintush/helm-unittest). Some samples that you can use to write your own tests can be found at [unittest github page](https://github.com/quintush/helm-unittest/tree/master/test/data/v3/basic).  [This document](https://github.com/quintush/helm-unittest/blob/master/DOCUMENT.md) describes the format for the tests.


## Deploy Vertica

To deploy Vertica, use the Helm charts from helm-charts/vertica. Override the default configuration settings with values that are specific to your Kubernetes cluster. We have a make target to deploy using the kind cluster that was setup in the previous section. This make target synchronously waits until all of the pods are in the ready state, and cleans up any left over deployment that might exist:

```
make deploy-kind
```

If the pods never get to the ready state, this step will timeout.  You can debug this step by describing any of the pods (if any exist) and looking at the events.  Use the following selector to get the pods that Vertica creates:
```
$ ~/git/vertica-kubernetes# kubectl get pods -l vertica.com/database=verticadb
NAME                                  READY   STATUS    RESTARTS   AGE
cluster-vertica-defaultsubcluster-0   1/1     Running   0          30m
cluster-vertica-defaultsubcluster-1   1/1     Running   0          30m
cluster-vertica-defaultsubcluster-2   1/1     Running   0          30m
```

## Run Integration Tests

The integration tests are run through Kubernetes itself.  We use [octopus as the testing framework](https://github.com/kyma-incubator/octopus).  This allows you to define tests and package them up for running in a test suite.  This test suite was chosen because it allows you to selectively run tests, have automatic retries for failed tests and allow the test to be run multiple times.

Before running the integration tests for the first time, you must setup Kubernetes with some required objects.  We have encapsulated everything that you need in the following script: 

```
scripts/setup-int-tests.sh
```

You only need to run this once - you do not need to run after each change.

We have a make target to run the integration tests against the currently deployed Vertica cluster: 
```
make run-int-tests
```

This command waits synchronously until the tests succeeds or fails. It runs with an S3 backend, so it creates a minIO tenant to store the communal data.

There are a few ways to monitor the progress of the test:

- Each test runs in its own pod.  To view the output of the test, issue the kubectl logs command.

```
kubectl logs oct-tp-testsuite-sanity-test-install-0
```

- Or you can tail the currently running test automatically using this script:

```
scripts/cur-oct-logs.sh
```

- Or you can use stern.  We run stern automatically to collect the output for each test. The output is saved in the `int-tests-output/` directory.  This output is overwritten each time we run the integration tests.

## Cleanup

The following make target cleans up the integration tests and deployment:

```
make clean-deploy clean-int-tests
```

