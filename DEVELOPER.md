# Introduction

This guide explains how to set up an environment to develop and test the Vertica operator.

# Repo structure

This repo contains the following directories and files:

- `docker-vertica/`: files that build a Vertica server container. This container is designed for [Administration Tools (admintools)](https://docs.vertica.com/latest/en/admin/using-admin-tools/admin-tools-reference/) deployments.

  The build process requires that you provide a Vertica RPM package that is version 11.0.0 or higher.

- `docker-vertica-v2/`: files that build the v2 Vertica server container. This container is designed for vclusterops deployments.
- `docker-operator/`: files that build the [VerticaDB operator](https://docs.vertica.com/latest/en/containerized/db-operator/) container.
- `docker-vlogger/`: files that build the vlogger sidecar container that sends the contents of `vertica.log` to STDOUT.
- `scripts/`: scripts that run Makefile targets and execute end-to-end (e2e) tests with tools in this repository.
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
- [go](https://golang.org/doc/install) (version 1.20)
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

# Kind

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
   NAME                       STATUS   ROLES                  AGE     VERSION
   devcluster-control-plane   Ready    control-plane,master   9m41s   v1.23.0
   ```

You have a master node and control plane that is ready to deploy and Vertica Kubernetes resources locally.

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
> testcluster
> ```

# Development setup

> **IMPORTANT**
> This repo's build tools require a Vertica version 11.0.0 or higher RPM for both admintools and vcluster deployments.
>
> You must name the RPM `vertica-x86_64.RHEL6.latest.rpm` and store it in the `docker-vertica/packages/` and `docker-vertica-v2/packages/` directories:
>
> ```shell
> cp /path/to/vertica-x86_64.RHEL6.latest.rpm docker-vertica/packages/
> cp /path/to/vertica-x86_64.RHEL6.latest.rpm docker-vertica-v2/packages/
> ```

1. build and push container
2. Generate controller files
3. Run linters
4. Run unit tests
5. Run the operator
6. Run e2e tests
7. Run soak tests
8. Troubleshooting

### Custom containers

To run Vertica in Kubernetes, we need to package Vertica inside a container. This container is later referenced in the YAML file when we install the Helm chart.

By default, we create containers that are stored in the local docker daemon. The tag is either `latest` or, if running in a Kind environment, it is `kind`. You can control the container names by setting the following environment variables prior to running the make target.

- **OPERATOR_IMG**: Operator image name.
- **VERTICA_IMG**: Vertica image name. Used interchangeably for v1 and v2 containers.
- **VLOGGER_IMG**: Vertica logger sidecar image name.
- **BUNDLE_IMG**: OLM bundle image name.

If necessary, these variables can include the url of the registry. For example, `export OPERATOR_IMG=myrepo:5000/verticadb-operator:latest`.

Vertica provides two container sizes: the default, full image, and the minimal image that does not include the 240MB Tensorflow package.

### Build and push the containers

The [Makefile](./Makefile) provides targest that build the images and push them to the Kind cluster in the current context:

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
   vertica-logger       1.0.0     62661d7c7b1d   19 seconds ago   7.39MB
   verticadb-operator   1.11.2    c9681519d897   22 seconds ago   64.3MB
   vertica-k8s          1.11.2    c7e8e144911d   2 minutes ago    1.34GB
   ubuntu               lunar     639282825872   2 weeks ago      70.3MB
   ...
   ```

   - `vertica-k8s`: long-running container that runs the Vertica daemon. This container is designed for admintools deployments. For details about the admintools deployment image, see the [Dockerfile](./docker-vertica/Dockerfile). For details about the vcluster deployment image, see the [Dockerfile](./docker-vertica-v2/Dockerfile).
   - `verticadb-operator`: runs the VerticaDB operator and webhook. For details, see the [Dockerfile](./docker-operator/Dockerfile).
   - `vertica-logger`: runs the vlogger sidecar container that sends the contents of `vertica.log` to STDOUT. For details, see the [Dockerfile](./docker-vlogger/Dockerfile).

   > **NOTE**
   > OLM deployments create an image for the operator [bundle](https://github.com/operator-framework/operator-registry/blob/v1.16.1/docs/design/operator-bundle.md). The contents of this directory are generated with the `docker-build-bundle` Make target.

2. Next, you have to make these containers available to the Kind cluster's control plane. Push them to the cluster with the following make target:

   ```shell
   make docker-push
   ```

   This command honors any environment variables that you used when you created the image.

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

# Developer Workflows

### Generate controller files

The VerticaDB operator uses the [Operator SDK framework](https://sdk.operatorframework.io/). This framework provides tools that generate files so that you do not have to manually write boilerplate code.

The following make target generates these boilerplate files and the manifests that deploy the VerticaDB operator:

```shell
make generate manifests
```

> **NOTE**
> After you make changes to your development environment, you might need to regenerate these files.

## Run the VerticaDB operator

You have two options to run the VerticaDB operator:

- [Local](#local-operator): run the operator synchronously in a shell.
- [Deployment object](#deployment-object): Package the operator in a container and deploy in Kubernetes as a deployment object.

The operator is cluster-scoped for both options, so it monitors CRs in all namespaces.

### Local operator

> **NOTE**
> When you run the operator locally, you cannot run [e2e tests](#e2e-tests). You can run only [ad-hoc tests](#unit-tests).

The local deployment option is the fastest method to get the operator up and running, but it has limitations:

- A local operator does not mimic the way that the operator runs in a real Kubernetes environment.
- The webhook is disabled. The webhook requires TLS certs that are available only when the operator is packaged in a container.

#### Install

To run the operator locally, enter the following command:

```shell
make install run
```

#### Stop

To stop the operator, press **Ctrl + C**.

### Deployment object

When you run the operator as a deployment obejct, it runs in a container in a real Kubernetes environment. By default, the operator is deployed in the `verticadb-operator` namespace and it creates it if necessary.

Vertica on Kubernetes supports two deployment models: Helm chart and [Operator Lifecycle Manager (OLM)](https://olm.operatorframework.io/). You can control the deployment model by passing the `DEPLOY_WITH` environment variable to the `make` command. `DEPLOY_WITH` accepts the following arguments:

- `helm`
- `olm`

The operator pod contains a webhook, which requires TLS certificates and each deployment model is different.

#### Helm

Deploy the operator with Helm and all its prerequisites with the following command:

```shell
DEPLOY_WITH=helm make config-transformer deploy
```

The Helm charts generate a self-signed TLS certificate. You can also provide a custom TLS certificate. For details, see `webhook.certSource` in [Helm chart parameters](https://docs.vertica.com/latest/en/containerized/db-operator/helm-chart-parameters/).

#### OLM

When installing with OLM, you need to have OLM setup. For details, see the [OLM documentation](https://olm.operatorframework.io/docs/advanced-tasks/adding-admission-and-conversion-webhooks/#certificate-authority-requirements).

To deploy olm all of its prereqs, use the following command:

```shell
DEPLOY_WITH=olm make setup-olm deploy
```

#### Remove

To remove the operator, run the `undeploy` make target. This command removes the operator for both Helm and OLM deployments:

```shell
make undeploy
```

# Testing

## Linting

A linter analyzes files to asses the code quality and identify errors. Vertica on Kubernetes runs three different linters:

- [Helm lint](https://helm.sh/docs/helm/helm_lint/): Runs the chart verification test that is built into Helm.
- [golint](https://pkg.go.dev/golang.org/x/lint/golint): Runs a few linters that you can run with Go.
- [hadolint](https://github.com/hadolint/hadolint): Checks the various Dockerfiles that we have in our repo.

Run all linters with the `lint` target:

```shell
make lint
```

## Unit Tests

This repo contains unit tests for both the Helm chart and the VerticaDB operator. Run the unit tests with the following make target:

```shell
make run-unit-tests
```

Helm chart unit tests are stored in `helm-charts/verticadb-operator/tests` and use the [helm-unittest plugin](https://github.com/quintush/helm-unittest). The helm-unittest repo includes [test samples and templates](https://github.com/quintush/helm-unittest/tree/master/test/data/v3/basic) so you can model your own tests. For details about the test format, see the [helm-unittest GitHub repository](https://github.com/quintush/helm-unittest/blob/master/DOCUMENT.md).

Unit tests for the VerticaDB operator use the Go testing infrastructure. Some tests run the operator against a mock Kubernetes control plane created with [envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest). Per Go standards, test files are stored in package directories and end with `_test.go`.

## e2e Tests

The end-to-end (e2e) tests are run through Kubernetes with the kuttl testing framework. The e2e tests only run on operators that were [deployed as an object](#deployment-object).

Ensure that your Kubernetes cluster has a default storageClass. Most of the e2e tests do not specify any storageClass and use the default. For details about setting your storageClass, refer to the [Kubernetes documentation](https://kubernetes.io/docs/tasks/administer-cluster/change-default-storage-class/).

1. Push the operator with the following command (if the operator is not already present):
   ```shell
   make docker-build-operator docker-push-operator
   ```
2. Start the test with the following make target. There is a lot of output generated by the tests, so it is advisable to pipe the output to a file.
   ```shell
   make run-int-tests | tee kuttl.out
   ```

### Running Individual Tests

You can also call `kubectl kuttl` from the command line if you want more control, such as running a single test or preventing cleanup when the test ends. For example, you can run a single e2e test and persist namespace with this command:

```shell
kubectl kuttl test --test <name-of-your-test> --skip-delete
```

For a complete list of flags that you can use with kuttl, see the [kuttl docs](https://kuttl.dev/docs/cli.html#flags).

The communal endpoint must be setup prior to calling the tests in this manner. You can set that up with a call to `make setup-minio`.

### Customizing Communal Endpoints

The default endpoint for e2e tests is minio with TLS, which is created through the `make setup-minio` target. The endpoint are set for the test when calling `scripts/setup-kustomize.sh`. That script can take a config file that you can use to override the endpoint and credentials.

Here are the steps on how to override them:

1. Make a copy of tests/kustomize-defaults.cfg

   ```shell
   cp tests/kustomize-defaults.cfg my-defaults.cfg
   ```

2. Edit my-defaults.cfg by setting your own access point, bucket, region and credentials that you want to use.

3. Set the KUSTOMIZE_CFG environment vairable to point to my-defaults.cfg

   ```shell
   export KUSTOMIZE_CFG=my-defaults.cfg
   ```

4. Setup the commmunal endpoint.

   1. AWS S3 BUCKET

      If you have an AWS account, you can configure your environment so that it uses AWS instead of minio.

      `Prerequisite:` The s3 bucket must already be created prior to running the tests.

      Here are the steps to set that up:

      - Edit my-defaults.cfg and fill in the details.

        - Fill in the ACCESSKEY and SECRETKEY with your unique IDs.
        - Use the chart below to know what to fill in depending on what AWS region you want to write to.

        | Env Name                   |                                                   Description |            Sample value            |
        | :------------------------- | ------------------------------------------------------------: | :--------------------------------: |
        | ENDPOINT                   | Endpoint and credentials for s3 communal access in the tests. | https://s3.us-east-1.amazonaws.com |
        | REGION                     |                                               The AWS region. |             us-east-1              |
        | S3_BUCKET                  |                                This is the name of the bucket |         aws-s3-bucket-name         |
        | PATH_PREFIX                |     his is used to place the communal path in a subdirectory. |             /\<userID>             |
        | COMMUNAL_EP_CERT_SECRET    |                                                               |           \<leave blank>           |
        | COMMUNAL_EP_CERT_NAMESPACE |                                                               |           \<leave blank>           |

   2. Google Cloud Storage

      `Prerequisite:` The Google Cloud bucket must already be created prior to running the tests.

      Here are the steps:

      1. You need to create a ‘User account HMAC’. This will give you an access key and secret that you can use later on.
      2. Edit my-defaults.cfg and fill in the details. Use the chart below as a guide.

      | Env Name          |                                                                                                       Description |  Sample value  |
      | :---------------- | ----------------------------------------------------------------------------------------------------------------: | :------------: |
      | ACCESSKEY         |                                                       Use the access key that you got when you generated the HMAC |
      | SECRETKEY         |                                                           Use the secret that you got when you generated the HMAC |
      | PATH_PROTOCOL     |                                                       This tells the kustomize scripts to setup for Google cloud. |     gs://      |
      | BUCKET_OR_CLUSTER |                                                                                        Name of the bucket to use. | gc-bucket-name |
      | PATH_PREFIX       | Include your user name here so that all dbs that we know who created the DBs. It must begin and end with a slash. |   /johndoe/    |

   3. Azure Blob Storage

      1. You have to decide whether to connect with the accountKey or a shared access signature (SAS). If it is a SAS, you can generate one in a self-serve manner.
         - In the WebUI (https://portal.azure.com) go to the storage container that you want access to
         - On the left is a link called “Shared access tokens”. Click that.
         - Fill in the form to create a SAS token.
      2. Edit my-defaults.cfg and fill in the details. Use the chart below as a guide.

      | Env Name              |                                                                                                                                                                               Description |  Sample value  |
      | :-------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------: | :------------: |
      | CONTAINERNAME         |                                                                                                                                                               Name of the azure container | container-name |
      | ACCOUNTKEY            |                                                                                                      If authenticating with an account key, fill this in. Otherwise it can be left blank. |
      | SHAREDACCESSSIGNATURE | If authenticating with a SAS, fill this in. This can be left blank if using an accountKey. Before to include it in quotes ("") because of the special characters that are typically used. |
      | PATH_PROTOCOL         |                                                                                                                                  Set this to tell the e2e tests that Azure is being used. |     azb://     |
      | BUCKET_OR_CLUSTER     |                                                                                                                                                                  Fill in the account name |  account-name  |
      | PATH_PREFIX           |                                                                         Include your user name here so that all dbs that we know who created the DBs. It must begin and end with a slash. |   /johndoe/    |

5. Run the integration tests.
   ```shell
   kubectl kuttl test
   ```

### Stern output

The e2e tests use stern to save off logs of some pods. This is done to aid in debugging any failures. If needed, the logs are stored in the `int-tests-output` directory by default. Cleanup of the stern process is only done if kuttl runs to completition. If you abort the kuttl run, then you will need to stop the stern process manually.

## Soak Tests

The soak test will test the operator over a long interval. It splits the test into multiple iterations. Each iteration generates a random workload that is comprised of pod kills and scaling. At the end of each iteration, the test waits for everything to come up. If the test is successful, it proceeds to another iteration. It repeats this process for a set number of iterations or indefinitely.

The tests in an iteration are run through kuttl. The random test generation is done by the kuttl-step-gen tool.

Here are the steps needed to run this test.

1. Create the databases that you want to test.
2. Create a config file to outline the databases to test and how you want the test framework to react. A sample one can be found in tests/soak/soak-sample.cfg.

```shell
cp tests/soak/soak-sample.cfg local-soak.cfg
vim local-soak.cfg
```

3. Decide on the number of iterations you would like to run:

```shell
export NUM_SOAK_ITERATIONS=10  # Can use -1 for infinite
```

4. Kick off the run.

```shell
make run-soak-tests
```

## Help

To see the full list of make targets, run the following command:

```shell
make help
```

# Add a changelog entry

The changelog file is generated by [Changie](https://github.com/miniscruff/changie). It separates the changelog generation from commit history, so any PR that has a notable change should add new changie entries.

In general you don't necessaryily need to set this up. One of the maintainers usually add changie entries to PRs that require them.

# Troubleshooting

The following sections provide troubleshooting tips for your deployment.

## Kubernetes Events

The operator generates Kubernetes events for some key scenarios. This can be a useful tool when trying to understand what the operator is doing. Use the following command to view the events:

```shell
kubectl describe vdb mydb

...<snip>...
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

## Retrieving Logs with vertica.log

You might need to inspect the contents of the vertica.log to diagnose a problem with the Vertica server. There are a few ways this can be done:

- Drop into the container and navigate to the directory where is is stored. The exact location depends on your CR. You can refer to the [Vertica documentation](https://www.vertica.com/docs/11.0.x/HTML/Content/Authoring/AdministratorsGuide/Monitoring/Vertica/MonitoringLogFiles.htm) to find the location.

- Deploy a sidecar to capture the vertica.log and print it to stdout. If this sidecar is enabled you can use `kubectl logs` to inspect it. This sidecar can be used by adding the following into your CR:

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

## Memory Profiling

The memory profiler lets you view where the big allocations are occurring and to help detect any memory leaks. The toolset is [Google's pprof](https://golang.org/pkg/net/http/pprof/).

By default, the memory profiler is disabled. To enable it, add a parameter when you start the operator. The following steps enable the memory profiler for a deployed operator.

1. Use `kubectl edit` to open the running deployment for editing:

   ```shell
   kubectl edit deployment verticadb-operator-controller-manager
   ```

2. Locate where the arguments are passed to the manager, and add `--enable-profiler`:

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
4. Port forward 6060 to access the webUI for the profiler. The name of the pod differs for each deployment, so be sure to find the one specific to your cluster:

   ```shell
   kubectl port-forward pod/verticadb-operator-controller-manager-5dd5b54df4-2krcr 6060:6060
   ```

5. Use a web browser or the standalone tool to connect to `http://localhost:6060/debug/pprof`.
   If you use a web browser, replace `localhost` with the host that you used in the previous `kubectl port-forward` command.
   Invoke the standalone tool with the following command:

   ```shell
   go tool pprof http://localhost:6060/debug/pprof
   ```
