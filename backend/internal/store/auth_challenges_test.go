package store

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthChallengeSurvivesPoolHandoffAndIsSingleUse(t *testing.T) {
	first := newTestStore(t)
	var database string
	if err := first.db.QueryRow(`select current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	second := schedulerSecondStore(t, database)
	ctx := context.Background()
	for _, kind := range []string{"captcha", "google-oauth"} {
		if err := first.CreateAuthChallenge(ctx, kind, "handoff", "1234", time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		var successes atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := second.ConsumeAuthChallenge(ctx, kind, "handoff", "1234")
				if err != nil {
					t.Error(err)
				}
				if ok {
					successes.Add(1)
				}
			}()
		}
		wg.Wait()
		if successes.Load() != 1 {
			t.Fatalf("%s consumed %d times", kind, successes.Load())
		}
	}
}

func TestAuthChallengeRejectsExpiredWrongAnswerAndCrossKind(t *testing.T) {
	repo := newTestStore(t)
	ctx := context.Background()
	for _, test := range []struct {
		name, answer string
		expiry       time.Time
	}{
		{"expired", "1234", time.Now().Add(-time.Minute)}, {"wrong", "0000", time.Now().Add(time.Minute)},
	} {
		if err := repo.CreateAuthChallenge(ctx, "captcha", test.name, "1234", test.expiry); err != nil {
			t.Fatal(err)
		}
		if ok, err := repo.ConsumeAuthChallenge(ctx, "captcha", test.name, test.answer); err != nil || ok {
			t.Fatalf("%s accepted: %v %v", test.name, ok, err)
		}
		if ok, err := repo.ConsumeAuthChallenge(ctx, "captcha", test.name, "1234"); err != nil || ok {
			t.Fatalf("%s replay accepted: %v %v", test.name, ok, err)
		}
	}
	if err := repo.CreateAuthChallenge(ctx, "captcha", "same", "1234", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ConsumeAuthChallenge(ctx, "google-oauth", "same", "1234"); err != nil || ok {
		t.Fatalf("cross-kind accepted: %v %v", ok, err)
	}
	if ok, err := repo.ConsumeAuthChallenge(ctx, "captcha", "same", "1234"); err != nil || !ok {
		t.Fatalf("original missing: %v %v", ok, err)
	}
	var id, answer []byte
	if err := repo.CreateAuthChallenge(ctx, "captcha", "plaintext-id", "1234", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRow(`select id_hash,answer_hash from auth_challenges limit 1`).Scan(&id, &answer); err != nil {
		t.Fatal(err)
	}
	if len(id) != 32 || len(answer) != 32 {
		t.Fatal("challenge not hashed")
	}
}
