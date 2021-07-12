# Apache 2.0 license
#
# Copyright (c) 2021 Vertica
#

ARG OS_VERSION="7.9.2009"
FROM centos:centos${OS_VERSION} as builder

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
  # fixed libraries.  We depend on malware check of container afterwards 
  && yum -q -y update \
  && yum -y update --security \
  # CentOS 8 - enable powertools to fix locales issue
  && bash -c "if [ \"$(rpm -E %{rhel})\" == '8' ]; then yum install -q -y dnf-plugins-core glibc-locale-source; yum -q config-manager --set-enabled powertools; fi" \
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
  # CentOS 8 - fixes - Unsupported locale character encoding: use a utf8 locale, not a ANSI_X3.4-1968 locale
  && bash -c "if [ \"$(rpm -E %{rhel})\" == '8' ]; then localedef -i en_US -f UTF-8 en_US.UTF-8; fi" \
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

# prepare (put inside) entrypoint script and make right permissions
ADD dbadmin/.bash_profile /home/dbadmin/
ADD ./docker-entrypoint.sh /opt/vertica/bin/

RUN set -x \
  && chmod a+x /opt/vertica/bin/docker-entrypoint.sh \
  && chown dbadmin:verticadba /home/dbadmin/.bash_profile \
  && chmod 600 /home/dbadmin/.bash_profile \
  && ssh-keygen -q -A \
  && mkdir -p /root/.ssh \
  && mkdir -p /home/dbadmin/.ssh \
  && ssh-keygen -q -t rsa -N '' -f /root/.ssh/id_rsa \
  && cp /root/.ssh/id_rsa.pub /root/.ssh/authorized_keys \
  # Allow root to ssh into dbadmin account -- needed for install_vertica
  && cat /root/.ssh/id_rsa.pub >> /home/dbadmin/.ssh/authorized_keys \
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
FROM centos:centos${OS_VERSION}

ARG DBADMIN_GID=5000
ARG DBADMIN_UID=5000

COPY --from=builder /opt/vertica /opt/vertica
COPY --from=builder /home/dbadmin /home/dbadmin
COPY --from=builder /root/.ssh /root/.ssh
COPY --from=builder /etc/ssh /etc/ssh
COPY --from=builder /var/spool/cron /var/spool/cron

ENV LANG en_US.utf8
ENV TZ UTC
ENV PATH "$PATH:/opt/vertica/bin:/opt/vertica/sbin"

ADD ./packages/init.d.functions /etc/rc.d/init.d/functions

RUN set -x \
  # COPY may not preserve file permissions on older docker versions.  This is a
  # workaround for that.  This will cause the image to increase a bit, but there
  # isn't much we copy over for this directory.
  && chown -R $DBADMIN_UID:$DBADMIN_GID /home/dbadmin \
  # Update is needed to be confident that we're picking up
  # fixed libraries.  We depend on malware check of container afterwards 
  && yum -y update --security \
  # CentOS 8 - enable powertools to fix locales issue
  && bash -c "if [ \"$(rpm -E %{rhel})\" == '8' ]; then yum install -q -y dnf-plugins-core glibc-locale-source; yum -q config-manager --set-enabled powertools; fi" \
  && yum install -y \
  cronie \
  dialog \
  iproute \
  mcelog \
  logrotate \
  openssh-server \
  openssh-clients \
  openssl \
  sudo \
  which \
  # CentOS 8 - fixes - Unsupported locale character encoding: use a utf8 locale, not a ANSI_X3.4-1968 locale
  && bash -c "if [ \"$(rpm -E %{rhel})\" == '8' ]; then localedef -i en_US -f UTF-8 en_US.UTF-8; fi" \
  && /usr/sbin/groupadd -r verticadba --gid ${DBADMIN_GID} \
  && /usr/sbin/useradd -r -m -s /bin/bash -g verticadba --uid ${DBADMIN_UID} dbadmin \
  # Allow passwordless sudo access from dbadmin
  && echo "dbadmin ALL=(ALL) NOPASSWD: ALL" | tee -a /etc/sudoers \
  && yum clean all \
  && /bin/rm -rf /var/cache/yum \
  # Used by the agent to see if networking is enabled.  Empty file is okay
  && touch /etc/sysconfig/network \
  # Permit ssh connections
  && rm -rf /run/nologin

ENTRYPOINT ["/opt/vertica/bin/docker-entrypoint.sh"]

# vertica port
EXPOSE 5433
# agent port
EXPOSE 5444
USER dbadmin
LABEL os_family="centos"
LABEL image_name="vertica_k8s"
LABEL maintainer="K8 Team"
