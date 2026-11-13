package cacheusage

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestCommandKeepsEachCacheResultIndependent(t *testing.T) {
	var output bytes.Buffer
	if err := Run(context.Background(), []string{t.TempDir(), "/"}, &output); err != nil {
		t.Fatal(err)
	}
	var results []PathUsage
	if err := json.Unmarshal(output.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Usage == nil || results[0].Error != "" || results[1].Usage != nil || results[1].Error == "" {
		t.Fatalf("incorrect results: %#v", results)
	}
	if err := Run(context.Background(), nil, &output); err == nil {
		t.Fatal("missing paths were accepted")
	}
}
