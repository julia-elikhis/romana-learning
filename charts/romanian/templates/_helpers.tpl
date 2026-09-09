{{- define "romanian.name" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "romanian.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "romanian.name" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- required "serviceAccount.name is required when create=false" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}
{{- define "romanian.claimName" -}}
{{- default (printf "%s-courses" (include "romanian.name" .) | trunc 63 | trimSuffix "-") .Values.storage.filesystem.persistence.existingClaim -}}
{{- end -}}
{{- define "romanian.selector" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "romanian.labels" -}}
{{ include "romanian.selector" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end -}}

{{- define "romanian.s3Endpoint" -}}
{{- if .Values.storage.s3.endpoint -}}
{{- .Values.storage.s3.endpoint -}}
{{- else if .Values.minio.enabled -}}
{{- $scheme := "http" -}}
{{- if .Values.minio.tls.enabled -}}{{- $scheme = "https" -}}{{- end -}}
{{- printf "%s://%s:%v" $scheme (include "minio.fullname" .Subcharts.minio) .Values.minio.service.port -}}
{{- else -}}
{{- fail "storage.s3.endpoint is required when bundled MinIO is disabled" -}}
{{- end -}}
{{- end -}}
