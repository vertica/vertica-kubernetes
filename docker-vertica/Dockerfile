# Apache 2.0 license
#
# Copyright (c) 2021 Vertica
#

ARG BASE_OS_VERSION="focal-20220316"
ARG BUILDER_OS_VERSION="7.9.2009"
FROM centos:centos${BUILDER_OS_VERSION} as builder

ARG VERTICA_RPM="vertica-x86_64.RHEL6.latest.rpm"
ARG MINIMAL=""
ARG DBADMIN_GID=5000
ARG DBADMIN_UID=5000

ADD ./packages/${VERTICA_RPM} /tmp/
# this is a script which removes unnecessary stuff from the
# container image
ADD ./packages/cleanup.sh /tmp/
ADD ./packages/package-checksum-patcher.py /tmp/

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
  sudo \
  which \
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
  && sh /tmp/cleanup.sh

ADD dbadmin/.bash_profile /home/dbadmin/
# Copy in a stable ssh key.  This is done so that Vertica pods can communicate
# with pods running an older image.  This is necessary when doing an online
# image change as the Vertica cluster will be running with two container
# versions at once.
ADD dbadmin/.ssh /home/dbadmin/.ssh
# prepare (put inside) entrypoint script and make right permissions
ADD ./docker-entrypoint.sh /opt/vertica/bin/

RUN set -x \
  && chmod a+x /opt/vertica/bin/docker-entrypoint.sh \
  && chown dbadmin:verticadba /home/dbadmin/.bash_profile \
  && chmod 600 /home/dbadmin/.bash_profile \
  && mkdir -p /root/.ssh \
  && cp -r /home/dbadmin/.ssh /root \
  && chmod 700 /root/.ssh \
  && chmod 600 /root/.ssh/* \
  && chmod 700 /home/dbadmin/.ssh \
  && chmod 600 /home/dbadmin/.ssh/* \
  && chown -R dbadmin:verticadba /home/dbadmin/ \
  # On older docker versions, the ownership of /opt/vertica is not preserved in
  # the COPY.  Working around this by changing it so that all files and
  # directories are world writeable.
  && chmod 777 -R /opt/vertica

##############################################################################################
FROM ubuntu:${BASE_OS_VERSION}

ARG DBADMIN_GID=5000
ARG DBADMIN_UID=5000

COPY --from=builder /opt/vertica /opt/vertica
COPY --from=builder /home/dbadmin /home/dbadmin
COPY --from=builder /root/.ssh /root/.ssh
COPY --from=builder /var/spool/cron/ /var/spool/cron/crontabs

ENV LANG en_US.utf8
ENV TZ UTC
ENV PATH "$PATH:/opt/vertica/bin:/opt/vertica/sbin"
ENV DEBIAN_FRONTEND noninteractive

ADD ./packages/init.d.functions /etc/rc.d/init.d/functions

RUN set -x \
  # COPY may not preserve file permissions on older docker versions.  This is a
  # workaround for that.  This will cause the image to increase a bit, but there
  # isn't much we copy over for this directory.
  && chown -R $DBADMIN_UID:$DBADMIN_GID /home/dbadmin \
  # Update is needed to be confident that we're picking up
  # fixed libraries. 
  && apt-get -y update \
  && apt-get install -y \
  locales \
  cron \
  sysstat \
  ca-certificates \
  ntp \
  dialog \
  iproute2 \
  libkeyutils1\
  procps \
  krb5-user \
  logrotate \
  openssh-client \
  openssh-server \
  openssl \
  sudo \
  && rm -rf /var/lib/apt/lists/* \
  # Make the "en_US.UTF-8" locale so vertica will be utf-8 enabled by default
  && localedef -i en_US -c -f UTF-8 -A /usr/share/locale/locale.alias en_US.UTF-8 \
  && mkdir -p /run/sshd \
  && ssh-keygen -q -A \
  && /usr/sbin/groupadd -r verticadba --gid ${DBADMIN_GID} \
  && /usr/sbin/useradd -r -m -s /bin/bash -g verticadba --uid ${DBADMIN_UID} dbadmin \
  # Allow passwordless sudo access from dbadmin
  && echo "dbadmin ALL=(ALL) NOPASSWD: ALL" | tee -a /etc/sudoers \
  && echo "dbadmin -       nofile  65536" >> /etc/security/limits.conf 
  
ENTRYPOINT ["/opt/vertica/bin/docker-entrypoint.sh"]

# vertica port
EXPOSE 5433
# agent port
EXPOSE 5444
USER dbadmin
LABEL os_family="ubuntu"
LABEL image_name="vertica_k8s"
LABEL maintainer="K8 Team"
