package cacheusage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type PathUsage struct {
	Path       string `json:"path"`
	Usage      *Usage `json:"usage,omitempty"`
	Error      string `json:"error,omitempty"`
	ObservedAt string `json:"observedAt,omitempty"`
}

func Run(ctx context.Context, paths []string, output io.Writer) error {
	if len(paths) < 1 || len(paths) > 2 {
		return fmt.Errorf("cache-usage requires one or two directories")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	results := make([]PathUsage, 0, len(paths))
	for _, path := range paths {
		result := PathUsage{Path: path}
		usage, err := Scan(ctx, path)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Usage = &usage
			result.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		results = append(results, result)
	}
	return json.NewEncoder(output).Encode(results)
}
