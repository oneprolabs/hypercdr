package store

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"
)

func (s *PostgresStore) CreateAuthChallenge(ctx context.Context, kind, id, answer string, expiry time.Time) error {
	// Bound cleanup work through the expiry index. Challenge creation never
	// depends on a process-local janitor surviving deployment.
	if _, err := s.db.ExecContext(ctx, `delete from auth_challenges where expires_at <= now()`); err != nil {
		return err
	}
	idHash, answerHash := sha256.Sum256([]byte(id)), sha256.Sum256([]byte(answer))
	_, err := s.db.ExecContext(ctx, `insert into auth_challenges(kind,id_hash,answer_hash,expires_at) values($1,$2,$3,$4)`, kind, idHash[:], answerHash[:], expiry.UTC())
	return err
}

func (s *PostgresStore) ConsumeAuthChallenge(ctx context.Context, kind, id, answer string) (bool, error) {
	idHash, answerHash := sha256.Sum256([]byte(id)), sha256.Sum256([]byte(answer))
	var expected []byte
	var valid bool
	// DELETE RETURNING is atomic across pools: wrong answers also consume the
	// challenge, preserving the original anti-replay and brute-force behavior.
	err := s.db.QueryRowContext(ctx, `delete from auth_challenges where kind=$1 and id_hash=$2 returning answer_hash, expires_at > now()`, kind, idHash[:]).Scan(&expected, &valid)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return valid && subtle.ConstantTimeCompare(expected, answerHash[:]) == 1, nil
}
