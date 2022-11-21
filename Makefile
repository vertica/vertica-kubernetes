# VERSION defines the project version for the bundle. 
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 1.8.0
export VERSION

# VLOGGER_VERSION defines the version to use for the Vertica logger image
# (see docker-vlogger).  This version is separate from VERSION above in
# order to have a different release cadence.
#
# When changing this, be sure to update the tags in docker-vlogger/README.md
VLOGGER_VERSION ?= 1.0.0

SHELL:=$(shell which bash)
REPO_DIR:=$(dir $(word $(words $(MAKEFILE_LIST)),$(MAKEFILE_LIST)))

# Current location of the kustomize config.  This dictates, amoung other things
# what communal endpoint to use for the e2e tests.  It reads in the contents
# and sets the environment variables that are present.
include tests/kustomize-defaults.cfg
KUSTOMIZE_CFG?=$(REPO_DIR)/tests/kustomize-defaults.cfg
include $(KUSTOMIZE_CFG)

# CHANNELS define the bundle channels used in the bundle. 
CHANNELS=stable
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=preview,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="preview,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle. 
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
DEFAULT_CHANNEL=stable
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
BUNDLE_DOCKERFILE=docker-bundle/Dockerfile

# Set the namespace
GET_NAMESPACE_SH=kubectl config view --minify --output 'jsonpath={..namespace}' 2> /dev/null
ifeq (, $(shell ${GET_NAMESPACE_SH}))
	NAMESPACE?=default
else
	NAMESPACE?=$(shell ${GET_NAMESPACE_SH})
endif

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
ENVTEST_K8S_VERSION = 1.24

# Image URL for the OLM catalog.  This is for testing purposes only.
ifeq ($(shell $(KIND_CHECK)), 1)
OLM_CATALOG_IMG ?= localhost:$(REG_PORT)/olm-catalog:$(TAG)
else
OLM_CATALOG_IMG ?= olm-catalog:$(TAG)
endif
export OLM_CATALOG_IMG

# Set this to YES if you want to create a vertica image of minimal size
MINIMAL_VERTICA_IMG ?=
# Name of the helm release that we will install/uninstall
HELM_RELEASE_NAME?=vdb-op
# Can be used to specify additional overrides when doing the helm install.
# For example to specify a custom webhook tls cert when deploying use this command:
#   HELM_OVERRIDES="--set webhook.tlsSecret=custom-cert" make deploy-operator
HELM_OVERRIDES?=
# Enables development mode by default. Is used only when the operator is deployed
# through the Makefile 
DEV_MODE?=true
# Maximum number of tests to run at once. (default 2)
# Set it to any value not greater than 8 to override the default one
E2E_PARALLELISM?=2
export E2E_PARALLELISM
# Set the e2e test directories.  For azb:// we avoid tests/e2e-extra because
# when running the Azure emulator, Azurite, revive_db fails.
ifeq ($(PATH_PROTOCOL), azb://)
E2E_TEST_DIRS?=tests/e2e
else
E2E_TEST_DIRS?=tests/e2e tests/e2e-extra
endif
# Additional arguments to pass to 'kubectl kuttl'
E2E_ADDITIONAL_ARGS?=

# Specify how to deploy the operator.  Allowable values are 'helm', 'olm' or 'random'.
# When deploying with olm, it is expected that `make setup-olm` has been run
# already.  When deploying with random, it will randomly pick between olm and helm.
DEPLOY_WITH?=helm
export DEPLOY_WITH
# Name of the test OLM catalog that we will create and deploy with in e2e tests
OLM_TEST_CATALOG_SOURCE=e2e-test-catalog

GOPATH?=${HOME}/go
TMPDIR?=$(PWD)
HELM_UNITTEST_PLUGIN_INSTALLED:=$(shell helm plugin list | grep -c '^unittest')
KUTTL_PLUGIN_INSTALLED:=$(shell kubectl krew list 2>/dev/null | grep -c '^kuttl')
STERN_PLUGIN_INSTALLED:=$(shell kubectl krew list 2>/dev/null | grep -c '^stern')
INTERACTIVE:=$(shell [ -t 0 ] && echo 1)
OPERATOR_CHART = $(shell pwd)/helm-charts/verticadb-operator

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

default: help

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

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(firstword $(MAKEFILE_LIST))

##@ Development

manifests: controller-gen ## Generate Role and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd paths="./..." output:crd:artifacts:config=config/crd/bases
	sed -i '/WATCH_NAMESPACE/d' config/rbac/role.yaml ## delete any line with the dummy namespace WATCH_NAMESPACE

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet lint go-junit-report envtest helm-ut ## Run tests.
ifdef INTERACTIVE
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./... -coverprofile cover.out
else
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test -v ./... -coverprofile cover.out 2>&1 | $(GO_JUNIT_REPORT) | tee ${LOGDIR}/unit-test-report.xml
endif	

.PHONY: helm-ut
helm-ut: install-unittest-plugin  ## Run the helm unittest
	helm unittest --helm3 --output-type JUnit --output-file $(TMPDIR)/unit-tests.xml helm-charts/verticadb-operator

.PHONY: lint
lint: config-transformer golangci-lint ## Lint the helm charts and the Go operator
	helm lint $(OPERATOR_CHART)
	scripts/dockerfile-lint
	$(GOLANGCI_LINT) run

.PHONY: install-unittest-plugin
install-unittest-plugin:
ifeq ($(HELM_UNITTEST_PLUGIN_INSTALLED), 0)
	helm plugin install https://github.com/quintush/helm-unittest
endif

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

.PHONY: run-int-tests
run-int-tests: install-kuttl-plugin install-stern-plugin kustomize vdb-gen setup-e2e-communal ## Run the integration tests
ifeq ($(DEPLOY_WITH), $(filter $(DEPLOY_WITH), olm random))
	$(MAKE) setup-olm
endif
	kubectl kuttl test --report xml --artifacts-dir ${LOGDIR} --parallel $(E2E_PARALLELISM) $(E2E_ADDITIONAL_ARGS) $(E2E_TEST_DIRS)

WAIT_TIME = 120s
run-scorecard-tests: bundle ## Run the scorecard tests
	$(OPERATOR_SDK) scorecard bundle --wait-time $(WAIT_TIME)

.PHONY: run-server-upgrade-tests
run-server-upgrade-tests: install-kuttl-plugin install-stern-plugin setup-e2e-communal ## Run integration tests for Vertica server upgrade
ifeq ($(DEPLOY_WITH), $(filter $(DEPLOY_WITH), olm random))
	$(MAKE) setup-olm
endif
ifeq ($(BASE_VERTICA_IMG), <not-set>)
	$(error $$BASE_VERTICA_IMG not set)
endif
	kubectl kuttl test --report xml --artifacts-dir ${LOGDIR} --parallel $(E2E_PARALLELISM) $(E2E_ADDITIONAL_ARGS) tests/e2e-server-upgrade/

setup-e2e-communal: ## Setup communal endpoint for use with e2e tests
ifeq ($(PATH_PROTOCOL), s3://)
	$(MAKE) setup-minio
else ifeq ($(PATH_PROTOCOL), webhdfs://)
	$(MAKE) setup-hadoop
else ifeq ($(PATH_PROTOCOL), azb://)
	$(MAKE) setup-azurite
else
	$(error cannot setup communal endpoint for this protocol: $(PATH_PROTOCOL))
	exit 1
endif

.PHONY: setup-minio
setup-minio: install-cert-manager ## Setup minio for use with the e2e tests
	scripts/setup-minio.sh

.PHONY: setup-hadoop
setup-hadoop: ## Setup hadoop cluster for use with the e2e tests
	scripts/setup-hadoop.sh

.PHONY: setup-azurite
setup-azurite: ## Setup azurite for use with the e2e tests
	scripts/setup-azurite.sh

.PHONY: setup-olm
setup-olm: operator-sdk bundle docker-build-bundle docker-push-bundle docker-build-olm-catalog docker-push-olm-catalog
	scripts/setup-olm.sh $(OLM_TEST_CATALOG_SOURCE)

##@ Build

build: generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/operator/main.go

run: manifests generate fmt vet ## Run a controller from your host.
	scripts/run-operator.sh

docker-build-operator: manifests generate fmt vet ## Build operator docker image with the manager.
	docker build -t ${OPERATOR_IMG} -f docker-operator/Dockerfile .

docker-build-vlogger:  ## Build vertica logger docker image
	docker build -t ${VLOGGER_IMG} -f docker-vlogger/Dockerfile .

docker-push-operator: ## Push operator docker image with the manager.
ifeq ($(shell $(KIND_CHECK)), 0)
	docker push ${OPERATOR_IMG}
else
	scripts/push-to-kind.sh -i ${OPERATOR_IMG}
endif

docker-push-vlogger:  ## Push vertica logger docker image
ifeq ($(shell $(KIND_CHECK)), 0)
	docker push ${VLOGGER_IMG}
else
	scripts/push-to-kind.sh -i ${VLOGGER_IMG}
endif

.PHONY: docker-build-vertica
docker-build-vertica: docker-vertica/Dockerfile ## Build vertica server docker image
	cd docker-vertica \
	&& make VERTICA_IMG=${VERTICA_IMG} MINIMAL_VERTICA_IMG=${MINIMAL_VERTICA_IMG}

.PHONY: docker-push
docker-push-vertica:  ## Push vertica server docker image
ifeq ($(shell $(KIND_CHECK)), 0)
	docker push ${VERTICA_IMG}
else
	scripts/push-to-kind.sh -i ${VERTICA_IMG}
endif

.PHONY: bundle 
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
ifneq ($(DEPLOY_WITH), $(filter $(DEPLOY_WITH), olm random))
	$(error Bundle can only be generated when deploying with OLM.  Current deployment method: $(DEPLOY_WITH))
endif
	scripts/gen-csv.sh $(USE_IMAGE_DIGESTS_FLAG)  $(VERSION) $(BUNDLE_METADATA_OPTS)
	mv bundle.Dockerfile $(BUNDLE_DOCKERFILE)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: docker-build-bundle
docker-build-bundle: bundle ## Build the bundle image
	docker build -f $(BUNDLE_DOCKERFILE) -t $(BUNDLE_IMG) .

.PHONY: docker-push-bundle
docker-push-bundle: ## Push the bundle image
	docker push $(BUNDLE_IMG)

docker-build-olm-catalog: opm ## Build an OLM catalog that includes our bundle (testing purposes only)
	$(OPM) index add --bundles $(BUNDLE_IMG) --tag $(OLM_CATALOG_IMG) --build-tool docker --skip-tls

docker-push-olm-catalog:
	docker push $(OLM_CATALOG_IMG)

docker-build: docker-build-vertica docker-build-operator docker-build-vlogger ## Build all docker images except OLM catalog

docker-push: docker-push-vertica docker-push-operator docker-push-vlogger ## Push all docker images except OLM catalog

echo-images:  ## Print the names of all of the images used
	@echo "OPERATOR_IMG=$(OPERATOR_IMG)"
	@echo "VERTICA_IMG=$(VERTICA_IMG)"
	@echo "BASE_VERTICA_IMG=$(BASE_VERTICA_IMG)"
	@echo "VLOGGER_IMG=$(VLOGGER_IMG)"
	@echo "BUNDLE_IMG=$(BUNDLE_IMG)"
	@echo "OLM_CATALOG_IMG=$(OLM_CATALOG_IMG)"

vdb-gen: generate manifests ## Builds the vdb-gen tool
	go build -o bin/$@ ./cmd/$@

##@ Deployment
# When changing this version be sure to update tests/external-images-common-ci.txt
CERT_MANAGER_VER=1.5.3
install-cert-manager: ## Install the cert-manager
	kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v$(CERT_MANAGER_VER)/cert-manager.yaml
	scripts/wait-for-cert-manager-ready.sh -t 180
	 
uninstall-cert-manager: ## Uninstall the cert-manager
	kubectl delete -f https://github.com/jetstack/cert-manager/releases/download/v$(CERT_MANAGER_VER)/cert-manager.yaml 

config-transformer: manifests kustomize kubernetes-split-yaml ## Generate release artifacts and helm charts from config/
	scripts/config-transformer.sh

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -


# For helm, we always include priv-reg-cred as an image pull secret.  This
# secret is created in e2e tests when we run with a private container registry.
# If this secret does not exist then it is simply ignored.
deploy-operator: manifests kustomize ## Using helm or olm, deploy the operator in the K8s cluster
ifeq ($(DEPLOY_WITH), helm)
	helm install --wait -n $(NAMESPACE) $(HELM_RELEASE_NAME) $(OPERATOR_CHART) --set image.repo=null --set image.name=${OPERATOR_IMG} --set logging.dev=${DEV_MODE} --set image.pullPolicy=$(HELM_IMAGE_PULL_POLICY) --set imagePullSecrets[0].name=priv-reg-cred $(HELM_OVERRIDES)
	scripts/wait-for-webhook.sh -n $(NAMESPACE) -t 60
else ifeq ($(DEPLOY_WITH), olm)
	scripts/deploy-olm.sh -n $(NAMESPACE) $(OLM_TEST_CATALOG_SOURCE)
	scripts/wait-for-webhook.sh -n $(NAMESPACE) -t 60
else ifeq ($(DEPLOY_WITH), random)
ifeq ($(shell (( $$RANDOM % 2 )); echo $$?),0)
	DEPLOY_WITH=helm $(MAKE) deploy-operator
else
	DEPLOY_WITH=olm $(MAKE) deploy-operator
endif
else
	$(error Unknown deployment method: $(DEPLOY_WITH))
endif


undeploy-operator: ## Undeploy operator that was previously deployed
	scripts/undeploy.sh -n $(NAMESPACE)

deploy: deploy-operator

undeploy: undeploy-operator

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
GO_JUNIT_REPORT = $(LOCALBIN)/go-junit-report
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v4.5.5
CONTROLLER_TOOLS_VERSION ?= v0.9.2
KIND_VERSION ?= v0.11.1
KUBERNETES_SPLIT_YAML_VERSION ?= v0.3.0
GO_JUNIT_REPORT_VERSION ?= latest
GOLANGCI_LINT_VER ?= 1.47.3

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(KUSTOMIZE) || curl -s $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(CONTROLLER_GEN) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(ENVTEST) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: go-junit-report
go-junit-report: $(GO_JUNIT_REPORT) ## Download go-junit-report locally if necessary.
$(GO_JUNIT_REPORT): $(LOCALBIN)
	test -s $(GO_JUNIT_REPORT) || GOBIN=$(LOCALBIN) go install github.com/jstemmer/go-junit-report@$(GO_JUNIT_REPORT_VERSION)

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
ifneq (${GOLANGCI_LINT_VER}, $(shell [ -f $(GOLANGCI_LINT) ] && $(GOLANGCI_LINT) version --format short 2>&1))
	@echo "golangci-lint missing or not version '${GOLANGCI_LINT_VER}', downloading..."
	curl -sSfL "https://raw.githubusercontent.com/golangci/golangci-lint/v${GOLANGCI_LINT_VER}/install.sh" | sh -s -- -b ./bin "v${GOLANGCI_LINT_VER}"
endif

krew: $(HOME)/.krew/bin/kubectl-krew ## Download krew plugin locally if necessary

$(HOME)/.krew/bin/kubectl-krew:
	scripts/setup-krew.sh

OPM = $(shell pwd)/bin/opm
OPM_VERSION = 1.23.0
opm: $(OPM)  ## Download opm locally if necessary
$(OPM):
	curl --silent --show-error --location --fail "https://github.com/operator-framework/operator-registry/releases/download/v$(OPM_VERSION)/linux-amd64-opm" --output $(OPM)
	chmod +x $(OPM)

OPERATOR_SDK = $(shell pwd)/bin/operator-sdk
operator-sdk: $(OPERATOR_SDK)  ## Download operator-sdk locally if necessary
$(OPERATOR_SDK):
	curl --silent --show-error --location --fail "https://github.com/operator-framework/operator-sdk/releases/download/v1.18.0/operator-sdk_linux_amd64" --output $(OPERATOR_SDK)
	chmod +x $(OPERATOR_SDK)
