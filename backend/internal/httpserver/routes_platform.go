package httpserver

// mountPlatformRoutes registers health, product metadata, and release APIs.
func (r *Router) mountPlatformRoutes() {
	r.handleRoute("GET /api/v1/schema", r.apiSchema)
	r.handleRoute("GET /api/v1/product-info", r.getProductInfo)
	r.handleRoute("GET /healthz", r.healthz)
	r.handleRoute("GET /readyz", r.readyz)
	r.handleRoute("GET /api/v1/platform/version", r.platformVersion)
	r.handleRoute("GET /api/v1/platform/releases", r.listPlatformReleases)
	r.handleRoute("GET /api/v1/platform/releases/{id}", r.getPlatformRelease)
	r.handleRoute("GET /api/v1/platform/available-releases", r.listAvailableReleases)
	r.handleRoute("POST /api/v1/platform/releases", r.createPlatformRelease)
	r.handleRoute("GET /api/v1/platform/upgrades", r.listPlatformUpgrades)
	r.handleRoute("GET /api/v1/platform/upgrades/precheck", r.precheckPlatformUpgrade)
	r.handleRoute("POST /api/v1/platform/upgrades", r.createPlatformUpgrade)
	r.handleRoute("POST /api/v1/platform/upgrades/{id}/status", r.updatePlatformUpgradeStatus)
}
