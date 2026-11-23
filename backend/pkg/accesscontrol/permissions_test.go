package accesscontrol

import "testing"

func TestBusinessPermissionCatalog(t *testing.T) {
	if !Valid(All()) || len(All()) != len(Catalog()) || len(All()) < 3 {
		t.Fatal("invalid business permission catalog")
	}
	for _, invalid := range [][]string{{"organization.manage"}, {"users.delete"}, {"sync.manage", "sync.manage"}, {"drill.execute", "unknown"}} {
		if Valid(invalid) {
			t.Fatalf("non-business or duplicate permissions accepted: %v", invalid)
		}
	}
	for _, permission := range All() {
		if !Allows("admin", false, "", nil, permission) || !Allows("operator", false, "", nil, permission) {
			t.Fatalf("built-in role missing permission: %s", permission)
		}
	}
	if Allows("operator", false, "custom", nil, "sync.manage") || Allows("unknown", false, "", All(), "sync.manage") {
		t.Fatal("unassigned custom permission or unknown role allowed")
	}
	if !Allows("operator", false, "custom", []string{"drill.execute"}, "drill.execute") || Allows("operator", false, "custom", []string{"drill.execute"}, "sync.manage") {
		t.Fatal("custom permission boundary violated")
	}
}
