package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const stripeVersion = "2026-11-18.endive"

type StripeError struct {
	Status          int
	Code, Parameter string
}

func (failure *StripeError) Error() string {
	return "Stripe rejected the request (HTTP " + strconv.Itoa(failure.Status) + ", code=" + failure.Code + ", parameter=" + failure.Parameter + ")"
}

var stripeErrorField = regexp.MustCompile(`^[a-zA-Z0-9_.\[\]-]{1,120}$`)

type StripeConfig struct {
	SecretKey     string
	WebhookSecret string
	Prices        map[string]string
}

func LoadStripeConfig(path string) (StripeConfig, error) {
	config := StripeConfig{Prices: map[string]string{}}
	if path == "" {
		return config, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return config, errors.New("Stripe configuration file cannot be opened")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return config, errors.New("Stripe configuration must be a private regular file no larger than 16 KiB")
	}
	body, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(body) > 16384 {
		return config, errors.New("Stripe configuration cannot be read")
	}
	values := map[string]string{}
	allowed := map[string]bool{"HCDR_STRIPE_MODE": true, "HCDR_STRIPE_SECRET_KEY": true, "HCDR_STRIPE_WEBHOOK_SECRET": true, "HCDR_STRIPE_STANDARD_PRICE_ID": true, "HCDR_STRIPE_BUSINESS_PRICE_ID": true}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !allowed[key] {
			return config, errors.New("Stripe configuration contains an unsupported entry")
		}
		if _, exists := values[key]; exists {
			return config, errors.New("Stripe configuration contains a duplicate entry")
		}
		values[key] = strings.TrimSpace(value)
	}
	if values["HCDR_STRIPE_MODE"] != "test" || !strings.HasPrefix(values["HCDR_STRIPE_SECRET_KEY"], "rk_test_") {
		return config, errors.New("Stripe sandbox requires test mode and a restricted test key")
	}
	config.SecretKey = values["HCDR_STRIPE_SECRET_KEY"]
	config.WebhookSecret = values["HCDR_STRIPE_WEBHOOK_SECRET"]
	if config.WebhookSecret != "" && !strings.HasPrefix(config.WebhookSecret, "whsec_") {
		return config, errors.New("Stripe webhook secret is invalid")
	}
	for _, plan := range []string{"standard", "business"} {
		price := values["HCDR_STRIPE_"+strings.ToUpper(plan)+"_PRICE_ID"]
		if !strings.HasPrefix(price, "price_") || strings.ContainsAny(price, " \t\r\n") {
			return config, errors.New("Stripe price ID is invalid")
		}
		config.Prices[plan] = price
	}
	if config.Prices["standard"] == config.Prices["business"] {
		return config, errors.New("Stripe plans require distinct prices")
	}
	return config, nil
}

type StripeClient struct {
	config  StripeConfig
	http    *http.Client
	baseURL string
}

func NewStripeClient(config StripeConfig) *StripeClient {
	return &StripeClient{config: config, http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, baseURL: "https://api.stripe.com/v1"}
}

func (client *StripeClient) Request(ctx context.Context, method, path, key string, values url.Values, result any) error {
	if client.config.SecretKey == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#\r\n") {
		return errors.New("Stripe request is invalid")
	}
	if method != http.MethodGet && key == "" {
		return errors.New("Stripe writes require an idempotency key")
	}
	var body io.Reader
	if method == http.MethodGet {
		if len(values) > 0 {
			path += "?" + values.Encode()
		}
	} else {
		body = strings.NewReader(values.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return errors.New("Stripe request cannot be created")
	}
	request.Header.Set("Authorization", "Bearer "+client.config.SecretKey)
	request.Header.Set("Stripe-Version", stripeVersion)
	if method != http.MethodGet {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return errors.New("Stripe request could not be completed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var payload struct {
			Error struct {
				Code      string `json:"code"`
				Parameter string `json:"param"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(&payload)
		failure := &StripeError{Status: response.StatusCode}
		if stripeErrorField.MatchString(payload.Error.Code) {
			failure.Code = payload.Error.Code
		}
		if stripeErrorField.MatchString(payload.Error.Parameter) {
			failure.Parameter = payload.Error.Parameter
		}
		return failure
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil || len(payload) >= 2<<20 {
		return errors.New("Stripe response exceeded the supported size")
	}
	if json.Unmarshal(payload, result) != nil {
		return errors.New("Stripe returned an invalid response")
	}
	return nil
}

func (client *StripeClient) ValidatePrices(ctx context.Context, plans []Plan) error {
	for _, plan := range plans {
		priceID := client.config.Prices[plan.ID]
		if priceID == "" {
			return errors.New("Stripe price is not configured for this plan")
		}
		var price struct {
			ID        string          `json:"id"`
			Active    bool            `json:"active"`
			Live      bool            `json:"livemode"`
			Amount    int64           `json:"unit_amount"`
			Currency  string          `json:"currency"`
			Type      string          `json:"type"`
			Scheme    string          `json:"billing_scheme"`
			Transform json.RawMessage `json:"transform_quantity"`
			Recurring struct {
				Interval string `json:"interval"`
				Count    int    `json:"interval_count"`
				Usage    string `json:"usage_type"`
			} `json:"recurring"`
		}
		if err := client.Request(ctx, http.MethodGet, "/prices/"+url.PathEscape(priceID), "", nil, &price); err != nil {
			return err
		}
		if price.ID != priceID || !price.Active || price.Live || price.Amount != plan.AmountMinor || price.Currency != plan.Currency || price.Type != "recurring" || price.Scheme != "per_unit" || price.Recurring.Interval != plan.Interval || price.Recurring.Count != 1 || price.Recurring.Usage != "licensed" || (len(price.Transform) > 0 && string(price.Transform) != "null") {
			return errors.New("Stripe sandbox price does not match the approved subscription plan")
		}
	}
	return nil
}

func VerifyStripeSignature(payload []byte, header, secret string, now time.Time) error {
	if secret == "" || len(header) > 4096 {
		return errors.New("Stripe webhook signature is invalid")
	}
	var timestamp string
	var signatures []string
	for _, entry := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			continue
		}
		if key == "t" {
			if timestamp != "" {
				return errors.New("Stripe webhook signature is invalid")
			}
			timestamp = value
		}
		if key == "v1" {
			signatures = append(signatures, value)
		}
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || now.Sub(time.Unix(seconds, 0)) > 5*time.Minute || time.Unix(seconds, 0).Sub(now) > 5*time.Minute {
		return errors.New("Stripe webhook timestamp is invalid")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(payload)
	expected := mac.Sum(nil)
	for _, signature := range signatures {
		actual, err := hex.DecodeString(signature)
		if err == nil && hmac.Equal(expected, actual) {
			return nil
		}
	}
	return errors.New("Stripe webhook signature is invalid")
}
