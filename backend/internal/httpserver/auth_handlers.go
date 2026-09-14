package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strings"
	"time"
)

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
		Email       string `json:"email"`
		Password    string `json:"password"`
		CaptchaID   string `json:"captchaId"`
		CaptchaCode string `json:"captchaCode"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(body.CaptchaID) == "" || strings.TrimSpace(body.CaptchaCode) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "captcha_required", "message": "Verification code is required"})
		return
	}

	if !r.consumeCaptcha(body.CaptchaID, body.CaptchaCode) {
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
