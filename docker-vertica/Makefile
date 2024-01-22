VERTICA_RPM?=$(notdir $(wildcard packages/vertica*.rpm))
BUILDER_OS_NAME?=almalinux
BUILDER_OS_VERSION?=8
BASE_OS_NAME?=rockylinux
BASE_OS_VERSION?=9
VERTICA_IMG?=vertica-k8s
MINIMAL_VERTICA_IMG?=
VERTICA_VERSION?=$(shell rpm --nosignature -qp --queryformat '%{VERSION}-%{RELEASE}' packages/$(VERTICA_RPM))
VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS?=

all: docker-build-vertica

.PHONY: docker-build-vertica
docker-build-vertica: Dockerfile packages/package-checksum-patcher.py
	docker pull ${BASE_OS_NAME}:$(BASE_OS_VERSION) ## make sure we use the latest base image
	docker buildx build \
		--load \
		-f Dockerfile \
		--label minimal=${MINIMAL_VERTICA_IMG} \
		--label os-version=${BASE_OS_VERSION} \
		--label vertica-version=${VERTICA_VERSION} \
		--build-arg MINIMAL=${MINIMAL_VERTICA_IMG} \
		--build-arg VERTICA_RPM=${VERTICA_RPM} \
		--build-arg BASE_OS_NAME=${BASE_OS_NAME} \
		--build-arg BASE_OS_VERSION=${BASE_OS_VERSION} \
		--build-arg BUILDER_OS_NAME=${BUILDER_OS_NAME} \
		--build-arg BUILDER_OS_VERSION=${BUILDER_OS_VERSION} \
		${VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS} \
		-t ${VERTICA_IMG} .
