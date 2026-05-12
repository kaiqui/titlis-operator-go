{{- define "castai.name" -}}titlis-castai-monitor{{- end }}

{{- define "castai.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "castai.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" -}}
{{- end }}

{{- define "castai.labels" -}}
helm.sh/chart: {{ include "castai.chart" . }}
app.kubernetes.io/name: {{ include "castai.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "castai.serviceAccountName" -}}
{{- include "castai.fullname" . -}}
{{- end -}}
