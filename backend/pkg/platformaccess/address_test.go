package platformaccess

import "testing"

func TestNormalize(t *testing.T) {
	for _, value := range []string{"https://APP.example.com/", "https://192.168.1.2:12443", "https://[::1]:12443", "http://localhost:3002"} {
		if _, err := Normalize(value); err != nil {
			t.Errorf("%q: %v", value, err)
		}
	}
	for _, value := range []string{"", "http://app.example.com", "https://a.example/path", "https://a.example?x=1", "https://user:pass@a.example", "https://a.example#fragment", "https://a.example:99999", "https://a.example:", "javascript:alert(1)"} {
		if _, err := Normalize(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}
