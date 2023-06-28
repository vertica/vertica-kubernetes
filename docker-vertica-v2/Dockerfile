# Apache 2.0 license
#
# Copyright (c) 2021 Vertica
#

ARG BASE_OS_VERSION="lunar"
ARG BUILDER_OS_VERSION="stream8"
ARG MINIMAL=""
ARG NO_KEYS=""
ARG S6_OVERLAY_VERSION=3.1.2.1
FROM quay.io/centos/centos:${BUILDER_OS_VERSION} as builder

ARG VERTICA_RPM="vertica-x86_64.RHEL6.latest.rpm"
ARG MINIMAL
ARG NO_KEYS
ARG DBADMIN_GID=5000
ARG DBADMIN_UID=5000

COPY ./packages/${VERTICA_RPM} /tmp/
# this is a script which removes unnecessary stuff from the
# container image
COPY ./packages/cleanup.sh /tmp/
COPY ./packages/package-checksum-patcher.py /tmp/

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN set -x \
  # Update is needed to be confident that we're picking up
  # fixed libraries.
  && yum -q -y update \
  # Using --nobest to make it easier yum install to work. This stage isn't used
  # for the final image, so any package is good enough. We just need to install
  # the vertica rpm and copy that over.
  && yum install -y --nobest \
  cronie \
  dialog \
  glibc \
  glibc-langpack-en \
  iproute \
  openssl \
  which \
  zlib-devel \
  && /usr/sbin/groupadd -r verticadba --gid ${DBADMIN_GID} \
  && /usr/sbin/useradd -r -m -s /bin/bash -g verticadba --uid ${DBADMIN_UID} dbadmin \
  && yum localinstall -q -y /tmp/${VERTICA_RPM} \
  # Run install_vertica script to prepare environment
  && /opt/vertica/sbin/install_vertica \
  --accept-eula \
  --debug \
  --dba-user-password-disabled \
  --failure-threshold NONE \
  --license CE \
  --hosts 127.0.0.1 \
  --no-system-configuration \
  --ignore-install-config \
  -U \
  --data-dir /home/dbadmin \
  && mkdir -p /home/dbadmin/licensing/ce \
  && cp -r /opt/vertica/config/licensing/* /home/dbadmin/licensing/ce/ \
  && mkdir -p /home/dbadmin/logrotate \
  && cp -r /opt/vertica/config/logrotate /home/dbadmin/logrotate/  \
  && cp /opt/vertica/config/logrotate_base.conf /home/dbadmin/logrotate/ \
  && if [[ ${NO_KEYS^^} != "YES" ]] ; then \
    mkdir -p /home/dbadmin/agent; \
    cp -r /opt/vertica/config/share/agent* /home/dbadmin/agent/; \
    cp /opt/vertica/config/apikeys.dat /home/dbadmin/agent/; \
  fi \
  && chown -R dbadmin:verticadba /opt/vertica \
  # reduce the size of the final image
  && rm -rf /opt/vertica/lib64  \
  && yum clean all \
  && sh /tmp/cleanup.sh

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN set -x \
  && chown -R dbadmin:verticadba /home/dbadmin/ \
  && if [[ ${NO_KEYS^^} == "YES" ]] ; then \
    rm -rf /opt/vertica/config/share/agent*; \
  fi

##############################################################################################
FROM ubuntu:${BASE_OS_VERSION} as initial

RUN set -x \
  # Update is needed to be confident that we're picking up
  # fixed libraries.
  && apt-get -y update \
  && apt-get -y upgrade \
  && apt-get clean \
  && apt-get autoremove \
  && rm -rf /var/lib/apt/lists/* /var/cache/debconf/*-old

# this squashes the image
FROM scratch
COPY --from=initial / /

ARG DBADMIN_GID=5000
ARG DBADMIN_UID=5000
# Controls the version of jre to be installed by apt-get. The list of all
# available jre packages can be queried through apt-cache. For instance,
# "apt-cache search jre | grep jre"
ARG JRE_PKG=openjdk-8-jre-headless
ARG MINIMAL
ARG NO_KEYS
ARG S6_OVERLAY_VERSION

COPY --from=builder /opt/vertica /opt/vertica
COPY --from=builder --chown=$DBADMIN_UID:$DBADMIN_GID /home/dbadmin /home/dbadmin
COPY --from=builder /var/spool/cron/ /var/spool/cron/crontabs

ENV PATH "$PATH:/opt/vertica/bin:/opt/vertica/sbin"
ENV DEBIAN_FRONTEND noninteractive

COPY ./packages/init.d.functions /etc/rc.d/init.d/functions

# For the init program (process 1), we use s6-overlay. This ensures none of the
# processes we start ever become zombie's. It will also restart long running
# processes like cron in case they fail.
#
# See https://github.com/just-containers/s6-overlay for instructions on how to
# setup and configure.
ADD https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-noarch.tar.xz /tmp
ADD https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-x86_64.tar.xz /tmp
COPY s6-rc.d/ /etc/s6-overlay/s6-rc.d/

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN set -x \
  # update needed because we just did a clean
  && apt-get -y update \
  && apt-get install -y --no-install-recommends \
  ca-certificates \
  cron \
  dialog \
  gdb \
  iproute2 \
  krb5-user \
  less \
  libkeyutils1\
  libz-dev \
  locales \
  logrotate \
  ntp \
  openssl \
  procps \
  sysstat \
  sudo \
  # Install jre if not minimal
  && if [[ ${MINIMAL^^} != "YES" ]] ; then \
    apt-get install -y --no-install-recommends $JRE_PKG; \
  fi \
  # install vim except in the -nokeys image \
  && if [[ ${NO_KEYS^^} != "YES" ]] ; then \
    apt-get install -y --no-install-recommends vim-tiny; \
  fi \
  && apt-get clean \
  && apt-get autoremove \
  && rm -rf /var/lib/apt/lists/* /var/cache/debconf/*-old \
  # Make the "en_US.UTF-8" locale so vertica will be utf-8 enabled by default
  && localedef -i en_US -c -f UTF-8 -A /usr/share/locale/locale.alias en_US.UTF-8 \
  && /usr/sbin/groupadd -r verticadba --gid ${DBADMIN_GID} \
  && /usr/sbin/useradd -r -m -s /bin/bash -g verticadba --uid ${DBADMIN_UID} dbadmin \
  # Allow passwordless sudo access from dbadmin
  && echo "dbadmin ALL=(ALL) NOPASSWD: ALL" | tee -a /etc/sudoers \
  && echo "dbadmin -       nofile  65536" >> /etc/security/limits.conf \
  # Set JAVA_HOME environment variable if not minimal, this will be loaded to all shells
  && if [[ ${MINIMAL^^} != "YES" ]] ; then \
    echo "JAVA_HOME=/usr" >> /etc/environment; \
  fi \
  # Create a symlink to python3 interpreter in vertica
  && update-alternatives --install /usr/bin/python python /opt/vertica/oss/python3/bin/python3 1 \
  && chmod u+s /usr/sbin/cron \
  # Untar the init program that was downloaded earlier
  && tar -C / -Jxpf /tmp/s6-overlay-x86_64.tar.xz \
  && tar -C / -Jxpf /tmp/s6-overlay-noarch.tar.xz \
  # Create a symlink to the rsync for use with vbr. This works around a problem
  # seen in some deployments where vbr cannot find rsync.
  && ln -s /opt/vertica/bin/rsync /usr/bin/rsync

ENTRYPOINT [ "/init" ]

# vertica port
EXPOSE 5433
# vertica-http port
EXPOSE 8443
# agent port
EXPOSE 5444
USER dbadmin
LABEL os-family="ubuntu"
LABEL image-name="vertica_k8s"
LABEL maintainer="K8s Team"
LABEL org.opencontainers.image.source=https://github.com/vertica/vertica-kubernetes/tree/main/docker-vertica \
      org.opencontainers.image.title='Vertica Server' \
      org.opencontainers.image.description='Runs the Vertica server that is optimized for use with the VerticaDB operator' \
      org.opencontainers.image.url=https://github.com/vertica/vertica-kubernetes/ \
      org.opencontainers.image.documentation=https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/ContainerizedVertica.htm
