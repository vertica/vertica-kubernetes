#!/bin/bash

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
perl -i -0777 -pe 's/verticadb-operator-system/{{ .Release.Namespace }}/g' $TEMPLATE_DIR/*
# 2. Template image names
perl -i -0777 -pe "s|image: controller|image: '{{ with .Values.image }}{{ join \"/\" (list .repo .name) }}{{ end }}'|" $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
perl -i -0777 -pe "s|image: gcr.io/kubebuilder/kube-rbac-proxy:v.*|image: '{{ with .Values.rbac_proxy_image }}{{ join \"/\" (list .repo .name) }}{{ end }}'|" $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
# 3. Template imagePullPolicy
perl -i -0777 -pe 's/imagePullPolicy: IfNotPresent/imagePullPolicy: {{ default "IfNotPresent" .Values.image.pullPolicy }}/' $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
# 4. Append imagePullSecrets
cat >>$TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml << END
{{ if .Values.imagePullSecrets }}
      imagePullSecrets:
{{ .Values.imagePullSecrets | toYaml | indent 8 }}
{{ end }}
END
# 5. Template the tls secret name
for fn in verticadb-operator-manager-deployment.yaml \
    verticadb-operator-serving-cert-certificate.yaml
do
  perl -i -0777 -pe 's/secretName: webhook-server-cert/secretName: {{ include "vdb-op.certSecret" . }}/' $TEMPLATE_DIR/$fn
done
for fn in $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
do
  # Include the secret only if not using webhook.certSource=internal
  perl -i -0777 -pe 's/(.*- name: webhook-cert\n.*secret:\n.*defaultMode:.*\n.*secretName:.*)/\{\{- if or (ne .Values.webhook.certSource "internal") (not (empty .Values.webhook.tlsSecret)) \}\}\n$1\n\{\{- end \}\}/g' $fn
  perl -i -0777 -pe 's/(.*- mountPath: .*\n.*name: webhook-cert\n.*readOnly:.*)/\{\{- if or (ne .Values.webhook.certSource "internal") (not (empty .Values.webhook.tlsSecret)) \}\}\n$1\n\{\{- end \}\}/g' $fn
done
for fn in verticadb-operator-selfsigned-issuer-issuer.yaml \
    verticadb-operator-serving-cert-certificate.yaml
do
  perl -i -pe 's/^/{{- if eq .Values.webhook.certSource "cert-manager" }}\n/ if 1 .. 1' $TEMPLATE_DIR/$fn
  echo "{{- end -}}" >> $TEMPLATE_DIR/$fn
done
# Include WEBHOOK_CERT_SOURCE in the config map
perl -i -0777 -pe 's/(\ndata:)/$1\n  WEBHOOK_CERT_SOURCE: {{ include "vdb-op.certSource" . }}/g' $TEMPLATE_DIR/verticadb-operator-manager-config-cm.yaml
# 7. Template the resource limits and requests
perl -i -0777 -pe 's/resources: template-placeholder/resources:\n          {{- toYaml .Values.resources | nindent 10 }}/' $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml

# 8.  Template the logging
perl -i -0777 -pe "s/--filepath=.*/--filepath={{ .Values.logging.filePath }}/" $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
perl -i -0777 -pe "s/--maxfilesize=.*/--maxfilesize={{ .Values.logging.maxFileSize }}/" $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
perl -i -0777 -pe "s/--maxfileage=.*/--maxfileage={{ .Values.logging.maxFileAge }}/" $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
perl -i -0777 -pe "s/--maxfilerotation=.*/--maxfilerotation={{ .Values.logging.maxFileRotation }}/" $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
perl -i -0777 -pe "s/--level=.*/--level={{ .Values.logging.level }}/" $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
perl -i -0777 -pe "s/--dev=.*/--dev={{ .Values.logging.dev }}/" $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml

# 9.  Template the serviceaccount, roles and rolebindings
perl -i -0777 -pe 's/serviceAccountName: verticadb-operator-manager/serviceAccountName: {{ include "vdb-op.serviceAccount" . }}/' $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
perl -i -0777 -pe 's/name: .*/name: {{ include "vdb-op.serviceAccount" . }}/' $TEMPLATE_DIR/verticadb-operator-manager-sa.yaml
cat << EOF >> $TEMPLATE_DIR/verticadb-operator-manager-sa.yaml
{{- if .Values.serviceAccountAnnotations }}
  annotations:
    {{- toYaml .Values.serviceAccountAnnotations | nindent 4 }}
{{- end }}
EOF
for f in  \
    verticadb-operator-leader-election-rolebinding-rb.yaml \
    verticadb-operator-proxy-rolebinding-crb.yaml \
    verticadb-operator-metrics-reader-crb.yaml \
    verticadb-operator-manager-clusterrolebinding-crb.yaml
do
    perl -i -0777 -pe 's/kind: ServiceAccount\n.*name: .*/kind: ServiceAccount\n  name: {{ include "vdb-op.serviceAccount" . }}/g' $TEMPLATE_DIR/$f
done

# 10.  Template the webhook access enablement
for f in $TEMPLATE_DIR/verticadb-operator-validating-webhook-configuration-validatingwebhookconfiguration.yaml \
    $TEMPLATE_DIR/verticadb-operator-mutating-webhook-configuration-mutatingwebhookconfiguration.yaml
do
    perl -i -pe 's/^/{{- if .Values.webhook.enable -}}\n/ if 1 .. 1' $f
    echo "{{- end }}" >> $f
    perl -i -0777 -pe 's/(  annotations:)/$1\n{{- if eq .Values.webhook.certSource "cert-manager" }}/' $f
    perl -i -0777 -pe 's/(    cert-manager.io.*)/$1\n{{- else }}\n    \{\}\n{{- end }}/' $f
done
for f in $TEMPLATE_DIR/verticadb-operator-webhook-config-cr.yaml \
  $TEMPLATE_DIR/verticadb-operator-webhook-config-crb.yaml
do
  perl -i -pe 's/^/{{- if .Values.webhook.enable -}}\n/ if 1 .. 1' $f
  echo "{{- end }}" >> $f
done

# 11.  Template the prometheus metrics service
perl -i -pe 's/^/{{- if hasPrefix "Enable" .Values.prometheus.expose -}}\n/ if 1 .. 1' $TEMPLATE_DIR/verticadb-operator-metrics-service-svc.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-metrics-service-svc.yaml

# 12.  Template the roles/rolebindings for access to the rbac proxy
for f in verticadb-operator-proxy-rolebinding-crb.yaml \
    verticadb-operator-proxy-role-cr.yaml \
    verticadb-operator-metrics-reader-cr.yaml \
    verticadb-operator-metrics-reader-crb.yaml
do
    perl -i -pe 's/^/{{- if and (.Values.prometheus.createProxyRBAC) (eq .Values.prometheus.expose "EnableWithAuthProxy") -}}\n/ if 1 .. 1' $TEMPLATE_DIR/$f
    echo "{{- end }}" >> $TEMPLATE_DIR/$f
    perl -i -0777 -pe 's/-metrics-reader/-{{ include "vdb-op.metricsRbacPrefix" . }}metrics-reader/g' $TEMPLATE_DIR/$f
    perl -i -0777 -pe 's/-(proxy-role.*)/-{{ include "vdb-op.metricsRbacPrefix" . }}$1/g' $TEMPLATE_DIR/$f
done

# 13.  Template the ServiceMonitor object for Promtheus operator
perl -i -pe 's/^/{{- if .Values.prometheus.createServiceMonitor -}}\n/ if 1 .. 1' $TEMPLATE_DIR/verticadb-operator-metrics-monitor-servicemonitor.yaml
echo "{{- end }}" >> $TEMPLATE_DIR/verticadb-operator-metrics-monitor-servicemonitor.yaml
perl -i -0777 -pe 's/(.*endpoints:)/$1\n{{- if eq "EnableWithAuthProxy" .Values.prometheus.expose }}/g' $TEMPLATE_DIR/verticadb-operator-metrics-monitor-servicemonitor.yaml
perl -i -0777 -pe 's/(.*insecureSkipVerify:.*)/$1\n{{- else }}\n  - path: \/metrics\n    port: metrics\n    scheme: http\n{{- end }}/g' $TEMPLATE_DIR/verticadb-operator-metrics-monitor-servicemonitor.yaml

# 14.  Template the metrics bind address
perl -i -0777 -pe 's/(METRICS_ADDR: )(.*)/$1 "{{ if eq "EnableWithAuthProxy" .Values.prometheus.expose }}127.0.0.1{{ end }}:{{ if eq "EnableWithAuthProxy" .Values.prometheus.expose }}8080{{ else }}8443{{ end }}"/' $TEMPLATE_DIR/verticadb-operator-manager-config-cm.yaml
perl -i -0777 -pe 's/(.*METRICS_ADDR:.*)/{{- if hasPrefix "Enable" .Values.prometheus.expose }}\n$1\n{{- else }}\n  METRICS_ADDR: ""\n{{- end }}/g' $TEMPLATE_DIR/verticadb-operator-manager-config-cm.yaml
perl -i -0777 -pe 's/(.*ports:\n.*containerPort: 9443\n.*webhook-server.*\n.*)/$1\n{{- if hasPrefix "EnableWithoutAuth" .Values.prometheus.expose }}\n        - name: metrics\n          containerPort: 8443\n          protocol: TCP\n{{- end }}/g' $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml

# 15.  Template the rbac container
perl -i -0777 -pe 's/(.*- args:.*\n.*secure)/{{- if eq .Values.prometheus.expose "EnableWithAuthProxy" }}\n$1/g' $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
# We need to put the matching end at the end of the container spec.
perl -i -0777 -pe 's/(memory: 64Mi)/$1\n{{- end }}/g' $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml

# 16.  Template places that refer to objects by name.  Do this in all files.
# In the config/ directory we hardcoded everything to start with
# verticadb-operator.
perl -i -0777 -pe 's/verticadb-operator/{{ include "vdb-op.name" . }}/g' $TEMPLATE_DIR/*yaml

# 17.  Mount TLS certs in the rbac proxy
for f in $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
do
    perl -i -0777 -pe 's/(.*--v=[0-9]+)/$1\n{{- if not (empty .Values.prometheus.tlsSecret) }}\n        - --tls-cert-file=\/cert\/tls.crt\n        - --tls-private-key-file=\/cert\/tls.key\n        - --client-ca-file=\/cert\/ca.crt\n{{- end }}/g' $f
    perl -i -0777 -pe 's/(volumes:)/$1\n{{- if not (empty .Values.prometheus.tlsSecret) }}\n      - name: auth-cert\n        secret:\n          secretName: {{ .Values.prometheus.tlsSecret }}\n{{- end }}/g' $f
    perl -i -0777 -pe 's/(name: kube-rbac-proxy)/$1\n{{- if not (empty .Values.prometheus.tlsSecret) }}\n        volumeMounts:\n        - mountPath: \/cert\n          name: auth-cert\n{{- end }}/g' $f
done

# 18.  Add pod scheduling options
cat << EOF >> $TEMPLATE_DIR/verticadb-operator-manager-deployment.yaml
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

# 19. Template the per-CR concurrency parameters
for f in $TEMPLATE_DIR/verticadb-operator-manager-config-cm.yaml
do
    perl -i -0777 -pe 's/(CONCURRENCY_VERTICADB: ).*/$1\{\{ .Values.reconcileConcurrency.verticadb | quote \}\}/g' $f
    perl -i -0777 -pe 's/(CONCURRENCY_VERTICAAUTOSCALER: ).*/$1\{\{ .Values.reconcileConcurrency.verticaautoscaler | quote \}\}/g' $f
    perl -i -0777 -pe 's/(CONCURRENCY_EVENTTRIGGER: ).*/$1\{\{ .Values.reconcileConcurrency.eventtrigger | quote \}\}/g' $f
    perl -i -0777 -pe 's/(CONCURRENCY_VERTICARESTOREPOINTSQUERY: ).*/$1\{\{ .Values.reconcileConcurrency.verticarestorepointsquery | quote \}\}/g' $f
    perl -i -0777 -pe 's/(CONCURRENCY_VERTICASCRUTINIZE: ).*/$1\{\{ .Values.reconcileConcurrency.verticascrutinize | quote \}\}/g' $f
done

# 20. Add permissions to manager ClusterRole to allow it to patch the CRD. This
# is only needed if the webhook cert is generated by the operator or provided
# by a Secret.
cat << EOF >> $TEMPLATE_DIR/verticadb-operator-webhook-config-cr.yaml
{{- if .Values.webhook.enable }}
- apiGroups:
  - apiextensions.k8s.io
  resources:
  - customresourcedefinitions
  verbs:
  - get
  - list
  - patch
  - update
{{- end }}
EOF

# 21. Change change ClusterRoles/ClusterRoleBindings for the manager to be
# Roles/RoleBindings if the operator is scoped to a single namespace.
for f in $TEMPLATE_DIR/verticadb-operator-manager-clusterrolebinding-crb.yaml \
    $TEMPLATE_DIR/verticadb-operator-manager-role-cr.yaml
do
    perl -i -0777 -pe 's/kind: ClusterRoleBinding/kind: {{ include "vdb-op.roleBindingKind" . }}/g' $f
    perl -i -0777 -pe 's/kind: ClusterRole/kind: {{ include "vdb-op.roleKind" . }}/g' $f
    perl -i -pe 's/^/{{- if .Values.controllers.enable -}}\n/ if 1 .. 1' $f
    echo "{{- end }}" >> $f
done

# 22. Template the operator config
for fn in $TEMPLATE_DIR/verticadb-operator-manager-config-cm.yaml
do
  perl -i -0777 -pe 's/(WEBHOOKS_ENABLED:).*/$1 {{ quote .Values.webhook.enable }}/g' $fn
  perl -i -0777 -pe 's/(CONTROLLERS_ENABLED:).*/$1 {{ quote .Values.controllers.enable }}/g' $fn
  perl -i -0777 -pe 's/(CONTROLLERS_SCOPE:).*/$1 {{ quote .Values.controllers.scope }}/g' $fn
  # Update the webhook-cert-secret configMap entry to include the actual name of the secret
  perl -i -0777 -pe 's/(WEBHOOK_CERT_SECRET: )(.*)/$1\{\{ include "vdb-op.certSecret" . \}\}/g' $fn
  perl -i -0777 -pe 's/(LOG_LEVEL: )(.*)/$1\{{ quote .Values.logging.level }}\n  LOG_FILE_PATH: {{ default "" .Values.logging.filePath | quote }}\n  LOG_MAX_FILE_SIZE: {{ default "" .Values.logging.maxFileSize | quote }}\n  LOG_MAX_FILE_AGE: {{ default "" .Values.logging.maxFileAge | quote }}\n  LOG_MAX_FILE_ROTATION: {{ default "" .Values.logging.maxFileRotation | quote }}\n  DEV_MODE: {{ default "" .Values.logging.dev | quote }}/g' $fn
done
