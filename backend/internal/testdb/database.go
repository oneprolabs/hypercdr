// Package testdb provisions isolated PostgreSQL databases for tests.
// It never reads the production HCDR_DATABASE_URL.
package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	_ "github.com/jackc/pgx/v5/stdlib"
	"net/url"
	"os"
	"testing"
	"time"
)

// New creates a fresh database and cleans it up after the test's stores close.
func New(t testing.TB) string {
	t.Helper()
	raw := os.Getenv("HCDR_TEST_DATABASE_URL")
	u, err := validateURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		t.Fatalf("test PostgreSQL unavailable: %v", err)
	}
	var suffix [12]byte
	if _, err = rand.Read(suffix[:]); err != nil {
		db.Close()
		t.Fatal(err)
	}
	name := "hcdr_test_" + hex.EncodeToString(suffix[:])
	if _, err = db.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer db.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, `DROP DATABASE "`+name+`" WITH (FORCE)`); err != nil {
			t.Errorf("drop isolated test database %s: %v", name, err)
		}
	})
	u.Path = "/" + name
	q := u.Query()
	q.Set("timezone", "UTC")
	u.RawQuery = q.Encode()
	return u.String()
}

func validateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Path != "/hypercdr_test_admin" || u.User == nil || u.User.Username() != "hypercdr_test" {
		return nil, errors.New("HCDR_TEST_DATABASE_URL must use dedicated hypercdr_test user and hypercdr_test_admin database; run scripts/test-backend.sh")
	}
	// A URL query must not override the validated user/database through pgx
	// connection options, including when we replace the path for each test.
	for key := range u.Query() {
		switch key {
		case "sslmode", "sslrootcert", "sslcert", "sslkey", "connect_timeout", "application_name", "timezone":
		default:
			return nil, errors.New("unsupported HCDR_TEST_DATABASE_URL query parameter: " + key)
		}
	}
	return u, nil
}
