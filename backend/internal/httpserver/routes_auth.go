package httpserver

func (r *Router) mountAuthRoutes() {
	r.mux.HandleFunc("GET /api/v1/auth/captcha", r.createCaptcha)
	r.mux.HandleFunc("POST /api/v1/auth/login", r.login)
	r.mux.HandleFunc("POST /api/v1/auth/forgot-password", r.forgotPassword)
	r.mux.HandleFunc("POST /api/v1/auth/reset-password", r.resetPassword)
	r.mux.HandleFunc("GET /api/v1/auth/config", r.authConfig)
	r.mux.HandleFunc("POST /api/v1/auth/logout", r.logout)
	r.mux.HandleFunc("GET /api/v1/auth/me", r.currentUser)
	r.mux.HandleFunc("PATCH /api/v1/auth/me", r.updateCurrentUser)
	r.mux.HandleFunc("POST /api/v1/auth/change-password", r.changeOwnPassword)
}
