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
{{- "" }}
{{- else }}
{{- include "vdb-op.name" . }}-controller-manager-service-cert
{{- end }}
{{- end }}
