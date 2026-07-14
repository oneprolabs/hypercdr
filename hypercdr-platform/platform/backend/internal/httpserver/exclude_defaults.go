package httpserver

import "hypercdr-platform/platform/backend/internal/protocol"

// defaultExcludedResources intentionally returns no implicit exclusions.
// HyperCDR keeps backups complete by default. If a restore target lacks a
// CRD/API needed by a captured resource, the restore should fail with a clear
// compatibility error instead of silently skipping that resource.
func defaultExcludedResources(sourceNamespace string) []protocol.ExcludeRule {
	return nil
}

func defaultExcludedResourcesForNamespaces(sourceNamespaces []string) []protocol.ExcludeRule {
	seen := map[protocol.ExcludeRule]struct{}{}
	var rules []protocol.ExcludeRule
	for _, namespace := range sourceNamespaces {
		for _, rule := range defaultExcludedResources(namespace) {
			if _, ok := seen[rule]; ok {
				continue
			}
			seen[rule] = struct{}{}
			rules = append(rules, rule)
		}
	}
	return rules
}
