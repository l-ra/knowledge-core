{{- define "pocket-id.fullname" -}}
{{- printf "%s-pocket-id" .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "pocket-id.labels" -}}
app.kubernetes.io/name: pocket-id
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: pocket-id-0.1.0
{{- end }}

{{- define "pocket-id.encryptionKey" -}}
{{- if .Values.encryptionKey -}}
{{ .Values.encryptionKey }}
{{- else -}}
{{- $secret := lookup "v1" "Secret" .Release.Namespace (include "pocket-id.fullname" .) -}}
{{- if $secret -}}
{{ index $secret.data "encryptionKey" | b64dec }}
{{- else -}}
{{ randAlphaNum 48 }}
{{- end -}}
{{- end -}}
{{- end }}

{{- define "pocket-id.appUrl" -}}
{{- if .Values.appUrl -}}
{{ trimSuffix "/" .Values.appUrl }}
{{- else -}}
http://{{ include "pocket-id.fullname" . }}:{{ .Values.service.port }}
{{- end -}}
{{- end }}
