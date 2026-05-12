{{- define "titlis.name" -}}titlis-operator{{- end }}

{{- define "titlis.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "titlis.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" -}}
{{- end }}

{{- define "titlis.labels" -}}
helm.sh/chart: {{ include "titlis.chart" . }}
app.kubernetes.io/name: {{ include "titlis.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "titlis.serviceAccountName" -}}
{{- include "titlis.fullname" . -}}
{{- end -}}
