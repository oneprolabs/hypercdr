package kube

import (
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestBucketLookupFromConfig(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]string
		want   minio.BucketLookupType
	}{
		{name: "unset uses auto", config: map[string]string{}, want: minio.BucketLookupAuto},
		{name: "path style true", config: map[string]string{"s3ForcePathStyle": "true"}, want: minio.BucketLookupPath},
		{name: "virtual host false", config: map[string]string{"s3ForcePathStyle": "false"}, want: minio.BucketLookupDNS},
		{name: "invalid uses auto", config: map[string]string{"s3ForcePathStyle": "invalid"}, want: minio.BucketLookupAuto},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bucketLookupFromConfig(tt.config); got != tt.want {
				t.Fatalf("bucket lookup = %v, want %v", got, tt.want)
			}
		})
	}
}
