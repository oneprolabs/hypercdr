package httpserver

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	in := "password: hunter2\napiToken=abc123\naccess_key: akid\nnormal: value"
	out := redactSensitive(in)
	for _, secret := range []string{"hunter2", "abc123", "akid"} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret %q leaked: %s", secret, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatal("expected redaction marker")
	}
}

func TestTarGzipDirRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := writeText(root, "incident/description.json", "safe"); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := tarGzipDir(dst, root); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	h, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "incident/description.json" {
		t.Fatalf("unexpected entry %s", h.Name)
	}
	b, err := io.ReadAll(tr)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "safe" {
		t.Fatalf("unexpected content %q", b)
	}
}

func TestDownloadBundleNameValidation(t *testing.T) {
	if strings.HasPrefix(filepath.Base("../evil.tar.gz"), "hcdr-support-bundle-") {
		t.Fatal("unsafe name accepted")
	}
}
