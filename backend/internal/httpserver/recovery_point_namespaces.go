package httpserver

import "hypercdr-platform/platform/backend/internal/store"

func recoveryPointNamespaces(point store.RestorePoint) []string {
	velero := mapFromAny(point.Metadata["velero"])
	manifest := mapFromAny(velero["manifest"])
	spec := mapFromAny(manifest["spec"])
	for _, value := range []any{spec["includedNamespaces"], point.Metadata["includedNamespaces"], velero["includedNamespaces"]} {
		if namespaces := uniqueNonEmptyStrings(stringArrayFromAny(value)); len(namespaces) > 0 {
			return namespaces
		}
	}
	return uniqueNonEmptyStrings([]string{point.SourceNamespace})
}
