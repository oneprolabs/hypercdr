package httpserver

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

type supportBundleRequest struct {
	SinceHours       int    `json:"sinceHours"`
	Description      string `json:"description"`
	Reproducible     string `json:"reproducible"`
	ScreenshotName   string `json:"screenshotName"`
	ScreenshotBase64 string `json:"screenshotBase64"`
}

func (r *Router) createSupportBundle(w http.ResponseWriter, req *http.Request) {
	var input supportBundleRequest
	if req.Body != nil {
		_ = json.NewDecoder(io.LimitReader(req.Body, 1<<20)).Decode(&input)
	}
	if input.SinceHours <= 0 || input.SinceHours > 168 {
		input.SinceHours = 24
	}
	if strings.TrimSpace(input.Description) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "problem_description_required", "message": "Describe the observed problem before generating a support bundle."})
		return
	}
	root, err := os.MkdirTemp("", "hcdr-support-bundle-")
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "bundle_create_failed"})
		return
	}
	defer os.RemoveAll(root)
	manifest := map[string]any{"generatedAt": time.Now().UTC().Format(time.RFC3339), "sinceHours": input.SinceHours, "redaction": "credentials, tokens, kubeconfig and Secret data are excluded"}
	_ = writeBundleJSON(root, "manifest.json", manifest)
	r.collectSupportBundle(root, input.SinceHours)
	_ = writeBundleJSON(root, "incident/description.json", map[string]any{"description": redactSensitive(input.Description), "reproducible": redactSensitive(input.Reproducible), "screenshot": "included only when explicitly uploaded"})
	if strings.HasPrefix(input.ScreenshotBase64, "data:image/") && len(input.ScreenshotBase64) < 14*1024*1024 {
		if comma := strings.IndexByte(input.ScreenshotBase64, ','); comma > 0 {
			if b, e := base64.StdEncoding.DecodeString(input.ScreenshotBase64[comma+1:]); e == nil {
				name := "incident/screenshot.png"
				if strings.HasPrefix(input.ScreenshotBase64, "data:image/jpeg") {
					name = "incident/screenshot.jpg"
				}
				if len(b) <= 10*1024*1024 && validImageBytes(b, name) {
					_ = writeTextBytes(root, name, b)
				}
			}
		}
	}
	name := "hcdr-support-bundle-" + time.Now().UTC().Format("20060102-150405") + ".tar.gz"
	path := filepath.Join(os.TempDir(), name)
	if err := tarGzipDir(path, root); err != nil {
		writeJSON(w, 500, map[string]any{"error": "bundle_archive_failed"})
		return
	}
	stat, _ := os.Stat(path)
	writeJSON(w, http.StatusCreated, map[string]any{"name": name, "downloadUrl": "/api/v1/support-bundles/" + name + "/download", "size": stat.Size(), "expiresAt": time.Now().Add(48 * time.Hour).UTC()})
}

func validImageBytes(b []byte, name string) bool {
	if strings.HasSuffix(name, ".png") {
		return len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n"
	}
	return len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff
}
func writeTextBytes(root, name string, content []byte) error {
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	return os.WriteFile(p, content, 0600)
}

func (r *Router) collectSupportBundle(root string, hours int) {
	since := fmt.Sprintf("%dh", hours)
	commands := map[string][]string{
		"platform/docker-ps.txt":         {"docker", "ps", "-a"},
		"platform/compose-ps.txt":        {"docker", "compose", "-f", "/deploy/docker-compose.yaml", "ps"},
		"platform/docker-info.txt":       {"docker", "info"},
		"platform/container-inspect.txt": {"sh", "-c", "for c in $(docker ps -a --format '{{.Names}}'); do echo \"===== $c =====\"; docker inspect --format '{{json .State}} {{json .Config.Labels}}' \"$c\" 2>&1 || true; done"},
		"platform/compose-config.txt":    {"docker", "compose", "-f", "/deploy/docker-compose.yaml", "config"},
		"platform/host.txt":              {"sh", "-c", "uname -a; df -h; free -m; date -u"},
		"storage/docker-volumes.txt":     {"docker", "volume", "ls"},
		"object-storage/config.txt":      {"sh", "-c", "env | sort | grep -Ei 'S3|MINIO|OBJECT|BUCKET|STORAGE' || true"},
		"platform/container-logs.txt":    {"sh", "-c", "for c in $(docker ps -a --format '{{.Names}}'); do echo \"===== $c =====\"; docker logs --since " + since + " \"$c\" 2>&1 || true; done"},
		"database/status.txt":            {"sh", "-c", "docker exec hypercdr-postgres sh -c 'pg_isready; psql -U hypercdr -d hypercdr -c \"select id,type,status,progress,error_code,error_message,created_at,completed_at from tasks order by created_at desc limit 100\"' 2>&1"},
		"network/connectivity.txt":       {"sh", "-c", "getent hosts registry-1.docker.io office.oneprocloud.com.cn 2>&1; (command -v ss >/dev/null && ss -tuna) || true"},
	}
	var wg sync.WaitGroup
	for name, args := range commands {
		name, args := name, args
		wg.Add(1)
		go func() { defer wg.Done(); _ = writeText(root, name, runRedactedCommand(args...)) }()
	}
	wg.Wait()
	// Collect read-only cluster-side diagnostics when an operator has supplied a
	// kubeconfig. Never copy the kubeconfig itself into the bundle.
	kubeconfig := os.Getenv("HCDR_SUPPORT_KUBECONFIG")
	if kubeconfig == "" {
		if _, err := os.Stat("/data/openshift/kubeconfig"); err == nil {
			kubeconfig = "/data/openshift/kubeconfig"
		}
	}
	if kubeconfig != "" {
		var clusterWG sync.WaitGroup
		for name, args := range map[string][]string{
			"openshift/pods.txt":             {"get", "pods", "-A", "-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,PHASE:.status.phase,READY:.status.containerStatuses[*].ready,RESTARTS:.status.containerStatuses[*].restartCount,NODE:.spec.nodeName"},
			"openshift/events.txt":           {"get", "events", "-A", "--field-selector", "type=Warning", "--sort-by=.lastTimestamp", "-o", "custom-columns=NAMESPACE:.metadata.namespace,REASON:.reason,MESSAGE:.message,LAST:.lastTimestamp"},
			"openshift/oadp-dpa.txt":         {"get", "dpa", "-n", "openshift-adp", "-o", "custom-columns=NAME:.metadata.name,PHASE:.status.phase,CONDITIONS:.status.conditions[*].message"},
			"openshift/oadp-operators.txt":   {"get", "csv,subscription", "-n", "openshift-adp"},
			"openshift/oadp-workloads.txt":   {"get", "deployment,daemonset,pvc", "-n", "openshift-adp"},
			"openshift/velero-resources.txt": {"get", "backupstoragelocation,volumesnapshotlocation,backup,restore", "-A", "-o", "custom-columns=KIND:.kind,NAMESPACE:.metadata.namespace,NAME:.metadata.name,PHASE:.status.phase,ERROR:.status.errors"},
			"storage/storageclasses.txt":     {"get", "storageclass,pvc", "-A"},
		} {
			name, args := name, args
			clusterWG.Add(1)
			go func() {
				defer clusterWG.Done()
				_ = writeText(root, name, runRedactedCommandWithKubeconfig(kubeconfig, args...))
			}()
		}
		clusterWG.Wait()
	} else {
		_ = writeText(root, "openshift/collection-status.txt", "kubeconfig not configured; cluster-side collection skipped (kubeconfig contents are never collected).\n")
	}
	if clusters, err := r.store.ListClusters(); err == nil {
		_ = writeBundleJSON(root, "clusters.json", clusters)
	}
	if tasks, err := r.store.ListTasks(""); err == nil {
		_ = writeBundleJSON(root, "tasks.json", tasks)
	}
	if logs, err := r.store.ListDiagnosticLogs(store.DiagnosticLogFilter{Limit: 5000, From: time.Now().Add(-time.Duration(hours) * time.Hour)}); err == nil {
		_ = writeBundleJSON(root, "diagnostic-logs.json", logs)
	}
	_ = writeText(root, "collection.txt", fmt.Sprintf("Collected by HyperCDR\nLog window: %dh\nSecrets and credentials were redacted or omitted.\nCluster-side Kubernetes/OpenShift resource details are included when the control plane has a current inventory snapshot.\n", hours))
}

func runRedactedCommand(args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	text := redactSensitive(string(out))
	if err != nil {
		text += "\n[command error] " + err.Error()
	}
	return text
}
func runRedactedCommandWithKubeconfig(kubeconfig string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "oc", args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig)
	out, err := cmd.CombinedOutput()
	text := redactSensitive(string(out))
	if err != nil {
		text += "\n[command error] " + err.Error()
	}
	return text
}
func redactSensitive(s string) string {
	lines := strings.Split(s, "\n")
	keyRE := regexp.MustCompile(`(?i)(password|passwd|token|secret(key)?|access[_-]?key|kubeconfig|authorization)\s*[:=]\s*[^\s,;]+`)
	for i, l := range lines {
		l = keyRE.ReplaceAllString(l, "$1=[REDACTED]")
		if strings.Contains(strings.ToLower(l), "data:") && strings.Contains(strings.ToLower(l), "secret") {
			lines[i] = "[REDACTED SECRET DATA]"
		} else {
			lines[i] = l
		}
	}
	return strings.Join(lines, "\n")
}
func writeText(root, name, content string) error {
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0600)
}
func writeBundleJSON(root, name string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return writeText(root, name, string(b))
}
func tarGzipDir(dst, root string) error {
	f, e := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		h := &tar.Header{Name: strings.TrimPrefix(strings.TrimPrefix(path, root), string(filepath.Separator)), Mode: 0600, Size: int64(len(b)), ModTime: info.ModTime()}
		if e = tw.WriteHeader(h); e != nil {
			return e
		}
		_, e = tw.Write(b)
		return e
	})
}

func (r *Router) downloadSupportBundle(w http.ResponseWriter, req *http.Request) {
	name := filepath.Base(req.PathValue("name"))
	if name == "." || !strings.HasPrefix(name, "hcdr-support-bundle-") || !strings.HasSuffix(name, ".tar.gz") {
		http.NotFound(w, req)
		return
	}
	p := filepath.Join(os.TempDir(), name)
	if _, e := os.Stat(p); e != nil {
		http.NotFound(w, req)
		return
	}
	defer os.Remove(p)
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename="+name)
	http.ServeFile(w, req, p)
}
