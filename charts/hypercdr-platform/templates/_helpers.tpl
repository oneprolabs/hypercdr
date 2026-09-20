{{- define "hypercdr.name" -}}
hypercdr
{{- end -}}

{{- define "hypercdr.registrationExecutorImage" -}}
{{- $tag := default .Values.platform.image.tag .Values.registrationExecutor.image.tag -}}
{{- include "hypercdr.imageRef" (list .Values.global.imageRegistry .Values.registrationExecutor.image.repository $tag) -}}
{{- end -}}

{{- define "hypercdr.imageRef" -}}
{{- $registry := index . 0 | trimSuffix "/" -}}
{{- if regexMatch "^(docker[.]io|[^/]+[.]aliyuncs[.]com)/[^/]+/[^/]+$" $registry -}}
{{- printf "%s:%s-%s" $registry (index . 1) (index . 2) -}}
{{- else -}}
{{- printf "%s/%s:%s" $registry (index . 1) (index . 2) -}}
{{- end -}}
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
{{- include "hypercdr.imageRef" (list .Values.global.imageRegistry .Values.platform.image.repository .Values.platform.image.tag) -}}
{{- end -}}

{{- define "hypercdr.databaseURL" -}}
{{- if eq .Values.postgresql.mode "external" -}}
{{- printf "postgres://%s:%s@%s:%v/%s?sslmode=disable" .Values.postgresql.external.username .Values.postgresql.external.password .Values.postgresql.external.host .Values.postgresql.external.port .Values.postgresql.external.database -}}
{{- else -}}
{{- printf "postgres://%s:%s@%s-postgres:5432/%s?sslmode=disable" .Values.postgresql.username .Values.postgresql.password (include "hypercdr.fullname" .) .Values.postgresql.database -}}
{{- end -}}
{{- end -}}
