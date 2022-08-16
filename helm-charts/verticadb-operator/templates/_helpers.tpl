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
