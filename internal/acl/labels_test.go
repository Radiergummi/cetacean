package acl

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

func TestParseACLLabels_ReadOnly(t *testing.T) {
	labels := map[string]string{
		"cetacean.acl.read": "group:dev,user:alice@example.com",
	}
	read, write := ParseACLLabels(labels)
	if len(read) != 2 {
		t.Fatalf("expected 2 read audiences, got %d", len(read))
	}
	if read[0] != "group:dev" || read[1] != "user:alice@example.com" {
		t.Fatalf("unexpected read audiences: %v", read)
	}
	if len(write) != 0 {
		t.Fatalf("expected 0 write audiences, got %d", len(write))
	}
}

func TestParseACLLabels_ReadAndWrite(t *testing.T) {
	labels := map[string]string{
		"cetacean.acl.read":  "group:*",
		"cetacean.acl.write": "group:ops",
	}
	read, write := ParseACLLabels(labels)
	if len(read) != 1 || read[0] != "group:*" {
		t.Fatalf("unexpected read: %v", read)
	}
	if len(write) != 1 || write[0] != "group:ops" {
		t.Fatalf("unexpected write: %v", write)
	}
}

func TestParseACLLabels_Whitespace(t *testing.T) {
	labels := map[string]string{
		"cetacean.acl.read": " group:dev , user:bob ",
	}
	read, _ := ParseACLLabels(labels)
	if len(read) != 2 || read[0] != "group:dev" || read[1] != "user:bob" {
		t.Fatalf("whitespace not trimmed: %v", read)
	}
}

func TestParseACLLabels_EmptyAndMissing(t *testing.T) {
	// No ACL labels at all.
	read, write := ParseACLLabels(map[string]string{"foo": "bar"})
	if read != nil || write != nil {
		t.Fatal("expected nil for labels without ACL entries")
	}

	// Empty value.
	read, write = ParseACLLabels(map[string]string{"cetacean.acl.read": ""})
	if len(read) != 0 {
		t.Fatalf("expected 0 audiences from empty value, got %d", len(read))
	}
}

func TestHasACLLabels(t *testing.T) {
	if hasACLLabels(map[string]string{"foo": "bar"}) {
		t.Fatal("should not detect ACL labels")
	}
	if !hasACLLabels(map[string]string{"cetacean.acl.read": "group:dev"}) {
		t.Fatal("should detect ACL labels")
	}
	if !hasACLLabels(map[string]string{"cetacean.acl.write": "group:ops"}) {
		t.Fatal("should detect ACL labels")
	}
}

func TestMatchLabelAudience(t *testing.T) {
	alice := &auth.Identity{Subject: "alice", Email: "alice@example.com", Groups: []string{"dev", "ops"}}

	tests := []struct {
		name      string
		audiences []string
		want      bool
	}{
		{"user match", []string{"user:alice"}, true},
		{"email match", []string{"user:*@example.com"}, true},
		{"group match", []string{"group:dev"}, true},
		{"wildcard", []string{"*"}, true},
		{"no match", []string{"user:bob", "group:marketing"}, false},
		{"empty list", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchLabelAudience(tt.audiences, alice)
			if got != tt.want {
				t.Errorf("matchLabelAudience(%v) = %v, want %v", tt.audiences, got, tt.want)
			}
		})
	}
}
