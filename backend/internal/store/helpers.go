package store

import "strings"

func defaultSMTPName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Default SMTP"
	}
	return strings.TrimSpace(name)
}

func isTerminalPlatformUpgradeStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled" || status == "rolled_back"
}

func isTerminalMigrationState(state string) bool {
	switch state {
	case "committed", "rolled-back", "failed", "revoked", "expired":
		return true
	default:
		return false
	}
}

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func isActiveStatus(status string) bool {
	switch status {
	case "queued", "dispatched", "accepted", "running", "syncing", "finalizing", "canceling":
		return true
	default:
		return false
	}
}
