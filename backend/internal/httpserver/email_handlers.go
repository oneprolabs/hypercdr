package httpserver

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hypercdr-platform/platform/backend/internal/store"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type emailSettingsRequest struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Security    string `json:"security"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	SenderName  string `json:"senderName"`
	SenderEmail string `json:"senderEmail"`
}

func (r *Router) getEmailSettings(w http.ResponseWriter, req *http.Request) {
	item, found, err := r.store.GetEmailSettings()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_settings_failed"})
		return
	}
	if !found {
		item = store.EmailSettings{Port: 587, Security: "starttls", SenderName: "HyperCDR"}
	}
	writeJSON(w, 200, item)
}
func validateEmailSettings(body emailSettingsRequest) (string, string) {
	if strings.TrimSpace(body.Name) == "" {
		return "invalid_name", "Configuration name is required."
	}
	if body.Port < 1 || body.Port > 65535 {
		return "invalid_port", "Port must be between 1 and 65535."
	}
	if body.Security != "none" && body.Security != "starttls" && body.Security != "tls" {
		return "invalid_security", "Select TLS, STARTTLS, or None."
	}
	if strings.TrimSpace(body.Host) == "" || !validUserEmail(body.SenderEmail) {
		return "email_settings_incomplete", "SMTP server and a valid sender email are required."
	}
	return "", ""
}
func (r *Router) updateEmailSettings(w http.ResponseWriter, req *http.Request) {
	var body emailSettingsRequest
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		body.Name = "Default SMTP"
	}
	if code, message := validateEmailSettings(body); code != "" {
		writeJSON(w, 400, map[string]any{"error": code, "message": message})
		return
	}
	current, _, _ := r.store.GetEmailSettings()
	ciphertext := current.PasswordCiphertext
	if body.Password != "" {
		var err error
		ciphertext, err = r.encryptSetting(body.Password)
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": "email_password_encrypt_failed"})
			return
		}
	}
	actor, _ := requestUser(req)
	item, err := r.store.UpsertEmailSettings(emailSettingsStoreInput(body, ciphertext, actor.ID))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_settings_update_failed"})
		return
	}
	writeJSON(w, 200, item)
}

func emailSettingsStoreInput(body emailSettingsRequest, ciphertext, updatedBy string) store.EmailSettingsInput {
	return store.EmailSettingsInput{Name: strings.TrimSpace(body.Name), Enabled: true, Host: strings.TrimSpace(body.Host), Port: body.Port, Security: body.Security, Username: strings.TrimSpace(body.Username), PasswordCiphertext: ciphertext, SenderName: strings.TrimSpace(body.SenderName), SenderEmail: strings.TrimSpace(body.SenderEmail), UpdatedBy: updatedBy}
}

func (r *Router) listEmailSettings(w http.ResponseWriter, _ *http.Request) {
	items, err := r.store.ListEmailSettings()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_settings_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (r *Router) createEmailSettings(w http.ResponseWriter, req *http.Request) {
	var body emailSettingsRequest
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	if code, message := validateEmailSettings(body); code != "" {
		writeJSON(w, 400, map[string]any{"error": code, "message": message})
		return
	}
	if body.Password == "" {
		writeJSON(w, 400, map[string]any{"error": "password_required", "message": "SMTP password is required for a new configuration."})
		return
	}
	ciphertext, err := r.encryptSetting(body.Password)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_password_encrypt_failed"})
		return
	}
	actor, _ := requestUser(req)
	item, err := r.store.CreateEmailSettings(emailSettingsStoreInput(body, ciphertext, actor.ID))
	if errors.Is(err, store.ErrEmailSettingsNameExists) {
		writeJSON(w, 409, map[string]any{"error": "email_settings_name_exists", "message": "An SMTP configuration with this name already exists."})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_settings_create_failed"})
		return
	}
	writeJSON(w, 201, item)
}

func (r *Router) updateEmailSettingsByID(w http.ResponseWriter, req *http.Request) {
	var body emailSettingsRequest
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	if code, message := validateEmailSettings(body); code != "" {
		writeJSON(w, 400, map[string]any{"error": code, "message": message})
		return
	}
	current, found, err := r.store.GetEmailSettingsByID(req.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_settings_failed"})
		return
	}
	if !found {
		writeJSON(w, 404, map[string]any{"error": "email_settings_not_found"})
		return
	}
	ciphertext := current.PasswordCiphertext
	if body.Password != "" {
		ciphertext, err = r.encryptSetting(body.Password)
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": "email_password_encrypt_failed"})
			return
		}
	}
	actor, _ := requestUser(req)
	item, _, err := r.store.UpdateEmailSettings(current.ID, emailSettingsStoreInput(body, ciphertext, actor.ID))
	if errors.Is(err, store.ErrEmailSettingsNameExists) {
		writeJSON(w, 409, map[string]any{"error": "email_settings_name_exists", "message": "An SMTP configuration with this name already exists."})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_settings_update_failed"})
		return
	}
	writeJSON(w, 200, item)
}

func (r *Router) deleteEmailSettings(w http.ResponseWriter, req *http.Request) {
	deleted, isDefault, err := r.store.DeleteEmailSettings(req.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_settings_delete_failed"})
		return
	}
	if isDefault {
		writeJSON(w, 409, map[string]any{"error": "default_email_settings", "message": "Set another SMTP configuration as default before deleting this one."})
		return
	}
	if !deleted {
		writeJSON(w, 404, map[string]any{"error": "email_settings_not_found"})
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": true})
}

func (r *Router) setDefaultEmailSettings(w http.ResponseWriter, req *http.Request) {
	item, found, err := r.store.SetDefaultEmailSettings(req.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_settings_default_failed"})
		return
	}
	if !found {
		writeJSON(w, 404, map[string]any{"error": "email_settings_not_found"})
		return
	}
	writeJSON(w, 200, item)
}

func (r *Router) sendEmailSettingsTest(w http.ResponseWriter, settings store.EmailSettings, recipient string) {
	now := time.Now().UTC()
	if err := r.sendConfiguredEmail(settings, recipient, "HyperCDR email test", "Your HyperCDR SMTP settings are working correctly."); err != nil {
		_ = r.store.UpdateEmailSettingsTestResult(settings.ID, "failed", err.Error(), now)
		r.logger.Warn("SMTP test failed", "configuration_id", settings.ID, "error", err)
		writeJSON(w, 502, map[string]any{"error": "smtp_test_failed", "message": err.Error()})
		return
	}
	_ = r.store.UpdateEmailSettingsTestResult(settings.ID, "succeeded", "", now)
	writeJSON(w, 200, map[string]any{"sent": true, "testedAt": now})
}

func (r *Router) testEmailSettingsByID(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Recipient string `json:"recipient"`
	}
	if decodeJSON(req, &body) != nil || !validUserEmail(body.Recipient) {
		writeJSON(w, 400, map[string]any{"error": "invalid_recipient"})
		return
	}
	settings, found, err := r.store.GetEmailSettingsByID(req.PathValue("id"))
	if err != nil || !found {
		writeJSON(w, 404, map[string]any{"error": "email_settings_not_found"})
		return
	}
	r.sendEmailSettingsTest(w, settings, strings.TrimSpace(body.Recipient))
}
func (r *Router) testEmailSettings(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Recipient string `json:"recipient"`
	}
	if decodeJSON(req, &body) != nil || !validUserEmail(body.Recipient) {
		writeJSON(w, 400, map[string]any{"error": "invalid_recipient"})
		return
	}
	settings, found, err := r.store.GetEmailSettings()
	if err != nil || !found {
		writeJSON(w, 409, map[string]any{"error": "email_not_configured"})
		return
	}
	r.sendEmailSettingsTest(w, settings, strings.TrimSpace(body.Recipient))
}

func (r *Router) encryptSetting(value string) (string, error) {
	key := sha256.Sum256([]byte(r.cfg.SecretKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}
func (r *Router) decryptSetting(value string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(r.cfg.SecretKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted setting")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	return string(plain), err
}
func (r *Router) consumeCaptcha(id, code string) bool {
	r.captchaMu.Lock()
	challenge, ok := r.captchas[id]
	delete(r.captchas, id)
	r.captchaMu.Unlock()
	return ok && time.Now().UTC().Before(challenge.ExpiresAt) && challenge.Code == strings.TrimSpace(code)
}

func validUserPassword(password string) bool { return len(password) >= 8 && len(password) <= 128 }

func validUserEmail(value string) bool {
	email := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(email)
	at := strings.LastIndexByte(email, '@')
	if err != nil || address.Address != email || at <= 0 || at == len(email)-1 || len(email) > 254 {
		return false
	}
	domain := email[at+1:]
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

func (r *Router) authConfig(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"googleEnabled":    strings.TrimSpace(r.cfg.GoogleClientID) != "" && strings.TrimSpace(r.cfg.GoogleClientSecret) != "",
		"challengeMode":    r.authChallengeMode(),
		"turnstileSiteKey": strings.TrimSpace(r.cfg.TurnstileSiteKey),
		"turnstileEnabled": r.turnstileEnabled(),
		"timeZone":         serverTimeZone(),
	})
}

// authTurnstileConfig exposes only public challenge metadata. The secret is
// never serialized; clients must treat configured=false as a blocked state.
func (r *Router) authTurnstileConfig(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	enabled := r.turnstileEnabled()
	site := strings.TrimSpace(r.cfg.TurnstileSiteKey)
	configured := site != "" && strings.TrimSpace(r.cfg.TurnstileSecretKey) != ""
	writeJSON(w, http.StatusOK, map[string]any{"code": "0000", "data": map[string]any{
		"enabled": enabled, "configured": configured, "site_key": site,
	}})
}

func (r *Router) turnstileEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(r.cfg.AuthChallengeMode), "turnstile")
}

func (r *Router) authChallengeMode() string {
	mode := strings.ToLower(strings.TrimSpace(r.cfg.AuthChallengeMode))
	if mode == "turnstile" && strings.TrimSpace(r.cfg.TurnstileSiteKey) != "" && strings.TrimSpace(r.cfg.TurnstileSecretKey) != "" {
		return "turnstile"
	}
	return "image"
}

func serverTimeZone() string {
	if value := strings.TrimSpace(os.Getenv("TZ")); value != "" {
		if _, err := time.LoadLocation(value); err == nil {
			return value
		}
	}
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if index := strings.LastIndex(target, "/zoneinfo/"); index >= 0 {
			value := target[index+len("/zoneinfo/"):]
			if _, err := time.LoadLocation(value); err == nil {
				return value
			}
		}
	}
	if raw, err := os.ReadFile("/etc/timezone"); err == nil {
		if value := strings.TrimSpace(string(raw)); value != "" {
			if _, err := time.LoadLocation(value); err == nil {
				return value
			}
		}
	}
	return "UTC"
}

func serverLocation() *time.Location {
	name := serverTimeZone()
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return location
}

func (r *Router) forgotPassword(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if decodeJSON(req, &body) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	response := map[string]any{"message": "If the email address is registered, we will send password reset instructions."}
	if !validUserEmail(body.Email) {
		writeJSON(w, http.StatusOK, response)
		return
	}
	mailSettings, mailConfigured := r.effectiveEmailSettings()
	if !mailConfigured && !r.cfg.PasswordResetRevealToken {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "password_reset_unavailable",
			"message": "Email password recovery is not configured. Contact the system administrator.",
		})
		return
	}
	token, found, err := r.identityProvider.CreatePasswordResetToken(req.Context(), body.Email, 15*time.Minute)
	if err != nil {
		r.logger.Error("failed to create reset token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "reset_request_failed"})
		return
	}
	if found && mailConfigured {
		if err := r.sendPasswordResetEmail(mailSettings, strings.ToLower(strings.TrimSpace(body.Email)), token); err != nil {
			r.logger.Error("failed to send password reset email", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "reset_email_failed", "message": "Password reset email could not be sent. Please contact the administrator."})
			return
		}
	}
	if found && r.cfg.PasswordResetRevealToken {
		response["resetToken"] = token
		response["expiresInSeconds"] = 900
	}
	writeJSON(w, http.StatusOK, response)
}

func (r *Router) effectiveEmailSettings() (store.EmailSettings, bool) {
	if item, found, err := r.store.GetEmailSettings(); err == nil && found {
		return item, strings.TrimSpace(item.Host) != "" && strings.TrimSpace(item.SenderEmail) != ""
	}
	if strings.TrimSpace(r.cfg.SMTPHost) == "" {
		return store.EmailSettings{}, false
	}
	fromName, fromEmail := "HyperCDR", r.cfg.SMTPFrom
	if parsed, err := mail.ParseAddress(r.cfg.SMTPFrom); err == nil {
		fromName, fromEmail = parsed.Name, parsed.Address
	}
	password, _ := r.encryptSetting(r.cfg.SMTPPassword)
	port, _ := strconv.Atoi(r.cfg.SMTPPort)
	return store.EmailSettings{Enabled: true, Host: r.cfg.SMTPHost, Port: port, Security: "starttls", Username: r.cfg.SMTPUsername, PasswordCiphertext: password, SenderName: fromName, SenderEmail: fromEmail}, true
}
func (r *Router) sendPasswordResetEmail(settings store.EmailSettings, recipient, token string) error {
	base := strings.TrimRight(r.cfg.PublicBaseURL, "/")
	resetURL := base + "/?auth=reset&reset_token=" + url.QueryEscape(token)
	return r.sendConfiguredEmail(settings, recipient, "Reset your HyperCDR password", "Use this link within 15 minutes to reset your HyperCDR password:\r\n"+resetURL+"\r\n\r\nIf you did not request this, you can ignore this email.")
}
func (r *Router) sendConfiguredEmail(settings store.EmailSettings, recipient, subject, body string) error {
	password, err := r.decryptSetting(settings.PasswordCiphertext)
	if err != nil && settings.PasswordConfigured {
		return err
	}
	fromHeader := (&mail.Address{Name: settings.SenderName, Address: settings.SenderEmail}).String()
	message := []byte("From: " + fromHeader + "\r\nTo: " + recipient + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n")
	address := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	var auth smtp.Auth
	if settings.Username != "" {
		auth = smtp.PlainAuth("", settings.Username, password, settings.Host)
	}
	if settings.Security == "starttls" {
		return smtp.SendMail(address, auth, settings.SenderEmail, []string{recipient}, message)
	}
	var conn net.Conn
	if settings.Security == "tls" {
		conn, err = tls.Dial("tcp", address, &tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = net.DialTimeout("tcp", address, 15*time.Second)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, settings.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if auth != nil {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(settings.SenderEmail); err != nil {
		return err
	}
	if err = client.Rcpt(recipient); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(message); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (r *Router) resetPassword(w http.ResponseWriter, req *http.Request) {
	var body struct{ Token, Password string }
	if decodeJSON(req, &body) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if !validUserPassword(body.Password) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "password_invalid", "message": "Password must be 8 to 128 characters"})
		return
	}
	found, err := r.identityProvider.ResetPassword(req.Context(), strings.TrimSpace(body.Token), body.Password)
	if err == nil && !found {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "reset_invalid", "message": "Reset link is invalid or expired"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "reset_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "Password updated. You can now sign in."})
}

func (r *Router) googleStart(w http.ResponseWriter, req *http.Request) {
	if r.cfg.GoogleClientID == "" || r.cfg.GoogleClientSecret == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "google_not_configured", "message": "Google sign-in is not configured"})
		return
	}
	state := store.NewPublicID() + store.NewPublicID()
	r.oauthMu.Lock()
	r.oauthStates[state] = time.Now().UTC().Add(10 * time.Minute)
	r.oauthMu.Unlock()
	callback := r.cfg.PublicBaseURL + "/api/v1/auth/google/callback"
	q := url.Values{"client_id": {r.cfg.GoogleClientID}, "redirect_uri": {callback}, "response_type": {"code"}, "scope": {"openid email profile"}, "state": {state}, "prompt": {"select_account"}}
	http.Redirect(w, req, "https://accounts.google.com/o/oauth2/v2/auth?"+q.Encode(), http.StatusFound)
}

func (r *Router) googleCallback(w http.ResponseWriter, req *http.Request) {
	state := req.URL.Query().Get("state")
	r.oauthMu.Lock()
	expiry, ok := r.oauthStates[state]
	delete(r.oauthStates, state)
	r.oauthMu.Unlock()
	if !ok || time.Now().UTC().After(expiry) {
		http.Redirect(w, req, "/?auth_error=google_state", http.StatusFound)
		return
	}
	callback := r.cfg.PublicBaseURL + "/api/v1/auth/google/callback"
	form := url.Values{"code": {req.URL.Query().Get("code")}, "client_id": {r.cfg.GoogleClientID}, "client_secret": {r.cfg.GoogleClientSecret}, "redirect_uri": {callback}, "grant_type": {"authorization_code"}}
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", form)
	if err != nil {
		http.Redirect(w, req, "/?auth_error=google_exchange", http.StatusFound)
		return
	}
	defer resp.Body.Close()
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token) != nil {
		http.Redirect(w, req, "/?auth_error=google_exchange", http.StatusFound)
		return
	}
	userReq, _ := http.NewRequestWithContext(req.Context(), http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	userReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	userResp, err := http.DefaultClient.Do(userReq)
	if err != nil {
		http.Redirect(w, req, "/?auth_error=google_profile", http.StatusFound)
		return
	}
	defer userResp.Body.Close()
	var profile struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if userResp.StatusCode != 200 || json.NewDecoder(io.LimitReader(userResp.Body, 1<<20)).Decode(&profile) != nil || !profile.EmailVerified {
		http.Redirect(w, req, "/?auth_error=google_profile", http.StatusFound)
		return
	}
	u, err := r.store.FindOrCreateGoogleUser(profile.Email)
	if err != nil {
		http.Redirect(w, req, "/?auth_error=google_account", http.StatusFound)
		return
	}
	if u.Status != "active" {
		http.Redirect(w, req, "/?auth_error=google_account_disabled", http.StatusFound)
		return
	}
	session, err := r.store.CreatePlatformSession(u.ID, time.Hour)
	if err != nil {
		http.Redirect(w, req, "/?auth_error=google_session", http.StatusFound)
		return
	}
	payload, _ := json.Marshal(map[string]any{"user": u, "session": map[string]any{"token": session.Token, "expiresAt": session.ExpiresAt.Format(time.RFC3339)}})
	http.Redirect(w, req, "/#google_auth="+url.QueryEscape(base64.RawURLEncoding.EncodeToString(payload)), http.StatusFound)
}

func randomDigits(length int) (string, error) {
	var builder strings.Builder
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		builder.WriteByte(byte('0' + n.Int64()))
	}
	return builder.String(), nil
}

func captchaImageDataURL(code string) string {
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="118" height="40" viewBox="0 0 118 40">
<defs><linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#f9fcff"/><stop offset="1" stop-color="#ddecf9"/></linearGradient></defs>
<rect width="118" height="40" rx="4" fill="url(#bg)"/>
<path d="M4 28 C24 6, 48 36, 72 14 S102 30, 114 10" fill="none" stroke="#0e7490" stroke-width="1.2" opacity=".45"/>
<path d="M2 12 C25 30, 54 4, 88 24 S108 18, 116 30" fill="none" stroke="#be185d" stroke-width="1" opacity=".28"/>
<g font-family="Georgia, 'Times New Roman', serif" font-size="24" font-weight="700">
<text x="16" y="28" fill="#1d4ed8" transform="rotate(-10 16 28)">%c</text>
<text x="40" y="27" fill="#0f766e" transform="rotate(7 40 27)">%c</text>
<text x="64" y="29" fill="#be123c" transform="rotate(-5 64 29)">%c</text>
<text x="88" y="27" fill="#4338ca" transform="rotate(9 88 27)">%c</text>
</g>
<g fill="#1e3a8a" opacity=".24"><circle cx="18" cy="11" r="1"/><circle cx="34" cy="33" r="1"/><circle cx="61" cy="9" r="1"/><circle cx="82" cy="34" r="1"/><circle cx="104" cy="16" r="1"/></g>
</svg>`, code[0], code[1], code[2], code[3])
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}
