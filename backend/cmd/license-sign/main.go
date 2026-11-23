package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"hypercdr-platform/platform/backend/pkg/licensing"
)

func run(args []string, output, diagnostic io.Writer) error {
	flags := flag.NewFlagSet("license-sign", flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	claimsFile := flags.String("claims", "", "path to the license claims JSON")
	privateKeyFile := flags.String("private-key", "", "path to the issuer's PKCS#8 PEM Ed25519 private key")
	keyID := flags.String("key-id", "", "configured public key ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *claimsFile == "" || *privateKeyFile == "" || *keyID == "" || flags.NArg() != 0 {
		return errors.New("--claims, --private-key and --key-id are required; keep the signing key outside the platform")
	}
	info, err := os.Stat(*privateKeyFile)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return errors.New("private key must be a regular file readable only by its owner (chmod 600)")
	}
	keyPEM, err := os.ReadFile(*privateKeyFile)
	if err != nil {
		return err
	}
	block, remaining := pem.Decode(keyPEM)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(remaining)) != 0 {
		return errors.New("expected a single PKCS#8 PEM private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return errors.New("invalid signing private key")
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return errors.New("the signing key must use Ed25519")
	}
	claimsRaw, err := os.ReadFile(*claimsFile)
	if err != nil {
		return err
	}
	if len(claimsRaw) > 16384 {
		return errors.New("claims file is too large")
	}
	var claims licensing.Claims
	decoder := json.NewDecoder(bytes.NewReader(claimsRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&claims); err != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid claims JSON")
	}
	encoded, err := licensing.Sign(claims, *keyID, key, time.Now().UTC())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, encoded)
	return err
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
