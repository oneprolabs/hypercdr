package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrDataProtectionSettings = errors.New("invalid data protection settings")

const LiveFilesystemRiskAcknowledgement = "live-filesystem-no-application-consistency-v1"

func validateDataProtectionRisk(settings DataProtectionSettings) error {
	if settings.RiskAcceptance != nil && (settings.RiskAcknowledgement != LiveFilesystemRiskAcknowledgement || settings.Mode != "live-filesystem" || settings.RiskAcceptance.ActorID == "" || settings.RiskAcceptance.AcceptedAt.IsZero()) {
		return fmt.Errorf("%w: incomplete risk acceptance audit", ErrDataProtectionSettings)
	}
	if settings.RiskAcknowledgement != "" && (settings.Mode != "live-filesystem" || settings.RiskAcknowledgement != LiveFilesystemRiskAcknowledgement) {
		return fmt.Errorf("%w: invalid file-level risk acknowledgement", ErrDataProtectionSettings)
	}
	return nil
}

func snapshotTaskDataProtection(tx *sql.Tx, input *TaskInput) error {
	if input.ProtectionPlanID == "" || (input.Type != "backup" && input.Type != "schedule-sync") {
		return nil
	}
	var raw []byte
	if err := tx.QueryRow(`select data_protection from protection_plans where id=$1`, input.ProtectionPlanID).Scan(&raw); err != nil {
		return err
	}
	var plan ProtectionPlan
	if err := decodePlanDataProtection(raw, &plan); err != nil {
		return err
	}
	if err := validateCSIOnlyProtection(plan.DataProtection); err != nil {
		return err
	}
	input.Payload["dataProtectionMode"] = plan.DataProtection.Mode
	input.Payload["dataProtectionRiskAcknowledgement"] = plan.DataProtection.RiskAcknowledgement
	input.Payload["dataProtectionRiskAcceptance"] = plan.DataProtection.RiskAcceptance
	input.Payload["consistencyGroup"] = nil
	if plan.DataProtection.Group != nil {
		encoded, err := json.Marshal(plan.DataProtection.Group)
		if err != nil {
			return err
		}
		var group map[string]any
		if err := json.Unmarshal(encoded, &group); err != nil {
			return err
		}
		input.Payload["consistencyGroup"] = group
	}
	input.Payload["dataProtectionSnapshotVersion"] = 1
	return nil
}

func validatePlanDataProtection(tx *sql.Tx, plan *ProtectionPlan) ([]byte, error) {
	settings := &plan.DataProtection
	if err := validateCSIOnlyProtection(*settings); err != nil {
		return nil, err
	}
	if settings.Mode == "" {
		settings.Mode = "live-filesystem"
	}
	if err := validateDataProtectionRisk(*settings); err != nil {
		return nil, err
	}
	switch settings.Mode {
	case "live-filesystem", "snapshot-crash-consistent":
		if settings.Group != nil {
			return nil, fmt.Errorf("%w: groups require application-consistent mode", ErrDataProtectionSettings)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported mode %q", ErrDataProtectionSettings, settings.Mode)
	}
	return json.Marshal(settings)
}

func validateCSIOnlyProtection(settings DataProtectionSettings) error {
	if settings.Mode == "application-consistent" || settings.Group != nil {
		return fmt.Errorf("%w: application stop/freeze coordination is no longer supported; explicitly select CSI snapshots", ErrDataProtectionSettings)
	}
	return nil
}

func decodePlanDataProtection(data []byte, plan *ProtectionPlan) error {
	if err := json.Unmarshal(data, &plan.DataProtection); err != nil {
		return fmt.Errorf("invalid persisted data protection configuration: %w", err)
	}
	if err := validateDataProtectionRisk(plan.DataProtection); err != nil {
		return err
	}
	switch plan.DataProtection.Mode {
	case "live-filesystem", "snapshot-crash-consistent":
		if plan.DataProtection.Group == nil {
			return nil
		}
	case "application-consistent":
		if plan.DataProtection.Group != nil {
			group := plan.DataProtection.Group
			var namespaces []string
			for _, workload := range group.Workloads {
				namespaces = append(namespaces, workload.Namespace)
			}
			for _, volume := range group.Volumes {
				namespaces = append(namespaces, volume.Namespace)
			}
			if err := group.Validate(namespaces); err != nil {
				return fmt.Errorf("invalid persisted consistency group: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("invalid persisted data protection configuration; refusing legacy fallback")
}
