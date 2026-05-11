{{- define "cadoo.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cadoo.fullname" -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cadoo.labels" -}}
app.kubernetes.io/name: {{ include "cadoo.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- /* Database URL: derived from bundled postgres or pulled from values. */ -}}
{{- define "cadoo.databaseURL" -}}
{{- if .Values.database.url -}}
{{ .Values.database.url }}
{{- else if .Values.postgres.enabled -}}
{{- $host := printf "%s-postgres" (include "cadoo.fullname" .) -}}
postgres://{{ .Values.postgres.user }}:{{ .Values.postgres.password }}@{{ $host }}:5432/{{ .Values.postgres.database }}?sslmode=disable
{{- end -}}
{{- end -}}

{{- /* env list shared by api/webhook/worker; insert under `env:`. */ -}}
{{- define "cadoo.envList" -}}
- name: DATABASE_URL
  value: {{ include "cadoo.databaseURL" . | quote }}
- name: LLM_GATEWAY_URL
  value: {{ .Values.llm.gatewayURL | quote }}
{{- range $k, $v := .Values.env }}
{{- if $v }}
- name: {{ $k }}
  value: {{ $v | quote }}
{{- end }}
{{- end }}
{{- end -}}

{{- /* envFrom list shared by api/webhook/worker; insert under `envFrom:`. */ -}}
{{- define "cadoo.envFromList" -}}
- secretRef:
    name: {{ .Values.secretsRef.name }}
{{- end -}}
