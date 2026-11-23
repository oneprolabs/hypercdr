package saas

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"hypercdr-platform/platform/backend/pkg/platform"
)

func TestOrganizationRolesLifecycleAndScope(t *testing.T) {
	service, mail := newTestService(t)
	owner := signup(t, service, mail, "owner-roles@example.test", "Role organization")
	other := signup(t, service, mail, "other-roles@example.test", "Other organization")
	admin := platform.Principal{ID: owner.ID, TenantID: owner.TenantID, Role: "admin"}
	otherAdmin := platform.Principal{ID: other.ID, TenantID: other.TenantID, Role: "admin"}
	member := platform.Principal{ID: owner.ID, TenantID: owner.TenantID, Role: "operator"}
	draft := map[string]any{"name": "Drill operator", "description": "Drills only", "permissions": []string{"resources.view", "drill.execute"}}
	if response := call(t, service, "POST", "/api/v1/saas/roles", draft, member); response.Code != 403 {
		t.Fatalf("member create: %d %s", response.Code, response.Body.String())
	}
	created := call(t, service, "POST", "/api/v1/saas/roles", draft, admin)
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var result struct{ ID string }
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || result.ID == "" {
		t.Fatal("missing role ID")
	}
	path := "/api/v1/saas/roles/" + result.ID
	if response := call(t, service, "POST", "/api/v1/saas/roles", draft, admin); response.Code != 409 {
		t.Fatalf("duplicate: %d %s", response.Code, response.Body.String())
	}
	if response := call(t, service, "POST", "/api/v1/saas/roles", draft, otherAdmin); response.Code != 201 {
		t.Fatalf("tenant-local name: %d %s", response.Code, response.Body.String())
	}
	for _, method := range []string{"PATCH", "DELETE"} {
		if response := call(t, service, method, path, draft, otherAdmin); response.Code != 404 {
			t.Fatalf("cross-tenant %s: %d", method, response.Code)
		}
		if response := call(t, service, method, "/api/v1/saas/roles/operator", draft, admin); response.Code != 409 {
			t.Fatalf("built-in %s: %d", method, response.Code)
		}
	}
	for _, permissions := range [][]string{{"organization.manage"}, {"sync.manage", "sync.manage"}} {
		invalid := map[string]any{"name": "Invalid", "permissions": permissions}
		if response := call(t, service, "POST", "/api/v1/saas/roles", invalid, admin); response.Code != 400 {
			t.Fatalf("invalid permissions: %d", response.Code)
		}
	}
	memberID := uuid.NewString()
	_, err := service.db.Exec(`insert into users(id,tenant_id,username,password_hash,role,status,custom_role_id) values($1,$2,$3,'unused','operator','active',$4)`, memberID, owner.TenantID, "role-member", result.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.db.Exec(`insert into users(id,tenant_id,username,password_hash,role,status,custom_role_id) values($1,$2,$3,'unused','operator','active',$4)`, uuid.NewString(), other.TenantID, "wrong-scope-member", result.ID); err == nil {
		t.Fatal("database accepted a cross-tenant role assignment")
	}
	session, err := service.identity.CreateSession(context.Background(), memberID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	before, ok, err := service.identity.AuthenticateSession(context.Background(), session.Token)
	if err != nil || !ok || before.RoleID != result.ID || fmt.Sprint(before.Permissions) != "[resources.view drill.execute]" {
		t.Fatalf("assigned role in session: %+v %v", before, err)
	}
	if response := call(t, service, "DELETE", path, nil, admin); response.Code != 409 {
		t.Fatalf("role in use: %d", response.Code)
	}
	updated := map[string]any{"name": "Backup operator", "permissions": []string{"sync.manage"}}
	if response := call(t, service, "PATCH", path, updated, admin); response.Code != 200 {
		t.Fatalf("update: %d %s", response.Code, response.Body.String())
	}
	after, ok, err := service.identity.AuthenticateSession(context.Background(), session.Token)
	if err != nil || !ok || after.RoleName != "Backup operator" || fmt.Sprint(after.Permissions) != "[sync.manage]" {
		t.Fatalf("stale session permissions: %+v %v", after, err)
	}
	if _, err := service.db.Exec(`update users set custom_role_id=null where id=$1`, memberID); err != nil {
		t.Fatal(err)
	}
	if response := call(t, service, "DELETE", path, nil, admin); response.Code != 200 {
		t.Fatalf("delete unused role: %d %s", response.Code, response.Body.String())
	}
}

func TestCustomRoleMemberAssignmentAndInvitation(t *testing.T) {
	service, mail := newTestService(t)
	owner := signup(t, service, mail, "role-assignment-owner@example.test", "Assignment organization")
	other := signup(t, service, mail, "role-assignment-other@example.test", "Other organization")
	admin := principal(owner)
	roleID := uuid.NewString()
	if _, err := service.db.Exec(`insert into organization_roles(id,tenant_id,name,permissions) values($1,$2,'Drill only','["resources.view","drill.execute"]')`, roleID, owner.TenantID); err != nil {
		t.Fatal(err)
	}
	memberID := directoryMember(t, service, owner.TenantID, "operator")
	memberPath := "/api/v1/saas/members/" + memberID
	assigned := call(t, service, "PATCH", memberPath, map[string]string{"roleId": roleID, "status": "active"}, admin)
	if assigned.Code != 200 {
		t.Fatalf("assign custom role: %d %s", assigned.Code, assigned.Body.String())
	}
	otherRoleID := uuid.NewString()
	if _, err := service.db.Exec(`insert into organization_roles(id,tenant_id,name) values($1,$2,'Foreign role')`, otherRoleID, other.TenantID); err != nil {
		t.Fatal(err)
	}
	if response := call(t, service, "PATCH", memberPath, map[string]string{"roleId": otherRoleID, "status": "active"}, admin); response.Code != 400 {
		t.Fatalf("cross-tenant assignment API: %d", response.Code)
	}
	if response := call(t, service, "POST", "/api/v1/saas/invitations", map[string]string{"email": "custom-invite@example.test", "roleId": roleID}, admin); response.Code != 201 {
		t.Fatalf("invite custom role: %d %s", response.Code, response.Body.String())
	}
	invitationToken := mail.token(t)
	if response := call(t, service, "POST", "/api/v1/saas/auth/complete", map[string]string{"token": invitationToken, "displayName": "Drill member", "password": "Test-password-123"}, platform.Principal{}); response.Code != 200 {
		t.Fatalf("accept invitation: %d %s", response.Code, response.Body.String())
	}
	var assignedRoleID string
	if err := service.db.QueryRow(`select custom_role_id from users where email='custom-invite@example.test'`).Scan(&assignedRoleID); err != nil || assignedRoleID != roleID {
		t.Fatalf("invitation role lost: %s %v", assignedRoleID, err)
	}
}
