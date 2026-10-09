package store

import (
	"context"
	"time"
)

// SchedulerRepository serializes scheduler ticks across API processes sharing
// one database, including the overlap during a blue/green deployment.
type SchedulerRepository interface {
	RunSchedulerExclusive(work func()) (bool, error)
}

// RunSchedulerExclusive holds a transaction-scoped advisory lock on a dedicated
// connection. Task writes use the normal pool; rollback releases the lock on
// success, panic, or failure without leaving a session lock in the pool.
func (s *PostgresStore) RunSchedulerExclusive(work func()) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	// Do not bind the transaction to the acquisition timeout: a legitimate tick
	// may take longer. The connection is always returned after rollback.
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var acquired bool
	// Stable, product-specific key. Each database has its own lock namespace.
	if err := tx.QueryRowContext(ctx, `select pg_try_advisory_xact_lock(1212376146, 1)`).Scan(&acquired); err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}
	work()
	return true, nil
}
