package testdb

import "testing"

func TestTestDatabaseURLCannotOverrideBusinessConnection(t *testing.T) {
	for _, raw := range []string{
		"", "postgres://hypercdr@localhost/hypercdr", "postgres://hypercdr_test@localhost/hypercdr",
		"postgres://hypercdr_test@localhost/hypercdr_test_admin?dbname=hypercdr",
		"postgres://hypercdr_test@localhost/hypercdr_test_admin?user=hypercdr",
		"postgres://hypercdr_test@localhost/hypercdr_test_admin?service=production",
	} {
		if _, err := validateURL(raw); err == nil {
			t.Fatalf("unsafe URL accepted: %s", raw)
		}
	}
	if _, err := validateURL("postgres://hypercdr_test@127.0.0.1/hypercdr_test_admin?sslmode=disable"); err != nil {
		t.Fatal(err)
	}
}
