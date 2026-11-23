package accesscontrol

import "slices"

type Permission struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Description string `json:"description"`
}

func Catalog() []Permission {
	return []Permission{
		{Key: "resources.view", Name: "View resources", Group: "Visibility", Description: "View this organization's clusters, applications, plans, restore points and tasks."},
		{Key: "cluster.register", Name: "Register clusters", Group: "Clusters", Description: "Create installation credentials and register clusters within the organization's license limit."},
		{Key: "cluster.manage", Name: "Manage clusters", Group: "Clusters", Description: "Update cluster settings, cache settings, inventory and cluster component upgrades."},
		{Key: "cluster.unregister", Name: "Unregister clusters", Group: "Clusters", Description: "Unregister clusters and clean up their managed resources."},
		{Key: "sync.manage", Name: "Manage Sync", Group: "Protection", Description: "Configure protection plans and start, retry or cancel backups."},
		{Key: "drill.execute", Name: "Run Drill", Group: "Recovery", Description: "Run, retry and clean up recovery drills."},
		{Key: "recovery.execute", Name: "Run recovery", Group: "Recovery", Description: "Run production restores and takeover operations."},
		{Key: "backup.delete", Name: "Manage restore point retention", Group: "Protection", Description: "Delete restore points and change their retention."},
		{Key: "storage.manage", Name: "Manage object storage", Group: "Configuration", Description: "Create, edit, test and delete this organization's storage repositories."},
		{Key: "policy.manage", Name: "Manage policies", Group: "Configuration", Description: "Create, edit and delete this organization's protection policies."},
		{Key: "task.abort", Name: "Force abort tasks", Group: "Operations", Description: "Force abort tasks; the permission for the corresponding operation is also required."},
		{Key: "diagnostics.manage", Name: "Collect diagnostics", Group: "Operations", Description: "Request and search cluster diagnostic logs."},
	}
}

func All() []string {
	permissions := Catalog()
	keys := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		keys = append(keys, permission.Key)
	}
	return keys
}

func Valid(keys []string) bool {
	known := All()
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !slices.Contains(known, key) || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func Allows(role string, systemAdmin bool, customRoleID string, permissions []string, required string) bool {
	if systemAdmin || role == "admin" {
		return true
	}
	if role != "operator" {
		return false
	}
	if customRoleID == "" {
		return slices.Contains(All(), required)
	}
	return slices.Contains(permissions, required)
}
