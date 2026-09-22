package config

import "testing"

func TestLoadDefaultsAuthChallengeModeToTurnstile(t *testing.T) {
	t.Setenv("HCDR_AUTH_CHALLENGE_MODE", "")
	if got := Load().AuthChallengeMode; got != "turnstile" {
		t.Fatalf("expected default auth challenge mode %q, got %q", "turnstile", got)
	}
}

func TestLoadHonoursExplicitAuthChallengeMode(t *testing.T) {
	for _, mode := range []string{"image", "turnstile"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HCDR_AUTH_CHALLENGE_MODE", mode)
			if got := Load().AuthChallengeMode; got != mode {
				t.Fatalf("expected auth challenge mode %q, got %q", mode, got)
			}
		})
	}
}
