package wsclient

import (
	"fmt"
	"strings"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func restoreServiceNodePortMappings(task protocol.TaskDispatchPayload) (map[string]map[string]int, error) {
	groups := map[string]map[string]int{}
	if task.Restore == nil {
		return groups, nil
	}
	for key, port := range task.Restore.ServiceNodePortMappings {
		namespace := restoreTargetNamespace(task)
		if source, service, qualified := strings.Cut(key, "/"); qualified {
			selected := source == task.Restore.SourceNamespace
			if len(task.Restore.SourceNamespaces) > 0 {
				selected = false
				for _, candidate := range task.Restore.SourceNamespaces {
					selected = selected || candidate == source
				}
			}
			if !selected || service == "" || strings.Contains(service, "/") {
				return nil, fmt.Errorf("invalid namespace in Service port mapping %q", key)
			}
			namespace = source
			if target := task.Restore.TargetNamespaces[source]; target != "" {
				namespace = target
			} else if len(task.Restore.SourceNamespaces) == 1 && task.Restore.TargetNamespace != "" {
				namespace = task.Restore.TargetNamespace
			}
			key = service
		}
		if groups[namespace] == nil {
			groups[namespace] = map[string]int{}
		}
		groups[namespace][key] = port
	}
	return groups, nil
}
