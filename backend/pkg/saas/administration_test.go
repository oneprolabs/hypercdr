package saas

import (
	"encoding/json"
	"fmt"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/platform"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func systemPrincipal(t *testing.T, s *Service) platform.Principal {
	t.Helper()
	var p platform.Principal
	p.SystemAdmin = true
	p.Role = "admin"
	p.TenantID = store.DefaultTenantID
	if e := s.db.QueryRow(`select id,username,coalesce(email,'') from users where is_system_admin order by created_at limit 1`).Scan(&p.ID, &p.Username, &p.Email); e != nil {
		t.Fatal(e)
	}
	return p
}
func directoryMember(t *testing.T, s *Service, org string, role string) string {
	t.Helper()
	id := uuid.NewString()
	_, e := s.db.Exec(`insert into users(id,tenant_id,username,email,email_verified_at,password_hash,role,status,must_change_password) values($1,$2,$3,$3,now(),'test-unusable-password',$4,'active',false)`, id, org, id+"@example.test", role)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func TestPlatformDirectoryBoundariesAndMemberProtection(t *testing.T) {
	s, m := newTestService(t)
	a := signup(t, s, m, "directory-a@example.test", "Organization A")
	b := signup(t, s, m, "directory-b@example.test", "Organization B")
	sys := systemPrincipal(t, s)
	for _, p := range []platform.Principal{principal(a), {ID: a.ID, TenantID: a.TenantID, Role: "operator"}} {
		for _, path := range []string{"/api/v1/saas/admin/users", "/api/v1/saas/admin/organizations", "/api/v1/saas/admin/organizations/" + b.TenantID + "/members"} {
			expect(t, call(t, s, "GET", path, nil, p), 403)
		}
	}
	w := call(t, s, "GET", "/api/v1/saas/admin/users", nil, sys)
	expect(t, w, 200)
	var out struct {
		Items []struct {
			ID, OrganizationID, OrganizationName string
			SystemAdmin                          bool
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, u := range out.Items {
		if u.ID == a.ID {
			found = u.OrganizationID == a.TenantID && u.OrganizationName == "Organization A"
		}
	}
	if !found {
		t.Fatal("directory omitted organization ownership")
	}
	expect(t, call(t, s, "GET", "/api/v1/saas/admin/organizations/not-a-uuid", nil, sys), 400)
	path := fmt.Sprintf("/api/v1/saas/admin/organizations/%s/members/%s", a.TenantID, a.ID)
	expect(t, call(t, s, "DELETE", path, nil, sys), 409)
	expect(t, call(t, s, "PATCH", path, map[string]string{"role": "operator", "status": "active"}, sys), 409)
	expect(t, call(t, s, "PATCH", "/api/v1/saas/admin/organizations/"+b.TenantID+"/members/"+a.ID, map[string]string{"role": "operator", "status": "active"}, sys), 404)
	expect(t, call(t, s, "DELETE", "/api/v1/saas/admin/organizations/"+sys.TenantID+"/members/"+sys.ID, nil, sys), 409)
	member := directoryMember(t, s, a.TenantID, "operator")
	// Resource ownership belongs to the organization and survives user deletion.
	ci := s.identity.(communityIdentity)
	cluster, e := ci.repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: a.TenantID, Name: "owned-resource", Type: "S3"})
	if e != nil {
		t.Fatal(e)
	}
	expect(t, call(t, s, "DELETE", "/api/v1/saas/admin/organizations/"+a.TenantID+"/members/"+member, nil, sys), 200)
	clusters, e := ci.repo.ListStorageRepositories()
	if e != nil {
		t.Fatal(e)
	}
	exists := false
	for _, c := range clusters {
		if c.ID == cluster.ID && c.TenantID == a.TenantID {
			exists = true
		}
	}
	if !exists {
		t.Fatal("deleting a user changed resource ownership")
	}
	// Organization members cannot mutate another organization, even by forging query parameters.
	expect(t, call(t, s, "DELETE", "/api/v1/saas/members/"+b.ID+"?organization="+b.TenantID, nil, principal(a)), 404)
	logs, e := ci.repo.ListAuditLogs(100, 0)
	if e != nil {
		t.Fatal(e)
	}
	audited := false
	for _, log := range logs {
		if log.Action == "member.deleted" && log.ActorID == sys.ID && log.Payload["targetOrganizationId"] == a.TenantID && log.Payload["actorOrganizationId"] == sys.TenantID {
			audited = true
		}
	}
	if !audited {
		t.Fatal("cross-organization audit lost actor or target organization")
	}
}
func TestOrganizationDisableRevokesSessionsAndProtectsSystemScope(t *testing.T) {
	s, m := newTestService(t)
	a := signup(t, s, m, "disable-org@example.test", "Disable test")
	sys := systemPrincipal(t, s)
	sess, e := s.identity.CreateSession(t.Context(), a.ID, 60*60*1e9)
	if e != nil {
		t.Fatal(e)
	}
	expect(t, call(t, s, "PATCH", "/api/v1/saas/admin/organizations/"+a.TenantID, map[string]string{"name": "Disable test", "description": "", "status": "disabled"}, sys), 200)
	if _, ok, e := s.identity.AuthenticateSession(t.Context(), sess.Token); e != nil || ok {
		t.Fatal("disabled organization retained a usable session", e)
	}
	expect(t, call(t, s, "PATCH", "/api/v1/saas/admin/organizations/"+sys.TenantID, map[string]string{"name": "Admin", "status": "disabled"}, sys), 409)
	expect(t, call(t, s, "PATCH", "/api/v1/saas/admin/organizations/"+a.TenantID, map[string]string{"name": "Disable test", "description": "", "status": "active"}, sys), 200)
	if _, ok, e := s.identity.AuthenticateSession(t.Context(), sess.Token); e != nil || ok {
		t.Fatal("re-enabling resurrected revoked session", e)
	}
}
func TestConcurrentAdministratorRemovalSerializesPerOrganization(t *testing.T) {
	s, m := newTestService(t)
	a := signup(t, s, m, "race-admin@example.test", "Race administrators")
	b := directoryMember(t, s, a.TenantID, "admin")
	sys := systemPrincipal(t, s)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, id := range []string{a.ID, b} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			codes <- call(t, s, "DELETE", "/api/v1/saas/admin/organizations/"+a.TenantID+"/members/"+id, nil, sys).Code
		}(id)
	}
	wg.Wait()
	close(codes)
	success, protected := 0, 0
	for code := range codes {
		if code == http.StatusOK {
			success++
		}
		if code == http.StatusConflict {
			protected++
		}
	}
	if success != 1 || protected != 1 {
		t.Fatalf("success=%d protected=%d", success, protected)
	}
}
func TestNewOrganizationInvitationCompletesInSelectedOrganization(t *testing.T) {
	s, m := newTestService(t)
	sys := systemPrincipal(t, s)
	w := call(t, s, "POST", "/api/v1/saas/admin/organizations", map[string]string{"name": "Created organization", "description": "Created by platform administrator"}, sys)
	expect(t, w, 201)
	var v struct{ ID string }
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/saas/admin/organizations/" + v.ID + "/invitations"
	expect(t, call(t, s, "POST", path, map[string]string{"email": "first-member@example.test", "roleId": "operator"}, sys), 409)
	expect(t, call(t, s, "POST", path, map[string]string{"email": "first-admin@example.test", "roleId": "admin"}, sys), 201)
	w = call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"token": m.token(t), "password": "Password123"}, platform.Principal{})
	expect(t, w, 200)
	var account struct{ User platform.Identity }
	if e := json.Unmarshal(w.Body.Bytes(), &account); e != nil {
		t.Fatal(e)
	}
	if account.User.TenantID != v.ID || account.User.Role != "admin" || account.User.SystemAdmin {
		t.Fatal("invitation completed in wrong scope or with global privileges")
	}
	expect(t, call(t, s, "GET", path, nil, sys), 200)
}
