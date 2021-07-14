VERTICA_RPM?=vertica-x86_64.RHEL6.latest.rpm
OS_VERSION?=7.9.2009
VERTICA_IMG?=vertica-k8s
MINIMAL_VERTICA_IMG?=

all: docker-build-vertica

.PHONY: docker-build-vertica
docker-build-vertica: Dockerfile packages/package-checksum-patcher.py
	docker build \
		-f Dockerfile \
		--label minimal=${MINIMAL_VERTICA_IMG} \
		--label os_version=${OS_VERSION} \
		--build-arg MINIMAL=${MINIMAL_VERTICA_IMG} \
		--build-arg VERTICA_RPM=${VERTICA_RPM} \
		--build-arg OS_VERSION=${OS_VERSION} \
		-t ${VERTICA_IMG} .
