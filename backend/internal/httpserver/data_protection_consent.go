package httpserver

import (
	"net/http"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func stampDataProtectionRiskAcceptance(req *http.Request, settings *store.DataProtectionSettings) bool {
	settings.RiskAcceptance = nil
	if settings.Mode != "live-filesystem" || settings.RiskAcknowledgement != store.LiveFilesystemRiskAcknowledgement {
		return true
	}
	actor, authenticated := requestUser(req)
	if !authenticated || actor.ID == "" {
		return false
	}
	settings.RiskAcceptance = &store.DataProtectionRiskAcceptance{ActorID: actor.ID, AcceptedAt: time.Now().UTC()}
	return true
}
