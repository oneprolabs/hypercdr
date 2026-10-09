package store

import "testing"

func TestRegistrationTaskIdentityIsDurablyUnique(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	create := func(owner, key, session string) error {
		_, err := repo.CreateTask(TaskInput{TenantID: actor.TenantID, Type: "cluster-registration", Payload: map[string]any{"ownerId": owner, "idempotencyKey": key, "sessionId": session}})
		return err
	}
	if err := create(actor.ID, "registration-key-0001", "session-a"); err != nil {
		t.Fatal(err)
	}
	if err := create(actor.ID, "registration-key-0001", "session-b"); err == nil {
		t.Fatal("duplicate owner request persisted")
	}
	if err := create(actor.ID, "registration-key-0002", "session-a"); err == nil {
		t.Fatal("upload session reused by a second task")
	}
	other, err := repo.CreateUser(actor.TenantID, "registration-identity@example.com", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := create(other.ID, "registration-key-0001", "session-c"); err != nil {
		t.Fatalf("different uploader key rejected: %v", err)
	}
	items, err := repo.ListTasksFiltered(TaskFilter{TenantID: actor.TenantID, Types: []string{"cluster-registration"}})
	if err != nil || len(items) != 2 {
		t.Fatalf("invalid identity partially committed: %+v %v", items, err)
	}
}
