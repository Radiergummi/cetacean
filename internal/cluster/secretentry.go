package cluster

import (
	"crypto/rand"
	"errors"
	"maps"
	"sync"
	"time"
)

// SecretEntryTTL is how long a person has to enter a secret's value.
const SecretEntryTTL = 15 * time.Minute

// maxSecretEntries bounds the entries open at once, so a client cannot fill
// memory by asking and never answering; the per-subject bound keeps one
// caller from using up what everyone else shares.
const (
	maxSecretEntries           = 1024
	maxSecretEntriesPerSubject = 16
)

var (
	ErrSecretEntryNotFound = errors.New("secret entry not found or expired")
	ErrSecretEntrySubject  = errors.New("secret entry was opened by someone else")
	ErrSecretEntryDone     = errors.New("secret entry already completed")
	ErrSecretEntriesFull   = errors.New("too many secret entries are waiting")
)

// SecretEntry is a secret an MCP caller asked a person to enter in the
// dashboard: everything but the value, which never passes through MCP.
type SecretEntry struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Labels   map[string]string `json:"labels"`
	Encoding string            `json:"encoding"`
	Expires  time.Time         `json:"expiresAt"`

	subject  string
	secretID string
}

// SecretID is the created secret's ID, once the entry is completed.
func (e SecretEntry) SecretID() string { return e.secretID }

// SecretEntries holds the entries waiting for a person. Both transports use
// it: MCP opens an entry and the dashboard's create completes it, and only
// for the subject that opened it.
type SecretEntries struct {
	mu      sync.Mutex
	entries map[string]*SecretEntry
	now     func() time.Time
}

func NewSecretEntries() *SecretEntries {
	return &SecretEntries{entries: map[string]*SecretEntry{}, now: time.Now}
}

// Open records an entry for subject, which must not be empty: an entry
// nobody can be matched to could be completed by anybody.
func (s *SecretEntries) Open(
	subject, name string,
	labels map[string]string,
	encoding string,
) (SecretEntry, error) {
	if subject == "" {
		return SecretEntry{}, ErrSecretEntrySubject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sweep()

	if len(s.entries) >= maxSecretEntries || s.openedBy(subject) >= maxSecretEntriesPerSubject {
		return SecretEntry{}, ErrSecretEntriesFull
	}

	entry := &SecretEntry{
		ID:       rand.Text(),
		Name:     name,
		Labels:   maps.Clone(labels),
		Encoding: encoding,
		Expires:  s.now().Add(SecretEntryTTL),
		subject:  subject,
	}
	s.entries[entry.ID] = entry

	return entry.copy(), nil
}

// For returns the unexpired entry id, if subject opened it.
func (s *SecretEntries) For(id, subject string) (SecretEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, err := s.lookup(id, subject)
	if err != nil {
		return SecretEntry{}, err
	}

	return entry.copy(), nil
}

// Complete records that subject created the entry's secret as secretID.
func (s *SecretEntries) Complete(id, subject, secretID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, err := s.lookup(id, subject)
	if err != nil {
		return err
	}

	if entry.secretID != "" {
		return ErrSecretEntryDone
	}

	entry.secretID = secretID

	return nil
}

// Close forgets an entry, completed or not.
func (s *SecretEntries) Close(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.entries, id)
}

func (s *SecretEntries) lookup(id, subject string) (*SecretEntry, error) {
	entry, ok := s.entries[id]
	if !ok || !s.now().Before(entry.Expires) {
		return nil, ErrSecretEntryNotFound
	}

	if subject == "" || entry.subject != subject {
		return nil, ErrSecretEntrySubject
	}

	return entry, nil
}

func (s *SecretEntries) openedBy(subject string) int {
	n := 0

	for _, entry := range s.entries {
		if entry.subject == subject {
			n++
		}
	}

	return n
}

func (s *SecretEntries) sweep() {
	now := s.now()

	for id, entry := range s.entries {
		if !now.Before(entry.Expires) {
			delete(s.entries, id)
		}
	}
}

func (e *SecretEntry) copy() SecretEntry {
	out := *e
	out.Labels = maps.Clone(e.Labels)

	if out.Labels == nil {
		out.Labels = map[string]string{}
	}

	return out
}
