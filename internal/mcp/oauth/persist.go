package oauth

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"time"

	json "github.com/goccy/go-json"
)

// refreshTokenStoreVersion is the on-disk format version. Bump it whenever the
// shape below changes incompatibly; readRefreshTokens refuses anything newer.
const refreshTokenStoreVersion = 1

// RefreshTokenSnapshot is the on-disk representation of a RefreshTokenStore.
//
// The store's own types are unexported, and deliberately stay that way: this
// is a separate, explicit shape so the file format is not hostage to a field
// rename inside the store.
//
// What lands on disk is not a credential. Tokens are keyed by their SHA-256
// hash exactly as they are in memory, so the file holds an identity mapping —
// who a hash belongs to — and never anything a client could present.
type RefreshTokenSnapshot struct {
	Version   int                              `json:"version"`
	Timestamp time.Time                        `json:"timestamp"`
	Tokens    map[string]RefreshTokenSnapEntry `json:"tokens"`
	Consumed  map[string]string                `json:"consumed"`
	Grants    map[string][]string              `json:"grants"`
}

// RefreshTokenSnapEntry is one live token: the claims bound to it plus both
// expiries, the per-token one and the grant family's absolute one.
type RefreshTokenSnapEntry struct {
	Subject        string    `json:"subject"`
	Groups         []string  `json:"groups,omitempty"`
	ClientID       string    `json:"clientId"`
	Resource       string    `json:"resource"`
	GrantID        string    `json:"grantId"`
	ExpiresAt      time.Time `json:"expiresAt"`
	GrantExpiresAt time.Time `json:"grantExpiresAt"`
}

// Snapshot returns a serializable copy of the store's current state.
func (s *RefreshTokenStore) Snapshot() RefreshTokenSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens := make(map[string]RefreshTokenSnapEntry, len(s.tokens))
	for hash, entry := range s.tokens {
		tokens[hash] = RefreshTokenSnapEntry{
			Subject:        entry.data.Subject,
			Groups:         append([]string(nil), entry.data.Groups...),
			ClientID:       entry.data.ClientID,
			Resource:       entry.data.Resource,
			GrantID:        entry.data.grantID,
			ExpiresAt:      entry.expiresAt,
			GrantExpiresAt: entry.grantExpiresAt,
		}
	}

	consumed := make(map[string]string, len(s.consumed))
	maps.Copy(consumed, s.consumed)

	grants := make(map[string][]string, len(s.grants))
	for grantID, hashes := range s.grants {
		grants[grantID] = append([]string(nil), hashes...)
	}

	return RefreshTokenSnapshot{
		Version:   refreshTokenStoreVersion,
		Timestamp: time.Now(),
		Tokens:    tokens,
		Consumed:  consumed,
		Grants:    grants,
	}
}

// Restore replaces the store's state from a snapshot, dropping grant families
// whose live token has already expired.
//
// A family holds exactly one live token — rotation deletes the old hash as it
// adds the new one — so a family with no unexpired token can never rotate
// again. Carrying its rotation history forward would only grow the file, since
// nothing can ever match those consumed hashes but a replay of a token that
// would fail on expiry anyway.
func (s *RefreshTokenStore) Restore(snap RefreshTokenSnapshot) {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tokens = make(map[string]refreshTokenEntry, len(snap.Tokens))
	live := make(map[string]bool, len(snap.Tokens))

	for hash, entry := range snap.Tokens {
		// Rotate caps a token's expiry at its family's, so in memory the
		// second check is implied by the first. The file is a trust boundary
		// the invariant does not cross: it can be stale, hand-edited or from
		// an older build, and a family past its absolute expiry must not come
		// back regardless of what a token entry claims.
		if now.After(entry.ExpiresAt) || now.After(entry.GrantExpiresAt) {
			continue
		}

		s.tokens[hash] = refreshTokenEntry{
			data: RefreshTokenData{
				Subject:  entry.Subject,
				Groups:   append([]string(nil), entry.Groups...),
				ClientID: entry.ClientID,
				Resource: entry.Resource,
				grantID:  entry.GrantID,
			},
			expiresAt:      entry.ExpiresAt,
			grantExpiresAt: entry.GrantExpiresAt,
		}
		live[entry.GrantID] = true
	}

	s.consumed = make(map[string]string, len(snap.Consumed))
	for hash, grantID := range snap.Consumed {
		if live[grantID] {
			s.consumed[hash] = grantID
		}
	}

	s.grants = make(map[string][]string, len(snap.Grants))
	for grantID, hashes := range snap.Grants {
		if live[grantID] {
			s.grants[grantID] = append([]string(nil), hashes...)
		}
	}
}

// writeRefreshTokens serializes a snapshot to path using a temporary file and
// an atomic rename, so a crash mid-write leaves the previous file intact.
func writeRefreshTokens(path string, snap RefreshTokenSnapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal refresh tokens: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := writeFileSynced(tmpPath, data); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("write refresh tokens tmp: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("rename refresh tokens: %w", err)
	}

	// The rename is only durable once the directory entry reaches the disk.
	// Without this a power loss can leave the old file — or no file — even
	// though every write above succeeded, which is exactly the case this
	// store exists to survive.
	if err := syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync refresh token dir: %w", err)
	}

	return nil
}

// writeFileSynced writes data to path and flushes it to the disk before
// returning, so the caller can treat a nil error as "this survives a crash".
func writeFileSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		f.Close() //nolint:errcheck // the write error is the one worth reporting
		return err
	}

	if err := f.Sync(); err != nil {
		f.Close() //nolint:errcheck // the sync error is the one worth reporting
		return err
	}

	return f.Close()
}

// syncDir flushes a directory's own entries, which is what makes a rename
// durable rather than merely visible to the running kernel.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck // read-only handle

	return d.Sync()
}

// readRefreshTokens reads a snapshot written by writeRefreshTokens.
func readRefreshTokens(path string) (RefreshTokenSnapshot, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-configured
	if err != nil {
		return RefreshTokenSnapshot{}, fmt.Errorf("read refresh tokens: %w", err)
	}

	var snap RefreshTokenSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return RefreshTokenSnapshot{}, fmt.Errorf("unmarshal refresh tokens: %w", err)
	}

	if snap.Version < 1 || snap.Version > refreshTokenStoreVersion {
		return RefreshTokenSnapshot{}, fmt.Errorf(
			"unsupported refresh token store version: got %d, supported 1..%d",
			snap.Version,
			refreshTokenStoreVersion,
		)
	}

	return snap, nil
}

// persistRefreshTokens returns a writer that saves every mutation to path.
//
// A failed write is logged and swallowed: the token is already valid in
// memory, so refusing to issue it would turn a durability problem into an
// outage. The operator learns that a restart will cost a re-authorization,
// which is the behaviour they had before this file existed.
func persistRefreshTokens(path string) func(RefreshTokenSnapshot) {
	return func(snap RefreshTokenSnapshot) {
		if err := writeRefreshTokens(path, snap); err != nil {
			slog.Warn(
				"MCP refresh token store write failed; tokens will not survive a restart",
				"error", err,
				"path", path,
			)
		}
	}
}
