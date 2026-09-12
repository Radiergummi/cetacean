package acl

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
)

func TestReloadPolicy_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	if err := os.WriteFile(
		path,
		[]byte(`{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`),
		0600,
	); err != nil {
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
	if err := os.WriteFile(
		path,
		[]byte(`{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`),
		0600,
	); err != nil {
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
	if err := os.WriteFile(
		path,
		[]byte(`{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`),
		0600,
	); err != nil {
		t.Fatal(err)
	}
	e := NewEvaluator()
	reloadPolicy(e, path)

	original := e.policy.Load()

	// Overwrite with structurally valid JSON but invalid grant (bad resource type).
	if err := os.WriteFile(
		path,
		[]byte(`{"grants":[{"resources":["badtype:*"],"audience":["*"],"permissions":["read"]}]}`),
		0600,
	); err != nil {
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

// pollUntil polls condition every interval until it returns true or deadline expires.
func pollUntil(
	t *testing.T,
	deadline time.Duration,
	interval time.Duration,
	condition func() bool,
	msg string,
) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if condition() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatal(msg)
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

	// Poll until the policy is reloaded instead of using a fixed sleep.
	id := &auth.Identity{Subject: "anyone"}
	pollUntil(t, 5*time.Second, 50*time.Millisecond, func() bool {
		return e.Can(id, "write", "node:any")
	}, "policy was not reloaded within deadline")

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
	if err := os.WriteFile(
		path,
		[]byte(`{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`),
		0600,
	); err != nil {
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

	// Wait for the debounce to fire (200ms) + margin, then verify unchanged.
	// We need a short sleep here because we're testing that nothing changes,
	// but we use a modest timeout rather than a flaky long one.
	time.Sleep(500 * time.Millisecond)

	after := e.policy.Load()
	if after != original {
		t.Fatal("policy should be unchanged after invalid update")
	}
}

// replaceViaRename writes content to a temporary file beside path and renames
// it over the top — the atomic replacement a deployment or an editor performs,
// and the one a watch on the file's own inode cannot see.
func replaceViaRename(t *testing.T, path, content string) {
	t.Helper()

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

// TestWatchPolicyFile_ReloadsAfterRenameOverWrite covers the write pattern the
// previous file watch could not see at all. It swaps the policy twice on
// purpose: a watcher that merely re-added the file after the first event would
// pass a single-swap test and stop reloading on the second.
func TestWatchPolicyFile_ReloadsAfterRenameOverWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")

	read := `{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`
	if err := os.WriteFile(path, []byte(read), 0600); err != nil {
		t.Fatal(err)
	}

	e := NewEvaluator()
	reloadPolicy(e, path)

	stop, err := WatchPolicyFile(e, path)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	id := &auth.Identity{Subject: "anyone"}

	replaceViaRename(
		t,
		path,
		`{"grants":[{"resources":["node:*"],"audience":["*"],"permissions":["write"]}]}`,
	)
	pollUntil(t, 5*time.Second, 50*time.Millisecond, func() bool {
		return e.Can(id, "write", "node:any")
	}, "policy was not reloaded after the first rename-over-write")

	replaceViaRename(
		t,
		path,
		`{"grants":[{"resources":["volume:*"],"audience":["*"],"permissions":["write"]}]}`,
	)
	pollUntil(t, 5*time.Second, 50*time.Millisecond, func() bool {
		return e.Can(id, "write", "volume:any")
	}, "policy was not reloaded after the second rename-over-write")
}

// Covers how a Kubernetes ConfigMap and a Docker secret actually update: the
// mounted name is a symlink into a versioned directory, and an update swaps the
// directory symlink beside it. No event ever names the policy file, so a watch
// filtering on its basename would see the update and discard it.
func TestWatchPolicyFile_ReloadsAfterSymlinkSwap(t *testing.T) {
	dir := t.TempDir()

	first := filepath.Join(dir, "..v1")
	if err := os.Mkdir(first, 0700); err != nil {
		t.Fatal(err)
	}

	read := `{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`
	if err := os.WriteFile(filepath.Join(first, "policy.json"), []byte(read), 0600); err != nil {
		t.Fatal(err)
	}

	data := filepath.Join(dir, "..data")
	if err := os.Symlink(first, data); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "policy.json")
	if err := os.Symlink(filepath.Join(data, "policy.json"), path); err != nil {
		t.Fatal(err)
	}

	e := NewEvaluator()
	reloadPolicy(e, path)

	stop, err := WatchPolicyFile(e, path)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	// Swap in a second version directory exactly as the ConfigMap writer does:
	// write the new content, point a temporary symlink at it, then rename that
	// over `..data`. The `policy.json` symlink is never touched.
	second := filepath.Join(dir, "..v2")
	if err := os.Mkdir(second, 0700); err != nil {
		t.Fatal(err)
	}

	write := `{"grants":[{"resources":["node:*"],"audience":["*"],"permissions":["write"]}]}`
	if err := os.WriteFile(filepath.Join(second, "policy.json"), []byte(write), 0600); err != nil {
		t.Fatal(err)
	}

	tmp := filepath.Join(dir, "..data_tmp")
	if err := os.Symlink(second, tmp); err != nil {
		t.Fatal(err)
	}

	// Creating the version directory and the temporary symlink are themselves
	// events in the watched directory, and on kqueue the only ones the swap
	// produces. Draining them first holds the watcher to noticing the swap
	// rather than to being woken near it.
	time.Sleep(300 * time.Millisecond)

	if err := os.Rename(tmp, data); err != nil {
		t.Fatal(err)
	}

	id := &auth.Identity{Subject: "anyone"}
	pollUntil(t, 5*time.Second, 50*time.Millisecond, func() bool {
		return e.Can(id, "write", "node:any")
	}, "policy was not reloaded after the symlink swap")
}
