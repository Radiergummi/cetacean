package cluster

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestSecretEntries_BindTheSubject(t *testing.T) {
	entries := NewSecretEntries()

	if _, err := entries.Open("", "x", nil, "utf8"); !errors.Is(err, ErrSecretEntrySubject) {
		t.Errorf("an entry without a subject opened: %v", err)
	}

	entry, err := entries.Open("alice", "db_password", map[string]string{"a": "b"}, "utf8")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := entries.For(entry.ID, "mallory"); !errors.Is(err, ErrSecretEntrySubject) {
		t.Errorf("another subject read the entry: %v", err)
	}

	if _, err := entries.For(entry.ID, ""); !errors.Is(err, ErrSecretEntrySubject) {
		t.Errorf("no subject read the entry: %v", err)
	}

	err = entries.Complete(entry.ID, "mallory", "sec1")
	if !errors.Is(err, ErrSecretEntrySubject) {
		t.Errorf("another subject completed the entry: %v", err)
	}

	if err := entries.Complete(entry.ID, "alice", "sec1"); err != nil {
		t.Fatal(err)
	}

	if err := entries.Complete(entry.ID, "alice", "sec2"); !errors.Is(err, ErrSecretEntryDone) {
		t.Errorf("completed twice: %v", err)
	}

	got, err := entries.For(entry.ID, "alice")
	if err != nil || got.SecretID() != "sec1" || got.Labels["a"] != "b" {
		t.Errorf("entry = %+v, %v", got, err)
	}

	entries.Close(entry.ID)

	if _, err := entries.For(entry.ID, "alice"); !errors.Is(err, ErrSecretEntryNotFound) {
		t.Errorf("a closed entry was found: %v", err)
	}
}

func TestSecretEntries_Expire(t *testing.T) {
	entries := NewSecretEntries()
	now := time.Now()
	entries.now = func() time.Time { return now }

	entry, _ := entries.Open("alice", "x", nil, "utf8")

	now = now.Add(SecretEntryTTL)

	if _, err := entries.For(entry.ID, "alice"); !errors.Is(err, ErrSecretEntryNotFound) {
		t.Errorf("an expired entry was found: %v", err)
	}
}

func TestSecretEntries_Bounded(t *testing.T) {
	entries := NewSecretEntries()

	for i := range maxSecretEntries {
		subject := fmt.Sprintf("user-%d", i/maxSecretEntriesPerSubject)
		if _, err := entries.Open(subject, "x", nil, "utf8"); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := entries.Open("bob", "x", nil, "utf8"); !errors.Is(err, ErrSecretEntriesFull) {
		t.Errorf("opened past the bound: %v", err)
	}
}

func TestSecretEntries_BoundedPerSubject(t *testing.T) {
	entries := NewSecretEntries()

	for range maxSecretEntriesPerSubject {
		if _, err := entries.Open("alice", "x", nil, "utf8"); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := entries.Open("alice", "x", nil, "utf8"); !errors.Is(err, ErrSecretEntriesFull) {
		t.Errorf("alice opened past the per-subject bound: %v", err)
	}

	if _, err := entries.Open("bob", "x", nil, "utf8"); err != nil {
		t.Errorf("alice's entries kept bob from opening one: %v", err)
	}
}
