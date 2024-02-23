{{/*
Expand the name of the chart.
*/}}
{{- define "vdb-op.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Choose the serviceAccount name
*/}}
{{- define "vdb-op.serviceAccount" -}}
{{- if .Values.serviceAccountNameOverride }}
{{- .Values.serviceAccountNameOverride }}
{{- else }}
{{- include "vdb-op.name" . }}-manager
{{- end }}
{{- end }}

{{/*
Choose the prefix for objects that is related to the metrics endpoint in the operator.
*/}}
{{- define "vdb-op.metricsRbacPrefix" -}}
{{- printf "%s-%s-" .Release.Namespace .Release.Name | trunc 63 }}
{{- end }}

{{/*
Choose the webhook certificate source
*/}}
{{- define "vdb-op.certSource" -}}
{{- if not (empty .Values.webhook.tlsSecret) }}
{{- "secret" }}
{{- else }}
{{- .Values.webhook.certSource }}
{{- end }}
{{- end }}

{{/*
Choose the secret that contains the webhook certificate.
This can be blank if the operator does not get the webhook from a secret (i.e.
it is generated internally)
*/}}
{{- define "vdb-op.certSecret" -}}
{{- if not (empty .Values.webhook.tlsSecret) }}
{{- .Values.webhook.tlsSecret }}
{{- else if eq .Values.webhook.certSource "internal" }}
{{- cat "" | quote }}
{{- else }}
{{- include "vdb-op.name" . }}-service-cert
{{- end }}
{{- end }}

{{/*
Choose between Role or ClusterRole for the manager.
*/}}
{{- define "vdb-op.roleKind" -}}
{{- if eq .Values.scope "namespace" }}
{{- cat "Role" }}
{{- else }}
{{- cat "ClusterRole" }}
{{- end }}
{{- end }}

{{/*
Choose between RoleBinding or ClusterRoleBinding for the manager.
*/}}
{{- define "vdb-op.roleBindingKind" -}}
{{- if eq .Values.scope "namespace" }}
{{- cat "RoleBinding" }}
{{- else }}
{{- cat "ClusterRoleBinding" }}
{{- end }}
{{- end }}