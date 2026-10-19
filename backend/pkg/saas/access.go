package saas

import (
	"database/sql"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/platformaccess"
	"net/http"
)

// Request-local copies avoid races when the address changes during active requests.
// Every API replica reads the same persisted value; environment is the seed only.
func (s *Service) withCurrentOrigin(w http.ResponseWriter, r *http.Request, next func(*Service)) {
	current := *s
	var origin string
	err := s.db.QueryRowContext(r.Context(), `select coalesce(public_endpoint,'') from platform_settings where tenant_id=$1`, store.DefaultTenantID).Scan(&origin)
	if err != nil && err != sql.ErrNoRows {
		internal(w)
		return
	}
	if origin != "" {
		var e error
		current.cfg.Origin, e = platformaccess.Normalize(origin)
		if e != nil {
			internal(w)
			return
		}
	}
	if s.google != nil {
		g := *s.google
		g.OAuth = s.google.OAuth
		g.OAuth.RedirectURL = current.cfg.Origin + "/api/v1/saas/auth/google/callback"
		current.google = &g
	}
	next(&current)
}
func originProblem(w http.ResponseWriter, origin string) {
	respond(w, 403, map[string]string{"error": "origin_not_allowed", "message": "This address does not match the platform access address. Use the official entry, or ask the platform administrator to update Settings → Access & Login.", "publicUrl": origin})
}
