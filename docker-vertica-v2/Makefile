VERTICA_X86_RPM?=$(notdir $(wildcard packages/vertica*.x86_64.*.rpm))
VERTICA_ARM64_RPM?=$(notdir $(wildcard packages/vertica*.aarch64.*.rpm))
BUILDER_OS_NAME?=almalinux
BUILDER_OS_VERSION?=8
BASE_OS_NAME?=rockylinux
BASE_OS_VERSION?=9
FOR_GITHUB_CI?=false
VERTICA_IMG?=vertica-k8s
MINIMAL_VERTICA_IMG?=
TARGET_ARCH?=linux/amd64
VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS?=

VERTICA_X86_VERSION?=$(shell rpm --nosignature -qp --queryformat '%{VERSION}-%{RELEASE}' packages/$(VERTICA_X86_RPM))
VERTICA_ARM_VERSION?=$(shell rpm --nosignature -qp --queryformat '%{VERSION}-%{RELEASE}' packages/$(VERTICA_ARM64_RPM))

all: docker-build-vertica

.PHONY: docker-build-vertica
docker-build-vertica: Dockerfile packages/package-checksum-patcher.py
	docker pull ${BASE_OS_NAME}:$(BASE_OS_VERSION) ## make sure we use the latest base image
	docker buildx build \
		--load \
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
