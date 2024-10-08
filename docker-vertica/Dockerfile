# (c) Copyright [2021-2024] Open Text.
# Licensed under the Apache License, Version 2.0 (the "License");
# You may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ARG BASE_OS_NAME
ARG BASE_OS_VERSION
ARG BUILDER_OS_NAME
ARG BUILDER_OS_VERSION
ARG MINIMAL=""
ARG S6_OVERLAY_VERSION=3.1.2.1
FROM ${BUILDER_OS_NAME}:${BUILDER_OS_VERSION} as builder

ARG VERTICA_RPM
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
  openssh-server \
  openssh-clients \
  openssl \
  binutils \
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
# versions at once. This step is required if not including SSH keys because
# conditional copy isn't a thing in Docker. If no SSH keys are to be included,
# then the next RUN will remove the key we just added.
COPY dbadmin/.ssh /home/dbadmin/.ssh

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN set -x \
  && mkdir -p /root/.ssh \
  && cp -r /home/dbadmin/.ssh /root \
  && chmod 700 /root/.ssh \
  && chmod 600 /root/.ssh/* \
  && chmod 700 /home/dbadmin/.ssh \
  && chmod 600 /home/dbadmin/.ssh/* \
  && chown -R dbadmin:verticadba /home/dbadmin/ \
  && chmod go-w /etc/ssh/sshd_config.d/* /etc/ssh/ssh_config.d/* 

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
  cronie \
  dialog \
  gdb \
  glibc-locale-source \
  iproute-tc \
  krb5-workstation \
  logrotate \
  openssh-clients \
  openssh-server \
  openssl \
  procps \
  sysstat \
  sudo \
  which \
  && if [[ $(rpm -E '%{rhel}') == "9" ]] ; then \
    yum install -y libxcrypt-compat; \
  fi \
  # Install jre if not minimal
  && if [[ ${MINIMAL^^} != "YES" ]] ; then \
    yum install -y $JRE_PKG; \
  fi \ 
  # RHEL 8 - enable powertools to fix locales issue
  && bash -c "if [ $(rpm -E '%{rhel}') == '8' ]; then yum install -q -y dnf-plugins-core glibc-locale-source; yum -q config-manager --set-enabled powertools; fi" \
  && yum clean all \
  && /bin/rm -rf /var/cache/yum \
  # Fixes unsupported locale character encoding: use a utf8 locale, not a
  # ANSI_X3.4-1968 locale
  && localedef -i en_US -f UTF-8 en_US.UTF-8 \
  # Set JAVA_HOME environment variable if not minimal, this will be loaded to all shells
  && if [[ ${MINIMAL^^} != "YES" ]] ; then \
    echo "JAVA_HOME=/usr" >> /etc/environment; \
  fi \
  # delete old host keys
  && rm -rf /etc/ssh/ssh_host* \
  # Permit ssh connections
  && rm -rf /run/nologin \
  # Create a symlink to the rsync for use with vbr. This works around a problem
  # seen in some deployments where vbr cannot find rsync.
  && ln -s /opt/vertica/bin/rsync /usr/bin/rsync

# this squashes the image
FROM scratch
COPY --from=initial / /

ARG DBADMIN_GID=5000
ARG DBADMIN_UID=5000
ARG S6_OVERLAY_VERSION

COPY --from=builder /opt/vertica /opt/vertica
COPY --from=builder --chown=$DBADMIN_UID:$DBADMIN_GID /home/dbadmin /home/dbadmin
COPY --from=builder /root/.ssh /root/.ssh
COPY --from=builder /var/spool/cron/ /var/spool/cron/crontabs
COPY --from=builder /etc/ssh/sshd_config.d/* /etc/ssh/sshd_config.d/
COPY --from=builder /etc/ssh/ssh_config.d/* /etc/ssh/ssh_config.d/

ENV PATH "$PATH:/opt/vertica/bin:/opt/vertica/sbin"

# For the init program (process 1), we use s6-overlay. This ensures none of the
# processes we start ever become zombie's. It will also restart long running
# processes like sshd and cron in case they fail.
#
# See https://github.com/just-containers/s6-overlay for instructions on how to
# setup and configure.
ADD https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-noarch.tar.xz /tmp
ADD https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-x86_64.tar.xz /tmp
COPY s6-rc.d/ /etc/s6-overlay/s6-rc.d/

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN set -x \
  && /usr/sbin/groupadd -r verticadba --gid ${DBADMIN_GID} \
  && /usr/sbin/useradd -r -m -s /bin/bash -g verticadba --uid ${DBADMIN_UID} dbadmin \
  # Allow passwordless sudo access from dbadmin
  && echo "dbadmin ALL=(ALL) NOPASSWD: ALL" | tee -a /etc/sudoers \
  && echo "dbadmin -       nofile  65536" >> /etc/security/limits.conf \
  # Untar the init program that was downloaded earlier
  && tar -C / -Jxpf /tmp/s6-overlay-x86_64.tar.xz \
  && tar -C / -Jxpf /tmp/s6-overlay-noarch.tar.xz

ENTRYPOINT [ "/init" ]

# vertica port
EXPOSE 5433
# vertica-http port
EXPOSE 8443
USER dbadmin
LABEL os-family="rockylinux"
LABEL image-name="vertica-k8s"
LABEL maintainer="K8s Team"
LABEL org.opencontainers.image.source=https://github.com/vertica/vertica-kubernetes/tree/main/docker-vertica \
      org.opencontainers.image.title='Vertica Server' \
      org.opencontainers.image.description='Runs the Vertica server that is optimized for use with the VerticaDB operator' \
      org.opencontainers.image.url=https://github.com/vertica/vertica-kubernetes/ \
      org.opencontainers.image.documentation=https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/ContainerizedVertica.htm \
      vertica-deployment-method='admintools'
