VERTICA_RPM?=vertica-x86_64.RHEL6.latest.rpm
# VERTICA_RPM?=vertica-10.1.0-0.x86_64.RHEL6.rpm
CONTAINER_REPO?=
TAG?=
OS_VERSION?=7.9.2009

all: docker-build-vertica

.PHONY: docker-build-vertica
docker-build-vertica: Dockerfile packages/package-checksum-patcher.py
	docker build \
		-f Dockerfile \
		--build-arg VERTICA_RPM=${VERTICA_RPM} \
		--build-arg OS_VERSION=${OS_VERSION} \
		-t ${CONTAINER_REPO}vertica-k8s:${TAG} .
