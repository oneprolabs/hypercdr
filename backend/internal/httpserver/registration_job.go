package httpserver

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var kubernetesNameUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

func (r *Router) createRegistrationExecutorJob(ctx context.Context, taskID string) error {
	return r.createIsolatedRegistrationJob(ctx, "register-"+taskID, taskID, true, []any{map[string]string{"name": "HCDR_REGISTRATION_TASK_ID", "value": taskID}})
}

func (r *Router) createRegistrationInspectionJob(ctx context.Context, sessionID string, contextName string) error {
	return r.createIsolatedRegistrationJob(ctx, "inspect-"+sessionID, sessionID, false, []any{
		map[string]string{"name": "HCDR_REGISTRATION_INSPECT_SESSION_ID", "value": sessionID},
		map[string]string{"name": "HCDR_REGISTRATION_INSPECT_CONTEXT", "value": contextName},
	})
}

func (r *Router) createIsolatedRegistrationJob(ctx context.Context, jobIdentity string, labelIdentity string, needsDatabase bool, actionEnv []any) error {
	if r.cfg.DeployMode != "helm" && r.cfg.DeployMode != "kubernetes" {
		return nil
	}
	if r.cfg.RegistrationExecutorImage == "" || (needsDatabase && r.cfg.RegistrationConfigSecret == "") {
		return errors.New("Helm registration executor image or database Secret is not configured")
	}
	apiEndpoint := r.cfg.RegistrationKubernetesAPI
	if apiEndpoint == "" {
		host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT_HTTPS")
		if host == "" || port == "" {
			return errors.New("in-cluster Kubernetes API endpoint is unavailable")
		}
		apiEndpoint = fmt.Sprintf("https://%s:%s", host, port)
	}
	token, err := os.ReadFile(r.cfg.RegistrationServiceTokenPath)
	if err != nil {
		return fmt.Errorf("read Kubernetes service-account token: %w", err)
	}
	ca, err := os.ReadFile(r.cfg.RegistrationServiceCAPath)
	if err != nil {
		return fmt.Errorf("read Kubernetes service-account CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return errors.New("Kubernetes service-account CA is invalid")
	}
	suffix := kubernetesNameUnsafe.ReplaceAllString(strings.ToLower(jobIdentity), "-")
	suffix = strings.Trim(suffix, "-")
	if len(suffix) > 40 {
		suffix = suffix[:40]
	}
	platformLabels := map[string]string{"app.kubernetes.io/component": "platform"}
	if r.cfg.RegistrationPlatformInstance != "" {
		platformLabels["app.kubernetes.io/instance"] = r.cfg.RegistrationPlatformInstance
	}
	containerEnv := append([]any{}, actionEnv...)
	containerEnv = append(containerEnv, map[string]string{"name": "HCDR_REGISTRATION_SESSION_DIR", "value": "/var/lib/hypercdr/registration-sessions"})
	if needsDatabase {
		containerEnv = append(containerEnv, map[string]any{"name": "HCDR_DATABASE_URL", "valueFrom": map[string]any{"secretKeyRef": map[string]string{"name": r.cfg.RegistrationConfigSecret, "key": "HCDR_DATABASE_URL"}}})
	}
	job := map[string]any{"apiVersion": "batch/v1", "kind": "Job", "metadata": map[string]any{"name": "hypercdr-" + suffix, "namespace": r.cfg.RegistrationExecutorNamespace, "labels": map[string]string{"app.kubernetes.io/component": "cluster-registration-executor", "hypercdr.io/operation-id": labelIdentity}}, "spec": map[string]any{
		"backoffLimit": 0, "activeDeadlineSeconds": 1500, "ttlSecondsAfterFinished": 300,
		"template": map[string]any{"metadata": map[string]any{"labels": map[string]string{"app.kubernetes.io/component": "cluster-registration-executor"}}, "spec": map[string]any{
			"restartPolicy": "Never", "automountServiceAccountToken": false, "securityContext": map[string]any{"seccompProfile": map[string]string{"type": "RuntimeDefault"}},
			"affinity":   map[string]any{"podAffinity": map[string]any{"requiredDuringSchedulingIgnoredDuringExecution": []any{map[string]any{"labelSelector": map[string]any{"matchLabels": platformLabels}, "topologyKey": "kubernetes.io/hostname"}}}},
			"containers": []any{map[string]any{"name": "executor", "image": r.cfg.RegistrationExecutorImage, "imagePullPolicy": "IfNotPresent", "env": containerEnv, "volumeMounts": []any{map[string]any{"name": "sessions", "mountPath": "/var/lib/hypercdr/registration-sessions"}, map[string]any{"name": "tmp", "mountPath": "/tmp"}}, "securityContext": map[string]any{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]any{"drop": []string{"ALL"}}}, "resources": map[string]any{"requests": map[string]string{"cpu": "100m", "memory": "128Mi"}, "limits": map[string]string{"cpu": "1", "memory": "512Mi"}}}},
			"volumes":    []any{map[string]any{"name": "sessions", "persistentVolumeClaim": map[string]string{"claimName": r.cfg.RegistrationSessionPVC}}, map[string]any{"name": "tmp", "emptyDir": map[string]any{"sizeLimit": "64Mi"}}},
		}},
	}}
	raw, _ := json.Marshal(job)
	endpoint := fmt.Sprintf("%s/apis/batch/v1/namespaces/%s/jobs", apiEndpoint, r.cfg.RegistrationExecutorNamespace)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}, Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("create registration Job: %w", err)
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("create registration Job returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(response)))
	}
	return nil
}
