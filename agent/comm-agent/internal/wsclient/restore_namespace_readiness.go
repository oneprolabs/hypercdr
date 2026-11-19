package wsclient

import (
	"context"
	"fmt"
	"strings"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func (c *Client) restoredNamespacesReadiness(ctx context.Context, task protocol.TaskDispatchPayload) (kube.NamespaceReadiness, error) {
	command := task.Restore
	if command == nil {
		return kube.NamespaceReadiness{}, fmt.Errorf("restore command is required")
	}
	sources := command.SourceNamespaces
	if len(sources) == 0 {
		sources = []string{command.SourceNamespace}
	}
	result := kube.NamespaceReadiness{Ready: true, NamespacePhase: "Active"}
	targets := []string{}
	seen := map[string]bool{}
	legacyMissing := map[string]bool{}
	foundPVCs := map[string]bool{}
	for index, source := range sources {
		target := source
		if mapped := command.TargetNamespaces[source]; mapped != "" {
			target = mapped
		} else if len(command.TargetNamespaces) == 0 && index == 0 && command.TargetNamespace != "" {
			target = command.TargetNamespace
		}
		if strings.TrimSpace(target) == "" || seen[target] {
			return result, fmt.Errorf("invalid or duplicate readiness namespace %q", target)
		}
		seen[target] = true
		targets = append(targets, target)
		expectations := kube.ReadinessExpectations{Known: command.ReadinessExpectationsKnown, RuntimeWorkloadsExpected: command.RuntimeWorkloadsExpected, PVCNames: command.ExpectedPVCs}
		scoped, scopedKnown := command.NamespaceReadiness[source]
		if scopedKnown {
			expectations = kube.ReadinessExpectations{Known: true, RuntimeWorkloadsExpected: scoped.RuntimeWorkloadsExpected, PVCNames: scoped.ExpectedPVCs}
		}
		legacy := len(sources) > 1 && !scopedKnown
		if legacy {
			expectations.Known = true
			expectations.RuntimeWorkloadsExpected = false
		}
		readiness, err := c.readiness.GetNamespaceReadiness(ctx, target, expectations)
		if err != nil {
			return result, fmt.Errorf("read readiness for namespace %q: %w", target, err)
		}
		if readiness.NamespacePhase != "Active" {
			result.NamespacePhase = readiness.NamespacePhase
			result.Ready = false
		}
		if !legacy && !readiness.Ready {
			result.Ready = false
		}
		if readiness.FailureCode != "" {
			result.FailureCode = readiness.FailureCode
			result.FailureMessage = target + ": " + readiness.FailureMessage
			result.Ready = false
		}
		result.PodCount += readiness.PodCount
		result.ReadyPodCount += readiness.ReadyPodCount
		result.WorkloadCount += readiness.WorkloadCount
		result.ReadyWorkloads += readiness.ReadyWorkloads
		result.ServiceCount += readiness.ServiceCount
		for _, object := range readiness.Objects {
			if object.Kind == "PersistentVolumeClaim" {
				if legacy && object.State == "Missing" {
					legacyMissing[object.Name] = true
					continue
				}
				if object.State == "Missing" {
					result.FailureCode = "RESTORE_EXPECTED_PVC_MISSING"
					result.FailureMessage = fmt.Sprintf("expected persistent volume claim %s/%s is missing after resource restoration completed", target, object.Name)
				}
				foundPVCs[object.Name] = true
				result.PVCCount++
				if object.State == "Bound" {
					result.ReadyPVCCount++
				} else {
					result.Ready = false
				}
			}
			object.Namespace = target
			result.Objects = append(result.Objects, object)
		}
		for _, event := range readiness.ImageEvents {
			event.Namespace = target
			result.ImageEvents = append(result.ImageEvents, event)
		}
		for _, name := range readiness.UnreadyPods {
			result.UnreadyPods = append(result.UnreadyPods, target+"/"+name)
			result.Ready = false
		}
		for _, name := range readiness.UnreadyWorkload {
			if legacy && strings.HasPrefix(name, "PersistentVolumeClaim/") && strings.HasSuffix(name, " (not restored)") {
				continue
			}
			result.UnreadyWorkload = append(result.UnreadyWorkload, target+"/"+name)
			result.Ready = false
		}
	}
	for _, name := range command.ExpectedPVCs {
		if legacyMissing[name] && !foundPVCs[name] {
			result.Objects = append(result.Objects, kube.ReadinessObject{Kind: "PersistentVolumeClaim", Name: name, State: "Missing"})
			result.PVCCount++
			result.Ready = false
			result.UnreadyWorkload = append(result.UnreadyWorkload, "PersistentVolumeClaim/"+name+" (not restored in selected namespaces)")
			result.FailureCode = "RESTORE_EXPECTED_PVC_MISSING"
			result.FailureMessage = fmt.Sprintf("expected persistent volume claim %q is missing from all selected restored namespaces", name)
		}
	}
	if (!command.ReadinessExpectationsKnown || command.RuntimeWorkloadsExpected) && result.PodCount == 0 && result.WorkloadCount == 0 {
		result.Ready = false
	}
	result.Namespace = strings.Join(targets, ", ")
	if result.Ready {
		result.Message = "All selected namespaces are ready."
	} else {
		result.Message = strings.Join(append(result.UnreadyPods, result.UnreadyWorkload...), ", ")
		if result.Message == "" {
			result.Message = "Waiting for restored resources in selected namespaces."
		}
	}
	return result, nil
}
