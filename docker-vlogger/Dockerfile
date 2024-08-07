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
# $DBPATH is set by the operator and is the /<localDataPath>/<dbName>.
# The tail can't be done until the vertica.log is created.  This is because the
# exact location isn't known until the server pod has come up and is added to
# the cluster. 
# Note: we use the '-F' option with tail so that it survives log rotations.
CMD ["sh", "-c", "FN=$DBPATH/v_*_catalog/vertica.log; until [ -f $FN ]; do sleep 5; done; tail -n 1 -F $FN"]

LABEL org.opencontainers.image.source=https://github.com/vertica/vertica-kubernetes/tree/main/docker-vlogger \
      org.opencontainers.image.title='Vertica Logger' \
      org.opencontainers.image.description='A sidecar utility container that assists with logging of the Vertica server image' \
      org.opencontainers.image.url=https://github.com/vertica/vertica-kubernetes/ \
      org.opencontainers.image.documentation=https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/ContainerizedVertica.htm

