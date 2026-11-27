package velero

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"hypercdr-platform/agent/comm-agent/internal/consistency"
	"k8s.io/apimachinery/pkg/util/validation"
)

func BuildConsistencyRestoreRules(record consistency.Record, namespaces []string) (string, error) {
	if record.ID == "" || len(validation.IsDNS1123Subdomain(record.ID)) != 0 || record.GroupID == "" || record.Strategy != "offline-v1" || record.Phase != consistency.Completed || !record.Resumed || !record.LocksReleased || record.Failure != "" || len(record.Workloads) == 0 || len(record.Volumes) == 0 || len(record.Volumes) != len(record.Snapshots) {
		return "", fmt.Errorf("consistency restore requires a completed, resumed and verified offline run")
	}
	selected := map[string]bool{}
	for _, namespace := range namespaces {
		selected[namespace] = true
	}
	if err := consistency.VerifyDurableSnapshots(record); err != nil {
		return "", err
	}
	if err := consistency.VerifyQuiescenceProof(record); err != nil {
		return "", err
	}
	for _, volume := range record.Volumes {
		if !selected[volume.Namespace] {
			return "", fmt.Errorf("consistency restore must include the complete recorded volume group")
		}
	}
	workloads := append([]consistency.WorkloadState(nil), record.Workloads...)
	sort.Slice(workloads, func(first, second int) bool {
		return workloads[first].Namespace+"/"+workloads[first].Kind+"/"+workloads[first].Name < workloads[second].Namespace+"/"+workloads[second].Kind+"/"+workloads[second].Name
	})
	seen := map[string]bool{}
	rules := ""
	ownerValue, _ := json.Marshal(record.ID)
	for _, workload := range workloads {
		resource := ""
		switch workload.Kind {
		case "Deployment":
			resource = "deployments.apps"
		case "StatefulSet":
			resource = "statefulsets.apps"
		}
		key := workload.Namespace + "/" + workload.Kind + "/" + workload.Name
		if resource == "" || workload.Name == "" || workload.UID == "" || workload.Generation <= 0 || workload.Replicas < 0 || !selected[workload.Namespace] || seen[key] {
			return "", fmt.Errorf("invalid consistency restore workload %s", key)
		}
		seen[key] = true
		patch, err := json.Marshal(map[string]any{
			"spec": map[string]any{"replicas": workload.Replicas},
			"metadata": map[string]any{"annotations": map[string]any{
				"hypercdr.io/consistency-owner":   nil,
				"hypercdr.io/consistency-resumed": nil,
			}},
		})
		if err != nil {
			return "", err
		}
		rules += "- conditions:\n    groupResource: " + resource + "\n    namespaces:\n    - " + yamlString(workload.Namespace) + "\n    resourceNameRegex: " + yamlString("^"+regexp.QuoteMeta(workload.Name)+"$") + "\n    matches:\n    - path: /metadata/annotations/hypercdr.io~1consistency-owner\n      value: " + yamlString(string(ownerValue)) + "\n  mergePatches:\n  - patchData: |\n      " + string(patch) + "\n"
	}
	return rules, nil
}
