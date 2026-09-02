package oauth

import (
	"os"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/config"
)

// restart round-trips a store through the on-disk format the way a process
// restart does, so a test asserts against what actually survives rather than
// against the in-memory maps it started from.
func restart(t *testing.T, s *RefreshTokenStore, path string) *RefreshTokenStore {
	t.Helper()

	if err := writeRefreshTokens(path, s.Snapshot()); err != nil {
		t.Fatalf("write: %v", err)
	}

	snap, err := readRefreshTokens(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	restored := NewRefreshTokenStore()
	restored.Restore(snap)

	return restored
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
	doomed := s.Issue(RefreshTokenData{Subject: "expired@example.com"}, 30*time.Millisecond)
	if !s.Rotate(doomed, 30*time.Millisecond).OK {
		t.Fatal("rotation before expiry should succeed")
	}
	time.Sleep(40 * time.Millisecond)

	restored := restart(t, s, path)

	if _, ok := restored.Validate(live); !ok {
		t.Fatal("the unexpired token should still validate")
	}

	// An expired family can never rotate again, so carrying its rotation
	// history forward would grow the file for no benefit.
	snap := restored.Snapshot()
	if len(snap.Tokens) != 1 {
		t.Errorf("tokens = %d, want only the live one", len(snap.Tokens))
	}
	if len(snap.Grants) != 1 {
		t.Errorf("grants = %d, want only the live family", len(snap.Grants))
	}
	if len(snap.Consumed) != 0 {
		t.Errorf("consumed = %d, want the expired family's history dropped", len(snap.Consumed))
	}
}

func TestRefreshTokenFileIsPrivateAndAtomic(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	s.Issue(RefreshTokenData{Subject: "user@example.com"}, time.Hour)

	if err := writeRefreshTokens(path, s.Snapshot()); err != nil {
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

			if _, err := readRefreshTokens(tc.path); err == nil {
				t.Fatal("expected an error the caller can log and carry on from")
			}
		})
	}
}

// onDisk reads back what the store has written without going through it, so
// these tests assert on the file rather than on the store's own memory.
func onDisk(t *testing.T, path string) RefreshTokenSnapshot {
	t.Helper()

	snap, err := readRefreshTokens(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	return snap
}

func TestIssueIsWrittenThrough(t *testing.T) {
	path := t.TempDir() + "/mcp-tokens.json"

	s := NewRefreshTokenStore()
	s.SetPersistFunc(persistRefreshTokens(path))

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
	s.SetPersistFunc(persistRefreshTokens(path))

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
	s.SetPersistFunc(persistRefreshTokens(path))

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

	cfg := ServerConfig{
		Issuer:      "https://cetacean.test",
		MCPResource: "https://cetacean.test/mcp",
		MCP: config.MCPConfig{
			AccessTokenTTL:  time.Hour,
			RefreshTokenTTL: 720 * time.Hour,
		},
		SigningKey:     []byte("test-signing-key-32bytes-padded!!"),
		TokenStorePath: path,
	}

	before := NewServer(cfg)
	token := before.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user@example.com",
		ClientID: "https://example.com/client",
		Resource: "https://cetacean.test/mcp",
	}, 720*time.Hour)

	// A second Server over the same path stands in for the process restarting.
	after := NewServer(cfg)

	if _, ok := after.refreshTokens.Validate(token); !ok {
		t.Fatal("a refresh token issued before the restart should still validate")
	}
}

func TestServerWithoutTokenStorePathWritesNothing(t *testing.T) {
	dir := t.TempDir()

	s := newTestServer(t)
	s.refreshTokens.Issue(RefreshTokenData{Subject: "user@example.com"}, time.Hour)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("wrote %d files with no store path configured, want none", len(entries))
	}
}
