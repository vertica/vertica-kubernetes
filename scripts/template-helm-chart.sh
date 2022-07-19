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

# A script that will add templating to the manifests in the helm chart template
# dir.  This will allow us to customize the deployment for different helm chart
# parameters.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
TEMPLATE_DIR=$1

if [ -z $TEMPLATE_DIR ]
then
    echo "*** Must specify directory to find the manifests to template"
    exit 1
fi

if [ ! -d $TEMPLATE_DIR ]
then
    echo "*** The directory $MANIFEST_DIR doesn't exist"
    exit 1
fi

# Add in the templating
# 1. Template the namespace
sed -i 's/verticadb-operator-system/{{ .Release.Namespace }}/g' $TEMPLATE_DIR/*
sed -i 's/verticadb-operator-.*-webhook-configuration/{{ .Release.Namespace }}-&/' $TEMPLATE_DIR/*
# 2. Template image names
sed -i "s|image: controller|image: '{{ with .Values.image }}{{ join \"/\" (list .repo .name) }}{{ end }}'|" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s|image: gcr.io/kubebuilder/kube-rbac-proxy:v0.11.0|image: '{{ with .Values.rbac_proxy_image }}{{ join \"/\" (list .repo .name) }}{{ end }}'|" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
# 3. Template imagePullPolicy
sed -i 's/imagePullPolicy: IfNotPresent/imagePullPolicy: {{ default "IfNotPresent" .Values.image.pullPolicy }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
# 4. Append imagePullSecrets
cat >>$TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml << END
{{ if .Values.imagePullSecrets }}
      imagePullSecrets:
{{ .Values.imagePullSecrets | toYaml | indent 8 }}
{{ end }}
END
# 5. Template the tls secret name
sed -i 's/secretName: webhook-server-cert/secretName: {{ default "webhook-server-cert" .Values.webhook.tlsSecret }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
for fn in verticadb-operator-selfsigned-issuer-issuer.yaml verticadb-operator-serving-cert-certificate.yaml
do
  sed -i '1s/^/{{- if not .Values.webhook.tlsSecret }}\n/' $TEMPLATE_DIR/$fn
  echo "{{- end -}}" >> $TEMPLATE_DIR/$fn
done
# 6. Template the caBundle
for fn in $(ls $TEMPLATE_DIR/*webhookconfiguration.yaml)
do
  sed -i 's/clientConfig:/clientConfig:\n    caBundle: {{ .Values.webhook.caBundle }}/' $fn
done
# 7. Template the resource limits and requests
sed -i 's/resources: template-placeholder/resources:\n          {{- toYaml .Values.resources | nindent 10 }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml

# 8.  Template the logging
sed -i "s/--filepath=.*/--filepath={{ .Values.logging.filePath }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s/--maxfilesize=.*/--maxfilesize={{ .Values.logging.maxFileSize }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s/--maxfileage=.*/--maxfileage={{ .Values.logging.maxFileAge }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s/--maxfilerotation=.*/--maxfilerotation={{ .Values.logging.maxFileRotation }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s/--level=.*/--level={{ .Values.logging.level }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s/--dev=.*/--dev={{ .Values.logging.dev }}/" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml

# 9.  Template the serviceaccount, roles and rolebindings
sed -i 's/serviceAccountName: verticadb-operator-controller-manager/serviceAccountName: {{ default "verticadb-operator-controller-manager" .Values.serviceAccountNameOverride }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i 's/--service-account-name=.*/--service-account-name={{ default "verticadb-operator-controller-manager" .Values.serviceAccountNameOverride }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
for f in verticadb-operator-controller-manager-sa.yaml \
    verticadb-operator-manager-role-role.yaml \
    verticadb-operator-manager-rolebinding-rb.yaml \
    verticadb-operator-leader-election-role-role.yaml \
    verticadb-operator-leader-election-rolebinding-rb.yaml \
    verticadb-operator-proxy-rolebinding-crb.yaml \
    verticadb-operator-proxy-role-cr.yaml
do
    sed -i '1s/^/{{- if not .Values.serviceAccountNameOverride -}}\n/' $TEMPLATE_DIR/$f
    echo "{{- end }}" >> $TEMPLATE_DIR/$f
done

# 10.  Template the webhook access enablement
sed -i '1s/^/{{- if .Values.webhook.enable -}}\n/' $TEMPLATE_DIR/verticadb-operator-validating-webhook-configuration-validatingwebhookconfiguration.yaml 
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-validating-webhook-configuration-validatingwebhookconfiguration.yaml
sed -i '1s/^/{{- if .Values.webhook.enable -}}\n/' $TEMPLATE_DIR/verticadb-operator-mutating-webhook-configuration-mutatingwebhookconfiguration.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-mutating-webhook-configuration-mutatingwebhookconfiguration.yaml
sed -i '1s/^/{{- if .Values.webhook.enable -}}\n/' $TEMPLATE_DIR/verticadb-operator-webhook-service-svc.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-webhook-service-svc.yaml

# 11.  Template the prometheus metrics service
sed -i '1s/^/{{- if hasPrefix "Enable" .Values.prometheus.expose -}}\n/' $TEMPLATE_DIR/verticadb-operator-metrics-service-svc.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-metrics-service-svc.yaml

# 12.  Template the roles/rolebindings for access to the rbac proxy
for f in verticadb-operator-proxy-rolebinding-crb.yaml \
    verticadb-operator-proxy-role-cr.yaml \
    verticadb-operator-metrics-reader-cr.yaml \
    verticadb-operator-metrics-reader-crb.yaml
do
    sed -i '1s/^/{{- if .Values.prometheus.createProxyRBAC -}}\n/' $TEMPLATE_DIR/$f
    echo "{{- end }}" >> $TEMPLATE_DIR/$f
done

# 13.  Template the ServiceMonitor object for Promtheus operator
sed -i '1s/^/{{- if .Values.prometheus.createServiceMonitor -}}\n/' $TEMPLATE_DIR/verticadb-operator-metrics-monitor-servicemonitor.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-metrics-monitor-servicemonitor.yaml
perl -i -0777 -pe 's/(.*endpoints:)/$1\n{{- if eq "EnableWithAuthProxy" .Values.prometheus.expose }}/g' $TEMPLATE_DIR/verticadb-operator-metrics-monitor-servicemonitor.yaml
perl -i -0777 -pe 's/(.*insecureSkipVerify:.*)/$1\n{{- else }}\n  - path: \/metrics\n    port: metrics\n    scheme: http\n{{- end }}/g' $TEMPLATE_DIR/verticadb-operator-metrics-monitor-servicemonitor.yaml

# 14.  Template the metrics bind address
sed -i 's/- --metrics-bind-address=.*/- --metrics-bind-address={{ if eq "EnableWithAuthProxy" .Values.prometheus.expose }}127.0.0.1{{ end }}:{{ if eq "EnableWithAuthProxy" .Values.prometheus.expose }}8080{{ else }}8443{{ end }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
perl -i -0777 -pe 's/(.*metrics-bind-address.*)/{{- if hasPrefix "Enable" .Values.prometheus.expose }}\n$1\n{{- end }}/g' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
perl -i -0777 -pe 's/(.*ports:\n.*containerPort: 9443\n.*webhook-server.*\n.*)/$1\n{{- if hasPrefix "EnableWithoutAuth" .Values.prometheus.expose }}\n        - name: metrics\n          containerPort: 8443\n          protocol: TCP\n{{- end }}/g' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml

# 15.  Template the rbac container
perl -i -0777 -pe 's/(.*- args:.*\n.*secure)/{{- if eq .Values.prometheus.expose "EnableWithAuthProxy" }}\n$1/g' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
# We need to put the matching end at the end of the container spec.
perl -i -0777 -pe 's/(memory: 64Mi)/$1\n{{- end }}/g' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
