{{/*
Expand the name of the chart.
*/}}
{{- define "knowledge-core.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "knowledge-core.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "knowledge-core.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "knowledge-core.labels" -}}
helm.sh/chart: {{ include "knowledge-core.chart" . }}
{{ include "knowledge-core.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "knowledge-core.selectorLabels" -}}
app.kubernetes.io/name: {{ include "knowledge-core.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "knowledge-core.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "knowledge-core.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "knowledge-core.databaseURL" -}}
{{- $pg := index .Subcharts "postgresql" -}}
{{- if and .Values.postgresql.enabled $pg -}}
{{- $user := .Values.postgresql.auth.username -}}
{{- $db := .Values.postgresql.auth.database -}}
{{- $host := printf "%s-postgresql" .Release.Name -}}
{{- $secret := printf "%s-postgresql" .Release.Name -}}
postgres://{{ $user }}:$(POSTGRES_PASSWORD)@{{ $host }}:5432/{{ $db }}?sslmode=disable
{{- else -}}
{{ required "KC_DATABASE_URL must be set when postgresql.enabled=false" .Values.externalDatabase.url }}
{{- end -}}
{{- end }}

{{- define "knowledge-core.authMode" -}}
{{- if .Values.pocketId.enabled -}}
oidc
{{- else -}}
{{ .Values.auth.mode }}
{{- end -}}
{{- end }}

{{- define "knowledge-core.oidcIssuer" -}}
{{- if .Values.pocketId.enabled -}}
{{- $base := .Values.pocketId.appUrl -}}
{{- if not $base -}}
http://{{ printf "%s-pocket-id" .Release.Name }}:{{ .Values.pocketId.service.port }}
{{- else -}}
{{ trimSuffix "/" $base }}
{{- end -}}
{{- else -}}
{{ .Values.auth.oidcIssuer | default "" }}
{{- end -}}
{{- end }}
