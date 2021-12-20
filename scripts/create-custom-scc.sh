#!/bin/bash

# (c) Copyright [2021] Micro Focus or one of its affiliates.
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

# A script that will create a custom scc to run VerticaDB on OpenShift

cat <<EOF | kubectl apply --validate=false -f -
kind: SecurityContextConstraints
apiVersion: security.openshift.io/v1
metadata:
  name: anyuid-extra
  annotations:
    kubernetes.io/description: anyuid-extra provides all features of the anyuid SCC
        but add SYS_CHROOT and AUDIT_WRITE capabilities.
requiredDropCapabilities:
- MKNOD
runAsUser:
  type: RunAsAny
seLinuxContext:
  type: MustRunAs
fsGroup:
  type: RunAsAny
supplementalGroups:
  type: RunAsAny
allowedCapabilities:
- SYS_CHROOT
- AUDIT_WRITE
groups:
- system:cluster-admins
volumes:
- configMap
- downwardAPI
- emptyDir
- persistentVolumeClaim
- projected
- secret
EOF