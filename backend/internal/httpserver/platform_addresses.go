package httpserver

import (
	"net/http"
	"strings"
)

func (r *Router) publicBaseURL(req *http.Request) string {
	if r.cfg.PublicBaseURL != "" {
		return r.cfg.PublicBaseURL
	}
	if r.cfg.BaseURL != "" {
		return r.cfg.BaseURL
	}
	proto := req.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
	}
	host := req.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = req.Host
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func (r *Router) agentWSEndpoint(req *http.Request) string {
	if r.cfg.AgentWSEndpoint != "" {
		return r.cfg.AgentWSEndpoint
	}
	base := r.publicBaseURL(req)
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://") + "/ws/agent"
	}
	return "ws://" + strings.TrimPrefix(base, "http://") + "/ws/agent"
}

func (r *Router) registryHost() string {
	registry := strings.TrimSpace(r.cfg.ImageRegistry)
	if registry == "" {
		registry = strings.TrimSpace(r.cfg.AgentImage)
	}
	registry = strings.TrimPrefix(strings.TrimPrefix(registry, "https://"), "http://")
	if registry == "" {
		return ""
	}
	host := strings.Split(registry, "/")[0]
	return host
}
