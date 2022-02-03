# (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

FROM alpine:3.15

# Tini - A tiny but valid init for containers
RUN apk add --no-cache tini
ENTRYPOINT ["/sbin/tini", "--"]
# $DBPATH is set by the operator and is the /<localDataPath>/<dbName>.
# The tail can't be done until the vertica.log is created.  This is because the
# exact location isn't known until the server pod has come up and is added to
# the cluster. 
# Note: we use the '-F' option with tail so that it survives log rotations.
CMD ["sh", "-c", "FN=$DBPATH/v_*_catalog/vertica.log; until [ -f $FN ]; do sleep 5; done; tail -n 1 -F $FN"]
