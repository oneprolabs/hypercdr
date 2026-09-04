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
	"strings"
	"time"
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
				_ = os.WriteFile(filepath.Join(root, "incident/screenshot.png"), b, 0600)
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

func (r *Router) collectSupportBundle(root string, hours int) {
	since := fmt.Sprintf("%dh", hours)
	commands := map[string][]string{
		"platform/docker-ps.txt":      {"docker", "ps", "-a"},
		"platform/docker-info.txt":    {"docker", "info"},
		"platform/compose-config.txt": {"docker", "compose", "-f", "/deploy/docker-compose.yaml", "config"},
		"platform/host.txt":           {"sh", "-c", "uname -a; df -h; free -m; date -u"},
		"platform/container-logs.txt": {"sh", "-c", "for c in $(docker ps -a --format '{{.Names}}'); do echo \"===== $c =====\"; docker logs --since " + since + " \"$c\" 2>&1 || true; done"},
		"database/status.txt":         {"sh", "-c", "docker exec hypercdr-postgres sh -c 'pg_isready; psql -U hypercdr -d hypercdr -c \"select id,type,status,progress,error_code,error_message,created_at,completed_at from tasks order by created_at desc limit 100\"' 2>&1"},
		"network/connectivity.txt":    {"sh", "-c", "getent hosts registry-1.docker.io office.oneprocloud.com.cn 2>&1; (command -v ss >/dev/null && ss -tuna) || true"},
	}
	for name, args := range commands {
		_ = writeText(root, name, runRedactedCommand(args...))
	}
	if clusters, err := r.store.ListClusters(); err == nil {
		_ = writeBundleJSON(root, "clusters.json", clusters)
	}
	if tasks, err := r.store.ListTasks(""); err == nil {
		_ = writeBundleJSON(root, "tasks.json", tasks)
	}
	_ = writeText(root, "collection.txt", fmt.Sprintf("Collected by HyperCDR\nLog window: %dh\nSecrets and credentials were redacted or omitted.\nCluster-side Kubernetes/OpenShift resource details are included when the control plane has a current inventory snapshot.\n", hours))
}

func runRedactedCommand(args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	text := redactSensitive(string(out))
	if err != nil {
		text += "\n[command error] " + err.Error()
	}
	return text
}
func redactSensitive(s string) string {
	for _, key := range []string{"PASSWORD", "TOKEN", "SECRET", "ACCESS_KEY", "SECRET_KEY", "KUBECONFIG"} {
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			if strings.Contains(strings.ToUpper(l), key) {
				lines[i] = "[REDACTED]"
			}
		}
		s = strings.Join(lines, "\n")
	}
	return s
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
