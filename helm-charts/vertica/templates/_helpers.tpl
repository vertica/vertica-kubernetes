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

{{/*
  Common labels for all objects
*/}}
{{- define "vertica.common-labels" -}}
app.kubernetes.io/name: vertica
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: helm
app.kubernetes.io/component: database
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
vertica.com/database: {{ .Values.db.name }}
{{- end }}

{{/*
  Labels specific for the server
*/}}
{{- define "vertica.server-labels" -}}
vertica.com/usage: server
vertica.com/subcluster: {{ include "vertica.defaultSubclusterName" . }}
{{- end }}

{{/*
  The name of the default subcluster
*/}}
{{- define "vertica.defaultSubclusterName" -}}
defaultsubcluster
{{- end }}

{{/*
Create a default fully qualified app name.

Some Kubernetes name fields are limited to 63 characters by the DNS naming spec.
We truncate to 60 so that we are under this limit while allowing a user to
append up to 3 more characters to distinguish some types.

If release name contains chart name it will be used as a full name.
*/}}
{{- define "vertica.fullname" -}}
{{- if .Values.fullnameOverride -}}
    {{- .Values.fullnameOverride | lower | trunc 60 | trimSuffix "-" -}}
{{- else -}}
    {{- $name := default .Chart.Name .Values.nameOverride -}}
    {{- if contains $name .Release.Name -}}
        {{- printf "%s-%s" .Release.Name (include "vertica.defaultSubclusterName" .) | lower | trunc 60 | trimSuffix "-" -}}
    {{- else -}}
        {{- printf "%s-%s-%s" .Release.Name $name (include "vertica.defaultSubclusterName" .) | lower | trunc 60 | trimSuffix "-" -}}
    {{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create the name of the headless service
*/}}
{{- define "vertica.headless-svc-name" -}}
{{ include "vertica.fullname" . }}-hl
{{- end -}}

{{/*
Create the name of the config map

This is similar logic to vertica.fullname expect the subcluster name is not
included.  We share a single cm for all subclusters.
*/}}
{{- define "vertica.configmap-name" -}}
{{- if .Values.fullnameOverride -}}
    {{- .Values.fullnameOverride | lower | trunc 60 | trimSuffix "-" -}}
{{- else -}}
    {{- $name := default .Chart.Name .Values.nameOverride -}}
    {{- if contains $name .Release.Name -}}
        {{- printf "%s" .Release.Name | lower | trunc 60 | trimSuffix "-" -}}
    {{- else -}}
        {{- printf "%s-%s" .Release.Name $name | lower | trunc 60 | trimSuffix "-" -}}
    {{- end -}}
{{- end -}}
{{- end -}}
