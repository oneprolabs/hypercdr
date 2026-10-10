package httpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEmailContractsMountedAndActualResponses(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	handler := NewRouter(config.Config{SecretKey: "isolated-contract-key"}, slog.Default(), repo)
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		if !strings.Contains(route.Pattern, "/email-settings") {
			continue
		}
		count++
		op := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}}
		if !applyEmailPayloadContract(route.Pattern, op) {
			t.Fatalf("missing %s", route.Pattern)
		}
	}
	if count != 9 {
		t.Fatalf("coverage %d", count)
	}
	session, err := repo.CreatePlatformSession(actor.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := repo.CreateUser(actor.TenantID, "email-tenant-admin@example.com", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.SetUserPassword(ordinary.ID, "test-password", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateUser(store.UserUpdateInput{ID: ordinary.ID, TenantID: ordinary.TenantID, Email: ordinary.Email, Role: "admin", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	denied, err := repo.CreatePlatformSession(ordinary.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	run := func(pattern, path, body, token string, status int) map[string]any {
		t.Helper()
		method, _, _ := strings.Cut(pattern, " ")
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s: %d %s", pattern, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "smtp-contract-secret") || strings.Contains(w.Body.String(), "passwordCiphertext") {
			t.Fatal("credential leaked")
		}
		var value map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if status < 400 {
			c, _, _ := emailPayloadContract(pattern)
			validateWireObject(t, value, wireSchema(c.Response))
		}
		return value
	}
	for _, route := range r.routeContracts {
		if !strings.Contains(route.Pattern, "/email-settings") {
			continue
		}
		path := strings.ReplaceAll(strings.TrimPrefix(route.Pattern, strings.Split(route.Pattern, " ")[0]+" "), "{id}", "11111111-1111-1111-1111-111111111111")
		run(route.Pattern, path, `{}`, denied.Token, 403)
	}
	run("GET /api/v1/email-settings", "/api/v1/email-settings", "", "", 401)
	run("GET /api/v1/email-settings", "/api/v1/email-settings", "", session.Token, 200)
	run("GET /api/v1/email-settings/configurations", "/api/v1/email-settings/configurations", "", session.Token, 200)
	input := `{"name":"Contract SMTP","host":"smtp.invalid","port":587,"security":"starttls","senderEmail":"noreply@example.com","password":"smtp-contract-secret"}`
	created := run("POST /api/v1/email-settings/configurations", "/api/v1/email-settings/configurations", input, session.Token, 201)
	id := created["id"].(string)
	run("POST /api/v1/email-settings/configurations", "/api/v1/email-settings/configurations", input, session.Token, 409)
	run("POST /api/v1/email-settings/configurations/{id}/default", "/api/v1/email-settings/configurations/"+id+"/default", `{}`, session.Token, 200)
	run("DELETE /api/v1/email-settings/configurations/{id}", "/api/v1/email-settings/configurations/"+id, "", session.Token, 409)
	update := strings.Replace(input, `"password":"smtp-contract-secret"`, `"password":""`, 1)
	run("PUT /api/v1/email-settings/configurations/{id}", "/api/v1/email-settings/configurations/"+id, update, session.Token, 200)
	stored, found, err := repo.GetEmailSettingsByID(id)
	if err != nil || !found {
		t.Fatal(err)
	}
	decryptor := &Router{cfg: config.Config{SecretKey: "isolated-contract-key"}}
	password, err := decryptor.decryptSetting(stored.PasswordCiphertext)
	if err != nil || password != "smtp-contract-secret" {
		t.Fatal("blank update did not retain credential")
	}
	run("PUT /api/v1/email-settings", "/api/v1/email-settings", update, session.Token, 200)
	secondInput := strings.Replace(input, "Contract SMTP", "Second SMTP", 1)
	second := run("POST /api/v1/email-settings/configurations", "/api/v1/email-settings/configurations", secondInput, session.Token, 201)
	run("DELETE /api/v1/email-settings/configurations/{id}", "/api/v1/email-settings/configurations/"+second["id"].(string), "", session.Token, 200)
	// Invalid recipients are rejected before any network/email operation.
	for _, path := range []string{"/api/v1/email-settings/test", "/api/v1/email-settings/configurations/" + id + "/test"} {
		pattern := "POST /api/v1/email-settings/test"
		if strings.Contains(path, "configurations") {
			pattern = "POST /api/v1/email-settings/configurations/{id}/test"
		}
		run(pattern, path, `{"recipient":"invalid"}`, session.Token, 400)
	}
}

func TestEmailContractsSuccessfulLocalSMTPResponse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			result <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		reader := bufio.NewReader(conn)
		_, err = fmt.Fprint(conn, "220 local.test ESMTP\r\n")
		if err != nil {
			result <- err
			return
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				result <- err
				return
			}
			command := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"), strings.HasPrefix(command, "MAIL FROM"), strings.HasPrefix(command, "RCPT TO"):
				_, err = fmt.Fprint(conn, "250 OK\r\n")
			case command == "DATA":
				_, err = fmt.Fprint(conn, "354 Send message\r\n")
				if err != nil {
					result <- err
					return
				}
				for {
					line, err = reader.ReadString('\n')
					if err != nil {
						result <- err
						return
					}
					if line == ".\r\n" {
						break
					}
				}
				_, err = fmt.Fprint(conn, "250 accepted\r\n")
			case command == "QUIT":
				_, err = fmt.Fprint(conn, "221 bye\r\n")
				result <- err
				return
			default:
				result <- fmt.Errorf("unexpected SMTP command %q", command)
				return
			}
			if err != nil {
				result <- err
				return
			}
		}
	}()
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	settings, err := repo.UpsertEmailSettings(store.EmailSettingsInput{Name: "Local SMTP", Enabled: true, Host: "127.0.0.1", Port: number, Security: "none", SenderEmail: "local@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{store: repo, logger: slog.Default()}
	w := httptest.NewRecorder()
	r.sendEmailSettingsTest(w, settings, "recipient@example.com")
	if w.Code != 200 {
		t.Fatalf("SMTP response %d %s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	c, _, _ := emailPayloadContract("POST /api/v1/email-settings/test")
	validateWireObject(t, response, wireSchema(c.Response))
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("local SMTP did not complete")
	}
	stored, _, err := repo.GetEmailSettingsByID(settings.ID)
	if err != nil || stored.LastTestStatus != "succeeded" {
		t.Fatalf("test result not persisted: %v", err)
	}
	_ = actor
}
