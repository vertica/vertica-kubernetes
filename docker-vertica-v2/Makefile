VERTICA_X86_RPM?=$(notdir $(wildcard packages/vertica*.x86_64.rpm))
VERTICA_ARM64_RPM?=$(notdir $(wildcard packages/vertica*.aarch64.rpm))
BUILDER_OS_NAME?=almalinux
BUILDER_OS_VERSION?=8
BASE_OS_NAME?=rockylinux
BASE_OS_VERSION?=9
FOR_GITHUB_CI?=false
VERTICA_IMG?=vertica-k8s
MINIMAL_VERTICA_IMG?=
TARGET_ARCH?=linux/amd64,linux/arm64
VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS?=
VERTICA_VERSION?=

VERTICA_X86_VERSION?=$(shell rpm --nosignature -qp --queryformat '%{VERSION}-%{RELEASE}' packages/$(VERTICA_X86_RPM))
VERTICA_ARM_VERSION?=$(shell rpm --nosignature -qp --queryformat '%{VERSION}-%{RELEASE}' packages/$(VERTICA_ARM64_RPM))

# Our Github runner where we run the build step is on ubuntu so the rpm query extraction fails
# We skip the verification of the package mismatch in that pipeline
ifeq ($(FOR_GITHUB_CI), false)
ifneq ($(VERTICA_X86_VERSION),)
$(info X86 RPM Found with Version $(VERTICA_X86_VERSION))
VERTICA_VERSION:=$(VERTICA_X86_VERSION)
ifneq ($(VERTICA_ARM_VERSION),)
$(info ARM64 RPM Found with Version $(VERTICA_ARM_VERSION))
ifneq ($(VERTICA_ARM_VERSION), $(VERTICA_X86_VERSION))
$(error RPM version mismatch $(VERTICA_X86_RPM) $(VERTICA_ARM64_RPM)")
exit 1
endif
endif
endif
endif

VERTICA_X86_VERSION?=$(shell rpm --nosignature -qp --queryformat '%{VERSION}-%{RELEASE}' packages/$(VERTICA_X86_RPM))
VERTICA_ARM_VERSION?=$(shell rpm --nosignature -qp --queryformat '%{VERSION}-%{RELEASE}' packages/$(VERTICA_ARM64_RPM))

ifdef VERTICA_X86_VERSION
VERTICA_VERSION=$(VERTICA_X86_VERSION)
ifdef VERTICA_ARM_VERSION
ifneq ($(VERTICA_ARM_VERSION), $(VERTICA_X86_VERSION))
$(error RPM version mismatch $(VERTICA_X86_RPM) $(VERTICA_ARM64_RPM))
exit 1
endif
endif
endif

all: docker-build-vertica

.PHONY: docker-build-vertica
docker-build-vertica: Dockerfile packages/package-checksum-patcher.py
	docker pull ${BASE_OS_NAME}:$(BASE_OS_VERSION) ## make sure we use the latest base image
	docker buildx build \
		-f Dockerfile \
		--platform ${TARGET_ARCH} \
		--label minimal=${MINIMAL_VERTICA_IMG} \
		--label os-version=${BASE_OS_VERSION} \
		--label vertica-version=${VERTICA_VERSION} \
		--build-arg MINIMAL=${MINIMAL_VERTICA_IMG} \
		--build-arg VERTICA_X86_RPM=${VERTICA_X86_RPM} \
		--build-arg VERTICA_ARM64_RPM=${VERTICA_ARM64_RPM} \
		--build-arg BASE_OS_NAME=${BASE_OS_NAME} \
		--build-arg BASE_OS_VERSION=${BASE_OS_VERSION} \
		--build-arg BUILDER_OS_NAME=${BUILDER_OS_NAME} \
		--build-arg BUILDER_OS_VERSION=${BUILDER_OS_VERSION} \
		--build-arg FOR_GITHUB_CI=${FOR_GITHUB_CI} \
		${VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS} \
		-t ${VERTICA_IMG} .
