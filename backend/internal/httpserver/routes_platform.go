package httpserver

// mountPlatformRoutes registers health, product metadata, and release APIs.
func (r *Router) mountPlatformRoutes() {
	r.mux.HandleFunc("GET /api/v1/product-info", r.getProductInfo)
	r.mux.HandleFunc("GET /healthz", r.healthz)
	r.mux.HandleFunc("GET /readyz", r.readyz)
	r.mux.HandleFunc("GET /api/v1/platform/version", r.platformVersion)
	r.mux.HandleFunc("GET /api/v1/platform/releases", r.listPlatformReleases)
	r.mux.HandleFunc("POST /api/v1/platform/releases", r.createPlatformRelease)
	r.mux.HandleFunc("GET /api/v1/platform/upgrades", r.listPlatformUpgrades)
	r.mux.HandleFunc("GET /api/v1/platform/upgrades/precheck", r.precheckPlatformUpgrade)
	r.mux.HandleFunc("POST /api/v1/platform/upgrades", r.createPlatformUpgrade)
}
