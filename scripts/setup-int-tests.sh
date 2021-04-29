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

set -o xtrace
set -o errexit

NAMESPACE=$(kubectl config view --minify --output 'jsonpath={..namespace}')
tmp_dir=$(mktemp -d -t octopus-XXXXXX)
trap "rm -rf $tmp_dir" 0 2 3 15  # Ensure deletion on script exit
cd $tmp_dir
git clone https://github.com/spilchen/octopus.git
cd octopus
if [[ -z NAMESPACE ]]
then
    HELM_NS="--namespace=$NAMESPACE"
fi
# We currently use an image from docker.io/spilchen.  This has the fix
# for test case ordering.  We will continue to use this until that is pushed to
# kyma-project.  When we use an official image the tag they use in the chart is
# pretty old.  You can find the latest one in:
# https://console.cloud.google.com/gcr/images/kyma-project/EU/incubator/develop/octopus
helm uninstall octopus || :
helm install chart/octopus --set image.pullPolicy=IfNotPresent --set image.registry=docker.io --set image.dir=spilchen/ --set image.version=a311366 --generate-name $HELM_NS --wait || :

# Install minIO operator
(
  set -x; cd "$(mktemp -d)" &&
  OS="$(uname | tr '[:upper:]' '[:lower:]')" &&
  ARCH="$(uname -m | sed -e 's/x86_64/amd64/' -e 's/\(arm\)\(64\)\?.*/\1\2/' -e 's/aarch64$/arm64/')" &&
  curl -fsSLO "https://github.com/kubernetes-sigs/krew/releases/latest/download/krew.tar.gz" &&
  tar zxvf krew.tar.gz &&
  KREW=./krew-"${OS}_${ARCH}" &&
  "$KREW" install krew
)
export PATH=$PATH:$HOME/.krew/bin
kubectl krew update
kubectl krew install minio
kubectl minio init
# By default kubectl-minio logs to a log file.  Delete it so that we don't track it in git.
rm $PWD/logs.log || :

# Setup rbac rules to allow the service account access to the cluster.
# By default the default service account created for the namespace has
# no access.  We need access so that we can deploy pods that access
# the cluster via kubectl.
tmpfile=$(mktemp /tmp/rbac-XXXXXX.yaml)
cat <<- EOF > $tmpfile
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: integration-test-role
rules:
- apiGroups:
  - ""
  resources:
  - services
  - pods
  - pods/exec
  - pods/log
  - configmaps
  - secrets
  verbs:
  - get
  - list
  - create
  - delete
- apiGroups:
  - "apps"
  resources:
  - statefulsets
  verbs:
  - get
  - list
  - create
- apiGroups:
  - "minio.min.io"
  resources:
  - tenants
  verbs:
  - get
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: integration-test-rb
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: integration-test-role
subjects:
- kind: ServiceAccount
  name: default
EOF
kubectl apply -f $tmpfile
