package wsclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type capacityCheckpoint struct {
	TaskID    string                      `json:"taskId"`
	CommandID string                      `json:"commandId"`
	Report    protocol.PlanCapacityReport `json:"report"`
}

func (c *Client) executePlanCapacityTask(task protocol.TaskDispatchPayload) {
	c.capacityMu.Lock()
	defer c.capacityMu.Unlock()
	command := task.PlanCapacity
	if command == nil || len(command.Plans) == 0 || command.Namespace == "" {
		_ = c.sendTaskFailed(task, "CAPACITY_COMMAND_INVALID", "Capacity collection requires a non-empty plan list and namespace")
		return
	}
	path := filepath.Join(c.cfg.StateDir, "plan-capacity-result.json")
	var checkpoint capacityCheckpoint
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &checkpoint); err != nil {
			_ = c.sendTaskFailed(task, "CAPACITY_STATE_INVALID", err.Error())
			return
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = c.sendTaskFailed(task, "CAPACITY_STATE_UNAVAILABLE", err.Error())
		return
	}
	if checkpoint.TaskID == task.TaskID && checkpoint.CommandID == task.CommandID {
		_ = c.sendCapacityReport(task, checkpoint.Report)
		return
	}
	if c.cfg.StateDir == "" {
		_ = c.sendTaskFailed(task, "CAPACITY_STATE_REQUIRED", "Persistent agent state is required for capacity collection")
		return
	}
	reader, ok := c.statsReader.(interface {
		CollectPlanCapacity(context.Context, string, []kube.PlanCapacityTarget) []kube.PlanCapacityResult
	})
	if !ok {
		_ = c.sendTaskFailed(task, "CAPACITY_READER_UNAVAILABLE", "Capacity collection is unavailable")
		return
	}
	if err := c.sendTaskAccepted(task); err != nil {
		return
	}
	plans := make([]kube.PlanCapacityTarget, 0, len(command.Plans))
	report := protocol.PlanCapacityReport{}
	for _, plan := range command.Plans {
		if plan.PlanID != "" && plan.ErrorMessage != "" {
			report.Plans = append(report.Plans, protocol.PlanCapacityResult{PlanID: plan.PlanID, Status: "failed", CollectedAt: time.Now().UTC(), ErrorMessage: plan.ErrorMessage})
			continue
		}
		if plan.PlanID == "" || plan.StorageLocation == "" || len(plan.Namespaces) == 0 {
			_ = c.sendTaskFailed(task, "CAPACITY_COMMAND_INVALID", "Plan identity, storage location and namespaces are required")
			return
		}
		plans = append(plans, kube.PlanCapacityTarget{PlanID: plan.PlanID, StorageLocation: plan.StorageLocation, BackupNamePrefix: backupNamePrefixForPlan(plan.PlanID), Namespaces: plan.Namespaces})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	for _, result := range reader.CollectPlanCapacity(ctx, command.Namespace, plans) {
		item := protocol.PlanCapacityResult{PlanID: result.PlanID, Status: "succeeded", MetadataBytes: result.Stats.MetadataBytes, KopiaBytes: result.Stats.KopiaBytes, TotalBytes: result.Stats.TotalBytes, CollectedAt: result.CollectedAt}
		if result.Error != nil {
			item.Status = "failed"
			item.ErrorMessage = result.Error.Error()
			item.MetadataBytes, item.KopiaBytes, item.TotalBytes = 0, 0, 0
		}
		report.Plans = append(report.Plans, item)
	}
	checkpoint = capacityCheckpoint{TaskID: task.TaskID, CommandID: task.CommandID, Report: report}
	data, err = json.Marshal(checkpoint)
	if err == nil {
		err = os.MkdirAll(c.cfg.StateDir, 0700)
	}
	if err == nil {
		err = os.WriteFile(path+".tmp", data, 0600)
	}
	if err == nil {
		err = os.Rename(path+".tmp", path)
	}
	if err != nil {
		_ = c.sendTaskFailed(task, "CAPACITY_STATE_WRITE_FAILED", err.Error())
		return
	}
	_ = c.sendCapacityReport(task, report)
}

func (c *Client) sendCapacityReport(task protocol.TaskDispatchPayload, report protocol.PlanCapacityReport) error {
	message := protocol.NewMessage(protocol.MessageKindEvent, protocol.MessageAgentTaskCompleted, c.cfg.ClusterID, c.cfg.AgentID, protocol.TaskCompletedPayload{AckRequired: true, TaskID: task.TaskID, CommandID: task.CommandID, Status: "succeeded", Operation: task.Type, Progress: 100, Message: "Plan capacity collection completed", PlanCapacity: &report})
	return c.sendReliableEvent(message)
}
