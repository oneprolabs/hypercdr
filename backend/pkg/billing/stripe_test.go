package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStripeConfigPrivateSandbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stripe.env")
	valid := "HCDR_STRIPE_MODE=test\nHCDR_STRIPE_SECRET_KEY=rk_test_example\nHCDR_STRIPE_STANDARD_PRICE_ID=price_standard\nHCDR_STRIPE_BUSINESS_PRICE_ID=price_business\nHCDR_STRIPE_WEBHOOK_SECRET=\n"
	for _, test := range []struct {
		name, body string
		mode       os.FileMode
		valid      bool
	}{
		{"private", valid, 0600, true},
		{"public", valid, 0644, false},
		{"live", strings.Replace(valid, "rk_test_", "rk_live_", 1), 0600, false},
		{"shell", valid + "export SECRET=oops\n", 0600, false},
		{"duplicate", valid + "HCDR_STRIPE_MODE=test\n", 0600, false},
		{"same price", strings.Replace(valid, "price_business", "price_standard", 1), 0600, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.body), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, test.mode); err != nil {
				t.Fatal(err)
			}
			_, err := LoadStripeConfig(path)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
}

func TestStripeSignature(t *testing.T) {
	now := time.Unix(1800000000, 0)
	payload := []byte(`{"id":"evt_test"}`)
	secret := "whsec_test"
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", now.Unix(), payload)
	header := fmt.Sprintf("t=%d,v1=00,v1=%s", now.Unix(), hex.EncodeToString(mac.Sum(nil)))
	if err := VerifyStripeSignature(payload, header, secret, now); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		body           []byte
		header, secret string
		now            time.Time
	}{
		{[]byte(`{}`), header, secret, now},
		{payload, header, "whsec_other", now},
		{payload, header, secret, now.Add(301 * time.Second)},
		{payload, header, secret, now.Add(-301 * time.Second)},
		{payload, header + ",t=1800000000", secret, now},
		{payload, header, "", now},
	} {
		if VerifyStripeSignature(test.body, test.header, test.secret, test.now) == nil {
			t.Fatal("accepted invalid signature")
		}
	}
}

func TestStripeTransportAndPriceValidation(t *testing.T) {
	price := `{"id":"price_standard","active":true,"livemode":false,"unit_amount":10000,"currency":"usd","type":"recurring","billing_scheme":"per_unit","transform_quantity":null,"recurring":{"interval":"month","interval_count":1,"usage_type":"licensed"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rk_test_example" || r.Header.Get("Stripe-Version") != stripeVersion {
			t.Error("missing pinned authentication/version")
		}
		if r.Method == "POST" {
			if r.Header.Get("Idempotency-Key") != "intent-1" {
				t.Error("missing idempotency")
			}
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"message":"sensitive provider payload"}}`)
			return
		}
		fmt.Fprint(w, price)
	}))
	defer server.Close()
	client := NewStripeClient(StripeConfig{SecretKey: "rk_test_example", Prices: map[string]string{"standard": "price_standard"}})
	client.baseURL = server.URL
	if err := client.ValidatePrices(context.Background(), DefaultPlans()[:1]); err != nil {
		t.Fatal(err)
	}
	price = strings.Replace(price, `"livemode":false`, `"livemode":true`, 1)
	if client.ValidatePrices(context.Background(), DefaultPlans()[:1]) == nil {
		t.Fatal("accepted live price")
	}
	if client.Request(context.Background(), "POST", "/customers", "", url.Values{}, new(any)) == nil {
		t.Fatal("write without idempotency accepted")
	}
	err := client.Request(context.Background(), "POST", "/customers", "intent-1", url.Values{}, new(any))
	if err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("provider error not safely handled: %v", err)
	}
}
