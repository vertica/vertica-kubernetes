# Apache 2.0 license
#
# Copyright (c) 2021 Vertica
#

ARG BASE_OS_VERSION="focal-20220801"
ARG BUILDER_OS_VERSION="7.9.2009"
ARG MINIMAL=""
FROM centos:centos${BUILDER_OS_VERSION} as builder

ARG VERTICA_RPM="vertica-x86_64.RHEL6.latest.rpm"
ARG MINIMAL
ARG DBADMIN_GID=5000
ARG DBADMIN_UID=5000

COPY ./packages/${VERTICA_RPM} /tmp/
# this is a script which removes unnecessary stuff from the
# container image
COPY ./packages/cleanup.sh /tmp/
COPY ./packages/package-checksum-patcher.py /tmp/
COPY ./packages/10-vertica-sshd.conf /etc/ssh/sshd_config.d/10-vertica-sshd.conf
COPY ./packages/10-vertica-ssh.conf /etc/ssh/ssh_config.d/10-vertica-ssh.conf

RUN set -x \
  # Update is needed to be confident that we're picking up
  # fixed libraries.
  && yum -q -y update \
  && yum install -y \
  cronie \
  dialog \
  iproute \
  mcelog \
  openssh-server \
  openssh-clients \
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
  && chown -R dbadmin:verticadba /opt/vertica \
  # reduce the size of the final image
  && rm -rf /opt/vertica/lib64  \
  && yum clean all \
  && sh /tmp/cleanup.sh

# Copy in a stable ssh key.  This is done so that Vertica pods can communicate
# with pods running an older image.  This is necessary when doing an online
# image change as the Vertica cluster will be running with two container
# versions at once.
COPY dbadmin/.ssh /home/dbadmin/.ssh
# prepare (put inside) entrypoint script and make right permissions
COPY ./docker-entrypoint.sh /usr/local/bin

RUN set -x \
  && chmod a+x /usr/local/bin/docker-entrypoint.sh \
  && mkdir -p /root/.ssh \
  && cp -r /home/dbadmin/.ssh /root \
  && chmod 700 /root/.ssh \
  && chmod 600 /root/.ssh/* \
  && chmod 700 /home/dbadmin/.ssh \
  && chmod 600 /home/dbadmin/.ssh/* \
  && chown -R dbadmin:verticadba /home/dbadmin/ \
  && chmod go-w /etc/ssh/sshd_config.d/* /etc/ssh/ssh_config.d/*


##############################################################################################
FROM ubuntu:${BASE_OS_VERSION}

ARG DBADMIN_GID=5000
ARG DBADMIN_UID=5000
# Controls the version of jre to be installed by apt-get. The list of all
# available jre packages can be queried through apt-cache. For instance,
# "apt-cache search jre | grep jre"
ARG JRE_PKG=openjdk-8-jre-headless
ARG MINIMAL

COPY --from=builder /opt/vertica /opt/vertica
COPY --from=builder --chown=$DBADMIN_UID:$DBADMIN_GID /home/dbadmin /home/dbadmin
COPY --from=builder /root/.ssh /root/.ssh
COPY --from=builder /var/spool/cron/ /var/spool/cron/crontabs
COPY --from=builder /usr/local/bin/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
COPY --from=builder /etc/ssh/sshd_config.d/* /etc/ssh/sshd_config.d/
COPY --from=builder /etc/ssh/ssh_config.d/* /etc/ssh/ssh_config.d/

ENV PATH "$PATH:/opt/vertica/bin:/opt/vertica/sbin"
ENV DEBIAN_FRONTEND noninteractive

COPY ./packages/init.d.functions /etc/rc.d/init.d/functions

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN set -x \
  # Update is needed to be confident that we're picking up
  # fixed libraries. 
  && apt-get -y update \
  && apt-get install -y --no-install-recommends \
  ca-certificates \
  cron \
  dialog \
  gdb \
  iproute2 \
  krb5-user \
  libkeyutils1\
  libz-dev \
  locales \
  logrotate \
  ntp \
  openssh-client \
  openssh-server \
  openssl \
  procps \
  sysstat \
  sudo \
  # Install jre if not minimal
  && if [[ ($MINIMAL != "YES" && $MINIMAL != "yes") ]] ; then \
    apt-get install -y --no-install-recommends $JRE_PKG; \
  fi \
  && rm -rf /var/lib/apt/lists/* \
  # Make the "en_US.UTF-8" locale so vertica will be utf-8 enabled by default
  && localedef -i en_US -c -f UTF-8 -A /usr/share/locale/locale.alias en_US.UTF-8 \
  && mkdir -p /run/sshd \
  && ssh-keygen -q -A \
  && /usr/sbin/groupadd -r verticadba --gid ${DBADMIN_GID} \
  && /usr/sbin/useradd -r -m -s /bin/bash -g verticadba --uid ${DBADMIN_UID} dbadmin \
  # Allow passwordless sudo access from dbadmin
  && echo "dbadmin ALL=(ALL) NOPASSWD: ALL" | tee -a /etc/sudoers \
  && echo "dbadmin -       nofile  65536" >> /etc/security/limits.conf \
  # Set JAVA_HOME environment variable if not minimal, this will be loaded to all shells
  && if [[ $MINIMAL != "YES" && $MINIMAL != "yes" ]]; then \
    echo "JAVA_HOME=/usr" >> /etc/environment; \
  fi \
  # Create a symlink to python3 interpreter in vertica
  && update-alternatives --install /usr/bin/python python /opt/vertica/oss/python3/bin/python3 1

ENTRYPOINT [ "/usr/local/bin/docker-entrypoint.sh" ]

# vertica port
EXPOSE 5433
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
