// Package googleidentity implements the shared Google OIDC verification boundary.
package googleidentity

import (
	"context"
	"errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

type Provider struct {
	OAuth    oauth2.Config
	Verifier *oidc.IDTokenVerifier
	Client   *http.Client
}
type Claims struct{ Subject, Email string }

func New(clientID, clientSecret, callbackURL string) *Provider {
	client := &http.Client{Timeout: 10 * time.Second}
	ctx := oidc.ClientContext(context.Background(), client)
	return &Provider{OAuth: oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, RedirectURL: callbackURL, Scopes: []string{oidc.ScopeOpenID, "email", "profile"}, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams}}, Verifier: oidc.NewVerifier("https://accounts.google.com", oidc.NewRemoteKeySet(ctx, "https://www.googleapis.com/oauth2/v3/certs"), &oidc.Config{ClientID: clientID}), Client: client}
}
func (g *Provider) Verify(ctx context.Context, code, verifier, nonce string) (Claims, error) {
	token, e := g.OAuth.Exchange(context.WithValue(ctx, oauth2.HTTPClient, g.Client), code, oauth2.VerifierOption(verifier))
	if e != nil {
		return Claims{}, errors.New("authorization_exchange_failed")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return Claims{}, errors.New("identity_token_missing")
	}
	id, e := g.Verifier.Verify(ctx, raw)
	if e != nil {
		return Claims{}, errors.New("identity_token_invalid")
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Nonce         string `json:"nonce"`
	}
	if id.Claims(&claims) != nil || !claims.EmailVerified || claims.Nonce != nonce || id.Subject == "" {
		return Claims{}, errors.New("identity_claims_invalid")
	}
	email, ok := verifiedEmail(claims.Email)
	if !ok {
		return Claims{}, errors.New("verified_email_required")
	}
	return Claims{Subject: id.Subject, Email: email}, nil
}

func verifiedEmail(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	a, e := mail.ParseAddress(value)
	return value, e == nil && a.Address == value && len(value) <= 254
}
