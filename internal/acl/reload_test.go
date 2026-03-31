package acl

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReloadPolicy_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(`{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}

	e := NewEvaluator()
	reloadPolicy(e, path)

	p := e.policy.Load()
	if p == nil {
		t.Fatal("policy should be set after reload")
	}
	if len(p.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(p.Grants))
	}
}

func TestReloadPolicy_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	// Write a valid policy first.
	if err := os.WriteFile(path, []byte(`{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	e := NewEvaluator()
	reloadPolicy(e, path)

	original := e.policy.Load()
	if original == nil || len(original.Grants) != 1 {
		t.Fatal("setup: initial policy not loaded")
	}

	// Overwrite with invalid JSON.
	if err := os.WriteFile(path, []byte(`not valid json`), 0600); err != nil {
		t.Fatal(err)
	}
	reloadPolicy(e, path)

	// Policy should be unchanged.
	after := e.policy.Load()
	if after != original {
		t.Fatal("policy should be unchanged after invalid reload")
	}
}

func TestReloadPolicy_InvalidGrants(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	// Write a valid policy first.
	if err := os.WriteFile(path, []byte(`{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	e := NewEvaluator()
	reloadPolicy(e, path)

	original := e.policy.Load()

	// Overwrite with structurally valid JSON but invalid grant (bad resource type).
	if err := os.WriteFile(path, []byte(`{"grants":[{"resources":["badtype:*"],"audience":["*"],"permissions":["read"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	reloadPolicy(e, path)

	after := e.policy.Load()
	if after != original {
		t.Fatal("policy should be unchanged after invalid grants reload")
	}
}

func TestReloadPolicy_MissingFile(t *testing.T) {
	e := NewEvaluator()
	// Should not panic; just logs error.
	reloadPolicy(e, "/nonexistent/path/policy.json")

	if p := e.policy.Load(); p != nil {
		t.Fatal("policy should remain nil for missing file")
	}
}

func TestWatchPolicyFile_InitialLoadAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	initialPolicy := `{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`
	if err := os.WriteFile(path, []byte(initialPolicy), 0600); err != nil {
		t.Fatal(err)
	}

	e := NewEvaluator()
	// Pre-load the initial policy (WatchPolicyFile doesn't do initial load).
	reloadPolicy(e, path)

	stop, err := WatchPolicyFile(e, path)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	p := e.policy.Load()
	if p == nil || len(p.Grants) != 1 {
		t.Fatal("initial policy not loaded")
	}

	// Update the file with a new grant.
	updatedPolicy := `{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]},{"resources":["node:*"],"audience":["*"],"permissions":["write"]}]}`
	if err := os.WriteFile(path, []byte(updatedPolicy), 0600); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce (200ms) + extra margin.
	time.Sleep(600 * time.Millisecond)

	p = e.policy.Load()
	if p == nil {
		t.Fatal("policy should still be set after update")
	}
	if len(p.Grants) != 2 {
		t.Fatalf("expected 2 grants after reload, got %d", len(p.Grants))
	}
}

func TestWatchPolicyFile_InvalidUpdateKeepsOldPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(path, []byte(`{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}

	e := NewEvaluator()
	reloadPolicy(e, path)

	stop, err := WatchPolicyFile(e, path)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	original := e.policy.Load()

	// Write invalid content.
	if err := os.WriteFile(path, []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}

	time.Sleep(600 * time.Millisecond)

	after := e.policy.Load()
	if after != original {
		t.Fatal("policy should be unchanged after invalid update")
	}
}
