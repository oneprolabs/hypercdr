package saas

import (
	"context"
	"time"
)

// Maintain bounds expired authentication state without retaining raw tokens.
// A delayed first sweep lets platform schema migrations finish at startup.
func (s *Service) Maintain(ctx context.Context) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			sweep, cancel := context.WithTimeout(ctx, 15*time.Second)
			_ = s.purgeExpired(sweep)
			cancel()
			timer.Reset(time.Hour)
		}
	}
}
func (s *Service) purgeExpired(ctx context.Context) error {
	for _, query := range []string{
		`delete from saas_auth_rate_limits where bucket in (select bucket from saas_auth_rate_limits where expires_at<now() limit 10000)`,
		`delete from saas_oauth_requests where state_hash in (select state_hash from saas_oauth_requests where expires_at<now() limit 10000)`,
		`delete from saas_onboarding_tokens where token_hash in (select token_hash from saas_onboarding_tokens where expires_at<now()-interval '30 days' limit 10000)`,
	} {
		if _, e := s.db.ExecContext(ctx, query); e != nil {
			return e
		}
	}
	return nil
}
