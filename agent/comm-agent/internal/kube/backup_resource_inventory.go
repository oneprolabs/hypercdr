package kube

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	maxBackupInventoryObjects = 50000
	maxBackupObjectBytes      = 8 << 20
)

type BackupResourceObject struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Namespace  string         `json:"namespace,omitempty"`
	Name       string         `json:"name"`
	Object     map[string]any `json:"object,omitempty"`
}

type BackupResourceInventory struct {
	BackupName string                 `json:"backupName"`
	Objects    []BackupResourceObject `json:"objects"`
	Complete   bool                   `json:"complete"`
}

func (a *DynamicManifestApplier) GetBackupResourceInventory(ctx context.Context, namespace, storageLocation, backupName string) (BackupResourceInventory, error) {
	bsl, err := a.readBackupStorageLocation(ctx, namespace, storageLocation)
	if err != nil {
		return BackupResourceInventory{}, err
	}
	if bsl.Provider != "" && bsl.Provider != "aws" {
		return BackupResourceInventory{}, fmt.Errorf("backup resource inventory only supports aws-compatible storage, got %s", bsl.Provider)
	}
	creds, err := a.readS3Credentials(ctx, namespace, bsl.Credential)
	if err != nil {
		return BackupResourceInventory{}, err
	}
	endpoint, secure, err := normalizeObjectStoreEndpoint(bsl.Endpoint, bsl.Secure)
	if err != nil {
		return BackupResourceInventory{}, err
	}
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""), Secure: secure, Region: bsl.Region})
	if err != nil {
		return BackupResourceInventory{}, err
	}
	key := backupObjectPrefix(bsl.Prefix, backupName) + backupName + ".tar.gz"
	object, err := client.GetObject(ctx, bsl.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return BackupResourceInventory{}, err
	}
	defer object.Close()
	items, err := readBackupResourceArchive(object)
	if err != nil {
		return BackupResourceInventory{}, fmt.Errorf("parse Velero backup %s: %w", backupName, err)
	}
	return BackupResourceInventory{BackupName: backupName, Objects: items, Complete: true}, nil
}

func readBackupResourceArchive(source io.Reader) ([]BackupResourceObject, error) {
	gz, err := gzip.NewReader(source)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	items := make([]BackupResourceObject, 0)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg || !strings.HasSuffix(header.Name, ".json") || !(strings.HasPrefix(header.Name, "resources/") || strings.Contains(header.Name, "/resources/")) {
			continue
		}
		if header.Size <= 0 || header.Size > maxBackupObjectBytes {
			return nil, fmt.Errorf("backup object %q has invalid size %d", header.Name, header.Size)
		}
		var object map[string]any
		if err := json.NewDecoder(io.LimitReader(archive, maxBackupObjectBytes)).Decode(&object); err != nil {
			return nil, fmt.Errorf("decode %q: %w", header.Name, err)
		}
		apiVersion, _ := object["apiVersion"].(string)
		kind, _ := object["kind"].(string)
		metadata, _ := object["metadata"].(map[string]any)
		name, _ := metadata["name"].(string)
		namespace, _ := metadata["namespace"].(string)
		if apiVersion == "" || kind == "" || name == "" {
			return nil, fmt.Errorf("backup object %q is missing apiVersion, kind, or metadata.name", header.Name)
		}
		items = append(items, BackupResourceObject{APIVersion: apiVersion, Kind: kind, Namespace: namespace, Name: name, Object: object})
		if len(items) > maxBackupInventoryObjects {
			return nil, fmt.Errorf("backup contains more than %d objects", maxBackupInventoryObjects)
		}
	}
	return items, nil
}
