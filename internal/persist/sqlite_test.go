package persist

import (
	"context"
	"path/filepath"
	"testing"

	"machring/internal/policy"
	"machring/internal/resource"
)

func TestSQLiteStorePersistsRulesAndResources(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "machring.db")
	ctx := context.Background()

	store, err := NewSQLite(dbPath, policy.DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	rules, err := store.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) == 0 {
		t.Fatal("default rules were not seeded")
	}

	record := resource.Record{
		ID:            "resource-1",
		UserGroup:     policy.GroupGuest,
		StorageDriver: "local",
		ObjectKey:     "2026/04/13/resource-1.jpg",
		PublicURL:     "http://example.test/r/resource-1",
		OriginalName:  "resource.jpg",
		Extension:     "jpg",
		Type:          resource.TypeImage,
		Size:          12,
		ContentType:   "image/jpeg",
		Hash:          "hash",
		Status:        resource.StatusActive,
		MonthWindow:   "2026-04",
		MonthlyLimit:  policy.GB,
	}
	if err := store.CreateResource(ctx, CreateResourceBundle{Record: record}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	reopened, err := NewSQLite(dbPath, policy.DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	loaded, err := reopened.Resource(ctx, "resource-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.OriginalName != "resource.jpg" || loaded.Type != resource.TypeImage {
		t.Fatalf("loaded resource = %#v", loaded)
	}
}

func TestSQLiteStorePolicyGroupsCopyAndActivate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "machring.db")
	ctx := context.Background()

	store, err := NewSQLite(dbPath, policy.DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	active, err := store.ActivePolicyGroup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != policy.DefaultGroupID {
		t.Fatalf("active policy group = %q, want %q", active.ID, policy.DefaultGroupID)
	}

	copied, err := store.CopyPolicyGroup(ctx, policy.DefaultGroupID, "实验策略组")
	if err != nil {
		t.Fatal(err)
	}

	rules, err := store.RulesForGroup(ctx, copied.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) == 0 {
		t.Fatal("copied policy group should contain rules")
	}

	if _, err := store.SetPolicyGroupActive(ctx, copied.ID, true); err != nil {
		t.Fatal(err)
	}

	active, err = store.ActivePolicyGroup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != copied.ID {
		t.Fatalf("active policy group = %q, want %q", active.ID, copied.ID)
	}
}
