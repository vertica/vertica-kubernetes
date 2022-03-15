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

# A script that will create the helm chart and add templating to it

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
KUSTOMIZE=$REPO_DIR/bin/kustomize
KUBERNETES_SPLIT_YAML=$REPO_DIR/bin/kubernetes-split-yaml
OPERATOR_CHART="$REPO_DIR/helm-charts/verticadb-operator"
TEMPLATE_DIR=$OPERATOR_CHART/templates
CRD_DIR=$OPERATOR_CHART/crds

$KUSTOMIZE build $REPO_DIR/config/default | $KUBERNETES_SPLIT_YAML --outdir $TEMPLATE_DIR -
mv $TEMPLATE_DIR/verticadbs.vertica.com-crd.yaml $CRD_DIR

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
# 5. Template the resource limits and requests
sed -i 's/resources: template-placeholder/resources:\n          {{- toYaml .Values.resources | nindent 10 }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml

# 6.  Template the logging
sed -i "s/--filepath=.*/--filepath={{ .Values.logging.filePath }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s/--maxfilesize=.*/--maxfilesize={{ .Values.logging.maxFileSize }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s/--maxfileage=.*/--maxfileage={{ .Values.logging.maxFileAge }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s/--maxfilerotation=.*/--maxfilerotation={{ .Values.logging.maxFileRotation }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s/--level=.*/--level={{ .Values.logging.level }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s/--dev=.*/--dev={{ .Values.logging.dev }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml

# 7.  Template the serviceaccount, roles and rolebindings
sed -i 's/serviceAccountName: verticadb-operator-controller-manager/serviceAccountName: {{ default "verticadb-operator-controller-manager" .Values.serviceAccountNameOverride }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i 's/--service-account-name=.*/--service-account-name={{ default "verticadb-operator-controller-manager" .Values.serviceAccountNameOverride }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i '1s/^/{{- if not .Values.serviceAccountNameOverride -}}\n/' $TEMPLATE_DIR/verticadb-operator-controller-manager-sa.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-controller-manager-sa.yaml
sed -i '1s/^/{{- if not .Values.serviceAccountNameOverride -}}\n/' $TEMPLATE_DIR/verticadb-operator-manager-role-role.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-manager-role-role.yaml
sed -i '1s/^/{{- if not .Values.serviceAccountNameOverride -}}\n/' $TEMPLATE_DIR/verticadb-operator-manager-rolebinding-rb.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-manager-rolebinding-rb.yaml
sed -i '1s/^/{{- if not .Values.serviceAccountNameOverride -}}\n/' $TEMPLATE_DIR/verticadb-operator-leader-election-role-role.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-leader-election-role-role.yaml
sed -i '1s/^/{{- if not .Values.serviceAccountNameOverride -}}\n/' $TEMPLATE_DIR/verticadb-operator-leader-election-rolebinding-rb.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-leader-election-rolebinding-rb.yaml

# 8.  Template the webhook access enablement
sed -i '1s/^/{{- if .Values.webhook.enable -}}\n/' $TEMPLATE_DIR/verticadb-operator-validating-webhook-configuration-validatingwebhookconfiguration.yaml 
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-validating-webhook-configuration-validatingwebhookconfiguration.yaml
sed -i '1s/^/{{- if .Values.webhook.enable -}}\n/' $TEMPLATE_DIR/verticadb-operator-mutating-webhook-configuration-mutatingwebhookconfiguration.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-mutating-webhook-configuration-mutatingwebhookconfiguration.yaml
sed -i '1s/^/{{- if .Values.webhook.enable -}}\n/' $TEMPLATE_DIR/verticadb-operator-webhook-service-svc.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-webhook-service-svc.yaml

# Delete openshift clusterRole and clusterRoleBinding files
rm $TEMPLATE_DIR/verticadb-operator-openshift-cluster-role-cr.yaml 
rm $TEMPLATE_DIR/verticadb-operator-openshift-cluster-rolebinding-crb.yaml
