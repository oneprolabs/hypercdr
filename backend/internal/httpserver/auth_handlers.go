package httpserver

import (
	"context"
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (r *Router) verifyTurnstile(req *http.Request, token string) (bool, []string, string) {
	log := r.logger
	if log == nil {
		log = slog.Default()
	}
	if strings.TrimSpace(r.cfg.TurnstileSecretKey) == "" {
		log.Warn("turnstile verification skipped: secret key not configured")
		return false, []string{"missing-input-secret"}, ""
	}
	form := url.Values{"secret": {r.cfg.TurnstileSecretKey}, "response": {token}}
	if ip := strings.TrimSpace(strings.Split(req.Header.Get("X-Forwarded-For"), ",")[0]); ip != "" {
		form.Set("remoteip", ip)
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.TurnstileVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		log.Warn("turnstile verification: failed to build request", "error", err)
		return false, []string{"client-error"}, ""
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		log.Warn("turnstile verification: cloudflare request failed", "error", err)
		return false, []string{"network-error"}, ""
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		log.Warn(
			"turnstile verification: non-OK status from cloudflare",
			"status", response.StatusCode,
			"body", string(body),
		)
		return false, []string{"siteverify-non-200"}, ""
	}
	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
		Hostname   string   `json:"hostname"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Warn(
			"turnstile verification: failed to decode response",
			"error", err,
			"body", string(body),
		)
		return false, []string{"decode-error"}, ""
	}
	if !result.Success {
		log.Warn(
			"turnstile verification rejected by cloudflare",
			"error_codes", result.ErrorCodes,
			"hostname", result.Hostname,
			"token_len", len(token),
		)
	}
	return result.Success, result.ErrorCodes, result.Hostname
}

func (r *Router) listCommunityUsers(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListUsers()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_users_failed"})
		return
	}
	admin := []store.User{}
	for _, item := range items {
		if item.SystemAdmin {
			item.Role = "System Administrator"
			item.RecoveryEmail, _, _ = r.store.GetAdminRecoveryEmail(item.ID)
			admin = append(admin, item)
			break
		}
	}
	writeJSON(w, 200, map[string]any{"items": admin})
}

func (r *Router) createCaptcha(w http.ResponseWriter, req *http.Request) {
	code, err := randomDigits(4)
	if err != nil {
		r.logger.Error("failed to generate captcha", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "captcha_generation_failed"})
		return
	}
	id := store.NewPublicID()
	now := time.Now().UTC()
	r.captchaMu.Lock()
	for captchaID, challenge := range r.captchas {
		if now.After(challenge.ExpiresAt) {
			delete(r.captchas, captchaID)
		}
	}
	r.captchas[id] = captchaChallenge{Code: code, ExpiresAt: now.Add(2 * time.Minute)}
	r.captchaMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"id":        id,
		"image":     captchaImageDataURL(code),
		"expiresAt": now.Add(2 * time.Minute).Format(time.RFC3339),
	})
}

func (r *Router) login(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Email          string `json:"email"`
		Password       string `json:"password"`
		CaptchaID      string `json:"captchaId"`
		CaptchaCode    string `json:"captchaCode"`
		TurnstileToken string `json:"turnstileToken"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if r.authChallengeMode() == "turnstile" {
		if strings.TrimSpace(body.TurnstileToken) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "challenge_required", "message": "Human verification is required"})
			return
		}
		ok, codes, hostname := r.verifyTurnstile(req, strings.TrimSpace(body.TurnstileToken))
		if !ok {
			r.logger.Warn("login rejected: turnstile failed", "email", body.Email, "error_codes", codes, "hostname", hostname)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "challenge_invalid", "message": "Human verification failed"})
			return
		}
	} else if strings.TrimSpace(body.CaptchaID) == "" || strings.TrimSpace(body.CaptchaCode) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "captcha_required", "message": "Verification code is required"})
		return
	}

	if r.authChallengeMode() == "image" && !r.consumeCaptcha(body.CaptchaID, body.CaptchaCode) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "captcha_invalid", "message": "Verification code is incorrect"})
		return
	}

	user, ok, err := r.identityProvider.Authenticate(req.Context(), body.Email, body.Password)
	if err != nil {
		r.logger.Error("failed to authenticate user", "email", body.Email, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "login_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_credentials", "message": "Username or password is incorrect"})
		return
	}

	session, err := r.identityProvider.CreateSession(req.Context(), user.ID, time.Hour)
	if err != nil {
		r.logger.Error("failed to create platform session", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "session_create_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
		"session": map[string]any{
			"token":     session.Token,
			"expiresAt": session.ExpiresAt.Format(time.RFC3339),
		},
	})
}

func bearerToken(req *http.Request) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(req.Header.Get("Authorization")), "Bearer "))
}
func requestUser(req *http.Request) (store.User, bool) {
	u, ok := req.Context().Value(requestUserContextKey{}).(store.User)
	return u, ok
}

func (r *Router) logout(w http.ResponseWriter, req *http.Request) {
	_ = r.identityProvider.DeleteSession(req.Context(), bearerToken(req))
	writeJSON(w, http.StatusOK, map[string]any{"loggedOut": true})
}
func (r *Router) currentUser(w http.ResponseWriter, req *http.Request) {
	u, ok := requestUser(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication_required"})
		return
	}
	writeJSON(w, http.StatusOK, u)
}
func (r *Router) updateCurrentUser(w http.ResponseWriter, req *http.Request) {
	u, ok := requestUser(req)
	if !ok {
		return
	}
	var body struct {
		DisplayName string  `json:"displayName"`
		Email       string  `json:"email"`
		TimeZone    *string `json:"timeZone"`
	}
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(body.Email) == "" {
		body.Email = u.Email
	}
	timeZone := u.TimeZone
	if body.TimeZone != nil {
		timeZone = strings.TrimSpace(*body.TimeZone)
		if timeZone != "" {
			if _, err := time.LoadLocation(timeZone); err != nil {
				writeJSON(w, 400, map[string]any{"error": "invalid_time_zone", "message": "Select a valid IANA time zone."})
				return
			}
		}
	}
	updated, _, err := r.identityProvider.UpdateProfile(req.Context(), EditionIdentityProfileUpdate{ID: u.ID, Email: body.Email, DisplayName: body.DisplayName, TimeZone: timeZone})
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "user_update_failed", "message": err.Error()})
		return
	}
	writeJSON(w, 200, updated)
}
func (r *Router) changeOwnPassword(w http.ResponseWriter, req *http.Request) {
	u, ok := requestUser(req)
	if !ok {
		return
	}
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
		RecoveryEmail   string `json:"recoveryEmail"`
	}
	if decodeJSON(req, &body) != nil || !validUserPassword(body.NewPassword) {
		writeJSON(w, 400, map[string]any{"error": "password_invalid", "message": "Password must be 8 to 128 characters."})
		return
	}
	if body.CurrentPassword == body.NewPassword {
		writeJSON(w, 400, map[string]any{"error": "password_unchanged", "message": "New password must be different from the temporary password."})
		return
	}
	if _, valid, _ := r.identityProvider.Authenticate(req.Context(), u.Email, body.CurrentPassword); !valid {
		writeJSON(w, 400, map[string]any{"error": "current_password_invalid", "message": "Current password is incorrect."})
		return
	}
	if u.SystemAdmin && u.MustChangePassword {
		if !validUserEmail(body.RecoveryEmail) {
			writeJSON(w, 400, map[string]any{"error": "recovery_email_required", "message": "Enter a valid recovery email before changing the temporary password."})
			return
		}
		if _, found, err := r.store.SetAdminRecoveryEmail(u.ID, body.RecoveryEmail); err != nil || !found {
			writeJSON(w, 500, map[string]any{"error": "recovery_email_update_failed", "message": "The recovery email could not be saved. Your password was not changed."})
			return
		}
	}
	updated, found, err := r.identityProvider.SetPassword(req.Context(), u.ID, body.NewPassword, false)
	if err != nil || !found {
		writeJSON(w, 500, map[string]any{"error": "password_update_failed"})
		return
	}
	writeJSON(w, 200, updated)
}
