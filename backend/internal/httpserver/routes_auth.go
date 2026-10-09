package httpserver

func (r *Router) mountAuthRoutes() {
	r.handleRoute("GET /api/v1/auth/captcha", r.createCaptcha)
	r.handleRoute("POST /api/v1/auth/login", r.login)
	r.handleRoute("POST /api/v1/auth/forgot-password", r.forgotPassword)
	r.handleRoute("POST /api/v1/auth/reset-password", r.resetPassword)
	r.handleRoute("GET /api/v1/auth/config", r.authConfig)
	r.handleRoute("GET /api/v1/auth/turnstile/config", r.authTurnstileConfig)
	r.handleRoute("POST /api/v1/auth/logout", r.logout)
	r.handleRoute("GET /api/v1/auth/me", r.currentUser)
	r.handleRoute("PATCH /api/v1/auth/me", r.updateCurrentUser)
	r.handleRoute("PATCH /api/v1/auth/me/theme", r.updateCurrentUserTheme)
	r.handleRoute("POST /api/v1/auth/change-password", r.changeOwnPassword)
}
