package store

import "testing"

func TestStorageRepositoryUpdateAndDelete(t *testing.T) {
	repo := NewMemoryStore()
	created, err := repo.CreateStorageRepository(StorageRepositoryInput{Name: "before", Type: "S3-Compatible", Endpoint: "http://old", Bucket: "old", AccessKey: "ak", SecretKey: "sk"})
	if err != nil {
		t.Fatal(err)
	}
	updated, ok, err := repo.UpdateStorageRepository(created.ID, StorageRepositoryInput{Name: "after", Type: "S3-Compatible", Endpoint: "http://new", Bucket: "new"})
	if err != nil || !ok {
		t.Fatalf("update storage: ok=%v err=%v", ok, err)
	}
	if updated.Name != "after" || updated.Endpoint != "http://new" || updated.Secret["secretKey"] != "sk" {
		t.Fatalf("unexpected updated storage: %#v", updated)
	}
	deleted, inUse, err := repo.DeleteStorageRepository(created.ID)
	if err != nil || !deleted || inUse {
		t.Fatalf("delete storage: deleted=%v inUse=%v err=%v", deleted, inUse, err)
	}
}

func TestPolicyUpdateAndDelete(t *testing.T) {
	repo := NewMemoryStore()
	created, err := repo.CreatePolicy(PolicyInput{Name: "before", Composition: "combined", ScheduleType: "interval", IntervalValue: 5, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	updated, ok, err := repo.UpdatePolicy(created.ID, PolicyInput{Name: "after", Composition: "combined", ScheduleType: "daily", Hour: 9, Minute: 30, RetentionCount: 5, Status: "active"})
	if err != nil || !ok {
		t.Fatalf("update policy: ok=%v err=%v", ok, err)
	}
	if updated.Name != "after" || updated.ScheduleType != "daily" || updated.Hour != 9 {
		t.Fatalf("unexpected updated policy: %#v", updated)
	}
	deleted, inUse, err := repo.DeletePolicy(created.ID)
	if err != nil || !deleted || inUse {
		t.Fatalf("delete policy: deleted=%v inUse=%v err=%v", deleted, inUse, err)
	}
}

func TestReferencedStorageAndPolicyCannotBeDeleted(t *testing.T) {
	repo := NewMemoryStore()
	storage, err := repo.CreateStorageRepository(StorageRepositoryInput{Name: "used-storage", Type: "S3", Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreatePolicy(PolicyInput{Name: "used-policy", Composition: "combined", ScheduleType: "daily", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	repo.plans["plan-1"] = ProtectionPlan{ID: "plan-1", StorageRepoID: storage.ID, PolicyID: policy.ID}
	if deleted, inUse, err := repo.DeleteStorageRepository(storage.ID); err != nil || deleted || !inUse {
		t.Fatalf("referenced storage delete: deleted=%v inUse=%v err=%v", deleted, inUse, err)
	}
	if deleted, inUse, err := repo.DeletePolicy(policy.ID); err != nil || deleted || !inUse {
		t.Fatalf("referenced policy delete: deleted=%v inUse=%v err=%v", deleted, inUse, err)
	}
}
