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
perl -i -0777 -pe 's/(verticadb-operator)(-.*-webhook-configuration)/$1-{{ .Release.Namespace }}$2/' $TEMPLATE_DIR/*
# 2. Template image names
sed -i "s|image: controller|image: '{{ with .Values.image }}{{ join \"/\" (list .repo .name) }}{{ end }}'|" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i "s|image: gcr.io/kubebuilder/kube-rbac-proxy:v.*|image: '{{ with .Values.rbac_proxy_image }}{{ join \"/\" (list .repo .name) }}{{ end }}'|" $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
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
for fn in verticadb-operator-controller-manager-deployment.yaml \
    verticadb-operator-serving-cert-certificate.yaml
do
  sed -i 's/secretName: webhook-server-cert/secretName: {{ include "vdb-op.certSecret" . }}/' $TEMPLATE_DIR/$fn
done
for fn in $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
do
  # Include the secret only if not using webhook.certSource=internal
  perl -i -0777 -pe 's/(.*- name: webhook-cert\n.*secret:\n.*defaultMode:.*\n.*secretName:.*)/\{\{- if or (ne .Values.webhook.certSource "internal") (not (empty .Values.webhook.tlsSecret)) \}\}\n$1\n\{\{- end \}\}/g' $fn
  perl -i -0777 -pe 's/(.*- mountPath: .*\n.*name: webhook-cert\n.*readOnly:.*)/\{\{- if or (ne .Values.webhook.certSource "internal") (not (empty .Values.webhook.tlsSecret)) \}\}\n$1\n\{\{- end \}\}/g' $fn
  # Update the --webhook-cert-secret option to include the actual name of the secret
  perl -i -0777 -pe 's/(- --webhook-cert-secret=)(.*)/$1\{\{ include "vdb-op.certSecret" . \}\}/g' $fn
  # Set ENABLE_WEBHOOK according to webhook.enable value
  perl -i -0777 -pe 's/(name: ENABLE_WEBHOOKS\n.*value:) .*/$1 {{ quote .Values.webhook.enable }}/g' $fn
done
for fn in verticadb-operator-selfsigned-issuer-issuer.yaml \
    verticadb-operator-serving-cert-certificate.yaml
do
  sed -i '1s/^/{{- if eq .Values.webhook.certSource "cert-manager" }}\n/' $TEMPLATE_DIR/$fn
  echo "{{- end -}}" >> $TEMPLATE_DIR/$fn
done
# Include WEBHOOK_CERT_SOURCE in the config map
perl -i -0777 -pe 's/(\ndata:)/$1\n  WEBHOOK_CERT_SOURCE: {{ include "vdb-op.certSource" . }}/g' $TEMPLATE_DIR/verticadb-operator-manager-config-cm.yaml
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
sed -i 's/serviceAccountName: verticadb-operator-controller-manager/serviceAccountName: {{ include "vdb-op.serviceAccount" . }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
sed -i 's/--service-account-name=.*/--service-account-name={{ include "vdb-op.serviceAccount" . }}/' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
for f in verticadb-operator-controller-manager-sa.yaml
do
    sed -i '1s/^/{{- if not .Values.serviceAccountNameOverride -}}\n/' $TEMPLATE_DIR/$f
    echo "{{- end }}" >> $TEMPLATE_DIR/$f
done
for f in verticadb-operator-manager-role-role.yaml \
    verticadb-operator-manager-rolebinding-rb.yaml \
    verticadb-operator-leader-election-role-role.yaml \
    verticadb-operator-leader-election-rolebinding-rb.yaml
do
    sed -i '1s/^/{{- if not .Values.skipRoleAndRoleBindingCreation -}}\n/' $TEMPLATE_DIR/$f
    echo "{{- end }}" >> $TEMPLATE_DIR/$f
done
for f in verticadb-operator-manager-rolebinding-rb.yaml \
    verticadb-operator-leader-election-rolebinding-rb.yaml \
    verticadb-operator-proxy-rolebinding-crb.yaml \
    verticadb-operator-metrics-reader-crb.yaml \
    verticadb-operator-manager-clusterrolebinding-crb.yaml
do
    perl -i -0777 -pe 's/kind: ServiceAccount\n.*name: .*/kind: ServiceAccount\n  name: {{ include "vdb-op.serviceAccount" . }}/g' $TEMPLATE_DIR/$f
done
# ClusterRole and ClusterRoleBinding's all need the namespace included in their
# names to make them unique for multiple operator deployments.
perl -i -0777 -pe 's/-manager-clusterrolebinding/-{{ .Release.Namespace }}-manager-clusterolebinding/g' $TEMPLATE_DIR/verticadb-operator-manager-clusterrolebinding-crb.yaml
for f in verticadb-operator-manager-clusterrolebinding-crb.yaml \
    verticadb-operator-manager-role-cr.yaml
do
  perl -i -0777 -pe 's/-manager-role/-{{ .Release.Namespace }}-manager-role/g' $TEMPLATE_DIR/$f
done
for f in verticadb-operator-metrics-reader-cr.yaml verticadb-operator-metrics-reader-crb.yaml
do
    perl -i -0777 -pe 's/-metrics-reader/-{{ .Release.Namespace }}-metrics-reader/g' $TEMPLATE_DIR/$f
done
for f in verticadb-operator-proxy-role-cr.yaml verticadb-operator-proxy-rolebinding-crb.yaml
do
    perl -i -0777 -pe 's/-(proxy-role.*)/-{{ .Release.Namespace }}-$1/g' $TEMPLATE_DIR/$f
done

# 10.  Template the webhook access enablement
for f in $TEMPLATE_DIR/verticadb-operator-validating-webhook-configuration-validatingwebhookconfiguration.yaml \
    $TEMPLATE_DIR/verticadb-operator-mutating-webhook-configuration-mutatingwebhookconfiguration.yaml
do
    sed -i '1s/^/{{- if .Values.webhook.enable -}}\n/' $f
    echo "{{- end }}" >> $f
    perl -i -0777 -pe 's/(  annotations:)/$1\n{{- if eq .Values.webhook.certSource "cert-manager" }}/' $f
    perl -i -0777 -pe 's/(    cert-manager.io.*)/$1\n{{- else }}\n    \{\}\n{{- end }}/' $f
done
sed -i '1s/^/{{- if .Values.webhook.enable -}}\n/' $TEMPLATE_DIR/verticadb-operator-webhook-service-svc.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-webhook-service-svc.yaml
# Related to this change is the --skip-webhook-patch option. This is needed if
# the helm chart provided the CA bundle or using cert-manager, which handles
# the CA bundle update itself.
perl -i -0777 -pe 's/(--webhook-cert-secret.*)/$1\n{{- if or (eq .Values.webhook.certSource "cert-manager") (.Values.webhook.caBundle) }}\n        - --skip-webhook-patch\n{{- end }}/g' $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml

# 11.  Template the prometheus metrics service
sed -i '1s/^/{{- if hasPrefix "Enable" .Values.prometheus.expose -}}\n/' $TEMPLATE_DIR/verticadb-operator-metrics-service-svc.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-metrics-service-svc.yaml

# 12.  Template the roles/rolebindings for access to the rbac proxy
for f in verticadb-operator-proxy-rolebinding-crb.yaml \
    verticadb-operator-proxy-role-cr.yaml \
    verticadb-operator-metrics-reader-cr.yaml \
    verticadb-operator-metrics-reader-crb.yaml
do
    sed -i '1s/^/{{- if and (.Values.prometheus.createProxyRBAC) (eq .Values.prometheus.expose "EnableWithAuthProxy") -}}\n/' $TEMPLATE_DIR/$f
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

# 16.  Template places that refer to objects by name.  Do this in all files.
# In the config/ directory we hardcoded everything to start with
# verticadb-operator.
sed -i 's/verticadb-operator/{{ include "vdb-op.name" . }}/g' $TEMPLATE_DIR/*yaml

# 17.  Mount TLS certs in the rbac proxy
for f in $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
do
    perl -i -0777 -pe 's/(.*--v=[0-9]+)/$1\n{{- if not (empty .Values.prometheus.tlsSecret) }}\n        - --tls-cert-file=\/cert\/tls.crt\n        - --tls-private-key-file=\/cert\/tls.key\n        - --client-ca-file=\/cert\/ca.crt\n{{- end }}/g' $f
    perl -i -0777 -pe 's/(volumes:)/$1\n{{- if not (empty .Values.prometheus.tlsSecret) }}\n      - name: auth-cert\n        secret:\n          secretName: {{ .Values.prometheus.tlsSecret }}\n{{- end }}/g' $f
    perl -i -0777 -pe 's/(name: kube-rbac-proxy)/$1\n{{- if not (empty .Values.prometheus.tlsSecret) }}\n        volumeMounts:\n        - mountPath: \/cert\n          name: auth-cert\n{{- end }}/g' $f
done

# 18.  Add pod scheduling options
cat << EOF >> $TEMPLATE_DIR/verticadb-operator-controller-manager-deployment.yaml
{{- if .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml .Values.nodeSelector | nindent 8 }}
{{- end }}
{{- if .Values.affinity }}
      affinity:
        {{- toYaml .Values.affinity | nindent 8 }}
{{- end }}
{{- if .Values.priorityClassName }}
      priorityClassName: {{ .Values.priorityClassName }}
{{- end }}
{{- if .Values.tolerations }}
      tolerations:
        {{- toYaml .Values.tolerations | nindent 8 }}
{{- end }}
EOF

# 19. There are clusterrole/clusterrolebinding that are only needed if the
# operator is going to patch the webhook. This is needed only if the operator
# is generating its own self-signed cert for the webhook or a secret was
# provided. For cert-manager, the cert-manager operator injects the CA and the
# operator doesn't need to handle that.
for f in verticadb-operator-manager-role-cr.yaml \
    verticadb-operator-manager-clusterrolebinding-crb.yaml
do
    sed -i '1s/^/{{- if and (.Values.webhook.enable) (or (eq .Values.webhook.certSource "internal") (.Values.webhook.tlsSecret)) -}}\n/' $TEMPLATE_DIR/$f
    echo "{{- end }}" >> $TEMPLATE_DIR/$f
done
