package saas

import "testing"

func TestSaaSOriginAndConfiguration(t *testing.T) {
	for _, test := range []struct {
		origin string
		valid  bool
	}{{"https://app.example.test", true}, {"http://localhost:13002", true}, {"http://127.0.0.1:13002", true}, {"http://192.168.8.149:13002", false}, {"https://app.example.test/path", false}, {"https://user:pass@app.example.test", false}} {
		t.Run(test.origin, func(t *testing.T) {
			t.Setenv("HCDR_PUBLIC_BASE_URL", test.origin)
			t.Setenv("HCDR_GOOGLE_CLIENT_ID", "")
			t.Setenv("HCDR_GOOGLE_CLIENT_SECRET", "")
			t.Setenv("HCDR_DEPLOYMENT_MODE", "saas")
			t.Setenv("HCDR_SAAS_SIGNUP_ENABLED", "true")
			_, e := ConfigFromEnvironment()
			if (e == nil) != test.valid {
				t.Fatalf("origin validation: %v", e)
			}
		})
	}
	t.Setenv("HCDR_PUBLIC_BASE_URL", "https://app.example.test")
	t.Setenv("HCDR_GOOGLE_CLIENT_ID", "id")
	t.Setenv("HCDR_GOOGLE_CLIENT_SECRET", "")
	if _, e := ConfigFromEnvironment(); e == nil {
		t.Fatal("partial Google credentials accepted")
	}
}
