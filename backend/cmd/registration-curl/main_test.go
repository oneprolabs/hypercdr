package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRunSupportsInstallerPostAndOutputFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected request: %s %#v", req.Method, req.Header)
		}
		body, _ := io.ReadAll(req.Body)
		if string(body) != `{"token":"value"}` {
			t.Fatalf("body = %s", body)
		}
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "response.json")
	if err := run([]string{"-sS", "-o", output, "-H", "Content-Type: application/json", "--data", `{"token":"value"}`, server.URL}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(output)
	if err != nil || string(raw) != `{"valid":true}` {
		t.Fatalf("response = %s err=%v", raw, err)
	}
}

func TestRunRejectsUnsupportedOptions(t *testing.T) {
	if err := run([]string{"--upload-file", "/etc/passwd", "https://example.invalid"}); err == nil {
		t.Fatal("unsupported curl capability was accepted")
	}
}
