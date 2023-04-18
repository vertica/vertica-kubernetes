VERTICA_RPM?=vertica-x86_64.RHEL6.latest.rpm
BUILDER_OS_VERSION?=7.9.2009
BASE_OS_VERSION?=focal-20220316
VERTICA_IMG?=vertica-k8s
MINIMAL_VERTICA_IMG?=
NO_KEYS?=
export VERTICA_VERSION?=$(shell rpm --nosignature -qp --queryformat '%{VERSION}-%{RELEASE}' packages/$(VERTICA_RPM) 2>/dev/null || strings 'packages/$(VERTICA_RPM)' | head -1 | sed -e s/vertica-//)

all: docker-build-vertica

packages/ldblink.so: packages/$(VERTICA_RPM)
	wget https://github.com/vertica/dblink/releases/download/v0.2.1/ldblink.so.ubuntu-v$${VERTICA_VERSION%%-*} -O $@ || (rm -f $@; false)

.PHONY: docker-build-vertica
docker-build-vertica: Dockerfile packages/package-checksum-patcher.py packages/$(VERTICA_RPM) packages/ldblink.so
	docker pull ubuntu:focal ## make sure we use the latest focal image
	docker build \
		--platform linux/amd64 \
		-f Dockerfile \
		--label minimal=${MINIMAL_VERTICA_IMG} \
		--label os-version=${BASE_OS_VERSION} \
		--label vertica-version=${VERTICA_VERSION} \
		--build-arg MINIMAL=${MINIMAL_VERTICA_IMG} \
		--build-arg VERTICA_RPM=${VERTICA_RPM} \
		--build-arg NO_KEYS=${NO_KEYS} \
		--build-arg BASE_OS_VERSION=${BASE_OS_VERSION} \
		--build-arg BUILDER_OS_VERSION=${BUILDER_OS_VERSION} \
		--build-arg DBLINK_VERSION=${BUILDER_OS_VERSION} \
		-t ${VERTICA_IMG} .
