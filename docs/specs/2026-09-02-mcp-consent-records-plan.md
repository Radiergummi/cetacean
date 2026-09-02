# MCP Consent Records Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remember a user's consent decision so an already-approved MCP client is not re-prompted on every authorization request.

**Architecture:** A `ConsentStore` mirroring the existing `RefreshTokenStore` — mutex, map, change hook — sharing the OAuth state file with it. Because two stores cannot each own the write, a `stateFile` coordinator takes ownership of the path and snapshots both. `handleAuthorizeGET` skips the consent page when a record matches, and revocation and theft detection clear records while expiry deliberately does not.

**Tech Stack:** Go 1.26.6, stdlib only (`crypto/sha256`, `slices`, `sync`), `github.com/goccy/go-json` for the file, `log/slog` for warnings.

**Spec:** `docs/specs/2026-09-02-mcp-consent-records-design.md`

## Global Constraints

- Package is `internal/mcp/oauth`. All new files live there.
- On-disk file is `<data_dir>/mcp-tokens.json`, mode `0600`, atomic tmp + rename + fsync. Do not rename the file.
- File format goes from version 1 to version 2. A v1 file must load without error and yield zero consent records.
- Consent records are written **only** for clients where `resolveClientMeta` returned `verified == true` (the CIMD path).
- A store's change hook fires **after** its mutex is released and **only** when state actually changed. Taking a snapshot acquires the mutex; firing the hook while holding it deadlocks.
- Every write failure is logged with `slog.Warn` and swallowed. The server keeps working from memory.
- Run `gofmt -w` on every file you touch. `golangci-lint run ./internal/...` must report `0 issues` before each commit.
- Comments explain *why*, not *what*. Match the density of the surrounding file.

---

### Task 1: Give the persistence layer ownership of the file

Today `RefreshTokenStore` holds a `persist func(RefreshTokenSnapshot)` and writes the file itself. A second store cannot be added that way — each would write the whole file from its own snapshot and clobber the other. This task introduces the coordinator with no change in behaviour and no change to the file format.

**Files:**
- Modify: `internal/mcp/oauth/persist.go`
- Modify: `internal/mcp/oauth/store.go`
- Modify: `internal/mcp/oauth/server.go`
- Test: `internal/mcp/oauth/persist_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `type oauthState struct` (unexported, embeds `RefreshTokenSnapshot`); `func writeState(path string, state oauthState) error`; `func readState(path string) (oauthState, error)`; `type stateFile struct { path string; tokens *RefreshTokenStore }` with method `func (f *stateFile) write()`; `func (s *RefreshTokenStore) SetOnChange(fn func())`. `RefreshTokenSnapshot` loses its `Version` and `Timestamp` fields.

- [ ] **Step 1: Update the existing tests to the new seams**

In `internal/mcp/oauth/persist_test.go`, replace the `restart` helper and every use of `persistRefreshTokens`:

```go
// restart round-trips a store through the on-disk format the way a process
// restart does, so a test asserts against what actually survives rather than
// against the in-memory maps it started from.
func restart(t *testing.T, s *RefreshTokenStore, path string) *RefreshTokenStore {
	t.Helper()

	if err := writeState(path, oauthState{
		Version:              oauthStateVersion,
		Timestamp:            time.Now(),
		RefreshTokenSnapshot: s.Snapshot(),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	state, err := readState(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	restored := NewRefreshTokenStore()
	restored.Restore(state.RefreshTokenSnapshot)

	return restored
}

// onDisk reads back what the store has written without going through it, so
// these tests assert on the file rather than on the store's own memory.
func onDisk(t *testing.T, path string) oauthState {
	t.Helper()

	state, err := readState(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	return state
}

// persisting wires a store to a file the way NewServer does.
func persisting(s *RefreshTokenStore, path string) {
	file := &stateFile{path: path, tokens: s}
	s.SetOnChange(file.write)
}
```

Then in each of `TestIssueIsWrittenThrough`, `TestRotateIsWrittenThrough`, `TestRevokeIsWrittenThrough`, `TestUnknownTokenRotationDoesNotWrite` and `TestUnknownTokenRevocationDoesNotWrite`, replace `s.SetPersistFunc(persistRefreshTokens(path))` with `persisting(s, path)`.

In `TestExpiredGrantFamilyIsDroppedOnLoad`, replace the write/restore block:

```go
	if err := writeState(path, oauthState{
		Version:              oauthStateVersion,
		Timestamp:            time.Now(),
		RefreshTokenSnapshot: snap,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	restored := NewRefreshTokenStore()
	restored.Restore(onDisk(t, path).RefreshTokenSnapshot)
```

In `TestRefreshTokenFileIsPrivateAndAtomic`, replace the write call:

```go
	if err := writeState(path, oauthState{
		Version:              oauthStateVersion,
		Timestamp:            time.Now(),
		RefreshTokenSnapshot: s.Snapshot(),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
```

In `TestReadRefreshTokensRejectsUnreadableFiles`, rename the call from `readRefreshTokens(tc.path)` to `readState(tc.path)`.

In `TestServerWithoutTokenStorePathKeepsTokensInMemory`, change the wiring assertion:

```go
	if s.refreshTokens.onChange != nil {
		t.Error("a server with no token store path should install no change hook")
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mcp/oauth/ 2>&1 | head -20`
Expected: FAIL, build error — `undefined: writeState`, `undefined: readState`, `undefined: oauthState`, `undefined: oauthStateVersion`, `undefined: stateFile`, `s.refreshTokens.onChange undefined`.

- [ ] **Step 3: Split the snapshot from the file in `persist.go`**

Replace the version constant and the `RefreshTokenSnapshot` declaration:

```go
// oauthStateVersion is the on-disk format version. Bump it whenever the shape
// below changes incompatibly; readState refuses anything newer.
const oauthStateVersion = 1

// RefreshTokenSnapshot is the serializable state of a RefreshTokenStore.
//
// The store's own types are unexported, and deliberately stay that way: this
// is a separate, explicit shape so the file format is not hostage to a field
// rename inside the store.
//
// What lands on disk is not a credential. Tokens are keyed by their SHA-256
// hash exactly as they are in memory, so the file holds an identity mapping —
// who a hash belongs to — and never anything a client could present.
type RefreshTokenSnapshot struct {
	Tokens   map[string]RefreshTokenSnapEntry `json:"tokens"`
	Consumed map[string]string                `json:"consumed"`
	Grants   map[string][]string              `json:"grants"`
}

// oauthState is the file itself: everything the OAuth server keeps across a
// restart, under one version and one timestamp. The store snapshots are
// embedded so the wire format stays flat.
type oauthState struct {
	Version              int       `json:"version"`
	Timestamp            time.Time `json:"timestamp"`
	RefreshTokenSnapshot           // tokens, consumed, grants
}
```

In `Snapshot()`, delete the `Version` and `Timestamp` fields from the returned literal so it reads:

```go
	return RefreshTokenSnapshot{
		Tokens:   tokens,
		Consumed: consumed,
		Grants:   grants,
	}
```

- [ ] **Step 4: Rename the file functions and add the coordinator**

Rename `writeRefreshTokens` to `writeState` and change its parameter, and rename `readRefreshTokens` to `readState`:

```go
// writeState serializes the OAuth state to path using a temporary file and an
// atomic rename, so a crash mid-write leaves the previous file intact.
func writeState(path string, state oauthState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal oauth state: %w", err)
	}
	// ... body unchanged, with error strings reworded from
	// "refresh tokens" to "oauth state" ...
}

// readState reads a file written by writeState.
func readState(path string) (oauthState, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-configured
	if err != nil {
		return oauthState{}, fmt.Errorf("read oauth state: %w", err)
	}

	var state oauthState
	if err := json.Unmarshal(data, &state); err != nil {
		return oauthState{}, fmt.Errorf("unmarshal oauth state: %w", err)
	}

	if state.Version < 1 || state.Version > oauthStateVersion {
		return oauthState{}, fmt.Errorf(
			"unsupported oauth state version: got %d, supported 1..%d",
			state.Version,
			oauthStateVersion,
		)
	}

	return state, nil
}
```

Delete `persistRefreshTokens` entirely and add the coordinator in its place:

```go
// stateFile owns the OAuth server's durable state on disk.
//
// The stores cannot each hold their own writer: they share one file, so each
// would serialize the whole thing from its own view and drop the other's. The
// file is the single writer, and every store points its change hook here.
type stateFile struct {
	path   string
	tokens *RefreshTokenStore
}

// write serializes every store's current state. A failed write is logged and
// swallowed: the state is already live in memory, so refusing to serve would
// turn a durability problem into an outage. The operator learns that a restart
// will cost a re-authorization, which is the behaviour they had before this
// file existed.
func (f *stateFile) write() {
	state := oauthState{
		Version:              oauthStateVersion,
		Timestamp:            time.Now(),
		RefreshTokenSnapshot: f.tokens.Snapshot(),
	}

	if err := writeState(f.path, state); err != nil {
		slog.Warn(
			"MCP OAuth state write failed; tokens will not survive a restart",
			"error", err,
			"path", f.path,
		)
	}
}
```

- [ ] **Step 5: Replace the store's persist hook in `store.go`**

Change the struct field and its two methods:

```go
	// onChange is called after every mutation, always with mu released. Nil
	// when the store is memory-only.
	onChange func()
}

// SetOnChange installs the callback fired after every mutation. Call it during
// setup, before the store serves any request: it is not guarded by the mutex,
// because a hook swapped mid-flight would race the mutations it records.
func (s *RefreshTokenStore) SetOnChange(fn func()) {
	s.onChange = fn
}

// writeThrough notifies the owner of the durable state that it changed. It
// must be called with mu released, since the owner takes a snapshot, which
// acquires it.
func (s *RefreshTokenStore) writeThrough() {
	if s.onChange == nil {
		return
	}

	s.onChange()
}
```

- [ ] **Step 6: Rewire `NewServer` in `server.go`**

Replace the `cfg.TokenStorePath != ""` block:

```go
	refreshTokens := NewRefreshTokenStore()
	if cfg.TokenStorePath != "" {
		// A missing file is the normal first start. Anything else — corrupt
		// JSON, bad permissions, a version from a newer build — costs every
		// client a re-authorization, so it is worth an operator's attention.
		// Neither is fatal: the server comes up empty and clients re-authorize,
		// exactly as they did before the store existed.
		if state, err := readState(cfg.TokenStorePath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				slog.Info("no MCP OAuth state yet", "path", cfg.TokenStorePath)
			} else {
				slog.Warn(
					"could not read MCP OAuth state; clients must re-authorize",
					"error", err,
					"path", cfg.TokenStorePath,
				)
			}
		} else {
			refreshTokens.Restore(state.RefreshTokenSnapshot)
			slog.Info("loaded MCP OAuth state", "grants", len(state.Grants))
		}

		file := &stateFile{path: cfg.TokenStorePath, tokens: refreshTokens}
		refreshTokens.SetOnChange(file.write)
	}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `gofmt -w internal/mcp/oauth/ && go test ./internal/mcp/oauth/`
Expected: PASS. Then `go build ./... && golangci-lint run ./internal/...` → `0 issues`.

- [ ] **Step 8: Commit**

```bash
git add internal/mcp/oauth/persist.go internal/mcp/oauth/store.go internal/mcp/oauth/server.go internal/mcp/oauth/persist_test.go
git commit -m "refactor(mcp): let the state file own the write, not the store

Two stores cannot each hold a writer for one file: each would serialize
the whole thing from its own view and drop the other's. stateFile owns
the path and snapshots every store; a store now only announces that it
changed. No behaviour or format change."
```

---

### Task 2: Fingerprint what the consent page showed

**Files:**
- Create: `internal/mcp/oauth/consent_store.go`
- Test: `internal/mcp/oauth/consent_store_test.go`

**Interfaces:**
- Consumes: `ClientMetadata` from `cimd.go` (fields `ClientName string`, `RedirectURIs []string`).
- Produces: `func consentFingerprint(meta *ClientMetadata) string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/mcp/oauth/consent_store_test.go`:

```go
package oauth

import (
	"testing"
)

func TestConsentFingerprintIgnoresRedirectURIOrder(t *testing.T) {
	// CIMD documents are client-controlled and array order carries no meaning,
	// so re-prompting on a reordering would be noise.
	a := consentFingerprint(&ClientMetadata{
		ClientName:   "Acme CLI",
		RedirectURIs: []string{"http://localhost:1/cb", "http://localhost:2/cb"},
	})
	b := consentFingerprint(&ClientMetadata{
		ClientName:   "Acme CLI",
		RedirectURIs: []string{"http://localhost:2/cb", "http://localhost:1/cb"},
	})

	if a != b {
		t.Errorf("reordering redirect_uris changed the fingerprint: %q vs %q", a, b)
	}
}

func TestConsentFingerprintChangesWithTheDecision(t *testing.T) {
	base := &ClientMetadata{
		ClientName:   "Acme CLI",
		RedirectURIs: []string{"http://localhost:1/cb"},
	}
	baseline := consentFingerprint(base)

	tests := []struct {
		name string
		meta *ClientMetadata
	}{
		{
			name: "renamed",
			meta: &ClientMetadata{
				ClientName:   "Totally Different",
				RedirectURIs: []string{"http://localhost:1/cb"},
			},
		},
		{
			name: "redirect uri added",
			meta: &ClientMetadata{
				ClientName:   "Acme CLI",
				RedirectURIs: []string{"http://localhost:1/cb", "http://evil.example/cb"},
			},
		},
		{
			name: "redirect uri removed",
			meta: &ClientMetadata{
				ClientName:   "Acme CLI",
				RedirectURIs: nil,
			},
		},
		{
			name: "redirect uri edited",
			meta: &ClientMetadata{
				ClientName:   "Acme CLI",
				RedirectURIs: []string{"http://localhost:1/cb2"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := consentFingerprint(tc.meta); got == baseline {
				t.Error("fingerprint did not change, so a stale approval would carry over")
			}
		})
	}
}

func TestConsentFingerprintSeparatesFields(t *testing.T) {
	// Without a separator between the name and the URIs, ("ab", "c") and
	// ("a", "bc") would hash alike and one client could impersonate another.
	a := consentFingerprint(&ClientMetadata{ClientName: "ab", RedirectURIs: []string{"c"}})
	b := consentFingerprint(&ClientMetadata{ClientName: "a", RedirectURIs: []string{"bc"}})

	if a == b {
		t.Error("concatenation collision: two different clients share a fingerprint")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mcp/oauth/ -run TestConsentFingerprint 2>&1 | head`
Expected: FAIL, build error — `undefined: consentFingerprint`.

- [ ] **Step 3: Write the implementation**

Create `internal/mcp/oauth/consent_store.go`:

```go
package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"slices"
)

// consentFingerprintDomain separates this hash from every other use of
// SHA-256 in the package, so a value from one can never be mistaken for the
// other, and lets a future change to what is fingerprinted invalidate old
// records rather than silently reinterpret them.
const consentFingerprintDomain = "cetacean-consent-v1"

// consentFingerprint hashes what the consent page showed the user: the
// client's name, and the exact set of URIs it may receive a code at.
//
// A CIMD client controls its own metadata document and may change it at any
// time. Binding a remembered approval to this hash means a rename or a new
// redirect_uri re-prompts, so an approval can never be inherited by a client
// the user would not recognise, or redirect somewhere they never saw.
//
// The URIs are sorted because array order in a client-controlled document
// carries no meaning; re-prompting on a reordering would be noise. Fields are
// NUL-separated so ("ab", "c") cannot hash the same as ("a", "bc").
func consentFingerprint(meta *ClientMetadata) string {
	uris := slices.Clone(meta.RedirectURIs)
	slices.Sort(uris)

	h := sha256.New()
	h.Write([]byte(consentFingerprintDomain))
	h.Write([]byte{0})
	h.Write([]byte(meta.ClientName))

	for _, uri := range uris {
		h.Write([]byte{0})
		h.Write([]byte(uri))
	}

	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/mcp/oauth/ && go test ./internal/mcp/oauth/ -run TestConsentFingerprint -v 2>&1 | tail -20`
Expected: PASS, 3 tests plus 4 subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/oauth/consent_store.go internal/mcp/oauth/consent_store_test.go
git commit -m "feat(mcp): fingerprint the client metadata a user consented to

A CIMD client controls its own metadata document. Binding a remembered
approval to a hash of the name and redirect URIs the consent page showed
means a rename or a new redirect_uri re-prompts, rather than inheriting
an approval the user never gave."
```

---

### Task 3: The consent store

**Files:**
- Modify: `internal/mcp/oauth/consent_store.go`
- Test: `internal/mcp/oauth/consent_store_test.go`

**Interfaces:**
- Consumes: `consentFingerprint` from Task 2.
- Produces: `type ConsentRecord struct { Subject, ClientID, Resource, Fingerprint string; GrantedAt time.Time }`; `func NewConsentStore() *ConsentStore`; `func (s *ConsentStore) SetOnChange(fn func())`; `func (s *ConsentStore) Allows(subject, clientID, resource, fingerprint string) bool`; `func (s *ConsentStore) Remember(subject, clientID, resource, fingerprint string)`; `func (s *ConsentStore) Forget(subject, clientID, resource string)`; `func (s *ConsentStore) Snapshot() []ConsentRecord`; `func (s *ConsentStore) Restore(records []ConsentRecord)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/mcp/oauth/consent_store_test.go` (and add `"time"` to its imports):

```go
const (
	testSubject     = "user@example.com"
	testClientID    = "https://example.com/client.json"
	testResource    = "https://cetacean.example.com/mcp"
	testFingerprint = "fingerprint-a"
)

func TestConsentStoreRemembersAnApproval(t *testing.T) {
	s := NewConsentStore()
	s.Remember(testSubject, testClientID, testResource, testFingerprint)

	if !s.Allows(testSubject, testClientID, testResource, testFingerprint) {
		t.Error("a remembered approval should be allowed")
	}
}

func TestConsentStoreRequiresAnExactMatch(t *testing.T) {
	s := NewConsentStore()
	s.Remember(testSubject, testClientID, testResource, testFingerprint)

	tests := []struct {
		name                                        string
		subject, clientID, resource, fingerprint    string
	}{
		{
			name:  "another user",
			subject: "someone@example.com", clientID: testClientID,
			resource: testResource, fingerprint: testFingerprint,
		},
		{
			name:  "another client",
			subject: testSubject, clientID: "https://other.example/client.json",
			resource: testResource, fingerprint: testFingerprint,
		},
		{
			// RFC 8707: an approval for one MCP endpoint must not cover another.
			name:  "another resource",
			subject: testSubject, clientID: testClientID,
			resource: "https://other.example.com/mcp", fingerprint: testFingerprint,
		},
		{
			// The client changed its metadata after the approval.
			name:  "changed metadata",
			subject: testSubject, clientID: testClientID,
			resource: testResource, fingerprint: "fingerprint-b",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if s.Allows(tc.subject, tc.clientID, tc.resource, tc.fingerprint) {
				t.Error("a non-matching request should re-prompt")
			}
		})
	}
}

func TestConsentStoreReplacesAStaleRecord(t *testing.T) {
	s := NewConsentStore()
	s.Remember(testSubject, testClientID, testResource, "fingerprint-a")
	s.Remember(testSubject, testClientID, testResource, "fingerprint-b")

	// A user must never hold two live approvals for one client, one of which
	// they no longer remember granting.
	if got := len(s.Snapshot()); got != 1 {
		t.Errorf("records = %d, want the stale one replaced", got)
	}
	if s.Allows(testSubject, testClientID, testResource, "fingerprint-a") {
		t.Error("the replaced approval should no longer be allowed")
	}
}

func TestConsentStoreForgets(t *testing.T) {
	s := NewConsentStore()
	s.Remember(testSubject, testClientID, testResource, testFingerprint)
	s.Forget(testSubject, testClientID, testResource)

	if s.Allows(testSubject, testClientID, testResource, testFingerprint) {
		t.Error("a forgotten approval should re-prompt")
	}
}

func TestConsentStoreNotifiesOnlyOnRealChanges(t *testing.T) {
	s := NewConsentStore()

	var writes int
	s.SetOnChange(func() { writes++ })

	s.Remember(testSubject, testClientID, testResource, testFingerprint)
	s.Remember(testSubject, testClientID, testResource, testFingerprint) // identical
	s.Forget(testSubject, "https://unknown.example/client.json", testResource)

	// Re-approving an unchanged client and forgetting an unknown one change
	// nothing, and a whole-file rewrite for each would be work for no reason.
	if writes != 1 {
		t.Errorf("writes = %d, want 1 (only the first Remember changed state)", writes)
	}
}

func TestConsentStoreSnapshotIsDeterministic(t *testing.T) {
	s := NewConsentStore()
	s.Remember("b@example.com", testClientID, testResource, testFingerprint)
	s.Remember("a@example.com", testClientID, testResource, testFingerprint)

	// Map iteration order is random; an unsorted snapshot would rewrite the
	// file with reordered records on every unrelated change.
	first := s.Snapshot()
	for range 5 {
		next := s.Snapshot()
		for i := range first {
			if first[i].Subject != next[i].Subject {
				t.Fatal("snapshot order is not stable")
			}
		}
	}
	if first[0].Subject != "a@example.com" {
		t.Errorf("first record = %q, want the sorted-first subject", first[0].Subject)
	}
}

func TestConsentStoreRestoreReplacesState(t *testing.T) {
	s := NewConsentStore()
	s.Remember("stale@example.com", testClientID, testResource, testFingerprint)

	s.Restore([]ConsentRecord{{
		Subject:     testSubject,
		ClientID:    testClientID,
		Resource:    testResource,
		Fingerprint: testFingerprint,
		GrantedAt:   time.Now(),
	}})

	if s.Allows("stale@example.com", testClientID, testResource, testFingerprint) {
		t.Error("Restore should replace state, not merge into it")
	}
	if !s.Allows(testSubject, testClientID, testResource, testFingerprint) {
		t.Error("the restored record should be allowed")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mcp/oauth/ -run TestConsentStore 2>&1 | head`
Expected: FAIL, build error — `undefined: NewConsentStore`, `undefined: ConsentRecord`.

- [ ] **Step 3: Write the implementation**

Append to `internal/mcp/oauth/consent_store.go`, adding `"slices"` (already imported), `"strings"`, `"sync"` and `"time"` to the import block:

```go
// ConsentRecord is one remembered approval: this user approved this client,
// as it looked at the time, for this MCP endpoint.
type ConsentRecord struct {
	Subject     string    `json:"subject"`
	ClientID    string    `json:"clientId"`
	Resource    string    `json:"resource"`
	Fingerprint string    `json:"fingerprint"`
	GrantedAt   time.Time `json:"grantedAt"`
}

// ConsentStore remembers approvals so an already-approved client does not
// re-prompt on every authorization request.
//
// Unlike the refresh token store, whose file holds only hashes and is
// therefore a confidentiality concern, a consent record is a capability:
// anyone able to write it can pre-approve a client and complete an
// authorization with no human in the loop. The file is mode 0600 and the
// caveat is the usual one — an attacker with host access has already won —
// but the property being protected is integrity, not secrecy.
type ConsentStore struct {
	mu      sync.Mutex
	records map[string]ConsentRecord

	// onChange is called after every mutation that changed state, always with
	// mu released. Nil when the store is memory-only.
	onChange func()
}

// NewConsentStore returns a ready-to-use ConsentStore.
func NewConsentStore() *ConsentStore {
	return &ConsentStore{
		records: make(map[string]ConsentRecord),
	}
}

// SetOnChange installs the callback fired after every mutation. Call it during
// setup, before the store serves any request: it is not guarded by the mutex,
// because a hook swapped mid-flight would race the mutations it records.
func (s *ConsentStore) SetOnChange(fn func()) {
	s.onChange = fn
}

// writeThrough notifies the owner of the durable state that it changed. It
// must be called with mu released, since the owner takes a snapshot, which
// acquires it.
func (s *ConsentStore) writeThrough() {
	if s.onChange == nil {
		return
	}

	s.onChange()
}

// consentKey identifies the approval, without the fingerprint: the fingerprint
// lives in the value, so a client whose metadata changed replaces its stale
// record rather than accumulating a second one alongside it.
//
// NUL cannot appear in a subject, an https:// client_id or a resource URL, so
// joining on it cannot let one triple spell another.
func consentKey(subject, clientID, resource string) string {
	return strings.Join([]string{subject, clientID, resource}, "\x00")
}

// Allows reports whether this exact approval was remembered — same user, same
// client, same MCP endpoint, and the same client metadata they were shown.
func (s *ConsentStore) Allows(subject, clientID, resource, fingerprint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, exists := s.records[consentKey(subject, clientID, resource)]

	return exists && record.Fingerprint == fingerprint
}

// Remember records an approval, replacing any earlier one for the same user,
// client and resource.
func (s *ConsentStore) Remember(subject, clientID, resource, fingerprint string) {
	key := consentKey(subject, clientID, resource)

	s.mu.Lock()
	existing, exists := s.records[key]
	unchanged := exists && existing.Fingerprint == fingerprint
	if !unchanged {
		s.records[key] = ConsentRecord{
			Subject:     subject,
			ClientID:    clientID,
			Resource:    resource,
			Fingerprint: fingerprint,
			GrantedAt:   time.Now(),
		}
	}
	s.mu.Unlock()

	// Re-approving an unchanged client changes nothing durable, and rewriting
	// the whole file for it would be work for no reason.
	if unchanged {
		return
	}

	s.writeThrough()
}

// Forget drops an approval. Called when a grant is revoked or burned for
// theft, so revocation cannot degrade into "grant one more silent
// re-authorization".
func (s *ConsentStore) Forget(subject, clientID, resource string) {
	key := consentKey(subject, clientID, resource)

	s.mu.Lock()
	_, existed := s.records[key]
	delete(s.records, key)
	s.mu.Unlock()

	if !existed {
		return
	}

	s.writeThrough()
}

// Snapshot returns the records in a stable order. Map iteration is random, and
// an unsorted snapshot would rewrite the file with reordered records on every
// unrelated change.
func (s *ConsentStore) Snapshot() []ConsentRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	records := make([]ConsentRecord, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}

	slices.SortFunc(records, func(a, b ConsentRecord) int {
		if c := strings.Compare(a.Subject, b.Subject); c != 0 {
			return c
		}
		if c := strings.Compare(a.ClientID, b.ClientID); c != 0 {
			return c
		}

		return strings.Compare(a.Resource, b.Resource)
	})

	return records
}

// Restore replaces the store's state from a snapshot.
func (s *ConsentStore) Restore(records []ConsentRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.records = make(map[string]ConsentRecord, len(records))
	for _, record := range records {
		s.records[consentKey(record.Subject, record.ClientID, record.Resource)] = record
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/mcp/oauth/ && go test ./internal/mcp/oauth/ -run TestConsent && go test -race ./internal/mcp/oauth/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/oauth/consent_store.go internal/mcp/oauth/consent_store_test.go
git commit -m "feat(mcp): add the consent store

Remembers an approval per user, client and RFC 8707 resource, carrying
the fingerprint of the metadata it was granted against. Matching is
exact on all four, so a changed client re-prompts and replaces its
stale record rather than accumulating a second one."
```

---

### Task 4: Persist consent records (file format v2)

**Files:**
- Modify: `internal/mcp/oauth/persist.go`
- Modify: `internal/mcp/oauth/server.go`
- Test: `internal/mcp/oauth/consent_store_test.go`

**Interfaces:**
- Consumes: `oauthState`, `stateFile`, `writeState`, `readState` from Task 1; `ConsentStore`, `ConsentRecord` from Task 3.
- Produces: `oauthState.Consent []ConsentRecord`; `stateFile.consent *ConsentStore`; `Server.consent *ConsentStore`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/mcp/oauth/consent_store_test.go` (add `"os"` to its imports):

```go
func TestConsentSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	cfg := ServerConfig{
		Issuer:         "https://cetacean.test",
		MCPResource:    testResource,
		MCP:            config.MCPConfig{AccessTokenTTL: time.Hour, RefreshTokenTTL: 720 * time.Hour},
		SigningKey:     []byte("test-signing-key-32bytes-padded!!"),
		TokenStorePath: path,
	}

	before := NewServer(cfg)
	before.consent.Remember(testSubject, testClientID, testResource, testFingerprint)

	// A second Server over the same path stands in for the process restarting.
	after := NewServer(cfg)

	if !after.consent.Allows(testSubject, testClientID, testResource, testFingerprint) {
		t.Fatal("an approval granted before the restart should still be allowed")
	}
}

func TestConsentIsWrittenThrough(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	consent := NewConsentStore()
	tokens := NewRefreshTokenStore()
	file := &stateFile{path: path, tokens: tokens, consent: consent}
	consent.SetOnChange(file.write)

	consent.Remember(testSubject, testClientID, testResource, testFingerprint)

	state, err := readState(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if state.Version != oauthStateVersion {
		t.Errorf("version = %d, want %d", state.Version, oauthStateVersion)
	}
	if len(state.Consent) != 1 {
		t.Fatalf("consent records on disk = %d, want 1", len(state.Consent))
	}
	if state.Consent[0].Fingerprint != testFingerprint {
		t.Errorf("fingerprint = %q", state.Consent[0].Fingerprint)
	}
}

func TestVersion1FileLoadsWithoutConsent(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	// A file written before consent records existed. It must load, not error:
	// an operator upgrading should keep their refresh tokens.
	v1 := []byte(`{"version":1,"tokens":{},"consumed":{},"grants":{}}`)
	if err := os.WriteFile(path, v1, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	state, err := readState(path)
	if err != nil {
		t.Fatalf("a v1 file should still load: %v", err)
	}
	if len(state.Consent) != 0 {
		t.Errorf("consent records = %d, want none", len(state.Consent))
	}
}
```

Add the config import if it is not already present in this file: `"github.com/radiergummi/cetacean/internal/config"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mcp/oauth/ -run 'TestConsentSurvives|TestConsentIsWritten|TestVersion1' 2>&1 | head`
Expected: FAIL, build error — `unknown field consent in struct literal of type stateFile`, `before.consent undefined`, `state.Consent undefined`.

- [ ] **Step 3: Add consent to the file format**

In `internal/mcp/oauth/persist.go`, bump the version and extend the state:

```go
// oauthStateVersion is the on-disk format version. Bump it whenever the shape
// below changes incompatibly; readState refuses anything newer.
//
// v2 added consent records. A v1 file loads and yields none, which is exactly
// the pre-upgrade behaviour: every client is prompted once more, then
// remembered.
const oauthStateVersion = 2
```

```go
type oauthState struct {
	Version              int       `json:"version"`
	Timestamp            time.Time `json:"timestamp"`
	RefreshTokenSnapshot           // tokens, consumed, grants

	// Consent is a slice rather than a map: it is readable in the file, and it
	// avoids inventing an encoding for a composite key built from three
	// free-form strings.
	Consent []ConsentRecord `json:"consent,omitempty"`
}
```

Extend the coordinator:

```go
type stateFile struct {
	path    string
	tokens  *RefreshTokenStore
	consent *ConsentStore
}

func (f *stateFile) write() {
	state := oauthState{
		Version:              oauthStateVersion,
		Timestamp:            time.Now(),
		RefreshTokenSnapshot: f.tokens.Snapshot(),
		Consent:              f.consent.Snapshot(),
	}

	if err := writeState(f.path, state); err != nil {
		slog.Warn(
			"MCP OAuth state write failed; tokens and approvals will not survive a restart",
			"error", err,
			"path", f.path,
		)
	}
}
```

- [ ] **Step 4: Wire the store into the server**

In `internal/mcp/oauth/server.go`, add the field to `Server`:

```go
type Server struct {
	cfg           ServerConfig
	tokenIssuer   *TokenIssuer
	cimd          *CIMDFetcher
	authCodes     *AuthCodeStore
	refreshTokens *RefreshTokenStore
	consent       *ConsentStore
	clients       *ClientRegistry // nil when DCREnabled is false
}
```

In `NewServer`, construct it and extend the persistence block:

```go
	refreshTokens := NewRefreshTokenStore()
	consent := NewConsentStore()

	if cfg.TokenStorePath != "" {
		// ... the existing read + log block, unchanged, plus: ...
		} else {
			refreshTokens.Restore(state.RefreshTokenSnapshot)
			consent.Restore(state.Consent)
			slog.Info("loaded MCP OAuth state",
				"grants", len(state.Grants),
				"approvals", len(state.Consent),
			)
		}

		file := &stateFile{path: cfg.TokenStorePath, tokens: refreshTokens, consent: consent}
		refreshTokens.SetOnChange(file.write)
		consent.SetOnChange(file.write)
	}
```

and add `consent: consent,` to the returned `&Server{...}` literal.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w internal/mcp/oauth/ && go test ./internal/mcp/oauth/ && go build ./...`
Expected: PASS, and every pre-existing test still passes.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/oauth/persist.go internal/mcp/oauth/server.go internal/mcp/oauth/consent_store_test.go
git commit -m "feat(mcp): persist consent records alongside refresh tokens

File format goes to v2. A v1 file loads and yields no records, so an
upgrading operator keeps their refresh tokens and is prompted once more
per client."
```

---

### Task 5: Revocation and theft clear consent; expiry does not

`revokeGrantLocked` is reached from three callers and they must not behave alike. The decision cannot live inside it, so it reports the family's identity and each caller decides.

**Files:**
- Modify: `internal/mcp/oauth/store.go`
- Modify: `internal/mcp/oauth/server.go`
- Test: `internal/mcp/oauth/consent_store_test.go`

**Interfaces:**
- Consumes: `ConsentStore.Forget` from Task 3; `Server.consent` from Task 4.
- Produces: `func (s *RefreshTokenStore) RevokeGrant(token string) (RefreshTokenData, bool)` (was `func(string)`); `revokeGrantLocked(grantID string) (RefreshTokenData, bool)`; `RotateResult.Data` populated on the theft path.

- [ ] **Step 1: Write the failing tests**

Append to `internal/mcp/oauth/consent_store_test.go`:

```go
// consentServer builds a Server with a memory-only state file and one
// remembered approval for the token it returns.
func consentServer(t *testing.T) (*Server, string) {
	t.Helper()

	s := newTestServer(t)
	s.consent.Remember(testSubject, testClientID, s.cfg.MCPResource, testFingerprint)

	token := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  testSubject,
		ClientID: testClientID,
		Resource: s.cfg.MCPResource,
	}, time.Hour)

	return s, token
}

func TestRevocationClearsConsent(t *testing.T) {
	s, token := consentServer(t)

	data, ok := s.refreshTokens.RevokeGrant(token)
	if !ok {
		t.Fatal("revoking a live token should report what it revoked")
	}
	s.consent.Forget(data.Subject, data.ClientID, data.Resource)

	// A record outliving revocation degrades revoke into "grant one more
	// silent re-authorization", which is worse than not revoking because it
	// appears to have worked.
	if s.consent.Allows(testSubject, testClientID, s.cfg.MCPResource, testFingerprint) {
		t.Error("revocation should have cleared the approval")
	}
}

func TestTheftClearsConsent(t *testing.T) {
	s, token := consentServer(t)

	rotated := s.refreshTokens.Rotate(token, time.Hour)
	if !rotated.OK {
		t.Fatal("the first rotation should succeed")
	}

	replay := s.refreshTokens.Rotate(token, time.Hour)
	if !replay.Theft {
		t.Fatal("re-presenting a consumed token should be detected as theft")
	}
	if replay.Data.Subject != testSubject {
		t.Fatalf("theft result should name the burned family, got subject %q", replay.Data.Subject)
	}
	s.consent.Forget(replay.Data.Subject, replay.Data.ClientID, replay.Data.Resource)

	if s.consent.Allows(testSubject, testClientID, s.cfg.MCPResource, testFingerprint) {
		t.Error("a burned grant family should not leave a silent re-grant behind")
	}
}

func TestExpiryDoesNotClearConsent(t *testing.T) {
	s := newTestServer(t)
	s.consent.Remember(testSubject, testClientID, s.cfg.MCPResource, testFingerprint)

	expired := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  testSubject,
		ClientID: testClientID,
		Resource: s.cfg.MCPResource,
	}, -time.Second)

	if s.refreshTokens.Rotate(expired, time.Hour).OK {
		t.Fatal("an expired token should not rotate")
	}

	// Consent outliving the refresh token is the entire point of the feature.
	if !s.consent.Allows(testSubject, testClientID, s.cfg.MCPResource, testFingerprint) {
		t.Error("expiry must not clear consent")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mcp/oauth/ -run 'ClearsConsent|DoesNotClearConsent' 2>&1 | head`
Expected: FAIL — `s.refreshTokens.RevokeGrant(token) used as value` (it returns nothing today), and `TestTheftClearsConsent` failing on `replay.Data.Subject` being empty.

- [ ] **Step 3: Make the store report what it revoked**

In `internal/mcp/oauth/store.go`, change `revokeGrantLocked` to report the family's identity:

```go
// revokeGrantLocked removes every hash ever associated with grantID from both
// tokens and consumed, then drops the grant record. It returns the identity
// the family belonged to, read from its live token — a family has exactly one,
// unless it has already expired.
//
// Whether revoking a family should also drop the user's consent record is the
// caller's decision, not this function's: an explicit revocation and a theft
// must clear it, and an expiry must not, because consent outliving the refresh
// token is the point of remembering it. Must be called with s.mu held.
func (s *RefreshTokenStore) revokeGrantLocked(grantID string) (RefreshTokenData, bool) {
	hashes := s.grants[grantID]

	var (
		owner RefreshTokenData
		found bool
	)

	for _, h := range hashes {
		if entry, live := s.tokens[h]; live && !found {
			owner = entry.data
			owner.Groups = append([]string(nil), entry.data.Groups...)
			found = true
		}

		delete(s.tokens, h)
		delete(s.consumed, h)
	}

	delete(s.grants, grantID)

	return owner, found
}
```

In `rotate`, populate the theft result and discard the identity on the expiry path:

```go
	if grantID, isConsumed := s.consumed[oldHash]; isConsumed {
		// Naming the burned family lets the caller clear the user's consent
		// record and log who was affected, neither of which was possible when
		// this returned a zero result.
		owner, _ := s.revokeGrantLocked(grantID)

		return RotateResult{Theft: true, Data: owner}, true
	}
```

```go
	if time.Now().After(entry.expiresAt) {
		// The identity is deliberately dropped: an expired grant must not
		// clear the user's consent.
		s.revokeGrantLocked(entry.data.grantID)

		return RotateResult{}, true
	}
```

Update the `RotateResult` doc comment, replacing the sentence claiming `Data` is zero on theft:

```go
// RotateResult is returned by RefreshTokenStore.Rotate. On the theft path Data
// names the family that was burned, so the caller can clear its consent record
// and log the affected subject.
```

Also update the step-2 comment inside `Rotate`'s algorithm doc block, which currently says the grant family is burned "and Data is intentionally zero".

Change `RevokeGrant` to return what it revoked:

```go
// RevokeGrant revokes every token in the grant family that contains the given
// token, whether that token is currently live or already consumed. It returns
// the identity the family belonged to, so the caller can clear the matching
// consent record, and false when the token belongs to no known family.
func (s *RefreshTokenStore) RevokeGrant(token string) (RefreshTokenData, bool) {
	owner, revoked := s.revokeGrant(token)
	if revoked {
		s.writeThrough()
	}

	return owner, revoked
}

// revokeGrant revokes the family and reports whether one was found. A token
// belonging to no known family is a no-op, and must not write.
func (s *RefreshTokenStore) revokeGrant(token string) (RefreshTokenData, bool) {
	h := hashToken(token)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Determine grantID from either the live tokens map or the consumed map.
	grantID := ""
	if entry, exists := s.tokens[h]; exists {
		grantID = entry.data.grantID
	} else if id, exists := s.consumed[h]; exists {
		grantID = id
	}

	if grantID == "" {
		return RefreshTokenData{}, false
	}

	owner, _ := s.revokeGrantLocked(grantID)

	return owner, true
}
```

- [ ] **Step 4: Clear consent from the two handlers**

In `internal/mcp/oauth/server.go`, in `HandleRevoke`:

```go
	token := r.FormValue("token") // #nosec G120 -- bounded above
	if token != "" {
		// Revoking must also drop the approval, or the next authorization
		// request is granted silently and revocation only appeared to work.
		if data, revoked := s.refreshTokens.RevokeGrant(token); revoked {
			s.consent.Forget(data.Subject, data.ClientID, data.Resource)
		}
	}
```

In the refresh-grant path, immediately inside the existing `if result.Theft {` block and before whatever it already does:

```go
	if result.Theft {
		// A replayed token means someone else holds a copy. Re-prompting is
		// the point: the next authorization must reach a human.
		s.consent.Forget(result.Data.Subject, result.Data.ClientID, result.Data.Resource)
```

- [ ] **Step 5: Fix the other call sites**

`RevokeGrant` now returns two values. Search for every remaining caller and discard the results where the identity is not needed:

Run: `grep -rn "RevokeGrant(" internal/ | grep -v "func "`

In `internal/mcp/oauth/server_test.go` and `store_test.go`, change bare statement calls `s.refreshTokens.RevokeGrant(rt)` to `_, _ = s.refreshTokens.RevokeGrant(rt)` only where the compiler complains; a bare call as a statement remains valid Go and needs no change.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w internal/mcp/oauth/ && go test ./internal/mcp/oauth/ && go test -race ./internal/mcp/... && golangci-lint run ./internal/...`
Expected: PASS and `0 issues`.

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/oauth/store.go internal/mcp/oauth/server.go internal/mcp/oauth/consent_store_test.go
git commit -m "feat(mcp): clear consent on revocation and theft, never on expiry

revokeGrantLocked is reached from three callers that must not behave
alike, so it now reports the family's identity and each caller decides.
An approval outliving revocation would make revoke a lie; one cleared by
expiry would defeat the point of remembering it."
```

---

### Task 6: Skip the consent page for an approved client

**Files:**
- Modify: `internal/mcp/oauth/server.go`
- Modify: `internal/mcp/oauth/consent.go`
- Modify: `docs/mcp.md`
- Modify: `CHANGELOG.md`
- Test: `internal/mcp/oauth/consent_store_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2–5.
- Produces: `func (s *Server) issueCodeAndRedirect(w http.ResponseWriter, r *http.Request, p authorizedRequest)`; `type authorizedRequest struct`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/mcp/oauth/consent_store_test.go` (add `"net/http"`, `"net/http/httptest"`, `"net/url"`, `"strings"` and `"github.com/radiergummi/cetacean/internal/auth"` to its imports):

```go
// authorizeGET drives a GET /oauth/authorize for a CIMD client whose metadata
// document is served by a local test server, and returns the response.
func authorizeGET(t *testing.T, s *Server, clientID, redirectURI string) *httptest.ResponseRecorder {
	t.Helper()

	target := "/oauth/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {computeS256Challenge("verifier-verifier-verifier-1234")},
		"code_challenge_method": {"S256"},
		"state":                 {"xyz"},
		"resource":              {s.cfg.MCPResource},
	}.Encode()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{
		Subject:  testSubject,
		Provider: "none",
	}))

	w := httptest.NewRecorder()
	s.HandleAuthorize(w, req)

	return w
}

func TestApprovedClientSkipsTheConsentPage(t *testing.T) {
	s, clientID, redirectURI, meta := cimdServer(t)
	s.consent.Remember(testSubject, clientID, s.cfg.MCPResource, consentFingerprint(meta))

	w := authorizeGET(t, s, clientID, redirectURI)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect with a code)", w.Code, http.StatusFound)
	}

	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if location.Query().Get("code") == "" {
		t.Error("no authorization code in the redirect")
	}
	if strings.Contains(w.Body.String(), "Authorization Request") {
		t.Error("the consent page was rendered despite a matching approval")
	}
}

func TestChangedMetadataRePrompts(t *testing.T) {
	s, clientID, redirectURI, _ := cimdServer(t)

	// An approval granted against different metadata than the client now
	// publishes must not carry over.
	s.consent.Remember(testSubject, clientID, s.cfg.MCPResource, "fingerprint-from-before")

	w := authorizeGET(t, s, clientID, redirectURI)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (the consent page)", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Authorization Request") {
		t.Error("expected the consent page to be rendered")
	}
}
```

Add the `cimdServer` helper to the same file. It reuses `serveMetadata` from `cimd_test.go`:

```go
// cimdServer stands up a CIMD client whose metadata document is served over
// TLS from loopback, and returns a Server wired to fetch it.
func cimdServer(t *testing.T) (srv *Server, clientID, redirectURI string, meta *ClientMetadata) {
	t.Helper()

	published := ClientMetadata{
		ClientName:   "Acme CLI",
		RedirectURIs: []string{"https://example.com/cb"},
	}

	var documentURL string
	httpSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveMetadata(documentURL, published)(w, r)
	}))
	t.Cleanup(httpSrv.Close)
	documentURL = httpSrv.URL + "/client.json"

	srv = newTestServer(t)

	// The document is served with a self-signed certificate, so the fetcher
	// needs the test server's client to trust it. newTestServer already sets
	// AllowLoopback.
	srv.cimd.Client = httpSrv.Client()

	// consentFingerprint reads only ClientName and RedirectURIs, so this local
	// copy fingerprints identically to the fetched document.
	return srv, documentURL, published.RedirectURIs[0], &published
}
```

And add the case the spec calls for that the two tests above do not cover — an unverified client must never skip the page, even with a record that matches perfectly:

```go
func TestDynamicallyRegisteredClientNeverSkipsTheConsentPage(t *testing.T) {
	s := newTestServer(t)

	const clientID = "dcr-generated-client-id"
	const redirectURI = "http://127.0.0.1:1/cb"

	s.clients.register(&ClientRegistration{
		ClientID:     clientID,
		ClientName:   "Self-Registered CLI",
		RedirectURIs: []string{redirectURI},
	})

	// Seed a record that matches on every field. It must still be ignored:
	// a DCR client's metadata is self-reported, so a fingerprint over it
	// proves nothing about who the client is.
	s.consent.Remember(testSubject, clientID, s.cfg.MCPResource, consentFingerprint(&ClientMetadata{
		ClientName:   "Self-Registered CLI",
		RedirectURIs: []string{redirectURI},
	}))

	w := authorizeGET(t, s, clientID, redirectURI)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (the consent page)", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Authorization Request") {
		t.Error("an unverified client skipped the consent page")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mcp/oauth/ -run 'ConsentPage|RePrompts' 2>&1 | head`
Expected: FAIL — `TestApprovedClientSkipsTheConsentPage` gets status 200 and the consent page, because nothing consults the store yet. `TestChangedMetadataRePrompts` and `TestDynamicallyRegisteredClientNeverSkipsTheConsentPage` pass already, since nothing skips the page yet; they are the regression guards for step 4.

- [ ] **Step 3: Extract the code-issuing tail**

In `internal/mcp/oauth/server.go`, add above `handleAuthorizeGET`:

```go
// authorizedRequest carries the validated parameters of an authorization
// request that is ready to receive a code.
type authorizedRequest struct {
	clientID      string
	redirectURI   string
	codeChallenge string
	resource      string
	state         string
	subject       string
	groups        []string
}

// issueCodeAndRedirect mints an authorization code and redirects the browser
// back to the client with it. Both the consent-page POST and the skipped-consent
// GET path end here, so they cannot drift apart.
func (s *Server) issueCodeAndRedirect(
	w http.ResponseWriter,
	r *http.Request,
	p authorizedRequest,
) {
	rawCode := s.authCodes.Issue(AuthCodeData{
		ClientID:      p.clientID,
		RedirectURI:   p.redirectURI,
		CodeChallenge: p.codeChallenge,
		Resource:      p.resource,
		Subject:       p.subject,
		Groups:        p.groups,
	}, authCodeTTL)

	redirectURI, _ := url.Parse(p.redirectURI)
	q := redirectURI.Query()
	q.Set("code", rawCode)

	if p.state != "" {
		q.Set("state", p.state)
	}

	// RFC 9207: name the issuer in the authorization response so a client
	// configured with several authorization servers cannot be tricked into
	// redeeming this code at the wrong one (mix-up attack).
	q.Set("iss", s.cfg.issuerID())
	redirectURI.RawQuery = q.Encode()

	//nolint:gosec // G710: p.redirectURI was exact-matched against the client's registered redirect_uris (HasRedirectURI) by both callers before reaching here; this is a pre-validated URI, not open redirect.
	http.Redirect(w, r, redirectURI.String(), http.StatusFound)
}
```

Then replace the tail of `handleAuthorizePOST`, from the `// Issue authorization code.` comment to the end of the function, with:

```go
	// Clear the CSRF cookie — the flow is complete.
	clearCSRFCookie(w, secure)

	// Remembering is limited to verified clients. A DCR client's metadata is
	// self-reported and its client_id does not survive a restart, so a record
	// keyed on one would be worthless at best.
	if verified {
		s.consent.Remember(identity.Subject, clientID, effectiveResource, consentFingerprint(meta))
	}

	s.issueCodeAndRedirect(w, r, authorizedRequest{
		clientID:      clientID,
		redirectURI:   redirectURIRaw,
		codeChallenge: codeChallenge,
		resource:      effectiveResource,
		state:         state,
		subject:       identity.Subject,
		groups:        identity.Groups,
	})
}
```

`handleAuthorizePOST` currently discards the `verified` return of `resolveClientMeta`; change `meta, _, errMsg := s.resolveClientMeta(r, clientID)` to `meta, verified, errMsg := s.resolveClientMeta(r, clientID)`.

- [ ] **Step 4: Consult the store in the GET path**

In `handleAuthorizeGET`, capture the effective resource by changing

```go
	if _, err := ValidateResourceIndicator(
```

to

```go
	effectiveResource, err := ValidateResourceIndicator(
```

(and adjust the following `err != nil` block accordingly, keeping the same `invalid_target` redirect).

Then insert this immediately after the identity check and before `issueCSRFNonce`:

```go
	// A remembered approval skips the page. Only for verified clients, and only
	// when the metadata still hashes to what the user was shown — a CIMD client
	// controls its own document and could otherwise redirect an inherited
	// approval somewhere the user never saw.
	//
	// Issuing a code from a GET is ordinary for an authorization endpoint, and
	// redirect_uri was exact-matched against the client's registered set above,
	// so a silently issued code still lands only where the client registered.
	if verified &&
		s.consent.Allows(identity.Subject, clientID, effectiveResource, consentFingerprint(meta)) {
		s.issueCodeAndRedirect(w, r, authorizedRequest{
			clientID:      clientID,
			redirectURI:   redirectURIRaw,
			codeChallenge: codeChallenge,
			resource:      effectiveResource,
			state:         state,
			subject:       identity.Subject,
			groups:        identity.Groups,
		})

		return
	}
```

- [ ] **Step 5: Tell the user the decision is remembered**

In `internal/mcp/oauth/consent.go`, add a field to `consentData`:

```go
type consentData struct {
	ClientName          string
	Verified            bool
	Remembered          bool
	RedirectURI         string
	// ... rest unchanged ...
}
```

and add this line to the template immediately after the `warning` div:

```html
{{if .Remembered}}<div class="warning">Approving will be remembered for <strong>{{.ClientName}}</strong> until you revoke its access.</div>{{end}}
```

In `handleAuthorizeGET`'s `renderConsent` call, set `Remembered: verified` — turning "approve once" into "approve until revoked" without saying so is not consent, and an unverified client is not remembered, so it must not claim to be.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w internal/mcp/oauth/ && go test ./internal/mcp/oauth/ && go test -race ./internal/mcp/...`
Expected: PASS, including every pre-existing authorize test.

- [ ] **Step 7: Update the docs**

In `docs/mcp.md`, replace the first "Known limitations" bullet's closing sentences about consent, and add a short section under the authorization documentation describing the behaviour:

```markdown
### Remembered approvals

Approving a client is remembered, so you are asked once rather than every time
its refresh token expires. A record ties your identity to that client, the MCP
endpoint it asked for, and a fingerprint of the client's name and redirect URIs
as you were shown them.

You are asked again when any of those change — including when a client updates
its own metadata document, since a client identified by URL controls what that
document says — and when the grant is revoked or Cetacean detects a stolen
refresh token. Clients that registered dynamically are never remembered: their
metadata is self-reported.

Approvals live in `mcp-tokens.json` beside the refresh tokens. Note the file's
threat model differs between the two: refresh tokens are stored as SHA-256
hashes, which are useless to anyone who steals the file, while an approval is a
capability — anyone able to *write* the file could pre-approve a client. The
file is mode `0600`, and as always an attacker with host access has already won.
```

In `CHANGELOG.md`, add under `### Added` in `[Unreleased]`:

```markdown
- Approving an MCP client is now remembered, so you are asked once instead of every time its access expires. Cetacean asks again if the client changes its name or where it sends you, if you revoke its access, or if a stolen token is detected
```

- [ ] **Step 8: Verify everything**

Run: `go build ./... && go test ./... && golangci-lint run ./internal/... . && golangci-lint fmt --diff ./... 2>&1 | diff /dev/null -`
Expected: all pass, `0 issues`, no formatting diff.

- [ ] **Step 9: Commit**

```bash
git add internal/mcp/oauth/server.go internal/mcp/oauth/consent.go internal/mcp/oauth/consent_store_test.go docs/mcp.md CHANGELOG.md
git commit -m "feat(mcp): skip the consent page for an approved client

A verified client whose metadata still matches what the user was shown
receives a code without a prompt. Both paths now share one
code-issuing helper, and the page says approving will be remembered."
```
