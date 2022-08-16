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
{{- include "vdb-op.name" . }}-controller-manager
{{- end }}
{{- end }}

{{/*
Choose the name of the tls secret to use for the http server
*/}}
{{- define "vdb-op.httpTLSSecret" -}}
{{- if .Values.http.tlsSecret }}
{{- .Values.http.tlsSecret }}
{{- else }}
{{- include "vdb-op.name" . }}-http-server-cert
{{- end }}
{{- end }}

{{/*
Choose the name of the tls secret to use for the webhook
*/}}
{{- define "vdb-op.webhookTLSSecret" -}}
{{- if .Values.webhook.tlsSecret }}
{{- .Values.webhook.tlsSecret }}
{{- else }}
{{- include "vdb-op.name" . }}-webhook-server-cert
{{- end }}
{{- end }}


