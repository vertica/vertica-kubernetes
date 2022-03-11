#!/bin/bash

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

# A script that will create service account roles and role bindings sample manifests

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
KUSTOMIZE=$REPO_DIR/bin/kustomize
SAMPLE_DIR="$REPO_DIR/config/samples"
RBAC_DIR="$SAMPLE_DIR/rbac"
KUBERNETES_SPLIT_YAML=$REPO_DIR/bin/kubernetes-split-yaml

mkdir -p config/overlays/rbac
cd config/overlays/rbac

cat <<- EOF > kustomization.yaml
namePrefix: verticadb-operator-

bases:
- ../../rbac
EOF

mkdir -p $RBAC_DIR

cd $REPO_DIR
$KUSTOMIZE build config/overlays/rbac | $KUBERNETES_SPLIT_YAML --outdir $RBAC_DIR -

cd $RBAC_DIR

cat <<- EOF > kustomization.yaml
resources:
- verticadb-operator-controller-manager-sa.yaml
- verticadb-operator-manager-role-role.yaml
- verticadb-operator-manager-rolebinding-rb.yaml
- verticadb-operator-leader-election-role-role.yaml
- verticadb-operator-leader-election-rolebinding-rb.yaml
EOF

sed -i '$d' $RBAC_DIR/verticadb-operator-controller-manager-sa.yaml
sed -i '$d' $RBAC_DIR/verticadb-operator-manager-rolebinding-rb.yaml
sed -i '$d' $RBAC_DIR/verticadb-operator-leader-election-rolebinding-rb.yaml
