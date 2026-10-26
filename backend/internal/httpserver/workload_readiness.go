package httpserver

import "strings"

func readinessEventLevel(payload map[string]any) string {
	if stringFromMap(payload, "readinessObservationError") != "" {
		return "warning"
	}
	for _, raw := range sliceFromAny(mapFromAny(payload["readiness"])["Objects"]) {
		object := mapFromAny(raw)
		if state := stringFromMap(object, "State"); state == "Ready" || state == "Bound" {
			continue
		}
		reason := strings.ToLower(stringFromMap(object, "Reason"))
		for _, marker := range []string{"backoff", "failed", "error", "unschedulable", "unhealthy"} {
			if strings.Contains(reason, marker) {
				return "warning"
			}
		}
	}
	return "info"
}
