# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 25.3.0-0
export VERSION

# VLOGGER_VERSION defines the version to use for the Vertica logger image
# (see docker-vlogger).  This version is separate from VERSION above in
# order to have a different release cadence.
#
# When changing this, be sure to update the tags in docker-vlogger/README.md
VLOGGER_VERSION ?= 2.0.0

REPO_DIR:=$(dir $(word $(words $(MAKEFILE_LIST)),$(MAKEFILE_LIST)))

# Current location of the kustomize config.  This dictates, amoung other things
# what communal endpoint to use for the e2e tests.  It reads in the contents
# and sets the environment variables that are present.
include tests/kustomize-defaults.cfg
KUSTOMIZE_CFG?=$(REPO_DIR)/tests/kustomize-defaults.cfg
include $(KUSTOMIZE_CFG)

# The location of the config file to use for the soak run. If this isn't set,
# then the `make run-soak-tests` target will fail.
SOAK_CFG?=local-soak.cfg
# The number of iterations to run the soak test for. A negative number will
# cause an infinite number of iterations to run.
NUM_SOAK_ITERATIONS?=1

# CHANNELS define the bundle channels used in the bundle:
# - stable: This was the channel named for the first version of the operator
#   when it was namespace scoped.
# - v2-stable: This is the new channel name to use for cluster scoped operator.
#   This corresponds with the 2.0.0 release of the operator.
CHANNELS=v2-stable
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
DEFAULT_CHANNEL?=v2-stable
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
BUNDLE_DOCKERFILE=docker-bundle/Dockerfile

LOGDIR?=$(shell pwd)

# Command we run to see if we are running in a kind environment
KIND_CHECK:=kubectl get node -o=jsonpath='{.items[0].spec.providerID}' 2> /dev/null | grep 'kind://' -c

# By default, we set the version of our operator as the TAG

TAG ?= $(VERSION)

# We pick an image tag based on the environment we are in.  We special case kind
# environments because we need to use a different imagePullPolicy -- kind
# environments load the images through 'kind load docker-image' so must use IfNotPresent.
# Note, the imagePullPolicy is the default picked by kubernetes that depends on the tag.
#
# Env      imagePullPolicy
# ---      ---------------
# kind       IfNotPresent
# other      Always

ifeq ($(shell $(KIND_CHECK)), 1)
  HELM_IMAGE_PULL_POLICY ?= IfNotPresent
else
  HELM_IMAGE_PULL_POLICY ?= Always
endif

# Image Repo to use when pushing/pulling any image
IMG_REPO?=
# Image URL to use for building/pushing of the operator
OPERATOR_IMG ?= $(IMG_REPO)verticadb-operator:$(TAG)
export OPERATOR_IMG
# Image URL to use for building/pushing of the vertica server
VERTICA_IMG ?= $(IMG_REPO)vertica-k8s:$(TAG)
export VERTICA_IMG
# This is the base image to use for some upgrade tests.  We will always
# upgrade to VERTICA_IMG, so BASE_VERTICA_IMG must be some image from a
# version earlier than VERTICA_IMG.
# Note, not all upgrade tests use this.  Some upgrade between one of the
# official vertica images and a bad-image.
#
# There is no default value for this image.  Any test will fail that requires
# this but it isn't set.
BASE_VERTICA_IMG ?= <not-set>
export BASE_VERTICA_IMG
# Image URL to use for the logger sidecar
VLOGGER_IMG ?= $(IMG_REPO)vertica-logger:$(VLOGGER_VERSION)
export VLOGGER_IMG
# If the current leg in the CI tests is leg-9
LEG9 ?= no
export LEG9
# What alpine image does the vlogger image use
VLOGGER_BASE_IMG?=alpine
# What version of alpine does the vlogger image use
VLOGGER_ALPINE_VERSION?=3.19
# The port number for the local registry
REG_PORT ?= 5000
# Image URL to use for the bundle.  We special case kind because to use it with
# kind it must be pushed to a local registry.
ifeq ($(shell $(KIND_CHECK)), 1)
BUNDLE_IMG ?= localhost:$(REG_PORT)/verticadb-operator-bundle:$(TAG)
else
# BUNDLE_IMG defines the repo/image:tag used for the bundle.
# You can use it as an arg. (E.g make docker-build-bundle BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMG_REPO)verticadb-operator-bundle:$(TAG)
endif
export BUNDLE_IMG

# USE_IMAGE_DIGESTS_FLAG are the flag passed to the operator-sdk generate bundle command
# to enable the use of SHA Digest for images
USE_IMAGE_DIGESTS_FLAG=

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	USE_IMAGE_DIGESTS_FLAG = -u
endif

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.30.0

# Image URL for the OLM catalog.  This is for testing purposes only.
ifeq ($(shell $(KIND_CHECK)), 1)
OLM_CATALOG_IMG ?= localhost:$(REG_PORT)/olm-catalog:$(TAG)
else
OLM_CATALOG_IMG ?= olm-catalog:$(TAG)
endif
export OLM_CATALOG_IMG

# Name of the namespace to deploy prometheus 
PROMETHEUS_NAMESPACE?=prometheus
# Prometheus variables that we wil be used for deployment 
PROMETHEUS_HELM_NAME?=prometheus
PROMETHEUS_INTERVAL?=5s
# The Prometheus adapter name and namespace used in VerticaAutoscaler
PROMETHEUS_ADAPTER_NAME ?= prometheus-adapter
PROMETHEUS_ADAPTER_NAMESPACE ?= prometheus-adapter
PROMETHEUS_ADAPTER_REPLICAS ?= 1
# The Prometheus service URL and port for Prometheus adapter to connect to
PROMETHEUS_TLS_URL ?= https://$(PROMETHEUS_HELM_NAME)-kube-prometheus-prometheus.$(PROMETHEUS_NAMESPACE).svc
PROMETHEUS_URL ?= http://$(PROMETHEUS_HELM_NAME)-kube-prometheus-prometheus.$(PROMETHEUS_NAMESPACE).svc
PROMETHEUS_PORT ?= 9090
DB_USER?=dbadmin
DB_PASSWORD?=
VDB_NAME?=verticadb-sample
VDB_NAMESPACE?=default

# Set this to YES if you want to create a vertica image of minimal size
MINIMAL_VERTICA_IMG ?=
# Name of the helm release that we will install/uninstall
HELM_RELEASE_NAME?=vdb-op
# Can be used to specify additional overrides when doing the helm install.
# For example to specify a custom webhook tls cert when deploying use this command:
#   HELM_OVERRIDES="--set webhook.tlsSecret=custom-cert" make deploy-operator
HELM_OVERRIDES ?=
PROMETHEUS_HELM_OVERRIDES ?=
PROMETHEUS_ADAPTER_HELM_OVERRIDES ?=
# Set this to true if you want to install grafana with the operator.
GRAFANA_ENABLED ?= false
# Set this to true if you want to install prometheus with the operator.
PROMETHEUS_ENABLED ?= false
# Maximum number of tests to run at once. (default 2)
# Set it to any value not greater than 8 to override the default one
E2E_PARALLELISM?=2
export E2E_PARALLELISM
# Set the e2e test directories.  We will include just a single test suite for
# now. If you want to use multiple, separate them with spaces. Any test can be
# driven separately using the `kubectl test --test=<testcase>` syntax.
E2E_TEST_DIRS?=tests/e2e-leg-1
# Additional arguments to pass to 'kubectl kuttl'
E2E_ADDITIONAL_ARGS?=

# Target Architecture that the docker image can run on
# By Default: linux/amd64,linux/arm64
# If you wish to build the image targeting other platforms you can use the --platform flag: https://docs.docker.com/build/building/multi-platform/
# (i.e. docker buildx build --platform=linux/amd64,linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
TARGET_ARCH?=linux/amd64

#
# Deployment Variables
# ====================
#
# The following set of variables get passed down to the operator through a
# configMap. Each variable that we export here is included in the config/
# kustomize bundle (see config/manager/operator-env).
#
# Specify how to deploy the operator.  Allowable values are 'helm' or 'olm'.
# When deploying with olm, it is expected that `make setup-olm` has been run
# already.
DEPLOY_WITH?=helm
export DEPLOY_WITH
#
# Set this to allow us to enable/disable the controllers in the operator.
# Disabling the controller will force the operator just to serve webhook
# requests.
CONTROLLERS_ENABLED?=true
export CONTROLLERS_ENABLED
#
# Set this to control if the webhook is enabled or disabled in the operator.
WEBHOOKS_ENABLED?=true
export WEBHOOKS_ENABLED

#set this to increase the threshold used by spam filter
BROADCASTER_BURST_SIZE?=100
export BROADCASTER_BURST_SIZE
#
# Use this to control what scope the controller is deployed at. It supports two
# values:
# - cluster - controllers are cluster scoped and will watch for objects in any
#             namespace
# - namespace - controllers are scoped to a single namespace and will watch for
#               objects in the namespace where the manager is deployed.
CONTROLLERS_SCOPE?=cluster
export CONTROLLERS_SCOPE

# Use this to control the maximum backoff duration for VDBcontroller
VDB_MAX_BACKOFF_DURATION?=1000
export VDB_MAX_BACKOFF_DURATION

# Use this to control the maximum backoff duration for VDBcontroller
SANDBOX_MAX_BACKOFF_DURATION?=1000
export SANDBOX_MAX_BACKOFF_DURATION

#
# The address the operators Prometheus metrics endpoint binds to. Setting this
# to 0 will disable metric serving.
METRICS_ADDR?=0.0.0.0:8443
export METRICS_ADDR
#
# The secret name that will be used to mount cert files in the operator
# for providing server certs to Prometheus metrics endpoint. Setting this
# to "" will use an auto-generated self-signed cert.
export METRICS_TLS_SECRET
#
# Controls exposing of the prometheus metrics endpoint. The valid values are:
# EnableWithAuth: A new service object will be created that exposes the
#    metrics endpoint.  Access to the metrics are controlled by rbac rules.
#    The metrics endpoint will use the https scheme.
# EnableWithoutAuth: Like EnableWithAuth, this will create a service
#    object to expose the metrics endpoint.  However, there is no authority
#    checking when using the endpoint.  Anyone who had network access
#    endpoint (i.e. any pod in k8s) will be able to read the metrics.  The
#    metrics endpoint will use the http scheme.
# EnableWithTLS: Like EnableWithAuth, this will create a service
#    object to expose the metrics endpoint.  However, there is no authority
#    checking when using the endpoint.  People with network access to the
#    endpoint (i.e. any pod in k8s) and the correct certs can read the metrics.
#    The metrics endpoint will use the https scheme. 
#    It needs to be used with tlsSecret. If tlsSecret is not set, the behavior
#    will be similar to EnableWithoutAuth, except that the endpoint will use 
#    https schema.
# Disable: Prometheus metrics are not exposed at all.
METRICS_EXPOSE_MODE?=Disable
export METRICS_EXPOSE_MODE
#
# The minimum logging level. Valid values are: debug, info, warn, and error.
LOG_LEVEL?=info
export LOG_LEVEL
#
# The operators concurrency with each CR. If the number is > 1, this means the
# operator can reconcile multiple CRs at the same time. Note, the operator never
# parallelizes reconcile iterations for the same CR. Only distinct CRs can be
# reconciled in parallel.
CONCURRENCY_VERTICADB?=5
CONCURRENCY_VERTICAAUTOSCALER?=1
CONCURRENCY_EVENTTRIGGER?=1
CONCURRENCY_VERTICARESTOREPOINTSQUERY?=1
CONCURRENCY_VERTICASCRUTINIZE?=1
CONCURRENCY_SANDBOXCONFIGMAP?=1
CONCURRENCY_VERTICAREPLICATOR?=3
export CONCURRENCY_VERTICADB \
  CONCURRENCY_VERTICAAUTOSCALER \
  CONCURRENCY_EVENTTRIGGER \
  CONCURRENCY_VERTICARESTOREPOINTSQUERY \
  CONCURRENCY_VERTICASCRUTINIZE \
  CONCURRENCY_SANDBOXCONFIGMAP \
  CONCURRENCY_VERTICAREPLICATOR

# Clear this variable if you don't want to wait for the helm deployment to
# finish before returning control. This exists to allow tests to attempt deploy
# when it should fail.
DEPLOY_WAIT?=--wait
# Name of the test OLM catalog that we will create and deploy with in e2e tests
OLM_TEST_CATALOG_SOURCE=e2e-test-catalog
# Name of the namespace to deploy the operator in
NAMESPACE?=verticadb-operator

# The Go version that we will build the operator with
GO_VERSION?=1.23.5
GOPATH?=${HOME}/go
TMPDIR?=$(PWD)
HELM_UNITTEST_VERSION?=3.9.3-0.2.11
KUTTL_PLUGIN_INSTALLED:=$(shell kubectl krew list 2>/dev/null | grep -c '^kuttl')
STERN_PLUGIN_INSTALLED:=$(shell kubectl krew list 2>/dev/null | grep -c '^stern')
OPERATOR_CHART = $(shell pwd)/helm-charts/verticadb-operator
PROMETHEUS_CHART=prometheus-community/kube-prometheus-stack

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

default: help

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(firstword $(MAKEFILE_LIST))

##@ Development

.PHONY: manifests
manifests: controller-gen go-generate ## Generate Role and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen go-generate ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: go-generate
go-generate:
	go generate ./...

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: go-generate ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet lint envtest helm-ut ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

.PHONY: helm-ut
helm-ut: ## Run the helm unittest
	docker run -i $(shell [ -t 0 ] && echo '-t') --rm -v $(OPERATOR_CHART):/apps quintush/helm-unittest:$(HELM_UNITTEST_VERSION) -3 .

.PHONY: lint
lint: config-transformer golangci-lint ## Lint the helm charts and the Go operator
	$(GOLANGCI_LINT) run
	helm lint $(OPERATOR_CHART)
	scripts/dockerfile-lint

.PHONY: run-unit-tests
run-unit-tests: test ## Run unit tests

.PHONY: install-kuttl-plugin
install-kuttl-plugin: krew
ifeq ($(KUTTL_PLUGIN_INSTALLED), 0)
	kubectl krew install kuttl
endif

.PHONY: install-stern-plugin
install-stern-plugin: krew
ifeq ($(STERN_PLUGIN_INSTALLED), 0)
	kubectl krew install stern
endif

.PHONY: init-e2e-env
init-e2e-env: install-kuttl-plugin install-stern-plugin kustomize ## Download necessary tools to run the integration tests

.PHONY: run-int-tests
run-int-tests: init-e2e-env vdb-gen cert-gen setup-e2e-communal ## Run the integration tests
ifeq ($(DEPLOY_WITH), $(filter $(DEPLOY_WITH), olm))
	$(MAKE) setup-olm
endif
	kubectl kuttl test --artifacts-dir ${LOGDIR} --parallel $(E2E_PARALLELISM) $(E2E_ADDITIONAL_ARGS) $(E2E_TEST_DIRS)

WAIT_TIME = 120s
run-scorecard-tests: bundle ## Run the scorecard tests
	$(OPERATOR_SDK) scorecard bundle --wait-time $(WAIT_TIME)

.PHONY: run-server-upgrade-tests
run-server-upgrade-tests: install-kuttl-plugin install-stern-plugin setup-e2e-communal ## Run integration tests for Vertica server upgrade
ifeq ($(DEPLOY_WITH), $(filter $(DEPLOY_WITH), olm))
	$(MAKE) setup-olm
endif
ifeq ($(BASE_VERTICA_IMG), <not-set>)
	$(error $$BASE_VERTICA_IMG not set)
endif
	kubectl kuttl test --report xml --artifacts-dir ${LOGDIR} --parallel $(E2E_PARALLELISM) $(E2E_ADDITIONAL_ARGS) tests/e2e-server-upgrade/

.PHONY: run-soak-tests
run-soak-tests: install-kuttl-plugin kuttl-step-gen  ## Run the soak tests
	scripts/soak-runner.sh -i $(NUM_SOAK_ITERATIONS) $(SOAK_CFG)

setup-e2e-communal: ## Setup communal endpoint for use with e2e tests
ifeq ($(PATH_PROTOCOL), s3://)
	$(MAKE) setup-minio
else ifeq ($(PATH_PROTOCOL), azb://)
	$(MAKE) setup-azurite
else ifeq ($(PATH_PROTOCOL), /)
	@echo "Nothing to setup for PATH_PROTOCOL=/"
else
	$(error cannot setup communal endpoint for this protocol: $(PATH_PROTOCOL))
	exit 1
endif

.PHONY: setup-minio
setup-minio: install-cert-manager install-kuttl-plugin ## Setup minio for use with the e2e tests
	scripts/setup-minio.sh

.PHONY: setup-azurite
setup-azurite: ## Setup azurite for use with the e2e tests
	scripts/setup-azurite.sh

.PHONY: setup-olm
setup-olm: operator-sdk bundle docker-build-bundle docker-push-bundle docker-build-olm-catalog docker-push-olm-catalog
	scripts/setup-olm.sh $(OLM_TEST_CATALOG_SOURCE)

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/operator/main.go

.PHONY: docker-build-operator
docker-build-operator: manifests generate fmt vet ## Build operator docker image with the manager.
	docker pull golang:${GO_VERSION} # Ensure we have the latest Go lang version
	docker buildx build \
		--tag ${OPERATOR_IMG} \
		--load \
		--platform ${TARGET_ARCH} \
		--build-arg GO_VERSION=${GO_VERSION} \
		-f docker-operator/Dockerfile .

docker-build-vlogger:  ## Build vertica logger docker image
	docker pull ${VLOGGER_BASE_IMG}:${VLOGGER_ALPINE_VERSION} # Ensure we have the latest alpine version
	cd docker-vlogger \
	&& docker buildx build \
		-t ${VLOGGER_IMG} \
		--load \
		--platform ${TARGET_ARCH} \
		--build-arg BASE_IMG=${VLOGGER_BASE_IMG} \
		--build-arg ALPINE_VERSION=${VLOGGER_ALPINE_VERSION} \
		-f Dockerfile .

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker buildx build --platform=linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-push-operator
docker-push-operator: ## Push operator docker image with the manager.
ifeq ($(shell $(KIND_CHECK)), 0)
	docker push ${OPERATOR_IMG}
else
	scripts/push-to-kind.sh -i ${OPERATOR_IMG}
endif

.PHONY: docker-push-vlogger
docker-push-vlogger:  ## Push vertica logger docker image
ifeq ($(shell $(KIND_CHECK)), 0)
	docker push ${VLOGGER_IMG}
else
	scripts/push-to-kind.sh -i ${VLOGGER_IMG}
endif

# Vertica client proxy is a pre-built image that we don't build. For this
# reason, pushing this image will just put it in kind and never to docker.
.PHONY: docker-push-vproxy
docker-push-vproxy:  ## Push vertica client proxy docker image
ifneq ($(strip $(VPROXY_IMG)),)
ifeq ($(shell $(KIND_CHECK)), 0)
	docker push ${VPROXY_IMG}
else
	scripts/push-to-kind.sh -i ${VPROXY_IMG}
endif
else
	$(info VPROXY_IMG is not set. Skipped pushing proxy image to K8s cluster.)
endif

# We have two versions of the vertica-k8s image. This is a staging effort. A
# new version is being created that has no admintools and relies exclusively on
# http REST interfaces. Eventually, we will go back to one version using the
# next generation one as *THE* vertica-k8s image.

# Using --no-cache is important so that we pick up the latest security fixes.
# Otherwise, we risk skipping the step in the docker build when we pull the
# latest base image.
VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS?="--no-cache --load"

.PHONY: docker-build-vertica
docker-build-vertica: docker-vertica/Dockerfile ## Build vertica server docker image
	cd docker-vertica \
	&& make \
		VERTICA_IMG=${VERTICA_IMG} \
		MINIMAL_VERTICA_IMG=${MINIMAL_VERTICA_IMG} \
		VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS="${VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS}"

.PHONY: docker-build-vertica-v2
docker-build-vertica-v2: docker-vertica-v2/Dockerfile ## Build next generation vertica server docker image
	cd docker-vertica-v2 \
	&& make \
		VERTICA_IMG=${VERTICA_IMG} \
		MINIMAL_VERTICA_IMG=${MINIMAL_VERTICA_IMG} \
		TARGET_ARCH=${TARGET_ARCH} \
		VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS=${VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS}

.PHONY: docker-push-vertica
docker-push-vertica:  ## Push vertica server image -- either v1 or v2.
ifeq ($(shell $(KIND_CHECK)), 0)
	docker push ${VERTICA_IMG}
else
	scripts/push-to-kind.sh -i ${VERTICA_IMG}
endif

# Normally the base image is a pre-built image that we don't build. For this
# reason, pushing this image will just put it in kind and never to docker.
.PHONY: docker-push-base-vertica
docker-push-base-vertica:  ## Push base vertica server image to kind
ifneq ($(BASE_VERTICA_IMG), <not-set>)
ifeq ($(shell $(KIND_CHECK)), 1)
	scripts/push-to-kind.sh -i ${BASE_VERTICA_IMG}
endif
endif

.PHONY: docker-push-extra-vertica
docker-push-extra-vertica: # Push a hard-coded image used in multi-online-upgrade test
ifeq ($(LEG9), yes)
ifeq ($(shell $(KIND_CHECK)), 1)
	scripts/push-to-kind.sh -i opentext/vertica-k8s-private:20250517-minimal
endif
endif

# PLATFORMS defines the target platforms that the image will be used for. Use
# this with docker-build-crossplatform-* targets.
PLATFORMS?=linux/arm64,linux/amd64

## Note: Deprecate this as we can use docker-build-operator as the single make target to build either a single or multiarch image
## Keep this as we still use this target to push external images
.PHONY: docker-build-crossplatform-operator
docker-build-crossplatform-operator: manifests generate fmt vet ## Build and push operator image for cross-platform support
	@echo "Deprecated. Please use docker-build-operator target with TARGET_ARCH argument"
	docker pull golang:${GO_VERSION} # Ensure we have the latest Go lang version
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' docker-operator/Dockerfile > Dockerfile.cross
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	docker buildx build \
		--push \
		--platform=$(PLATFORMS) \
		--build-arg GO_VERSION=${GO_VERSION} \
		--tag ${OPERATOR_IMG} \
		-f Dockerfile.cross \
		.
	- docker buildx rm project-v3-builder
	rm Dockerfile.cross

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
ifneq ($(DEPLOY_WITH), $(filter $(DEPLOY_WITH), olm))
	$(error Bundle can only be generated when deploying with OLM.  Current deployment method: $(DEPLOY_WITH))
endif
	scripts/gen-csv.sh $(USE_IMAGE_DIGESTS_FLAG)  $(VERSION) $(BUNDLE_METADATA_OPTS)
	mv bundle.Dockerfile $(BUNDLE_DOCKERFILE)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: docker-build-bundle
docker-build-bundle: bundle ## Build the bundle image
	docker buildx build --load -f $(BUNDLE_DOCKERFILE) -t $(BUNDLE_IMG) .

.PHONY: docker-push-bundle
docker-push-bundle: ## Push the bundle image
	docker push $(BUNDLE_IMG)

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: docker-build-olm-catalog
docker-build-olm-catalog: opm ## Build an OLM catalog that includes our bundle (testing purposes only)
	$(OPM) index add --mode semver --bundles $(BUNDLE_IMGS) --tag $(OLM_CATALOG_IMG) --container-tool docker --skip-tls $(FROM_INDEX_OPT)

.PHONY: docker-push-olm-catalog
docker-push-olm-catalog:
	docker push $(OLM_CATALOG_IMG)

.PHONY: docker-build
docker-build: docker-build-vertica-v2 docker-build-operator docker-build-vlogger ## Build all docker images except OLM catalog

.PHONY: docker-push
docker-push: docker-push-vertica docker-push-base-vertica docker-push-extra-vertica docker-push-operator docker-push-vlogger docker-push-vproxy ## Push all docker images except OLM catalog

.PHONY: echo-images
echo-images:  ## Print the names of all of the images used
	@echo "OPERATOR_IMG=$(OPERATOR_IMG)"
	@echo "VERTICA_IMG=$(VERTICA_IMG)"
	@echo "BASE_VERTICA_IMG=$(BASE_VERTICA_IMG)"
	@echo "VLOGGER_IMG=$(VLOGGER_IMG)"
	@echo "VPROXY_IMG=$(VPROXY_IMG)"
	@echo "BUNDLE_IMG=$(BUNDLE_IMG)"
	@echo "OLM_CATALOG_IMG=$(OLM_CATALOG_IMG)"

.PHONY: kuttl-step-gen
kuttl-step-gen: ## Builds the kuttl-step-gen tool
	go build -o bin/$@ ./cmd/$@

.PHONY: vdb-gen
vdb-gen: generate manifests ## Builds the vdb-gen tool
	CGO_ENABLED=0 go build -o bin/$@ ./cmd/$@

.PHONY: cert-gen
cert-gen: ## Builds the cert-gen tool
	CGO_ENABLED=0 go build -o bin/$@ ./cmd/$@

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = true
endif

# When changing this version be sure to update tests/external-images-common-ci.txt
CERT_MANAGER_VER=1.5.3
.PHONY: install-cert-manager
install-cert-manager: ## Install the cert-manager
	kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v$(CERT_MANAGER_VER)/cert-manager.yaml
	scripts/wait-for-cert-manager-ready.sh -t 180

.PHONY: uninstall-cert-manager
uninstall-cert-manager: ## Uninstall the cert-manager
	kubectl delete -f https://github.com/jetstack/cert-manager/releases/download/v$(CERT_MANAGER_VER)/cert-manager.yaml

.PHONY: config-transformer
config-transformer: manifests kustomize kubernetes-split-yaml helm-dependency-update ## Generate release artifacts and helm charts from config/
	scripts/config-transformer.sh

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side=true --force-conflicts -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -


# For helm, we always include priv-reg-cred as an image pull secret.  This
# secret is created in e2e tests when we run with a private container registry.
# If this secret does not exist then it is simply ignored.
deploy-operator: manifests kustomize ## Using helm or olm, deploy the operator in the K8s cluster
ifeq ($(DEPLOY_WITH), helm)
	$(MAKE) helm-dependency-update
	helm install $(DEPLOY_WAIT) -n $(NAMESPACE) --create-namespace $(HELM_RELEASE_NAME) $(OPERATOR_CHART) --set image.repo=null --set image.name=${OPERATOR_IMG} --set image.pullPolicy=$(HELM_IMAGE_PULL_POLICY) --set imagePullSecrets[0].name=priv-reg-cred --set controllers.scope=$(CONTROLLERS_SCOPE) --set controllers.vdbMaxBackoffDuration=$(VDB_MAX_BACKOFF_DURATION) --set controllers.sandboxMaxBackoffDuration=$(SANDBOX_MAX_BACKOFF_DURATION) --set grafana.enabled=${GRAFANA_ENABLED} --set prometheus-server.enabled=${PROMETHEUS_ENABLED}  $(HELM_OVERRIDES) --set cache.enable=$(CACHE_ENABLED)
	scripts/wait-for-webhook.sh -n $(NAMESPACE) -t 60
else ifeq ($(DEPLOY_WITH), olm)
	scripts/deploy-olm.sh -n $(NAMESPACE) $(OLM_TEST_CATALOG_SOURCE)
	scripts/wait-for-webhook.sh -n $(NAMESPACE) -t 60
else
	$(error Unknown deployment method: $(DEPLOY_WITH))
endif

deploy-webhook: manifests kustomize ## Using helm, deploy just the webhook in the k8s cluster
ifeq ($(DEPLOY_WITH), helm)
	helm install $(DEPLOY_WAIT) -n $(NAMESPACE) --create-namespace $(HELM_RELEASE_NAME) $(OPERATOR_CHART) --set image.repo=null --set image.name=${OPERATOR_IMG} --set image.pullPolicy=$(HELM_IMAGE_PULL_POLICY) --set imagePullSecrets[0].name=priv-reg-cred --set webhook.enable=true,controllers.enable=false $(HELM_OVERRIDES)
	scripts/wait-for-webhook.sh -n $(NAMESPACE) -t 60
else
	$(error Unsupported deployment method for webhook only: $(DEPLOY_WITH))
endif

.PHONY: deploy-prometheus
deploy-prometheus:
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo update
	helm install $(DEPLOY_WAIT) -n $(PROMETHEUS_NAMESPACE) --create-namespace $(PROMETHEUS_HELM_NAME) $(PROMETHEUS_CHART) --values prometheus/values.yaml $(PROMETHEUS_HELM_OVERRIDES)

.PHONY: deploy-prometheus-tls
deploy-prometheus-tls:
	kubectl create ns $(PROMETHEUS_NAMESPACE) || true
	scripts/setup-prometheus-tls.sh $(PROMETHEUS_TLS_URL) $(PROMETHEUS_HELM_NAME)
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo update
	helm install $(DEPLOY_WAIT) -n $(PROMETHEUS_NAMESPACE) --create-namespace $(PROMETHEUS_HELM_NAME) $(PROMETHEUS_CHART) --values prometheus/values-tls.yaml $(PROMETHEUS_HELM_OVERRIDES)

.PHONY: undeploy-prometheus
undeploy-prometheus: undeploy-prometheus-service-monitor-by-release
	helm uninstall $(PROMETHEUS_HELM_NAME) -n $(PROMETHEUS_NAMESPACE)

.PHONY: port-forward-prometheus
port-forward-prometheus:  ## Expose the prometheus endpoint so that you can connect to it through http://localhost:9090
	kubectl port-forward -n $(PROMETHEUS_NAMESPACE) svc/$(PROMETHEUS_HELM_NAME)-kube-prometheus-prometheus 9090

.PHONY: port-forward-prometheus-server
port-forward-prometheus-server:  ## Expose the prometheus endpoint so that you can connect to it through http://localhost:9090
	kubectl port-forward -n $(NAMESPACE) svc/$(HELM_RELEASE_NAME)-prometheus-server-prometheus 9090

.PHONY: port-forward-grafana
port-forward-grafana:  ## Expose the grafana endpoint so that you can connect to it through http://localhost:3000
	kubectl port-forward -n $(NAMESPACE) svc/$(HELM_RELEASE_NAME)-grafana 3000:80

.PHONY: deploy-prometheus-service-monitor
deploy-prometheus-service-monitor:
	scripts/deploy-prometheus.sh -n $(VDB_NAMESPACE) -l $(PROMETHEUS_HELM_NAME) -i $(PROMETHEUS_INTERVAL) -a deploy -u $(DB_USER) -p '$(DB_PASSWORD)' -d $(VDB_NAME)

.PHONY: undeploy-prometheus-service-monitor
undeploy-prometheus-service-monitor:
	scripts/deploy-prometheus.sh -n $(VDB_NAMESPACE) -l $(PROMETHEUS_HELM_NAME) -i $(PROMETHEUS_INTERVAL) -a undeploy -u $(DB_USER) -p '$(DB_PASSWORD)' -d $(VDB_NAME)

.PHONY: undeploy-prometheus-service-monitor-by-release
undeploy-prometheus-service-monitor-by-release:
	scripts/deploy-prometheus.sh -l $(PROMETHEUS_HELM_NAME) -a undeploy_by_release

.PHONY: deploy-prometheus-adapter
deploy-prometheus-adapter:  ## Setup prometheus adapter for VerticaAutoscaler
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo update
	helm install $(DEPLOY_WAIT) -n $(PROMETHEUS_ADAPTER_NAMESPACE) --create-namespace $(PROMETHEUS_ADAPTER_NAME) prometheus-community/prometheus-adapter --values prometheus/adapter.yaml --set prometheus.url=$(PROMETHEUS_URL) --set prometheus.port=$(PROMETHEUS_PORT) --set replicas=$(PROMETHEUS_ADAPTER_REPLICAS) $(PROMETHEUS_ADAPTER_HELM_OVERRIDES)

.PHONY: deploy-prometheus-adapter-tls
deploy-prometheus-adapter-tls: ## Setup prometheus adapter for VerticaAutoscaler
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo update
	helm install $(DEPLOY_WAIT) -n $(PROMETHEUS_ADAPTER_NAMESPACE) --create-namespace $(PROMETHEUS_ADAPTER_NAME) prometheus-community/prometheus-adapter --values prometheus/adapter-tls.yaml --set prometheus.url=$(PROMETHEUS_TLS_URL) --set prometheus.port=$(PROMETHEUS_PORT) --set replicas=$(PROMETHEUS_ADAPTER_REPLICAS) $(PROMETHEUS_ADAPTER_HELM_OVERRIDES)

.PHONY: undeploy-prometheus-adapter
undeploy-prometheus-adapter:  ## Remove prometheus adapter
	helm uninstall $(PROMETHEUS_ADAPTER_NAME) -n $(PROMETHEUS_ADAPTER_NAMESPACE)

.PHONY: deploy-keda
deploy-keda: ## Deploy keda operator for autoscaling
	helm repo add kedacore https://kedacore.github.io/charts
	helm repo update
	helm install keda kedacore/keda --namespace keda --create-namespace

.PHONY: undeploy-keda
undeploy-keda: ## Undeploy keda operator previously deployed
	helm uninstall keda -n keda

.PHONY: undeploy-operator
undeploy-operator: ## Undeploy operator that was previously deployed
	scripts/undeploy.sh $(if $(filter false,$(ignore-not-found)),,-i)

.PHONY: deploy
deploy: deploy-operator

.PHONY: undeploy
undeploy: undeploy-operator

.PHONY: create-tls-secret
create-tls-secret: cert-gen
	bin/cert-gen $(SECRET_NAME) $(SECRET_NAMESPACE) $(DB_USER) | kubectl apply -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
KIND ?= $(LOCALBIN)/kind
KUBERNETES_SPLIT_YAML ?= $(LOCALBIN)/kubernetes-split-yaml
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.2
CONTROLLER_TOOLS_VERSION ?= v0.15.0
ENVTEST_VERSION ?= release-0.18
KIND_VERSION ?= v0.20.0
KUBERNETES_SPLIT_YAML_VERSION ?= v0.3.0
GOLANGCI_LINT_VERSION ?= v1.61.0

## Tool architecture
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# We replaced the default download script found in the operator-sdk with a
# direct download. I was htting the GitHub rate limiter by using the
# script available in the kustomize repo (install_kustomize.sh). A direct
# download allows us to manage retries easier.
KUSTOMIZE_DOWNLOAD_URL?=https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F$(KUSTOMIZE_VERSION)/kustomize_$(KUSTOMIZE_VERSION)_$(GOOS)_$(GOARCH).tar.gz
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary
$(KIND): $(LOCALBIN)
	test -s $(KIND) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@$(KIND_VERSION)

.PHONY: kubernetes-split-yaml
kubernetes-split-yaml: $(KUBERNETES_SPLIT_YAML) ## Download kubernetes-split-yaml locally if necessary.
$(KUBERNETES_SPLIT_YAML): $(LOCALBIN)
	test -s $(KUBERNETES_SPLIT_YAML) || GOBIN=$(LOCALBIN) go install github.com/mogensen/kubernetes-split-yaml@$(KUBERNETES_SPLIT_YAML_VERSION)

.PHONY: golangci-lint $(GOLANGCI_LINT)
golangci-lint: $(GOLANGCI_LINT)
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

krew: $(HOME)/.krew/bin/kubectl-krew ## Download krew plugin locally if necessary

$(HOME)/.krew/bin/kubectl-krew:
	scripts/setup-krew.sh

.PHONY: opm
OPM = $(shell pwd)/bin/opm
OPM_VERSION = 1.26.5
opm: $(OPM)  ## Download opm locally if necessary
$(OPM):
	curl --silent --show-error --retry 10 --retry-max-time 1800 --location --fail "https://github.com/operator-framework/operator-registry/releases/download/v$(OPM_VERSION)/$(GOOS)-$(GOARCH)-opm" --output $(OPM)
	chmod +x $(OPM)

OPERATOR_SDK = $(shell pwd)/bin/operator-sdk
OPERATOR_SDK_VERSION = 1.38.0
operator-sdk: $(OPERATOR_SDK)  ## Download operator-sdk locally if necessary
$(OPERATOR_SDK):
	curl --silent --show-error --retry 10 --retry-max-time 1800 --location --fail "https://github.com/operator-framework/operator-sdk/releases/download/v$(OPERATOR_SDK_VERSION)/operator-sdk_$(GOOS)_$(GOARCH)" --output $(OPERATOR_SDK)
	chmod +x $(OPERATOR_SDK)

ISTIOCTL = $(shell pwd)/bin/istioctl
ISTIOCTL_VERSION = 1.23.3
istioctl: $(ISTIOCTL)  ## Download istioctl locally if necessary
$(ISTIOCTL):
	curl --silent --show-error --retry 10 --retry-max-time 1800 --location --fail "https://github.com/istio/istio/releases/download/$(ISTIOCTL_VERSION)/istio-$(ISTIOCTL_VERSION)-$(GOOS)-$(GOARCH).tar.gz" | tar xvfz - istio-$(ISTIOCTL_VERSION)/bin/istioctl -O > $(ISTIOCTL)
	chmod +x $(ISTIOCTL)

.PHONY: helm-dependency-update
helm-dependency-update: ## Update helm chart dependencies
	helm dependency update $(OPERATOR_CHART)


##@ Release

change-operator-version: ## Change the operator version in source files. Override VERSION on command line to change the value in the Makefile.
	scripts/change-operator-version.sh $(VERSION)

CHANGIE = $(shell pwd)/bin/changie
# Be sure to update DEVELOPER.md when switching to a new changie version
CHANGIE_VERSION = 1.2.0
changie: $(CHANGIE) ## Download changie locally if necessary
$(CHANGIE): $(LOCALBIN) ## Download changie locally if necessary
	curl --silent --show-error --location --fail https://github.com/miniscruff/changie/releases/download/v$(CHANGIE_VERSION)/changie_$(CHANGIE_VERSION)_$(GOOS)_$(GOARCH).tar.gz | tar xvfz - changie
	mv changie $(CHANGIE)
	chmod +x $(CHANGIE)

.PHONY: gen-changelog
gen-changelog: changie ## Generate the changelog
	@cd $(REPO_DIR)
	$(CHANGIE) batch $(VERSION)
	$(CHANGIE) merge

.PHONY: tag
tag: ## Create a tag for the next version of the operator
	@git tag -d v$(VERSION) 2> /dev/null || true
	git tag --sign --message "verticadb-operator $(VERSION)" v$(VERSION)
	git verify-tag --verbose v$(VERSION)

.PHONY: push-tag
push-tag: ## Push the tag up to GitHub
	git push origin v$(VERSION)

.PHONY: echo-versions
echo-versions:  ## Print the current versions for various components
	@echo "VERSION=$(VERSION)"
	@echo "VLOGGER_VERSION=$(VLOGGER_VERSION)"

.PHONY: echo-vars
echo-vars:  echo-images echo-versions  ## Print out internal state
	@echo "DEPLOY_WITH=$(DEPLOY_WITH)"

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default > dist/install.yaml
	
# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary (ideally with version)
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef
