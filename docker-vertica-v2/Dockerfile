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
FROM ${BUILDER_OS_NAME}:${BUILDER_OS_VERSION} AS builder

ARG VERTICA_X86_RPM
ARG VERTICA_ARM64_RPM
ARG MINIMAL

COPY ./packages/*.rpm /tmp/
# this is a script which removes unnecessary stuff from the
# container image
COPY ./packages/cleanup.sh /tmp/
COPY ./packages/package-checksum-patcher.py /tmp/
COPY ./packages/httpstls.json /tmp/

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
# Update is needed to be confident that we're picking up
# fixed libraries.
# Using --nobest to make it easier yum install to work. This stage isn't used
# for the final image, so any package is good enough. We just need to install
# the vertica rpm and copy that over.

# It can be challenging to identify the exact architecture that the build file is targetting when doing a multi-platform build
# The pain is described in https://github.com/BretFisher/multi-platform-docker-build quite well
# We only need to do this because we have separate vertica RPMs for ARM and x86. 
# There is no need for us to differentiate between the vriants of ARM. So we consider those as equivalent for our RPM install.

# Don't move the ARG from here. The ARG is only read by the build system if its declared after the SHELL
ARG TARGETARCH
RUN set -x \
  && yum -y update \
  && yum install -y --nobest \
  dialog \
  glibc \
  glibc-langpack-en \
  iproute \
  openssl \
  && /usr/sbin/groupadd -r verticadba \
  && /usr/sbin/useradd -r -m -s /bin/bash -g verticadba dbadmin \
  && ls /tmp/ \
  && if [[ "$TARGETARCH" == "arm64" ]] ; then \
      uname -m && \
      yum localinstall -y /tmp/${VERTICA_ARM64_RPM} ; \
    elif [[ "$TARGETARCH" == "amd64" ]] ; then \
      yum localinstall -y /tmp/${VERTICA_X86_RPM} ; \
    else \
      exit 1; \
    fi \
  && mkdir -p /opt/vertica/config/https_certs \
  && cp /tmp/httpstls.json /opt/vertica/config/https_certs/ \
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
FROM ${BASE_OS_NAME}:${BASE_OS_VERSION} AS initial

# Controls the version of jre to be installed. The list of all available jre
# packages can be queried through dnf. For instance, "dnf search openjdk"
ARG JRE_PKG=java-1.8.0-openjdk-headless
ARG MINIMAL
ARG FOR_GITHUB_CI

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN set -x \
  # update needed because we just did a clean
  && yum -y update \
  && yum install -y \
  ca-certificates \
  gdb \
  glibc-locale-source \
  iproute-tc \
  krb5-workstation \
  openssl \
  procps \
  sysstat \
  which \
  # Install packages in full vertica image for e2e tests
  && if [[ $FOR_GITHUB_CI == "true" ]] && [[ ${MINIMAL^^} != "YES" ]] ; then \
    yum install -y diffutils gcc-c++ boost-devel libcurl-devel bzip2-devel bzip2 perl java-1.8.0-openjdk-devel zlib-devel \
    && ln -s /opt/vertica/oss/python3/bin/python3 /usr/bin/python \
    && ln -s /usr/lib64/libbz2.so /usr/lib64/libbz2.so.1.0; \
  fi \
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

COPY --from=builder /opt/vertica /opt/vertica
COPY --from=builder /home/dbadmin /home/dbadmin

ENV PATH "$PATH:/opt/vertica/bin:/opt/vertica/sbin"
ENV JAVA_HOME "/usr"
ENV VERTICA_STARTUP_LOG_DUPLICATE "/proc/1/fd/1"

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