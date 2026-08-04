package kube

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const maxBackupContentsBytes = 64 << 20

func (a *DynamicManifestApplier) ReadVeleroBackupContents(ctx context.Context, namespace string, backupName string, limit int) ([]BackupContentResource, bool, error) {
	if namespace == "" || backupName == "" {
		return nil, false, errors.New("Velero namespace and backup name are required")
	}
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	resource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "downloadrequests"}).Namespace(namespace)
	name := fmt.Sprintf("hypercdr-contents-%d", time.Now().UnixNano())
	request := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "velero.io/v1", "kind": "DownloadRequest",
		"metadata": map[string]any{"name": name, "namespace": namespace},
		"spec":     map[string]any{"target": map[string]any{"kind": "BackupContents", "name": backupName}},
	}}
	if _, err := resource.Create(ctx, request, metav1.CreateOptions{}); err != nil {
		return nil, false, fmt.Errorf("create Velero backup contents request: %w", err)
	}
	defer func() { _ = resource.Delete(context.Background(), name, metav1.DeleteOptions{}) }()

	var downloadURL string
	for downloadURL == "" {
		select {
		case <-ctx.Done():
			return nil, false, fmt.Errorf("wait for Velero backup contents URL: %w", ctx.Err())
		case <-time.After(400 * time.Millisecond):
		}
		current, err := resource.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, false, fmt.Errorf("read Velero backup contents request: %w", err)
		}
		downloadURL, _, _ = unstructured.NestedString(current.Object, "status", "downloadURL")
		phase, _, _ := unstructured.NestedString(current.Object, "status", "phase")
		if strings.EqualFold(phase, "Processed") && downloadURL == "" {
			return nil, false, errors.New("Velero did not return a backup contents URL")
		}
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, false, err
	}
	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		return nil, false, fmt.Errorf("download Velero backup contents: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, fmt.Errorf("download Velero backup contents: HTTP %s", response.Status)
	}
	gz, err := gzip.NewReader(io.LimitReader(response.Body, maxBackupContentsBytes))
	if err != nil {
		return nil, false, fmt.Errorf("open Velero backup archive: %w", err)
	}
	defer gz.Close()

	items := make([]BackupContentResource, 0)
	// The canonical resource catalog is the same one consumed by Velero's
	// archive.Parser: resources/<group-resource>/{cluster,namespaces}/....
	// Version directories contain alternate representations used during
	// restore version negotiation; they are not additional resources.
	seenResources := map[string]struct{}{}
	truncated := false
	tarReader := tar.NewReader(gz)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("read Velero backup archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		archiveItem, role := classifyVeleroArchivePath(header.Name)
		if role != veleroArchiveCanonicalResource {
			continue
		}
		if header.Size <= 0 || header.Size > 8<<20 {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tarReader, 8<<20))
		if err != nil {
			return nil, false, err
		}
		var object map[string]any
		if json.Unmarshal(data, &object) != nil {
			continue
		}
		apiVersion, _ := object["apiVersion"].(string)
		kind, _ := object["kind"].(string)
		metadata, _ := object["metadata"].(map[string]any)
		nameValue, _ := metadata["name"].(string)
		namespaceValue, _ := metadata["namespace"].(string)
		if apiVersion == "" || kind == "" || nameValue == "" {
			continue
		}
		group, resourceName := splitVeleroGroupResource(archiveItem.groupResource)
		identity := strings.Join([]string{apiVersion, kind, namespaceValue, nameValue}, "\x00")
		if _, exists := seenResources[identity]; exists {
			continue
		}
		seenResources[identity] = struct{}{}
		images, storageClasses := resourceReferences(object)
		items = append(items, BackupContentResource{APIVersion: apiVersion, Kind: kind, Namespace: namespaceValue, Name: nameValue, Group: group, Resource: resourceName, ClusterScoped: archiveItem.clusterScoped, Images: images, StorageClasses: storageClasses})
		if len(items) >= limit {
			truncated = true
			break
		}
	}
	return items, truncated, nil
}

type veleroArchiveRole uint8

const (
	veleroArchiveOther veleroArchiveRole = iota
	veleroArchiveCanonicalResource
	veleroArchiveVersionRepresentation
)

type veleroArchiveItem struct {
	groupResource string
	namespace     string
	clusterScoped bool
	version       string
	preferred     bool
}

// classifyVeleroArchivePath models the Velero archive layout rather than
// inferring resources from arbitrary JSON files in the tarball.
func classifyVeleroArchivePath(name string) (veleroArchiveItem, veleroArchiveRole) {
	parts := strings.Split(strings.Trim(name, "/"), "/")
	for index, part := range parts {
		if part != "resources" || index+3 >= len(parts) {
			continue
		}
		item := veleroArchiveItem{groupResource: parts[index+1]}
		remainder := parts[index+2:]
		if len(remainder) == 2 && remainder[0] == "cluster" && isVeleroResourceFile(remainder[1]) {
			item.clusterScoped = true
			return item, veleroArchiveCanonicalResource
		}
		if len(remainder) == 3 && remainder[0] == "namespaces" && remainder[1] != "" && isVeleroResourceFile(remainder[2]) {
			item.namespace = remainder[1]
			return item, veleroArchiveCanonicalResource
		}
		// Version representations repeat the same scope layout below a
		// version directory. Record their role, but never add them to the
		// Restore Content resource catalog.
		if len(remainder) >= 3 {
			version := remainder[0]
			item.preferred = strings.HasSuffix(version, "-preferredversion")
			item.version = strings.TrimSuffix(version, "-preferredversion")
			versionScope := remainder[1:]
			if (len(versionScope) == 2 && versionScope[0] == "cluster" && isVeleroResourceFile(versionScope[1])) ||
				(len(versionScope) == 3 && versionScope[0] == "namespaces" && versionScope[1] != "" && isVeleroResourceFile(versionScope[2])) {
				item.clusterScoped = versionScope[0] == "cluster"
				if !item.clusterScoped {
					item.namespace = versionScope[1]
				}
				return item, veleroArchiveVersionRepresentation
			}
		}
		return veleroArchiveItem{}, veleroArchiveOther
	}
	return veleroArchiveItem{}, veleroArchiveOther
}

func isVeleroResourceFile(name string) bool {
	return strings.HasSuffix(name, ".json")
}

func splitVeleroGroupResource(groupResource string) (string, string) {
	resource, group, found := strings.Cut(groupResource, ".")
	if !found {
		group = ""
	}
	return group, resource
}

func resourceReferences(object map[string]any) ([]string, []string) {
	images, storage := map[string]struct{}{}, map[string]struct{}{}
	var walk func(any, string)
	walk = func(value any, key string) {
		switch typed := value.(type) {
		case map[string]any:
			for childKey, child := range typed {
				walk(child, childKey)
			}
		case []any:
			for _, child := range typed {
				walk(child, key)
			}
		case string:
			trimmed := strings.TrimSpace(typed)
			if trimmed == "" {
				return
			}
			if key == "image" {
				images[trimmed] = struct{}{}
			}
			if key == "storageClassName" {
				storage[trimmed] = struct{}{}
			}
		}
	}
	// Only spec is restored as desired configuration. Pod status may contain a
	// different runtime-normalized image name (and imageID), which is not a
	// field users can map during restore and must not become a mapping option.
	if spec, ok := object["spec"]; ok {
		walk(spec, "spec")
	}
	imageList, storageList := make([]string, 0, len(images)), make([]string, 0, len(storage))
	for value := range images {
		imageList = append(imageList, value)
	}
	for value := range storage {
		storageList = append(storageList, value)
	}
	slices.Sort(imageList)
	slices.Sort(storageList)
	return imageList, storageList
}
