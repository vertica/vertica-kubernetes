# Apache 2.0 license
#
# Copyright (c) 2021 Vertica
#

ARG BASE_OS_NAME
ARG BASE_OS_VERSION
ARG BUILDER_OS_VERSION="8"
ARG MINIMAL=""
ARG S6_OVERLAY_VERSION=3.1.2.1
FROM almalinux:${BUILDER_OS_VERSION} as builder

ARG VERTICA_RPM
ARG MINIMAL

COPY ./packages/${VERTICA_RPM} /tmp/
# this is a script which removes unnecessary stuff from the
# container image
COPY ./packages/cleanup.sh /tmp/
COPY ./packages/package-checksum-patcher.py /tmp/
COPY ./packages/httpstls.json /tmp/

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN set -x \
  # Update is needed to be confident that we're picking up
  # fixed libraries.
  && yum -y update \
  # Using --nobest to make it easier yum install to work. This stage isn't used
  # for the final image, so any package is good enough. We just need to install
  # the vertica rpm and copy that over.
  && yum install -y --nobest \
  dialog \
  glibc \
  glibc-langpack-en \
  iproute \
  openssl \
  which \
  zlib-devel \
  && /usr/sbin/groupadd -r verticadba \
  && /usr/sbin/useradd -r -m -s /bin/bash -g verticadba dbadmin \
  && yum localinstall -y /tmp/${VERTICA_RPM} \
  && mkdir -p /opt/vertica/config/https_certs \
  && cp /tmp/httpstls.json /opt/vertica/config/https_certs/ \
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
  && chown -R dbadmin:verticadba /opt/vertica \
  # reduce the size of the final image
  && rm -rf /opt/vertica/lib64  \
  && yum clean all \
  && sh /tmp/cleanup.sh

##############################################################################################
FROM ${BASE_OS_NAME}:${BASE_OS_VERSION} as initial

# Controls the version of jre to be installed. The list of all available jre
# packages can be queried through dnf. For instance, "dnf search openjdk"
ARG JRE_PKG=java-1.8.0-openjdk-headless
ARG MINIMAL

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN set -x \
  # update needed because we just did a clean
  && yum -y update \
  && yum install -y \
  ca-certificates \
  dnf-plugins-core \
  gdb \
  glibc-locale-source \
  iproute-tc \
  krb5-workstation \
  libxcrypt-compat \
  openssl \
  procps \
  sysstat \
  which \
  # Install jre if not minimal
  && if [[ ${MINIMAL^^} != "YES" ]] ; then \
    yum install -y $JRE_PKG; \
  fi \ 
  && yum clean all \
  && /bin/rm -rf /var/cache/yum \
  # Fixes unsupported locale character encoding: use a utf8 locale, not a
  # ANSI_X3.4-1968 locale
  && localedef -i en_US -f UTF-8 en_US.UTF-8 \
  # Set JAVA_HOME environment variable if not minimal, this will be loaded to all shells
  && if [[ ${MINIMAL^^} != "YES" ]] ; then \
    echo "JAVA_HOME=/usr" >> /etc/environment; \
  fi \
  # Cannot set sudo because the ID we run the container may not exist in the
  # passwd file. Set a simple root password so you can do some commands as root
  # if you need be. This isn't a security violation per se since you can control
  # at the pod level or above if the container is allowed to have elevated privileges.
  && echo "root:root" | chpasswd \
  && echo "* -       nofile  65536" >> /etc/security/limits.conf \
  && echo 'export PS1="[\H] \w\$ "' >> /etc/bash.bashrc

# this squashes the image
FROM scratch
COPY --from=initial / /

ARG S6_OVERLAY_VERSION

COPY --from=builder /opt/vertica /opt/vertica
COPY --from=builder /home/dbadmin /home/dbadmin

ENV PATH "$PATH:/opt/vertica/bin:/opt/vertica/sbin"
ENV JAVA_HOME "/usr"
ENV VERTICA_STARTUP_LOG_DUPLICATE "/proc/1/fd/1"

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
  # Untar the init program that was downloaded earlier
  && tar -C / -Jxpf /tmp/s6-overlay-x86_64.tar.xz \
  && tar -C / -Jxpf /tmp/s6-overlay-noarch.tar.xz

ENTRYPOINT [ "/init" ]

# vertica port
EXPOSE 5433
# vertica-http port
EXPOSE 8443
# Choosing a system user that isn't root to avoid a twistlock violation. The
# actual user is selected at runtime by setting the UID/GID in the pod spec.
USER daemon
LABEL os-family="rockylinux"
LABEL image-name="vertica-k8s"
LABEL maintainer="K8s Team"
LABEL org.opencontainers.image.source=https://github.com/vertica/vertica-kubernetes/tree/main/docker-vertica-v2 \
      org.opencontainers.image.title='Vertica Server' \
      org.opencontainers.image.description='Runs the Vertica server that is optimized for use with the VerticaDB operator' \
      org.opencontainers.image.url=https://github.com/vertica/vertica-kubernetes/ \
      org.opencontainers.image.documentation=https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/ContainerizedVertica.htm \
      vertica-deployment-method='vclusterops'
