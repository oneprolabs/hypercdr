package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	db := os.Getenv("HCDR_DATABASE_URL")
	deployDir := env("HCDR_DEPLOY_DIR", "/deploy")
	hostDeployDir := env("HCDR_HOST_DEPLOY_DIR", "/var/lib/hypercdr")
	healthURL := env("HCDR_PLATFORM_HEALTH_URL", "http://hypercdr-platform-api:18080/healthz")
	if db == "" {
		logger.Error("HCDR_DATABASE_URL is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	repo, err := store.NewPostgresStoreWithoutMigrations(ctx, db)
	cancel()
	if err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer repo.Close()
	executorID := env("HOSTNAME", "platform-upgrader")
	for {
		jobs, err := repo.ListPlatformUpgradeJobs()
		if err != nil {
			logger.Error("list jobs", "error", err)
		} else {
			for _, job := range jobs {
				if job.Status == "queued" {
					run(repo, job, deployDir, hostDeployDir, healthURL, executorID, logger)
					break
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
}

func run(repo store.Store, job store.PlatformUpgradeJob, deployDir, hostDeployDir, healthURL, executorID string, logger *slog.Logger) {
	update := func(status, step string, progress int, errCode, errMessage string, done bool) {
		_, _, _ = repo.UpdatePlatformUpgradeJob(store.PlatformUpgradeJobUpdate{ID: job.ID, Status: status, Step: step, Progress: progress, ErrorCode: errCode, ErrorMessage: errMessage, ExecutorID: executorID, MarkStarted: true, MarkDone: done})
	}
	fail := func(step string, err error) {
		logger.Error("platform upgrade failed", "job", job.ID, "step", step, "error", err)
		update("failed", step, 100, "UPGRADE_FAILED", err.Error(), true)
	}
	envPath := filepath.Join(deployDir, ".env")
	values, err := readEnv(envPath)
	if err != nil {
		fail("prechecking", err)
		return
	}
	oldAPI, oldFrontend, oldUpgrader, oldVersion := values["PLATFORM_API_IMAGE"], values["PLATFORM_FRONTEND_IMAGE"], values["PLATFORM_UPGRADER_IMAGE"], values["RELEASE_VERSION"]
	if oldAPI == "" || oldFrontend == "" || oldUpgrader == "" {
		fail("prechecking", fmt.Errorf("deployment images are missing from %s", envPath))
		return
	}
	targetUpgrader, err := platformComponentImage(job.APIImage, "platform-upgrader", job.TargetVersion)
	if err != nil {
		fail("prechecking", err)
		return
	}
	_, _, _ = repo.UpdatePlatformUpgradeJob(store.PlatformUpgradeJobUpdate{ID: job.ID, PreviousAPIImage: oldAPI, PreviousFrontendImage: oldFrontend, ExecutorID: executorID, Progress: 1, MarkStarted: true})
	update("backing_up", "backing_up", 15, "", "", false)
	backupDir := filepath.Join(deployDir, "backups")
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		fail("backing_up", err)
		return
	}
	backupPath := filepath.Join(backupDir, "platform-"+time.Now().Format("20060102-150405")+".sql")
	backup, err := os.OpenFile(backupPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fail("backing_up", err)
		return
	}
	cmd := compose(deployDir, "exec", "-T", "hypercdr-postgres", "pg_dump", "-U", "hypercdr", "-d", "hypercdr")
	cmd.Stdout = backup
	err = cmd.Run()
	backup.Close()
	if err != nil {
		fail("backing_up", err)
		return
	}
	_, _, _ = repo.UpdatePlatformUpgradeJob(store.PlatformUpgradeJobUpdate{ID: job.ID, BackupPath: backupPath, Status: "pulling", Step: "pulling", Progress: 30, ExecutorID: executorID, MarkStarted: true})
	update("pulling", "pulling_api", 34, "", "", false)
	if err = runCmd(exec.Command("docker", "pull", job.APIImage)); err != nil {
		fail("pulling", err)
		return
	}
	update("pulling", "pulling_frontend", 40, "", "", false)
	if err = runCmd(exec.Command("docker", "pull", job.FrontendImage)); err != nil {
		fail("pulling", err)
		return
	}
	update("pulling", "pulling_upgrader", 46, "", "", false)
	if err = runCmd(exec.Command("docker", "pull", targetUpgrader)); err != nil {
		fail("pulling", err)
		return
	}
	values["PLATFORM_API_IMAGE"], values["PLATFORM_FRONTEND_IMAGE"], values["PLATFORM_UPGRADER_IMAGE"] = job.APIImage, job.FrontendImage, targetUpgrader
	values["RELEASE_VERSION"] = job.TargetVersion
	if err = writeEnv(envPath, values); err != nil {
		fail("switching_api", err)
		return
	}
	rollback := func(cause error) {
		update("rolling_back", "rolling_back", 90, "", "", false)
		values["PLATFORM_API_IMAGE"], values["PLATFORM_FRONTEND_IMAGE"], values["PLATFORM_UPGRADER_IMAGE"] = oldAPI, oldFrontend, oldUpgrader
		values["RELEASE_VERSION"] = oldVersion
		_ = writeEnv(envPath, values)
		_ = runCmd(compose(deployDir, "up", "-d", "hypercdr-platform-api", "hypercdr-platform-frontend"))
		fail("rolled_back", cause)
	}
	update("switching_api", "switching_api", 55, "", "", false)
	if err = runCmd(compose(deployDir, "up", "-d", "hypercdr-platform-api")); err != nil {
		rollback(err)
		return
	}
	update("verifying_api", "verifying_api", 70, "", "", false)
	if err = waitHealth(healthURL, 90*time.Second); err != nil {
		rollback(err)
		return
	}
	update("switching_frontend", "switching_frontend", 82, "", "", false)
	if err = runCmd(compose(deployDir, "up", "-d", "hypercdr-platform-frontend")); err != nil {
		rollback(err)
		return
	}
	update("verifying", "verifying", 92, "", "", false)
	if err = waitHealth(healthURL, 60*time.Second); err != nil {
		rollback(err)
		return
	}
	if _, _, err = repo.ActivatePlatformRelease(job.ReleaseID, "platform-upgrader"); err != nil {
		rollback(err)
		return
	}
	update("succeeded", "completed", 100, "", "", true)
	logger.Info("platform upgrade completed", "job", job.ID, "version", job.TargetVersion)
	if err = scheduleUpgraderReplacement(hostDeployDir, executorID); err != nil {
		logger.Error("schedule upgrader replacement", "job", job.ID, "error", err)
	}
}

func platformComponentImage(apiImage, component, version string) (string, error) {
	lastSlash := strings.LastIndex(apiImage, "/")
	if lastSlash < 0 || strings.TrimSpace(version) == "" {
		return "", fmt.Errorf("cannot derive %s image from %q", component, apiImage)
	}
	return apiImage[:lastSlash+1] + component + ":" + version, nil
}

func scheduleUpgraderReplacement(hostDeployDir, executorID string) error {
	if !filepath.IsAbs(hostDeployDir) || hostDeployDir == "/" {
		return fmt.Errorf("unsafe host deploy directory: %q", hostDeployDir)
	}
	name := "hypercdr-upgrader-switch-" + strings.ReplaceAll(executorID, "_", "-")
	if len(name) > 60 {
		name = name[:60]
	}
	script := "sleep 2; docker compose --project-name hypercdr --project-directory /deploy --env-file /deploy/.env -f /deploy/docker-compose.yaml up -d hypercdr-platform-upgrader"
	return runCmd(exec.Command("docker", "run", "--rm", "-d", "--name", name,
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", hostDeployDir+":/deploy",
		"docker:27-cli", "sh", "-c", script))
}

func compose(dir string, args ...string) *exec.Cmd {
	projectName := env("HCDR_COMPOSE_PROJECT_NAME", "hypercdr")
	all := append([]string{"compose", "--project-name", projectName, "--project-directory", dir, "--env-file", filepath.Join(dir, ".env"), "-f", filepath.Join(dir, "docker-compose.yaml")}, args...)
	return exec.Command("docker", all...)
}
func runCmd(cmd *exec.Cmd) error { cmd.Stdout = os.Stdout; cmd.Stderr = os.Stderr; return cmd.Run() }
func waitHealth(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 4 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return nil
			}
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("health check timeout: %s", url)
}
func readEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	return values, scanner.Err()
}
func writeEnv(path string, values map[string]string) error {
	tmp := path + ".upgrade.tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value, ok := values[key]; ok {
			fmt.Fprintf(f, "%s=%s\n", key, value)
		}
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
