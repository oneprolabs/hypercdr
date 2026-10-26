package store

import (
	"bytes"
	"encoding/json"
	"errors"
)

var ErrBackupResultConflict = errors.New("backup result conflicts with accepted execution result")
var ErrBackupResultIncomplete = errors.New("backup result is incomplete")

func backupMetricsEqual(first, second map[string]any) bool {
	firstJSON, firstErr := json.Marshal(first)
	secondJSON, secondErr := json.Marshal(second)
	return firstErr == nil && secondErr == nil && bytes.Equal(firstJSON, secondJSON)
}

func CompleteBackupMetrics(metrics map[string]any) bool {
	raw, err := json.Marshal(metrics)
	if err != nil {
		return false
	}
	var values struct {
		SchemaVersion int `json:"schemaVersion"`
		Logical       struct {
			Known      bool  `json:"known"`
			TotalBytes int64 `json:"totalBytes"`
		} `json:"logical"`
		NewData struct {
			Known      bool  `json:"known"`
			TotalBytes int64 `json:"totalBytes"`
		} `json:"newData"`
	}
	if json.Unmarshal(raw, &values) != nil {
		return false
	}
	return values.SchemaVersion == 2 && values.Logical.Known && values.Logical.TotalBytes > 0 && values.NewData.Known && values.NewData.TotalBytes >= 0
}
