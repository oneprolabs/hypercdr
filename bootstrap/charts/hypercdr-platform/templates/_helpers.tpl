{{- define "hypercdr.name" -}}
hypercdr
{{- end -}}

{{- define "hypercdr.fullname" -}}
{{- printf "%s-platform" (include "hypercdr.name" .) -}}
{{- end -}}

{{- define "hypercdr.labels" -}}
app.kubernetes.io/name: {{ include "hypercdr.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "hypercdr.selectorLabels" -}}
app.kubernetes.io/name: {{ include "hypercdr.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "hypercdr.platformImage" -}}
{{- printf "%s/%s:%s" (.Values.global.imageRegistry | trimSuffix "/") .Values.platform.image.repository .Values.platform.image.tag -}}
{{- end -}}

{{- define "hypercdr.databaseURL" -}}
{{- if eq .Values.postgresql.mode "external" -}}
{{- printf "postgres://%s:%s@%s:%v/%s?sslmode=disable" .Values.postgresql.external.username .Values.postgresql.external.password .Values.postgresql.external.host .Values.postgresql.external.port .Values.postgresql.external.database -}}
{{- else -}}
{{- printf "postgres://%s:%s@%s-postgres:5432/%s?sslmode=disable" .Values.postgresql.username .Values.postgresql.password (include "hypercdr.fullname" .) .Values.postgresql.database -}}
{{- end -}}
{{- end -}}
