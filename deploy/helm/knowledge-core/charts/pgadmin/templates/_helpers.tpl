{{- define "pgadmin.fullname" -}}
{{- printf "%s-pgadmin" .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "pgadmin.labels" -}}
app.kubernetes.io/name: pgadmin
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: pgadmin-0.1.0
{{- end }}

{{- define "pgadmin.password" -}}
{{- if .Values.auth.password -}}
{{ .Values.auth.password }}
{{- else -}}
{{- $secret := lookup "v1" "Secret" .Release.Namespace (include "pgadmin.fullname" .) -}}
{{- if $secret -}}
{{ index $secret.data "password" | b64dec }}
{{- else -}}
{{ randAlphaNum 24 }}
{{- end -}}
{{- end -}}
{{- end }}

{{- define "pgadmin.postgresHost" -}}
{{- default (printf "%s-postgresql" .Release.Name) .Values.postgres.host }}
{{- end }}

{{- define "pgadmin.postgresPassword" -}}
{{- if .Values.postgres.password -}}
{{ .Values.postgres.password }}
{{- else -}}
{{- $secret := lookup "v1" "Secret" .Release.Namespace (printf "%s-postgresql" .Release.Name) -}}
{{- if $secret -}}
{{ index $secret.data "password" | b64dec }}
{{- else -}}
{{ randAlphaNum 32 }}
{{- end -}}
{{- end -}}
{{- end }}
