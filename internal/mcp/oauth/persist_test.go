package oauth

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// restart round-trips a store through the on-disk format the way a process
// restart does, so a test asserts against what actually survives rather than
// against the in-memory maps it started from.
func restart(t *testing.T, s *RefreshTokenStore, path string) *RefreshTokenStore {
	t.Helper()

	if err := writeState(path, tokenState(s.Snapshot())); err != nil {
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

// tokenState wraps a refresh-token snapshot in the file envelope, so a new
// envelope field is reconsidered in one place rather than at each writer.
func tokenState(snap RefreshTokenSnapshot) oauthState {
	return oauthState{
		Version:              oauthStateVersion,
		Timestamp:            time.Now(),
		RefreshTokenSnapshot: snap,
	}
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

// persisting wires a store to a file the way NewServer does, and returns the
// ConsentStore sharing that file so a caller can drive the other half of it.
// Both hooks are installed, so these tests exercise the same stateFile wiring
// NewServer builds rather than a partial stand-in.
func persisting(s *RefreshTokenStore, path string) *ConsentStore {
	consent := NewConsentStore(testConsentTTL)

	file := &stateFile{path: path, tokens: s, consent: consent}
	s.SetOnChange(file.write)
	consent.SetOnChange(file.write)

	return consent
}

func TestRefreshTokenSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	token := s.Issue(RefreshTokenData{
		Subject:  "user@example.com",
		Groups:   []string{"ops"},
		ClientID: "https://example.com/client",
		Resource: "https://cetacean.example.com/mcp",
	}, time.Hour)

	restored := restart(t, s, path)

	data, ok := restored.Validate(token)
	if !ok {
		t.Fatal("token issued before the restart should still validate")
	}
	if data.Subject != "user@example.com" {
		t.Errorf("subject = %q", data.Subject)
	}
	if data.ClientID != "https://example.com/client" {
		t.Errorf("client id = %q", data.ClientID)
	}
	if data.Resource != "https://cetacean.example.com/mcp" {
		t.Errorf("resource = %q", data.Resource)
	}
	if len(data.Groups) != 1 || data.Groups[0] != "ops" {
		t.Errorf("groups = %v", data.Groups)
	}
}

func TestTheftDetectionSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	first := s.Issue(RefreshTokenData{Subject: "user@example.com"}, time.Hour)

	rotated := s.Rotate(first, time.Hour)
	if !rotated.OK {
		t.Fatal("rotation before the restart should succeed")
	}

	restored := restart(t, s, path)

	// The pre-restart token was consumed by the rotation. Re-presenting it is
	// the signal that someone else holds a copy, and it must still burn the
	// grant family — persisting live tokens without the rotation history would
	// silently turn this back into a successful rotation.
	replay := restored.Rotate(first, time.Hour)
	if !replay.Theft {
		t.Fatal("replaying a token consumed before the restart should be detected as theft")
	}

	if _, ok := restored.Validate(rotated.NewToken); ok {
		t.Fatal("theft should have burned the whole grant family, including the live token")
	}
}

func TestExpiredGrantFamilyIsDroppedOnLoad(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	live := s.Issue(RefreshTokenData{Subject: "live@example.com"}, time.Hour)

	// A family that rotates once and then expires. Rotation leaves a consumed
	// hash behind, so this exercises all three maps, not just tokens.
	doomed := s.Issue(RefreshTokenData{Subject: "expired@example.com"}, time.Hour)
	if !s.Rotate(doomed, time.Hour).OK {
		t.Fatal("rotation before expiry should succeed")
	}

	// Age that family into the past by editing the snapshot rather than
	// sleeping: a real store reaches this state by sitting on disk across the
	// token's lifetime, and a wall-clock sleep only makes the test flaky.
	snap := s.Snapshot()
	past := time.Now().Add(-time.Minute)

	for hash, entry := range snap.Tokens {
		if entry.Subject != "expired@example.com" {
			continue
		}

		entry.ExpiresAt = past
		entry.GrantExpiresAt = past
		snap.Tokens[hash] = entry
	}

	if err := writeState(path, tokenState(snap)); err != nil {
		t.Fatalf("write: %v", err)
	}

	restored := NewRefreshTokenStore()
	restored.Restore(onDisk(t, path).RefreshTokenSnapshot)

	if _, ok := restored.Validate(live); !ok {
		t.Fatal("the unexpired token should still validate")
	}

	// An expired family can never rotate again, so carrying its rotation
	// history forward would grow the file for no benefit.
	reloaded := restored.Snapshot()
	if len(reloaded.Tokens) != 1 {
		t.Errorf("tokens = %d, want only the live one", len(reloaded.Tokens))
	}
	if len(reloaded.Grants) != 1 {
		t.Errorf("grants = %d, want only the live family", len(reloaded.Grants))
	}
	if len(reloaded.Consumed) != 0 {
		t.Errorf("consumed = %d, want the expired family's history dropped", len(reloaded.Consumed))
	}
}

func TestRefreshTokenFileIsPrivateAndAtomic(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	s.Issue(RefreshTokenData{Subject: "user@example.com"}, time.Hour)

	if err := writeState(path, tokenState(s.Snapshot())); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("mode = %04o, want 0600", perm)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("data dir holds %d files, want only the store itself", len(entries))
	}
}

func TestReadRefreshTokensRejectsUnreadableFiles(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		content []byte
	}{
		{name: "missing", path: dir + "/absent.json"},
		{name: "corrupt", path: dir + "/corrupt.json", content: []byte("{not json")},
		{
			name:    "from a newer version",
			path:    dir + "/future.json",
			content: []byte(`{"version":99,"tokens":{},"consumed":{},"grants":{}}`),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.content != nil {
				if err := os.WriteFile(tc.path, tc.content, 0600); err != nil {
					t.Fatalf("write fixture: %v", err)
				}
			}

			if _, err := readState(tc.path); err == nil {
				t.Fatal("expected an error the caller can log and carry on from")
			}
		})
	}
}

func TestIssueIsWrittenThrough(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	persisting(s, path)

	// Nothing calls save: a token the client already holds must be durable by
	// the time the token endpoint answers, or a crash loses exactly the token
	// this is meant to keep.
	s.Issue(RefreshTokenData{Subject: "user@example.com"}, time.Hour)

	if got := len(onDisk(t, path).Tokens); got != 1 {
		t.Errorf("tokens on disk = %d, want 1", got)
	}
}

func TestRotateIsWrittenThrough(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	persisting(s, path)

	token := s.Issue(RefreshTokenData{Subject: "user@example.com"}, time.Hour)
	if !s.Rotate(token, time.Hour).OK {
		t.Fatal("rotation should succeed")
	}

	snap := onDisk(t, path)
	if len(snap.Tokens) != 1 {
		t.Errorf("tokens on disk = %d, want the replacement only", len(snap.Tokens))
	}
	if len(snap.Consumed) != 1 {
		t.Errorf("consumed on disk = %d, want the rotated-away hash", len(snap.Consumed))
	}
}

func TestRevokeIsWrittenThrough(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	persisting(s, path)

	token := s.Issue(RefreshTokenData{Subject: "user@example.com"}, time.Hour)
	s.RevokeGrant(token)

	// A revocation that only reaches memory would come back on the next
	// restart, which is worse than not revoking at all.
	if got := len(onDisk(t, path).Tokens); got != 0 {
		t.Errorf("tokens on disk = %d, want the revoked family gone", got)
	}
}

func TestServerCarriesRefreshTokensAcrossRestart(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	before := newPersistingServer(t, path, "https://cetacean.test/mcp")
	token := before.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user@example.com",
		ClientID: "https://example.com/client",
		Resource: "https://cetacean.test/mcp",
	}, 720*time.Hour)

	// A second Server over the same path stands in for the process restarting.
	after := newPersistingServer(t, path, "https://cetacean.test/mcp")

	if _, ok := after.refreshTokens.Validate(token); !ok {
		t.Fatal("a refresh token issued before the restart should still validate")
	}
}

func TestServerWithoutStatePathKeepsTokensInMemory(t *testing.T) {
	s := newTestServer(t)

	token := s.refreshTokens.Issue(RefreshTokenData{Subject: "user@example.com"}, time.Hour)
	if _, ok := s.refreshTokens.Validate(token); !ok {
		t.Fatal("a memory-only store should still issue usable tokens")
	}

	// With no path configured there is nowhere a write could be observed, so
	// the assertion is on the wiring itself: no persister was installed, which
	// is what keeps a read-only deployment from warning on every mutation.
	if s.refreshTokens.onChange != nil {
		t.Error("a server with no token store path should install no change hook")
	}
}

func TestUnknownTokenRotationDoesNotWrite(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	persisting(s, path)
	s.Issue(RefreshTokenData{Subject: "user@example.com"}, time.Hour)

	// Removing the file makes a subsequent write observable: anyone can post a
	// made-up refresh token to the token endpoint, and answering each one with
	// a whole-file rewrite hands an unauthenticated caller an amplified write.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	s.Rotate("not-a-real-token", time.Hour)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("rotating an unknown token wrote to disk; it changes no state")
	}
}

func TestUnknownTokenRevocationDoesNotWrite(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	persisting(s, path)
	s.Issue(RefreshTokenData{Subject: "user@example.com"}, time.Hour)

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	s.RevokeGrant("not-a-real-token")

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("revoking an unknown token wrote to disk; it changes no state")
	}
}

func TestStateFileSerializesConcurrentWriters(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	tokens := NewRefreshTokenStore()
	consent := persisting(tokens, path)

	// Two independent stores now write the same file from their own hooks.
	// Unserialized, each snapshots at its own moment and opens the same temp
	// path, so the rename publishes either a stale snapshot — silently losing
	// a token or an approval still live in memory — or structurally mixed
	// bytes that fail to parse at the next start, costing every client a
	// re-authorization.
	//
	// This exercises that path rather than proving it: the corruption is in
	// the file, not in memory, so the race detector cannot see it and the
	// interleaving does not reproduce on demand. It is a regression guard on
	// the invariant — a published file always parses and always holds both
	// stores — not a reproduction of the unserialized failure.
	const rounds = 25

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		for i := range rounds {
			tokens.Issue(RefreshTokenData{
				Subject:  fmt.Sprintf("token-%d@example.com", i),
				ClientID: testClientID,
				Resource: testResource,
			}, time.Hour)
		}
	}()

	go func() {
		defer wg.Done()

		for i := range rounds {
			subject := fmt.Sprintf("consent-%d@example.com", i)
			consent.Remember(keyFor(subject, testClientID, testResource), testFingerprint)
		}
	}()

	wg.Wait()

	state, err := readState(path)
	if err != nil {
		t.Fatalf("the published file must always parse: %v", err)
	}

	// Every mutation completed before the write it triggered, so whichever
	// write held the lock last saw both stores complete — and the file it
	// published must hold everything.
	if got := len(state.Tokens); got != rounds {
		t.Errorf("tokens on disk = %d, want %d", got, rounds)
	}
	if got := len(state.Consent); got != rounds {
		t.Errorf("consent records on disk = %d, want %d", got, rounds)
	}
}
