VERTICA_RPM?=vertica-x86_64.RHEL6.latest.rpm
BUILDER_OS_VERSION?=8
BASE_OS_VERSION?=lunar
VERTICA_IMG?=vertica-k8s
MINIMAL_VERTICA_IMG?=
VERTICA_VERSION?=$(shell rpm --nosignature -qp --queryformat '%{VERSION}-%{RELEASE}' packages/$(VERTICA_RPM))
VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS?=

all: docker-build-vertica

.PHONY: docker-build-vertica
docker-build-vertica: Dockerfile packages/package-checksum-patcher.py
	docker pull ubuntu:$(BASE_OS_VERSION) ## make sure we use the latest ubuntu image
	docker buildx build \
		--load \
		-f Dockerfile \
		--label minimal=${MINIMAL_VERTICA_IMG} \
		--label os-version=${BASE_OS_VERSION} \
		--label vertica-version=${VERTICA_VERSION} \
		--build-arg MINIMAL=${MINIMAL_VERTICA_IMG} \
		--build-arg VERTICA_RPM=${VERTICA_RPM} \
		--build-arg BASE_OS_VERSION=${BASE_OS_VERSION} \
		--build-arg BUILDER_OS_VERSION=${BUILDER_OS_VERSION} \
		${VERTICA_ADDITIONAL_DOCKER_BUILD_OPTIONS} \
		-t ${VERTICA_IMG} .
