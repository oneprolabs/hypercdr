package httpserver

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestRiskConsentUsesAuthenticatedActorAndServerTime(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/protection-plans", nil)
	settings := store.DataProtectionSettings{Mode: "live-filesystem", RiskAcknowledgement: store.LiveFilesystemRiskAcknowledgement, RiskAcceptance: &store.DataProtectionRiskAcceptance{ActorID: "forged", AcceptedAt: time.Unix(1, 0)}}
	if stampDataProtectionRiskAcceptance(req, &settings) || settings.RiskAcceptance != nil {
		t.Fatal("anonymous or supplied actor accepted")
	}
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, store.User{ID: "authenticated", TenantID: "tenant"}))
	before := time.Now().UTC()
	if !stampDataProtectionRiskAcceptance(req, &settings) || settings.RiskAcceptance == nil || settings.RiskAcceptance.ActorID != "authenticated" || settings.RiskAcceptance.AcceptedAt.Before(before) || settings.RiskAcceptance.AcceptedAt.After(time.Now().UTC()) || settings.RiskAcceptance.AcceptedAt.Location() != time.UTC {
		t.Fatal("acceptance was not stamped by the server")
	}
	settings.Mode = "snapshot-crash-consistent"
	settings.RiskAcknowledgement = ""
	if !stampDataProtectionRiskAcceptance(req, &settings) || settings.RiskAcceptance != nil {
		t.Fatal("risk-free mode retained supplied audit identity")
	}
}
