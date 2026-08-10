{{- define "postgresql.fullname" -}}
{{- printf "%s-postgresql" .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "postgresql.labels" -}}
app.kubernetes.io/name: postgresql
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: postgresql-0.1.0
{{- end }}

{{- define "postgresql.password" -}}
{{- if .Values.auth.password -}}
{{ .Values.auth.password }}
{{- else -}}
{{- $secret := lookup "v1" "Secret" .Release.Namespace (include "postgresql.fullname" .) -}}
{{- if $secret -}}
{{ index $secret.data "password" | b64dec }}
{{- else -}}
{{ randAlphaNum 32 }}
{{- end -}}
{{- end -}}
{{- end }}
