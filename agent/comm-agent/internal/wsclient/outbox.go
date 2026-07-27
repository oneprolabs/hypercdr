package wsclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type eventOutbox struct {
	mu    sync.Mutex
	path  string
	items map[string]json.RawMessage
}

type taskLedger struct {
	mu         sync.Mutex
	path       string
	TaskIDs    map[string]struct{}         `json:"taskIds"`
	CommandIDs map[string]struct{}         `json:"commandIds"`
	PlanIDs    map[string]struct{}         `json:"planIds"`
	Records    map[string]taskLedgerRecord `json:"records,omitempty"`
}

type taskLedgerRecord struct {
	TaskID        string           `json:"taskId"`
	CommandID     string           `json:"commandId,omitempty"`
	Type          string           `json:"type"`
	PlanID        string           `json:"planId,omitempty"`
	Task          json.RawMessage  `json:"task,omitempty"`
	Object        taskLedgerObject `json:"object"`
	TerminalAcked bool             `json:"terminalAcked,omitempty"`
	CreatedAt     string           `json:"createdAt,omitempty"`
	UpdatedAt     string           `json:"updatedAt,omitempty"`
}

type taskLedgerObject struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
}

func newEventOutbox(stateDir string) (*eventOutbox, error) {
	if stateDir == "" {
		stateDir = "/var/lib/hypercdr-agent"
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	box := &eventOutbox{
		path:  filepath.Join(stateDir, "event-outbox.json"),
		items: map[string]json.RawMessage{},
	}
	if data, err := os.ReadFile(box.path); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &box.items)
	}
	return box, nil
}

func newTaskLedger(stateDir string) (*taskLedger, error) {
	if stateDir == "" {
		stateDir = "/var/lib/hypercdr-agent"
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	ledger := &taskLedger{
		path:       filepath.Join(stateDir, "task-ledger.json"),
		TaskIDs:    map[string]struct{}{},
		CommandIDs: map[string]struct{}{},
		PlanIDs:    map[string]struct{}{},
		Records:    map[string]taskLedgerRecord{},
	}
	if data, err := os.ReadFile(ledger.path); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, ledger)
	}
	ledger.ensureMaps()
	return ledger, nil
}

func (l *taskLedger) remember(taskID string, commandID string, planID string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureMaps()
	if taskID != "" {
		l.TaskIDs[taskID] = struct{}{}
	}
	if commandID != "" {
		l.CommandIDs[commandID] = struct{}{}
	}
	if planID != "" {
		l.PlanIDs[planID] = struct{}{}
	}
	return l.flushLocked()
}

func (l *taskLedger) hasTask(taskID string) bool {
	if l == nil || taskID == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.TaskIDs[taskID]
	return ok
}

func (l *taskLedger) hasCommand(commandID string) bool {
	if l == nil || commandID == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.CommandIDs[commandID]
	return ok
}

func (l *taskLedger) hasPlan(planID string) bool {
	if l == nil || planID == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.PlanIDs[planID]
	return ok
}

func (l *taskLedger) ensureMaps() {
	if l.TaskIDs == nil {
		l.TaskIDs = map[string]struct{}{}
	}
	if l.CommandIDs == nil {
		l.CommandIDs = map[string]struct{}{}
	}
	if l.PlanIDs == nil {
		l.PlanIDs = map[string]struct{}{}
	}
	if l.Records == nil {
		l.Records = map[string]taskLedgerRecord{}
	}
}

func (l *taskLedger) recordTask(taskID string, commandID string, planID string, task any, object taskLedgerObject) error {
	if l == nil || taskID == "" {
		return nil
	}
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureMaps()
	now := nowRFC3339()
	record := l.Records[taskID]
	if record.CreatedAt == "" {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	record.TaskID = taskID
	record.CommandID = commandID
	record.PlanID = planID
	record.Task = raw
	record.Object = object
	var meta struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &meta)
	record.Type = meta.Type
	record.TerminalAcked = false
	l.Records[taskID] = record
	if taskID != "" {
		l.TaskIDs[taskID] = struct{}{}
	}
	if commandID != "" {
		l.CommandIDs[commandID] = struct{}{}
	}
	if planID != "" {
		l.PlanIDs[planID] = struct{}{}
	}
	return l.flushLocked()
}

func (l *taskLedger) markTerminalAcked(taskID string) error {
	if l == nil || taskID == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureMaps()
	record, ok := l.Records[taskID]
	if !ok {
		return nil
	}
	record.TerminalAcked = true
	record.UpdatedAt = nowRFC3339()
	l.Records[taskID] = record
	return l.flushLocked()
}

func (l *taskLedger) recoverableRecords() []taskLedgerRecord {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensureMaps()
	records := make([]taskLedgerRecord, 0, len(l.Records))
	for _, record := range l.Records {
		if record.TerminalAcked || len(record.Task) == 0 || record.Object.Kind == "" || record.Object.Name == "" {
			continue
		}
		records = append(records, record)
	}
	return records
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (l *taskLedger) flushLocked() error {
	tmp := l.path + ".tmp"
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

func (o *eventOutbox) add(message any) error {
	if o == nil {
		return nil
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	var meta struct {
		MessageID string `json:"messageId"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return err
	}
	if meta.MessageID == "" {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.items[meta.MessageID] = raw
	return o.flushLocked()
}

func (o *eventOutbox) remove(messageID string) error {
	if o == nil || messageID == "" {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.items, messageID)
	return o.flushLocked()
}

func (o *eventOutbox) list() []json.RawMessage {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	items := make([]json.RawMessage, 0, len(o.items))
	for _, raw := range o.items {
		copyRaw := append(json.RawMessage(nil), raw...)
		items = append(items, copyRaw)
	}
	return items
}

func (o *eventOutbox) hasTask(taskID string) bool {
	if o == nil || taskID == "" {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, raw := range o.items {
		var meta struct {
			Payload struct {
				TaskID string `json:"taskId"`
			} `json:"payload"`
		}
		if json.Unmarshal(raw, &meta) == nil && meta.Payload.TaskID == taskID {
			return true
		}
	}
	return false
}

func (o *eventOutbox) flushLocked() error {
	tmp := o.path + ".tmp"
	data, err := json.MarshalIndent(o.items, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, o.path)
}
