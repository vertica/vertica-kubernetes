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

# A script that will create the helm chart and add templating to it

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
KUSTOMIZE=$REPO_DIR/bin/kustomize
KUBERNETES_SPLIT_YAML=$REPO_DIR/bin/kubernetes-split-yaml
OPERATOR_CHART="$REPO_DIR/helm-charts/verticadb-operator"
TEMPLATE_DIR=$OPERATOR_CHART/templates

$KUSTOMIZE build $REPO_DIR/config/default | $KUBERNETES_SPLIT_YAML --outdir $TEMPLATE_DIR -
mv $TEMPLATE_DIR/verticadbs.vertica.com-crd.yaml $OPERATOR_CHART/crds

# Add in the templating
# 1. Template the namespace
sed -i 's/verticadb-operator-system/{{ .Release.Namespace }}/g' $TEMPLATE_DIR/*
sed -i 's/verticadb-operator-.*-webhook-configuration/{{ .Release.Namespace }}-&/' $TEMPLATE_DIR/*
# 2. Template the image name
sed -i "s/image: controller/image: '{{ .Values.image.name }}'/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
# 3. Template the tls secret name
sed -i 's/secretName: webhook-server-cert/secretName: {{ default "webhook-server-cert" .Values.webhook.tlsSecret }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
for fn in verticadb-operator-selfsigned-issuer-issuer.yaml verticadb-operator-serving-cert-certificate.yaml
do
  sed -i '1s/^/{{- if not .Values.webhook.tlsSecret }}\n/' $TEMPLATE_DIR/$fn
  echo "{{- end -}}" >> $TEMPLATE_DIR/$fn
done
# 4. Template the caBundle
for fn in $(ls $TEMPLATE_DIR/*webhookconfiguration.yaml)
do
  sed -i 's/clientConfig:/clientConfig:\n    caBundle: {{ .Values.webhook.caBundle }}/' $fn
done
