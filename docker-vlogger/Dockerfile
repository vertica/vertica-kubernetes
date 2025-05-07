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

# A docker container that will tail the vertica.log.  This allows vertica to
# follow the idiomatic way in Kubernetes of logging to stdout.

ARG ALPINE_VERSION
ARG BASE_IMG
FROM ${BASE_IMG}:${ALPINE_VERSION}

# Tini - A tiny but valid init for containers
RUN apk add --no-cache tini
ENTRYPOINT ["/sbin/tini", "--"]

COPY ./tail_vertica_log.sh /tmp/tail_vertica_log.sh
RUN chmod +x /tmp/tail_vertica_log.sh
CMD ["/bin/sh", "/tmp/tail_vertica_log.sh"]

LABEL org.opencontainers.image.source=https://github.com/vertica/vertica-kubernetes/tree/main/docker-vlogger \
      org.opencontainers.image.title='Vertica Logger' \
      org.opencontainers.image.description='A sidecar utility container that assists with logging of the Vertica server image' \
      org.opencontainers.image.url=https://github.com/vertica/vertica-kubernetes/ \
      org.opencontainers.image.documentation=https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/ContainerizedVertica.htm

