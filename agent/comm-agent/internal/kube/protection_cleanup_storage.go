package kube

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/minio/minio-go/v7"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
)

func IsObjectStorageCleanupError(err error) bool {
	if err == nil {
		return false
	}
	var networkError net.Error
	if errors.As(err, &networkError) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch minio.ToErrorResponse(err).Code {
	case "NoSuchBucket", "NoSuchKey", "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch", "RequestTimeout", "ServiceUnavailable", "SlowDown", "InternalError":
		return true
	}
	return false
}

func (a *DynamicManifestApplier) WaitForVeleroBackupRecordDeleted(ctx context.Context, namespace, name string) error {
	resource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}).Namespace(namespace)
	err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := resource.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		return fmt.Errorf("local backup record %q was not removed: %w", name, err)
	}
	return nil
}

func (a *DynamicManifestApplier) CheckProtectionCleanupStorage(ctx context.Context, namespace, storageLocation string) error {
	bsl, err := a.readBackupStorageLocation(ctx, namespace, storageLocation)
	if err != nil {
		return err
	}
	if bsl.Provider != "" && bsl.Provider != "aws" {
		return fmt.Errorf("backup object cleanup only supports aws-compatible storage, got %s", bsl.Provider)
	}
	creds, err := a.readS3Credentials(ctx, namespace, bsl.Credential)
	if err != nil {
		return err
	}
	client, err := newObjectStoreClient(bsl, creds)
	if err != nil {
		return err
	}
	exists, err := client.BucketExists(ctx, bsl.Bucket)
	if err != nil {
		return err
	}
	if !exists {
		return minio.ErrorResponse{Code: "NoSuchBucket", Message: fmt.Sprintf("Backup bucket %q does not exist. Use force cleanup to remove the configuration and local backup records.", bsl.Bucket), BucketName: bsl.Bucket, StatusCode: 404}
	}
	return nil
}
