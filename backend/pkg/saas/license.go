package saas

import (
	"errors"
	"net/http"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/licensing"
	"hypercdr-platform/platform/backend/pkg/platform"
)

func (s *Service) license(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if r.Method == http.MethodPost {
		if !admin(w, p) {
			return
		}
		if s.cfg.SaaS {
			problem(w, 409, "online_subscription_required", "This deployment uses online subscriptions. License file activation is only available in private deployments.")
			return
		}
		if len(s.cfg.LicensePublicKeys) == 0 {
			problem(w, 503, "license_verification_unconfigured", "Commercial license activation is not configured. Contact the platform administrator.")
			return
		}
		var in struct {
			License string `json:"license"`
		}
		if !decode(r, &in) {
			problem(w, 400, "invalid_license", "Select a valid license file or paste the signed license.")
			return
		}
		verified, err := licensing.Verify(in.License, p.TenantID, s.cfg.LicensePublicKeys, time.Now().UTC())
		if err != nil {
			s.audit(r, p, "license.activation_denied", p.TenantID, p.TenantID)
			problem(w, 400, "invalid_license", "The license is invalid, expired, not yet valid or issued for a different organization.")
			return
		}
		changed, err := store.ActivateTenantLicense(r.Context(), s.db, verified, verified.Encoded, p.ID)
		if errors.Is(err, store.ErrLicenseRevision) {
			problem(w, 409, "license_revision_conflict", "A newer or different license revision is already activated. Request an updated license.")
			return
		}
		if err != nil {
			internal(w)
			return
		}
		if changed {
			s.audit(r, p, "license.activated", p.TenantID, verified.ID)
		}
	}
	usage, err := store.ReadTenantLicenseUsage(r.Context(), s.db, p.TenantID)
	if err != nil {
		problem(w, 503, "license_status_unavailable", "License information is unavailable. Retry shortly.")
		return
	}
	respond(w, 200, map[string]any{"usage": usage, "canActivate": !s.cfg.SaaS && (p.Role == "admin" || p.SystemAdmin), "activationConfigured": !s.cfg.SaaS && len(s.cfg.LicensePublicKeys) > 0})
}
