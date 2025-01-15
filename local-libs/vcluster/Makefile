default: help

# Note: This file and siblings are under github.com/vertica/vcluster/

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

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: ## Run unit tests
	go test ./... -coverprofile coverage.out

.PHONY: lint
lint: golangci-lint ## Lint the code
	$(GOLANGCI_LINT) run

.PHONY: build
build: fmt vet ## Build vcluster binary.
	go build -o bin/vcluster main.go

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
# see [sandbox]/__golint_version__.txt
GOLANGCI_LINT_VERSION ?= 1.56.0

.PHONY: golangci-lint $(GOLANGCI_LINT)
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint
$(GOLANGCI_LINT): $(LOCALBIN)
ifneq (${GOLANGCI_LINT_VERSION}, $(shell [ -f $(GOLANGCI_LINT) ] && $(GOLANGCI_LINT) version --format short 2>&1))
	@echo "golangci-lint missing or not version '${GOLANGCI_LINT_VERSION}', downloading..."
	curl --retry 10 --retry-max-time 1800 -sSfL "https://raw.githubusercontent.com/golangci/golangci-lint/v${GOLANGCI_LINT_VERSION}/install.sh" | sh -s -- -b ./bin "v${GOLANGCI_LINT_VERSION}"
endif
