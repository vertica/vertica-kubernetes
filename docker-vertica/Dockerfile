# Apache 2.0 license
#
# Copyright (c) 2021 Vertica
#

ARG OS_VERSION="centos7"
FROM centos:centos${OS_VERSION}

ARG VERTICA_RPM="vertica-x86_64.RHEL6.latest.rpm"

ENV LANG en_US.utf8
ENV TZ UTC
ENV PATH "$PATH:/opt/vertica/bin:/opt/vertica/sbin"

ADD ./packages/${VERTICA_RPM} /tmp/
# this is a script which removes unnecessary stuff from the
# container image
ADD ./packages/cleanup.sh /tmp/
ADD ./packages/package-checksum-patcher.py /tmp/
ADD ./packages/init.d.functions /etc/rc.d/init.d/functions

RUN set -x \
    # Update is needed to be confident that we're picking up
    # fixed libraries.  We depend on malware check of container afterwards 
 && yum -q -y update \
 && yum -y update --security \
 # CentOS 8 - enable powertools to fix locales issue
 && bash -c "if [ \"$(rpm -E %{rhel})\" == '8' ]; then yum install -q -y dnf-plugins-core; yum -q config-manager --set-enabled powertools; fi" \
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
 && /usr/sbin/groupadd -r verticadba \
 && /usr/sbin/useradd -r -m -s /bin/bash -g verticadba dbadmin \
 # Allow passwordless sudo access from dbadmin
 && echo "dbadmin ALL=(ALL) NOPASSWD: ALL" | tee -a /etc/sudoers \
 && yum localinstall -q -y /tmp/${VERTICA_RPM} \
 # CentOS 8 - fixes issue "Unsupported locale character encoding: use a utf8 locale, not a ANSI_X3.4-1968 locale"
 && bash -c "if [ \"$(rpm -E %{rhel})\" == '8' ]; then localedef -i en_US -f UTF-8 en_US.UTF-8; fi" \
 && yum clean all \
 && /bin/rm -rf /tmp/vertica* /var/cache/yum \
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
  && chown -R dbadmin:verticadba /opt/vertica \
  # reduce the size of the final image
  && rm -rf /opt/vertica/lib64  \
  && sh /tmp/cleanup.sh
        

# prepare (put inside) entrypoint script and make right permissions
ADD dbadmin/.bash_profile /home/dbadmin/
ADD ./re-ip-node.py /opt/vertica/bin/
ADD ./docker-entrypoint.sh /opt/vertica/bin/

RUN set -x \
  # Used by the agent to see if networking is enabled.  Empty file is okay
  && touch /etc/sysconfig/network \
  && chmod a+x /opt/vertica/bin/docker-entrypoint.sh \
  && chown dbadmin:verticadba /home/dbadmin/.bash_profile \
  && chmod 600 /home/dbadmin/.bash_profile \
  && ssh-keygen -q -A -f /etc/ssh/\
  && mkdir -p /root/.ssh \
  && ssh-keygen -q -t rsa -N '' -f /root/.ssh/id_rsa \
  && cp /root/.ssh/id_rsa.pub /root/.ssh/authorized_keys \
  # Allow root to ssh into dbadmin account -- needed for install_vertica
  && cat /root/.ssh/id_rsa.pub >> /home/dbadmin/.ssh/authorized_keys \
  && chmod 700 /root/.ssh \
  && chmod 600 /root/.ssh/*

ENTRYPOINT ["/opt/vertica/bin/docker-entrypoint.sh"]

# vertica port
EXPOSE 5433
# agent port
EXPOSE 5444
USER dbadmin
LABEL os_family="centos"
LABEL os_version=$OS_VERSION
LABEL image_name="vertica_k8s"
LABEL maintainer="K8 Team"
LABEL has_package_patcher=yes
