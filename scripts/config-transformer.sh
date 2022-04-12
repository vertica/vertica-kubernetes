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

# A script that will transform the manifests generated from config/.  It will
# generate release artifacts and a helm chart.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
KUSTOMIZE=$REPO_DIR/bin/kustomize
KUBERNETES_SPLIT_YAML=$REPO_DIR/bin/kubernetes-split-yaml
OPERATOR_CHART="$REPO_DIR/helm-charts/verticadb-operator"
TEMPLATE_DIR=$OPERATOR_CHART/templates
CRD_DIR=$OPERATOR_CHART/crds

rm $TEMPLATE_DIR/*yaml 2>/dev/null || true
$KUSTOMIZE build $REPO_DIR/config/default | $KUBERNETES_SPLIT_YAML --outdir $TEMPLATE_DIR -
mv $TEMPLATE_DIR/verticadbs.vertica.com-crd.yaml $CRD_DIR

# Delete openshift clusterRole and clusterRoleBinding files
rm $TEMPLATE_DIR/verticadb-operator-openshift-cluster-role-cr.yaml 
rm $TEMPLATE_DIR/verticadb-operator-openshift-cluster-rolebinding-crb.yaml

# Generate release artifacts from the splity yaml's just generated.  This is
# done before templating the helm charts so that the yaml's can be used
# directly with a 'kubectl apply' command.
$SCRIPT_DIR/gen-release-artifacts.sh $TEMPLATE_DIR

# Add templating to the manifests in templates/ so that we can use helm
# parameters to customize the deployment.
$SCRIPT_DIR/template-helm-chart.sh $TEMPLATE_DIR
