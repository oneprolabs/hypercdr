package store

import (
	"errors"
	"testing"
	"time"
)

func TestCommunityMigrationAuthorizationIsSingleUseAndFreezeIsDurable(t *testing.T) {
	repo := NewMemoryStore()
	if _, err := repo.UpsertPlatformSettings(PlatformSettingsInput{AgentNamespace: "hypercdr-agent", PublicEndpoint: "https://community.example"}); err != nil {
		t.Fatal(err)
	}
	authorization, err := repo.CreateCommunityMigrationAuthorization("00000000-0000-0000-0000-00000000a001", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.ConsumeCommunityMigrationAuthorization(authorization.Token, "enterprise-instance", "hypercdr-community-migration/v1", "test-public-key", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if session.SourceInstanceID == "" || session.TargetInstanceID != "enterprise-instance" || session.SessionToken == "" {
		t.Fatalf("unexpected session: %#v", session)
	}
	if _, err := repo.ConsumeCommunityMigrationAuthorization(authorization.Token, "other", "hypercdr-community-migration/v1", "test-public-key", time.Hour); !errors.Is(err, ErrTokenInvalid) && !errors.Is(err, ErrTokenUsed) {
		t.Fatalf("authorization was reusable: %v", err)
	}
	if _, ok, err := repo.UpdateCommunityMigrationState(session.ID, "frozen", "test", "frozen", true); err != nil || !ok {
		t.Fatalf("freeze failed: %v", err)
	}
	if frozen, err := repo.HasCommunityMigrationFreeze(); err != nil || !frozen {
		t.Fatalf("freeze not durable: %v, %v", frozen, err)
	}
	if _, ok, err := repo.UpdateCommunityMigrationState(session.ID, "rolled-back", "test", "rollback", false); err != nil || !ok {
		t.Fatalf("rollback failed: %v", err)
	}
	if frozen, err := repo.HasCommunityMigrationFreeze(); err != nil || frozen {
		t.Fatalf("freeze remained after rollback: %v, %v", frozen, err)
	}
}

func TestExpiredCommunityMigrationReleasesFreezeAndAllowsNewSession(t *testing.T) {
	repo := NewMemoryStore()
	if _, err := repo.UpsertPlatformSettings(PlatformSettingsInput{AgentNamespace: "hypercdr-agent", PublicEndpoint: "https://community.example"}); err != nil {
		t.Fatal(err)
	}
	first, _ := repo.CreateCommunityMigrationAuthorization("00000000-0000-0000-0000-00000000a001", time.Minute)
	session, err := repo.ConsumeCommunityMigrationAuthorization(first.Token, "enterprise-one", "hypercdr-community-migration/v1", "key", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.UpdateCommunityMigrationState(session.ID, "frozen", "test", "frozen", true); err != nil || !ok {
		t.Fatalf("freeze failed: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if frozen, err := repo.HasCommunityMigrationFreeze(); err != nil || frozen {
		t.Fatalf("expired migration retained freeze: frozen=%v err=%v", frozen, err)
	}
	second, _ := repo.CreateCommunityMigrationAuthorization("00000000-0000-0000-0000-00000000a001", time.Minute)
	if _, err = repo.ConsumeCommunityMigrationAuthorization(second.Token, "enterprise-two", "hypercdr-community-migration/v1", "key", time.Hour); err != nil {
		t.Fatalf("new migration was blocked after expiry: %v", err)
	}
}
