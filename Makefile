# VERSION defines the project version for the bundle. 
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

SHELL:=$(shell which bash)

# CHANNELS define the bundle channels used in the bundle. 
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "preview,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=preview,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="preview,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle. 
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# BUNDLE_IMG defines the image:tag used for the bundle. 
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= controller-bundle:$(VERSION)

# Set the namespace
GET_NAMESPACE_SH=kubectl config view --minify --output 'jsonpath={..namespace}'
ifeq (, $(shell ${GET_NAMESPACE_SH}))
	NAMESPACE?=default
else
	NAMESPACE?=$(shell ${GET_NAMESPACE_SH})
endif

LOCAL_SOAK_CFG=./local-soak.cfg
ifneq (,$(wildcard $(LOCAL_SOAK_CFG)))
	SOAK_CFG?=-c $(LOCAL_SOAK_CFG)
endif

GOLANGCI_LINT_VER=1.41.1

# Command we run to see if we are running in a kind environment
KIND_CHECK=kubectl get node -o=jsonpath='{.items[0].spec.providerID}' | grep 'kind://' -c

# We pick an image tag based on the environment we are in.  We special case kind
# environments because we need to use a different imagePullPolicy -- kind
# environments load the images through 'kind load docker-image' so must use IfNotPresent.
# Note, the imagePullPolicy is the default picked by kubernetes that depends on the tag.
#
# Env     Tag      imagePullPolicy
# ---     ---      ---------------
# kind    kind     IfNotPresent
# other   latest   Always
ifeq ($(shell $(KIND_CHECK)), 1)
  TAG?=kind
else
  TAG?=latest 
endif

# Image URL to use for building/pushing of the operator
OPERATOR_IMG ?= verticadb-operator:$(TAG)
# Image URL to use for building/pushing of the webhook
WEBHOOK_IMG ?= verticadb-webhook:$(TAG)
# Image URL to use for building/pushing of the vertica server
VERTICA_IMG ?= vertica-k8s:$(TAG)
# Set this to YES if you want to create a vertica image of minimal size
MINIMAL_VERTICA_IMG ?=
# Produce CRDs that work back to Kubernetes 2.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"
# Name of the helm release that we will install/uninstall
HELM_RELEASE_NAME?=vdb-op
WEBHOOK_RELEASE_NAME?=vdb-webhook

GOPATH?=${HOME}/go
TMPDIR?=$(PWD)
HELM_UNITTEST_PLUGIN_INSTALLED=$(shell helm plugin list | grep -c '^unittest')
KUTTL_PLUGIN_INSTALLED=$(shell kubectl krew list | grep -c '^kuttl')

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: build

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
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, Role and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	sed -i '/WATCH_NAMESPACE/d' config/rbac/role.yaml ## delete any line with the dummy namespace WATCH_NAMESPACE

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: install-unittest-plugin manifests generate fmt vet lint
	helm unittest --helm3 --output-type JUnit --output-file $(TMPDIR)/unit-tests.xml helm-charts/verticadb-operator
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.2/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test ./... -coverprofile cover.out

.PHONY: lint
lint: helm-create-resources ## Lint the helm charts and the Go operator
	helm lint helm-charts/verticadb-operator
ifneq (${GOLANGCI_LINT_VER}, "$(shell ./bin/golangci-lint version --format short 2>&1)")
	@echo "golangci-lint missing or not version '${GOLANGCI_LINT_VER}', downloading..."
	curl -sSfL "https://raw.githubusercontent.com/golangci/golangci-lint/v${GOLANGCI_LINT_VER}/install.sh" | sh -s -- -b ./bin "v${GOLANGCI_LINT_VER}"
endif
	./bin/golangci-lint run

.PHONY: install-unittest-plugin
install-unittest-plugin:
ifeq ($(HELM_UNITTEST_PLUGIN_INSTALLED), 0)
	helm plugin install https://github.com/quintush/helm-unittest
endif

.PHONY: run-unit-tests
run-unit-tests: test ## Run unit tests

.PHONY: install-kuttl-plugin
install-kuttl-plugin:
ifeq ($(KUTTL_PLUGIN_INSTALLED), 0)
	kubectl krew install kuttl
endif

.PHONY: run-int-tests
run-int-tests: install-kuttl-plugin vdb-gen ## Run the integration tests
	kubectl kuttl test --report xml

.PHONY: run-soak-tests
run-soak-tests: install-kuttl-plugin kuttl-step-gen  ## Run the soak tests
	scripts/soak-runner.sh $(SOAK_CFG)

##@ Build

build: generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/operator/main.go

run: manifests generate fmt vet ## Run a controller from your host.
	go run cmd/operator/main.go -enable-profiler

docker-build-operator: test ## Build operator docker image with the manager.
	docker build -t ${OPERATOR_IMG} -f docker-operator/Dockerfile .

docker-build-webhook: test ## Build webhook docker image.
	docker build -t ${WEBHOOK_IMG} -f docker-webhook/Dockerfile .

docker-push-operator: ## Push operator docker image with the manager.
ifeq ($(shell $(KIND_CHECK)), 0)
	docker push ${OPERATOR_IMG}
else
	scripts/push-to-kind.sh -i ${OPERATOR_IMG}
endif

docker-push-webhook: ## Push webhook docker image.
ifeq ($(shell $(KIND_CHECK)), 0)
	docker push ${WEBHOOK_IMG}
else
	scripts/push-to-kind.sh -i ${WEBHOOK_IMG}
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

docker-build: docker-build-vertica docker-build-operator docker-build-webhook  ## Build all docker images

docker-push: docker-push-vertica docker-push-operator docker-push-webhook  ## Push all docker images

kuttl-step-gen: ## Builds the kuttl-step-gen tool
	go build -o bin/$@ ./cmd/$@

vdb-gen: ## Builds the vdb-gen tool
	go build -o bin/$@ ./cmd/$@

##@ Deployment
CERT_MANAGER_VER=1.3.1
install-cert-manager: ## Install the cert-manager
	kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v$(CERT_MANAGER_VER)/cert-manager.yaml
	scripts/wait-for-cert-manager-ready.sh -t 180
     
uninstall-cert-manager: ## Uninstall the cert-manager
	kubectl delete -f https://github.com/jetstack/cert-manager/releases/download/v$(CERT_MANAGER_VER)/cert-manager.yaml 

OPERATOR_CHART = $(shell pwd)/helm-charts/verticadb-operator
WEBHOOK_CHART = $(shell pwd)/helm-charts/verticadb-webhook
helm-create-resources: manifests kustomize ## Generate all the verticadb operator helm chart template files and crd
	mkdir -p config/overlays/all-but-crd
	cd config/overlays/all-but-crd && echo "" > kustomization.yaml
	cd config/overlays/all-but-crd && $(KUSTOMIZE) edit add base ../../default
	cd config/overlays/all-but-crd && $(KUSTOMIZE) edit set image controller='{{ .Values.image.name }}'
	cd config/overlays/all-but-crd && echo "patchesStrategicMerge:"  >> kustomization.yaml
	cd config/overlays/all-but-crd && echo "  - delete-crd.yaml"  >> kustomization.yaml
	echo -e '$$patch: delete\napiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: verticadbs.vertica.com' > config/overlays/all-but-crd/delete-crd.yaml

	mkdir -p config/overlays/only-crd
	cd config/overlays/only-crd && echo "" > kustomization.yaml
	cd config/overlays/only-crd && $(KUSTOMIZE) edit add base ../../crd

	mkdir -p config/overlays/only-webhook
	cd config/overlays/only-webhook && echo "" > kustomization.yaml
	cd config/overlays/only-webhook && $(KUSTOMIZE) edit add base ../../webhook-manager
	cd config/overlays/only-webhook && $(KUSTOMIZE) edit set image controller='{{ .Values.image.name }}'

	$(KUSTOMIZE) build config/overlays/all-but-crd/ | sed 's/verticadb-operator-system/{{ .Release.Namespace }}/g' > $(OPERATOR_CHART)/templates/operator.yaml
	mkdir -p $(OPERATOR_CHART)/crds
	$(KUSTOMIZE) build config/overlays/only-crd/ > $(OPERATOR_CHART)/crds/verticadbs.vertica.com-crd.yaml

	mkdir -p $(WEBHOOK_CHART)/templates
	$(KUSTOMIZE) build config/overlays/only-webhook/ | sed 's/verticadb-operator/verticadb-webhook/g' | sed 's/\/manager/\/webhook/g' > $(WEBHOOK_CHART)/templates/webhook-manager.yaml
	cp -r $(OPERATOR_CHART)/crds $(WEBHOOK_CHART)

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -


deploy-operator: manifests kustomize ## Using helm, deploy the controller to the K8s cluster specified in ~/.kube/config.
	helm install -n $(NAMESPACE) $(HELM_RELEASE_NAME) $(OPERATOR_CHART) --set image.name=${OPERATOR_IMG}

deploy-webhook: manifests kustomize install-cert-manager ## Using helm, deploy the webhook to the K8s cluster specified in ~/.kube/config.
	helm install -n vertica $(WEBHOOK_RELEASE_NAME) $(WEBHOOK_CHART) --set image.name=${WEBHOOK_IMG} --create-namespace

undeploy-operator: ## Using helm, undeploy controller from the K8s cluster specified in ~/.kube/config.
	helm uninstall -n $(NAMESPACE) $(HELM_RELEASE_NAME)

undeploy-webhook: ## Using helm, undeploy webhook from the K8s cluster specified in ~/.kube/config.
	helm uninstall -n vertica $(WEBHOOK_RELEASE_NAME)
	$(MAKE) uninstall-cert-manager

deploy: deploy-operator deploy-webhook

undeploy: undeploy-operator undeploy-webhook


CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

.PHONY: bundle ## Generate bundle manifests and metadata, then validate generated files.
bundle: manifests kustomize
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(OPERATOR_IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

.PHONY: bundle-build ## Build the bundle image.
bundle-build:
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .
