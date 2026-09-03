package oauth

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	json "github.com/goccy/go-json"
)

// oauthStateVersion is the on-disk format version. Bump it whenever the shape
// below changes incompatibly; readState refuses anything newer.
//
// v2 added consent records. A v1 file loads and yields none, which is exactly
// the pre-upgrade behaviour: every client is prompted once more, then
// remembered.
const oauthStateVersion = 2

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

	// Consent is a slice rather than a map: it is readable in the file, and it
	// avoids inventing an encoding for a composite key built from three
	// free-form strings.
	Consent []ConsentRecord `json:"consent,omitempty"`
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
		Tokens:   tokens,
		Consumed: consumed,
		Grants:   grants,
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

// writeState serializes the OAuth state to path using a temporary file and an
// atomic rename, so a crash mid-write leaves the previous file intact.
func writeState(path string, state oauthState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal oauth state: %w", err)
	}

	// The temp file gets a unique name rather than a fixed path + ".tmp".
	// stateFile's mutex already serializes writers inside this process, but
	// two processes pointed at one data directory would otherwise open and
	// truncate the same temp file and interleave their bytes into it, and the
	// rename would publish something that fails to parse — costing every
	// client a re-authorization. A lost update between two processes is
	// survivable; unparseable bytes are not. The cost is that an unclean kill
	// mid-write leaves an orphan file behind instead of reusing one slot.
	tmpPath, err := writeTempSynced(path, data)
	if err != nil {
		return fmt.Errorf("write oauth state tmp: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("rename oauth state: %w", err)
	}

	// The rename is only durable once the directory entry reaches the disk.
	// Without this a power loss can leave the old file — or no file — even
	// though every write above succeeded, which is exactly the case this
	// store exists to survive.
	if err := syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync oauth state dir: %w", err)
	}

	return nil
}

// writeTempSynced creates a uniquely named sibling of path, writes data to it
// and flushes it to the disk before returning, so the caller can treat a nil
// error as "this survives a crash". It returns the path it created, which the
// caller owns: on error it is removed here, on success the caller renames it.
//
// os.CreateTemp already creates with mode 0600, which is what this file needs.
// The name it picks matches tempFileSuffix, so sweepTempFiles can recognise an
// orphan left behind by an unclean kill.
func writeTempSynced(path string, data []byte) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+tempFileSuffix)
	if err != nil {
		return "", err
	}

	tmpPath := f.Name()

	if _, err := f.Write(data); err != nil {
		f.Close()          //nolint:errcheck // the write error is the one worth reporting
		os.Remove(tmpPath) //nolint:errcheck
		return "", err
	}

	if err := f.Sync(); err != nil {
		f.Close()          //nolint:errcheck // the sync error is the one worth reporting
		os.Remove(tmpPath) //nolint:errcheck
		return "", err
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return "", err
	}

	return tmpPath, nil
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

// tempFileSuffix is appended to the state file's name to form the pattern
// os.CreateTemp expands. It is shared by the writer and the sweeper so the
// names one creates are exactly the ones the other reclaims.
const tempFileSuffix = ".*.tmp"

// changeNotifier is the write-through hook the stores share. Both hold state
// that belongs to one file, and neither knows what owns it, so both report a
// mutation the same way.
type changeNotifier struct {
	// onChange is called after every mutation that changed state, always with
	// the store's mutex released. Nil when the store is memory-only.
	onChange func()
}

// SetOnChange installs the callback fired after every mutation. Call it during
// setup, before the store serves any request: it is not guarded by a mutex,
// because a hook swapped mid-flight would race the mutations it records.
func (n *changeNotifier) SetOnChange(fn func()) {
	n.onChange = fn
}

// writeThrough notifies the owner of the durable state that it changed. It
// must be called with the store's mutex released, since the owner takes a
// snapshot, which acquires it.
func (n *changeNotifier) writeThrough() {
	if n.onChange == nil {
		return
	}

	n.onChange()
}

// sweepTempFiles removes temp files orphaned by an unclean kill mid-write.
// Each holds a full copy of the state, so leaving them to accumulate would
// both fill the data directory and scatter extra copies of it around. Called
// once at startup, where a stray readdir costs nothing and no writer is racing
// it: a temp file that a live writer still owns cannot exist yet.
func sweepTempFiles(path string) {
	orphans, err := filepath.Glob(path + tempFileSuffix)
	if err != nil {
		return // the pattern is a constant, so this cannot fire
	}

	for _, orphan := range orphans {
		if err := os.Remove(orphan); err != nil {
			slog.Warn("could not remove orphaned MCP OAuth state temp file",
				"error", err,
				"path", orphan,
			)
		}
	}
}

// stateFile owns the OAuth server's durable state on disk.
//
// The stores cannot each hold their own writer: they share one file, so each
// would serialize the whole thing from its own view and drop the other's. The
// file is the single writer, and every store points its change hook here.
type stateFile struct {
	// mu makes "the single writer" true rather than aspirational. Two stores
	// mutating concurrently each call write, and unserialized they would
	// snapshot at different moments and rename in either order — publishing a
	// snapshot taken before the other's mutation and silently dropping a token
	// or an approval that is still live in memory. Held across the whole of
	// write so the snapshot and the rename that publishes it stay one step.
	//
	// Neither store's mutex is held while its hook runs, so taking this one
	// here cannot deadlock against them.
	mu sync.Mutex

	path    string
	tokens  *RefreshTokenStore
	consent *ConsentStore
}

// write serializes every store's current state. A failed write is logged and
// swallowed: the state is already live in memory, so refusing to serve would
// turn a durability problem into an outage. The operator learns that a restart
// will cost a re-authorization, which is the behaviour they had before this
// file existed.
func (f *stateFile) write() {
	f.mu.Lock()
	defer f.mu.Unlock()

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
