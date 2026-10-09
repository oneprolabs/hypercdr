package store

import (
	"errors"
	"testing"
)

func TestApplicationTagReplacementRejectsForeignOrMissingTagAtomically(t *testing.T) {
	repo := newTestStore(t)
	_, app := seedPlanApplication(t, repo)
	own, err := repo.CreateTag(DefaultTenantID, "existing")
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.CreateTenant(TenantInput{Name: "Other", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := repo.CreateTag(other.ID, "foreign")
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.SetApplicationTags(app.ID, []string{own.ID}); err != nil || !found {
		t.Fatalf("initial tags: %v", err)
	}
	for _, id := range []string{foreign.ID, NewPublicID()} {
		if _, _, err := repo.SetApplicationTags(app.ID, []string{own.ID, id}); !errors.Is(err, ErrTenantResourceMismatch) {
			t.Fatalf("invalid tag accepted: %v", err)
		}
		var count int
		if err := repo.db.QueryRow(`select count(*) from application_tags where application_id=$1 and tag_id=$2`, app.ID, own.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("existing association lost: %d %v", count, err)
		}
	}
	if _, found, err := repo.SetApplicationTags(app.ID, []string{own.ID, own.ID}); err != nil || !found {
		t.Fatalf("duplicate valid tags: %v", err)
	}
	if _, found, err := repo.SetApplicationTags(app.ID, nil); err != nil || !found {
		t.Fatalf("clear tags: %v", err)
	}
	var count int
	if err := repo.db.QueryRow(`select count(*) from application_tags where application_id=$1`, app.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("clear failed: %d %v", count, err)
	}
}
