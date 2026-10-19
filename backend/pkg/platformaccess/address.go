// Package platformaccess validates and persists the platform's canonical origin.
package platformaccess

import (
	"errors"
	"hypercdr-platform/platform/backend/internal/store"
	"net"
	"net/url"
	"strings"
)

func Normalize(value string) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(value), "/"))
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("Enter an HTTPS origin, for example https://app.example.com (without a path, query or credentials).")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())) {
		return "", errors.New("Use HTTPS; HTTP is allowed only for localhost development.")
	}
	if strings.ContainsAny(u.Host, "\\\r\n\t ") || strings.HasSuffix(u.Host, ":") {
		return "", errors.New("Invalid host or port.")
	}
	if p := u.Port(); p != "" {
		var n int
		for _, c := range p {
			if c < '0' || c > '9' {
				return "", errors.New("Invalid port.")
			}
			n = n*10 + int(c-'0')
			if n > 65535 {
				return "", errors.New("Invalid port.")
			}
		}
		if n == 0 {
			return "", errors.New("Invalid port.")
		}
	}
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

// Save updates only the origin, retaining registry, namespace and instance identity.
func Save(repo store.Store, value string) (string, error) {
	origin, err := Normalize(value)
	if err != nil {
		return "", err
	}
	s, found, err := repo.GetPlatformSettings()
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("Platform settings are not initialized; start the platform once before configuring its address.")
	}
	_, err = repo.UpsertPlatformSettings(store.PlatformSettingsInput{InstanceID: s.InstanceID, ImageRegistry: s.ImageRegistry, AgentNamespace: s.AgentNamespace, VeleroVersion: s.VeleroVersion, PublicEndpoint: origin})
	return origin, err
}
