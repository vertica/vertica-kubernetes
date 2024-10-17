# Introduction

This guide explains how to set up an environment to develop and test the Vertica operator.

# Repo structure

This repo contains the following directories and files:

- `docker-vertica/`: files that build a Vertica server container. This container is designed for [Administration Tools (admintools)](https://docs.vertica.com/latest/en/admin/using-admin-tools/admin-tools-reference/) deployments.
- `docker-vertica-v2/`: files that build the v2 Vertica server container. This container is designed for vclusterops deployments.
- `docker-operator/`: files that build the [VerticaDB operator](https://docs.vertica.com/latest/en/containerized/db-operator/) container.
- `docker-vlogger/`: files that build the vlogger sidecar container that sends the contents of `vertica.log` to STDOUT.
- `scripts/`: scripts that run Makefile targets and execute end-to-end (e2e) tests.
- `api/`: defines the custom resource definition (CRD) spec.
- `pkg/`: includes all packages for the operator.
- `cmd/`: source code for all executables in this repository.
- `bin/`: binary dependencies that this repository compiles or downloads.
- `config/`: generated files of all manifests that make up the operator. [config/samples/](https://github.com/vertica/vertica-kubernetes/tree/main/config/samples) contains sample specs for all Vertica CRDs.

- `tests/`: test files for e2e and soak tests.
- `changes/`: changelog for past releases and details about changes for the upcoming release.
- `hack/`: file that contains the copyright boilerplate included on all generated files.
- `helm-charts/`: Helm charts that this repository builds.

# Software requirements

Before you begin, you must manually install the following software:

- [docker](https://docs.docker.com/get-docker/) (version 23.0)
- [go](https://golang.org/doc/install) (version 1.22.5)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) (version 1.20.1)
- [helm](https://helm.sh/docs/intro/install/) (version 3.5.0)
- [kubectx](https://github.com/ahmetb/kubectx/releases/download/v0.9.1/kubectx) (version 0.9.1)
- [kubens](https://github.com/ahmetb/kubectx/releases/download/v0.9.1/kubens) (version 0.9.1)
- [krew](https://github.com/kubernetes-sigs/krew/releases/tag/v0.4.1) (version 0.4.1)

  After installation, you must add `$HOME/.krew/bin` to your PATH.

- [kuttl](https://github.com/kudobuilder/kuttl/) (version 0.9.0)
- [changie](https://github.com/miniscruff/changie) (version 1.2.0)
- [jq](https://stedolan.github.io/jq/download/) (version 1.5+)

> **NOTE**
> Some [Makefile](./Makefile) targets install additional software in this repo's `bin/` directory.

# Help

To simplify complex setup and teardown tasks, this repo uses a [Makefile](./Makefile). For a full list and description of make targets, run the following command:

```shell
make help
```

# Kind environment setup

Kind (**K**ubernetes **IN** **D**ocker) runs a Kubernetes cluster where each cluster node is a Docker container. Because the requirements are minimal&mdash;you can set it up on a laptop&mdash;Kind is the preferred method to test and develop Kubernetes locally.

All automated e2e tests in this repo run against a Kind cluster.

## Cluster setup

The `scripts/kind.sh` helper script sets up Kind and creates a cluster to test Vertica.

1. The following command creates a single-node cluster named `devcluster`:

   ```shell
   ./scripts/kind.sh init devcluster
   ```

   The previous command pulls the [kindest/node](https://hub.docker.com/r/kindest/node/) image and the [kind registry image](https://kind.sigs.k8s.io/docs/user/local-registry/), and starts them as containers:

   ```shell
   docker image ls
   REPOSITORY     TAG       IMAGE ID       CREATED         SIZE
   registry       2         ff1857193a0b   2 days ago      25.4MB
   kindest/node   v1.23.0   b3dd68fe0a8c   22 months ago   1.46GB

   docker container ls
   CONTAINER ID   IMAGE                  COMMAND                  CREATED              STATUS              PORTS                       NAMES
   6740fc7ab88a   kindest/node:v1.23.0   "/usr/local/bin/entr…"   About a minute ago   Up About a minute   127.0.0.1:38577->6443/tcp   devcluster-control-plane
   907665ae2da6   registry:2             "/entrypoint.sh /etc…"   2 minutes ago        Up About a minute   127.0.0.1:5000->5000/tcp    kind-registry
   ```

2. After the command completes, use `kubectx` to change to the new cluster's context, which is named `kind-<cluster-name>`:

   ```shell
   kubectx kind-devcluster
   Switched to context "kind-devcluster".
   ```

3. To test the container, check the status of the cluster nodes with kubectl:

   ```shell
   kubectl get nodes
   NAME                       STATUS   ROLES                  AGE   VERSION
   devcluster-control-plane   Ready    control-plane,master   47s   v1.23.0
   ```

You have a master node and control plane that is ready to deploy any Vertica Kubernetes resources locally.

## Cluster cleanup

When you no longer need a cluster, you can delete it with the helper script. The following command deletes the cluster named `devcluster`:

```shell
./scripts/kind.sh term devcluster
...
Deleting cluster "devcluster" ...
kind-registry
```

> **NOTE**
> If you forgot a cluster name, run Kind directly to return all installed clusters. First, you must add `kind` to your path:
>
> ```shell
> PATH=$PATH:path/to/vertica-kubernetes/bin/kind
> kind get clusters
> devcluster
> ```

# Build the images

> **IMPORTANT**
> This repo requires a Vertica RPM with the following version requirements:
>
> - `docker-vertica`: 11.0.1 or higher.
> - `docker-vertica-v2`: 24.1.0 or higher.
>
> You must store the RPM in the `<image-name>/packages` directory:
>
> ```shell
> cp /path/to/vertica-x86_64.RHEL6.latest.rpm docker-vertica/packages/
> cp /path/to/vertica-x86_64.RHEL6.latest.rpm docker-vertica-v2/packages/
> ```

## Custom image names

You might want to give one or more of the Vertica images a unique name. Before you [build and push the containers](#build-and-push-the-containers), set one of these environment variables to change the name of the associated image:

- `OPERATOR_IMG`: VerticaDB operator image.
- `VERTICA_IMG`: Vertica server image. Use this interchangeably for v1 and v2 containers.
- `VLOGGER_IMG`: Vertica sidecar logger.
- `BUNDLE_IMG`: Operator Lifecycle Manager (OLM) bundle image.

The custom name can include the registry URL. The following example creates a custom `verticadb-operator` image name that includes the registry URL:

```
export OPERATOR_IMG=myrepo:5000/my-vdb-operator:latest
```

In a production environment, the default tag is `latest`. In a Kind environment, the default tag is `kind`.

## Build and push the containers

Vertica provides two container sizes:

- Full image (default image).
- Minimal image that does not include the 240MB Tensorflow package.

The following steps build the images and push them to the Kind cluster in the current context:

> **NOTE**
> Due to the size of the Vertica image, this step might take up to 10 minutes.

1. To build the containers, run the `docker-build` target. By itself, the target uses the default image:

   ```shell
   make docker-build
   ```

   To build the minimal container, include `MINIMAL_VERTICA_IMG=YES`:

   ```shell
   make docker-build MINIMAL_VERTICA_IMG=YES
   ```

   When the command completes, you have the following images on your machine:

   ```shell
   docker image ls
   REPOSITORY           TAG       IMAGE ID       CREATED          SIZE
   vertica-logger       1.0.1     ec5ef9959692   5 days ago       7.45MB
   verticadb-operator   2.2.0     abb3f97a68b0   5 days ago       80.2MB
   vertica-k8s          2.2.0     46e4511cabf1   5 days ago       1.62GB
   rockylinux           9         639282825872   2 weeks ago      70.3MB
   ...
   ```

   - `vertica-k8s`: long-running container that runs the Vertica daemon. This container is designed for admintools deployments. For details about the admintools deployment image, see the [Dockerfile](./docker-vertica/Dockerfile). For details about the vcluster deployment image, see the [Dockerfile](./docker-vertica-v2/Dockerfile).
   - `verticadb-operator`: runs the VerticaDB operator and webhook. For details, see the [Dockerfile](./docker-operator/Dockerfile).
   - `vertica-logger`: runs the vlogger sidecar container that sends the contents of `vertica.log` to STDOUT. For details, see the [Dockerfile](./docker-vlogger/Dockerfile).
   - `rockylinux9: serves as the default base image for the `vertica-k8s` image. The `make docker-build` command pulls this image each time.

   If your image builds fail silently, confirm that there is enough disk space in your Docker repository to store the built images:

   ```shell
   docker system df
   TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
   Images          6         2         2.983GB   1.501GB (50%)
   Containers      2         2         3.099MB   0B (0%)
   Local Volumes   25        2         21.53GB   17.19GB (79%)
   Build Cache     57        0         3.456GB   3.456GB
   ```

   For details about the `df` command options, flags, and output, see the [Docker documentation](https://docs.docker.com/engine/reference/commandline/system_df/).

   > **NOTE**
   > OLM deployments create an image for the operator [bundle](https://github.com/operator-framework/operator-registry/blob/v1.16.1/docs/design/operator-bundle.md). The contents of this directory are generated with the `docker-build-bundle` make target.

2. Next, you have to make these containers available to the Kind cluster. Push them to the cluster with the following command:

   ```shell
   make docker-push
   ```

   This command honors any environment variables that you used when you created the image.

# Developer Workflows

The following sections show you how to install and maintain the VerticaDB operator in the Kind development environment.

After you complete this setup, you can deploy and test [Vertica CRDs](https://docs.vertica.com/latest/en/containerized/custom-resource-definitions/) in your local development environment.

## Generate controller files

The VerticaDB operator uses the [Operator SDK framework](https://sdk.operatorframework.io/). This framework provides tools that generate files so that you do not have to manually write boilerplate code.

The following make target generates these boilerplate files and the manifests that deploy the VerticaDB operator:

```shell
make generate manifests
```

> **IMPORTANT**
> After you make changes to your development environment, you might need to regenerate these files.

## Run the VerticaDB operator

In order to run the operator, you must run it inside Kubernetes by packaging it in a container. It cannot run standalone outside of Kubernetes.

Vertica on Kubernetes supports two deployment models: Helm chart and [Operator Lifecycle Manager (OLM)](https://olm.operatorframework.io/). You specify the deployment model with the `DEPLOY_WITH` environment variable in the `make` command. By default, the operator is deployed in the `verticadb-operator` namespace. If that namespace does not exists, it creates it if necessary.

By default, the operator is cluster-scoped, meaning it monitors CRs in all namespaces. But when deployed with helm, it can be run as namespace scoped as well by setting the `scope` parameter to `namespace`.

The operator pod contains a webhook, which requires TLS certificates. The TLS setup for each deployment model is different.

### Helm deployment

Deploy the operator with Helm and all its prerequisites:
First make sure DEPLOY_WITH is set up properly in Makefile:
```shell
DEPLOY_WITH=helm
```
Next run the following command
```shell
make config-transformer deploy
```

The operator generates a self-signed TLS certificate at runtime. You can also provide a custom TLS certificate. For details, see `webhook.certSource` in [Helm chart parameters](https://docs.vertica.com/latest/en/containerized/db-operator/helm-chart-parameters/).

### OLM deployment

You must configure OLM deployments when you run an operator with a webhook. For details, see the [OLM documentation](https://olm.operatorframework.io/docs/advanced-tasks/adding-admission-and-conversion-webhooks/).

Deploy OLM and all its prerequisites:
First make sure DEPLOY_WITH is set up properly in Makefile:
```shell
DEPLOY_WITH=olm
```
Next run the following command
```shell
make setup-olm deploy
```

### Remove the operator

The `undeploy` make target removes the operator from the environment. The following command removes both Helm and OLM deployments:

```shell
make undeploy
...
release "vdb-op" uninstalled
```

## Add a changelog entry

The changelog file is generated by [Changie](https://github.com/miniscruff/changie). It separates the changelog generation from commit history, so that any PR that makes a notable change should add new changie entries.

In general, you do not need to set this up. One of the maintainers usually add changie entries to PRs that require them.

# Testing

This repo provides linters for your source code and testing tools that verify that your operator is production-ready.

## Linting

A linter analyzes files to asses the code quality and identify errors. Vertica on Kubernetes runs three different linters:

- [Helm lint](https://helm.sh/docs/helm/helm_lint/): Runs Helm's built-in chart verification test.
- [golint](https://pkg.go.dev/golang.org/x/lint/golint): Runs a few Go linters.
- [hadolint](https://github.com/hadolint/hadolint): Checks the various Dockerfiles that we have in our repo.

Run all linters with the `lint` target:

```shell
make lint
```

This make target installs the `hadolint/hadolint` image into your local docker repo:

```shell
[k8admin@docd01 vertica-kubernetes]$ docker image ls
REPOSITORY               TAG            IMAGE ID       CREATED          SIZE
...
hadolint/hadolint        2.12.0         12fa10a87864   11 months ago    2.43MB
...
```

## Unit Tests

The unit tests verify both the Helm chart and the VerticaDB operator. Run the unit tests with the following command:

```shell
make run-unit-tests
```

This make target pulls the `quintush/helm-unittest` image into your local docker repo:

```shell
[k8admin@docd01 vertica-kubernetes]$ docker image ls
REPOSITORY               TAG            IMAGE ID       CREATED          SIZE
...
quintush/helm-unittest   3.9.3-0.2.11   83c439b2cf46   9 months ago     105MB
...
```

Helm chart unit tests are stored in `helm-charts/verticadb-operator/tests` and use the [helm-unittest plugin](https://github.com/quintush/helm-unittest). The helm-unittest repo includes [test samples and templates](https://github.com/quintush/helm-unittest/tree/master/test/data/v3/basic) so you can model your own tests. For details about the test format, see the [helm-unittest GitHub repository](https://github.com/quintush/helm-unittest/blob/master/DOCUMENT.md).

Unit tests for the VerticaDB operator use the Go testing infrastructure. Some tests run the operator against a mock Kubernetes control plane created with [envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest). Per Go standards, test files are stored in package directories and end with `_test.go`.

## e2e Tests

The e2e tests use the [kuttl](https://github.com/kudobuilder/kuttl/) testing framework. To run the tests:

1. Push the operator to the cluster. If the cluster is already present, the command returns with a message that the operator is already present:
   ```shell
   make docker-build-operator docker-push-operator
   ```
2. Start the tests with the `run-int-tests` make target. These tests generate a large amount of output, so we recommend that you pipe the output to a file. This command uses `tee` to send output to STDOUT and a file named `kuttl.out`:
   ```shell
   make run-int-tests | tee kuttl.out
   ```

### Run Individual e2e Tests

You can run individual tests from the command line with the [KUTTLE CLI](https://kuttl.dev/docs/cli.html#setup-the-kuttl-kubectl-plugin).

Prerequisite steps:

1. Before you can run an individual test, you need to set up a communal endpoint. To set up a [MinIO](https://min.io/) endpoint, run the following make target:
>
> ```shell
> make setup-minio
> ```
>
    To set up a different communal endpoint, see [Custom communal endpoints](#custom-communal-endpoints).

2. You also need to set up some environmental variables:
>
> ```shell
> export VERTICA_DEPLOYMENT_METHOD=vclusterops
> ```
    VERTICA_DEPLOYMENT_METHOD=vclusterops lets the backend use vcluster package to manage vertica clusters. If it is not set, the default value will be admintools and the vertica image must be admintools compatible.
>
> ```shell
> export LICENSE_FILE=/file_path/license.key
> unset LICENSE_FILE
> ```
    If the total number of nodes used in a test case is more than 3, you have to set up LICENSE_FILE with the license file path.
    If it is no more than 3, you must unset LICENSE_FILE.

>
> ```shell
> export BASE_VERTICA_IMG=opentext/vertica-k8s:24.2.0-1
> export VERTICA_IMG=opentext/vertica-k8s:latest
> ```
>
    BASE_VERTICA_IMG and VERTICA_IMG are used for the upgrade test cases. The BASE_VERTICA_IMG is the base vertica vertion that will be installed. VERTICA_IMG is the vertica version that the base version will be upgraded to. The version in VERTICA_IMG must be higher than that in BASE_VERTICA_IMG.


3. kuttl-test.yaml is the configuration file for e2e test cases. There is a "timeout" field in it. If your server is not fast enough, you may need to increase that value to pass the test cases. There is another field "parallel" that controls the maximum number of tests to run at once. It is set to 2 by default. You can set it to 1 if your server is not fast enough.

4. To avoid downloading the same image multiple times, you can run the following commands to download the images and push them to the kind cluster before you run the test cases.
>
> ```shell
> scripts/push-to-kind.sh -i $VERTICA_IMG
> scripts/push-to-kind.sh -i $BASE_VERTICA_IMG
>
    This will speed up test case execution and avoid timeout.


To run an individual test, pass the `--test` command the name of a [test suite directory](./tests/). For example, this command runs all tests in the [http-custom-certs](./tests/e2e-leg-6/http-custom-certs/) directory:

```shell
make init-e2e-env && kubectl kuttl test --test http-custom-certs
```

### Custom communal endpoints

Internally, the e2e make target (`run-int-tests`) sets up a [MinIO](https://min.io/) endpoint with TLS for testing. However, you might want to test with a communal endpoint that mimics your development environment. The default MinIO configuration is stored in the [tests/kustomize-defaults.cfg](./tests/kustomize-defaults.cfg) file, but you can create a configuration file that overrides these defaults.

The following steps create a configuration file and run e2e tests with a custom communal endpoint:

1. Copy `tests/kustomize-defaults.cfg` to another file for editing. The following command creates a copy named `my-defaults.cfg`:
   ```shell
   cp tests/kustomize-defaults.cfg my-defaults.cfg
   ```
2. Edit `my-defaults.cfg` and add information about your communal endpoint.

   For details about the required settings for each supported storage location, see [Communal storage settings](#communal-storage-settings). For general help about supported communal storage, see [Configuring communal storage](https://docs.vertica.com/latest/en/containerized/configuring-communal-storage/).

3. To override the default configuration file, set the `KUSTOMIZE_CFG` environment variable to `my-defaults.cfg`:

   ```shell
   export KUSTOMIZE_CFG=my-defaults.cfg
   ```

4. Run the integration tests:
   ```shell
   make run-int-tests | tee kuttl.out
   ```

#### Amazon Web Services S3

> **IMPORTANT**
> You must create the S3 bucket before you run the tests.

| Environment variable | Description                                              | Example                              |
| :------------------- | :------------------------------------------------------- | :----------------------------------- |
| `ACCESSKEY`          | Your AWS access key credential.                          |                                      |
| `SECRETKEY`          | Your AWS secret key credential.                          |                                      |
| `ENDPOINT`           | Endpoint and credentials for S3 communal access.         | `https://s3.us-east-1.amazonaws.com` |
| `REGION`             | AWS region.                                              | `us-east-1`                          |
| `S3_BUCKET`          | Name of the S3 bucket.                                   | `aws-s3-bucket-name`                 |
| `PATH_PREFIX`        | Places the communal path in a subdirectory in this repo. | `/<userID>`                          |

#### Google Cloud Storage

> **IMPORTANT**
> You must create the Google Cloud bucket before you run the tests.

| Environment variable | Description                                                                                | Example          |
| :------------------- | :----------------------------------------------------------------------------------------- | :--------------- |
| `ACCESSKEY`          | HMAC access key.                                                                           |                  |
| `SECRETKEY`          | HMAC secret key.                                                                           |                  |
| `PATH_PROTOCOL`      | Sets the Google Cloud Storage protocol.                                                    | `gs://`          |
| `BUCKET_OR_CLUSTER`  | Name of the Google Cloud bucket.                                                           | `gc-bucket-name` |
| `PATH_PREFIX`        | Identifies the developer that created the database. Must begin and end with a slash (`/`). | `/username/`     |

#### Azure Block Storage

You can access Azure Block Storage with an accountKey or shared access signature (SAS). To generate a SAS token, complete the following:

1. In [Microsoft Azure](https://portal.azure.com), go to the storage container that you want to use for testing.
2. In the left navigation, select the **Shared access tokens** link.
3. Complete the form to generate a SAS token.

| Environment variable    | Description                                                                                     | Example          |
| :---------------------- | :---------------------------------------------------------------------------------------------- | :--------------- |
| `CONTAINERNAME`         | Name of the Azure container.                                                                    | `container-name` |
| `ACCOUNTKEY`            | accountKey authentication only.                                                                 |                  |
| `SHAREDACCESSSIGNATURE` | SAS authentication only. Enclose the SAS token in quotes (`""`) to preserve special characters. | `"sas-token"`    |
| `PATH_PROTOCOL`         | Sets the Azure Block Storage protocol.                                                          | `azb://`         |
| `BUCKET_OR_CLUSTER`     | The account name.                                                                               | `account-name`   |
| `PATH_PREFIX`           | Identifies the developer that created the database. Must begin and end with a slash (`/`).      | `/username/`     |

### Pod logs

The e2e tests use [stern](https://github.com/stern/stern) to persist some pod logs to help debug any failures. The e2e tests create the `int-tests-output/` directory to store the logs.

The stern process completes when the kuttle tests run to completion. If you abort a kuttle test, then you must stop the stern process manually.

## Soak tests

The soak tests evaluate the operator over a long interval. The test is split into multiple iterations, and each iteration generates a random workload that is comprised of pod kills and scaling operations. If the tests succeed, the next iteration begins. You can set the number of iterations that the soak test runs.

Soak tests are run with kuttl, and the random test generation is done with the [kuttl-step-gen tool](cmd/kuttl-step-gen/main.go).

To run the soak tests, create a configuration file that outlines the databases that you want to test and how you want the test framework to react. We provide a sample configuration file in [tests/soak/soak-sample.cfg](./tests/soak/soak-sample.cfg).

The following steps run the soak tests:

1. Create the databases that you want to test.
2. Copy the configuration file and make your edits:

   ```shell
   cp tests/soak/soak-sample.cfg local-soak.cfg
   vim local-soak.cfg
   ```

3. Set the number of iterations that you want to run with the `NUM_SOAK_ITERATIONS` environment variable:

   ```shell
   export NUM_SOAK_ITERATIONS=10
   ```

   To run infinite soak tests, set `NUM_SOAK_ITERATIONS` to `-1`.

4. To start the tests, run the following make target:

   ```shell
   make run-soak-tests
   ```

# Troubleshooting

The following sections provide troubleshooting tips for your deployment.

## Kubernetes Events

The operator generates Kubernetes events for some key scenarios. This is helpful when you need to understand what tasks the operator is working on. Use the following command to view the events:

```shell
kubectl describe vdb mydb
...
Events:
  Type    Reason                   Age    From                Message
  ----    ------                   ----   ----                -------
  Normal  Installing               2m10s  verticadb-operator  Calling update_vertica to add the following pods as new hosts: mydb-sc1-0
  Normal  InstallSucceeded         2m6s   verticadb-operator  Successfully called update_vertica to add new hosts and it took 3.5882135s
  Normal  CreateDBStart            2m5s   verticadb-operator  Calling 'admintools -t create_db'
  Normal  CreateDBSucceeded        92s    verticadb-operator  Successfully created database with subcluster 'sc1'. It took 32.5709857s
  Normal  ClusterRestartStarted    36s    verticadb-operator  Calling 'admintools -t start_db' to restart the cluster
  Normal  ClusterRestartSucceeded  28s    verticadb-operator  Successfully called 'admintools -t start_db' and it took 8.8401312s
```

## Retrieve `vertica.log`

You might need to inspect the contents of the `vertica.log` to diagnose a problem with the Vertica server. You can SSH into a container or use a sidecar logger.

### Exec into a container

Drop into the container and navigate to the directory where it is stored:

```shell
docker exec -it <container-name> /bin/bash
cd path/to/vertica.log
```

The exact location of `vertica.log` depends on your CR. For additional details about Vertica log files, see the [Vertica documentation](https://docs.vertica.com/latest/en/admin/monitoring/monitoring-log-files/) to find the location.

### Sidecar logger

Deploy a sidecar to capture the contents of `vertica.log` and print it to STDOUT. If you use the sidecar logger, you can inspect the file with `kubectl logs`.

To use a sidecar logger in your CR, add the following into your CR:

```shell
spec:
   ...
   sidecars:
   - name: vlogger
   image: vertica/vertica-logger:latest
```

The `sidecars[i].image` shown here is a container that Vertica publishes on its docker repository. After the sidecar container is running, inspect the logs with the following command:

```shell
kubectl logs <vertica-pod-name> -c vlogger
```

1. Use `kubectl edit` to open the running deployment for editing:

   ```shell
   kubectl edit deployment verticadb-operator-manager
   ```

2. Locate the `args` array that passes values to the deployment manager, and add `--enable-profiler`:

   ```shell
         ...
         - args:
           - --health-probe-bind-address=:8081
           - --metrics-bind-address=127.0.0.1:8080
           - --leader-elect
           - --enable-profiler
           command:
           - /manager
         ...
   ```

3. Wait until the operator is redeployed.
4. Port forward 6060 to access the profiler's user interface (UI). The name of the pod differs for each deployment, so make sure that you find the one specific to your cluster:

   ```shell
   kubectl port-forward pod/verticadb-operator-manager-5dd5b54df4-2krcr 6060:6060
   ```

5. Use a web browser or the standalone tool to connect to the profiler's UI at `http://localhost:6060/debug/pprof`.
   If you use a web browser, replace `localhost` with the host that you used in the previous `kubectl port-forward` command.
   Alternatively, you can invoke the standalone tool with the following command:

   ```shell
   go tool pprof http://localhost:6060/debug/pprof
   ```
