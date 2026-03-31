package acl

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

// --- extractGrantsFromRaw ---

func TestExtractGrantsFromRaw(t *testing.T) {
	tests := []struct {
		name  string
		input []any
		want  int
	}{
		{
			name: "valid grants",
			input: []any{
				map[string]any{
					"resources":   []any{"service:*"},
					"permissions": []any{"read"},
				},
			},
			want: 1,
		},
		{
			name: "multiple grants",
			input: []any{
				map[string]any{
					"resources":   []any{"service:*"},
					"permissions": []any{"read"},
				},
				map[string]any{
					"resources":   []any{"node:*"},
					"permissions": []any{"write"},
				},
			},
			want: 2,
		},
		{
			name:  "empty array",
			input: []any{},
			want:  0,
		},
		{
			name:  "nil",
			input: nil,
			want:  0,
		},
		{
			name: "missing resources",
			input: []any{
				map[string]any{
					"permissions": []any{"read"},
				},
			},
			want: 0,
		},
		{
			name: "missing permissions",
			input: []any{
				map[string]any{
					"resources": []any{"service:*"},
				},
			},
			want: 0,
		},
		{
			name: "wrong item type",
			input: []any{
				"not a map",
				42,
			},
			want: 0,
		},
		{
			name: "nested structure with multiple resources and permissions",
			input: []any{
				map[string]any{
					"resources":   []any{"service:webapp-*", "service:api-*"},
					"permissions": []any{"read", "write"},
				},
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGrantsFromRaw(tt.input)
			if len(got) != tt.want {
				t.Errorf("extractGrantsFromRaw() returned %d grants, want %d", len(got), tt.want)
			}
		})
	}
}

func TestExtractGrantsFromRaw_FieldValues(t *testing.T) {
	raw := []any{
		map[string]any{
			"resources":   []any{"service:webapp", "node:orb-*"},
			"permissions": []any{"read", "write"},
		},
	}
	grants := extractGrantsFromRaw(raw)
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	g := grants[0]
	if len(g.Resources) != 2 || g.Resources[0] != "service:webapp" || g.Resources[1] != "node:orb-*" {
		t.Errorf("resources = %v", g.Resources)
	}
	if len(g.Permissions) != 2 || g.Permissions[0] != "read" || g.Permissions[1] != "write" {
		t.Errorf("permissions = %v", g.Permissions)
	}
}

func TestExtractGrantsFromRaw_MixedTypesInArrays(t *testing.T) {
	raw := []any{
		map[string]any{
			"resources":   []any{"service:*", 42, nil},
			"permissions": []any{"read"},
		},
	}
	grants := extractGrantsFromRaw(raw)
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	g := grants[0]
	if len(g.Resources) != 1 || g.Resources[0] != "service:*" {
		t.Fatalf("expected resources [service:*], got %v", g.Resources)
	}
}

// --- TailscaleSource ---

func TestTailscaleSource(t *testing.T) {
	validID := &auth.Identity{
		Subject: "user@example.com",
		Raw: map[string]any{
			"caps": map[string]any{
				"example.com/cap/cetacean": []any{
					map[string]any{
						"resources":   []any{"service:*"},
						"permissions": []any{"read"},
					},
				},
			},
		},
	}

	tests := []struct {
		name string
		src  TailscaleSource
		id   *auth.Identity
		want int
	}{
		{
			name: "valid cap_grants",
			src:  TailscaleSource{Capability: "example.com/cap/cetacean"},
			id:   validID,
			want: 1,
		},
		{
			name: "missing caps key",
			src:  TailscaleSource{Capability: "example.com/cap/cetacean"},
			id:   &auth.Identity{Subject: "bob", Raw: map[string]any{"other": "data"}},
			want: 0,
		},
		{
			name: "wrong capability key",
			src:  TailscaleSource{Capability: "wrong.cap"},
			id:   validID,
			want: 0,
		},
		{
			name: "nil Raw",
			src:  TailscaleSource{Capability: "example.com/cap/cetacean"},
			id:   &auth.Identity{Subject: "alice"},
			want: 0,
		},
		{
			name: "nil identity",
			src:  TailscaleSource{Capability: "example.com/cap/cetacean"},
			id:   nil,
			want: 0,
		},
		{
			name: "empty capability",
			src:  TailscaleSource{Capability: ""},
			id:   validID,
			want: 0,
		},
		{
			name: "malformed caps - wrong type",
			src:  TailscaleSource{Capability: "example.com/cap/cetacean"},
			id: &auth.Identity{
				Subject: "alice",
				Raw:     map[string]any{"caps": "not-a-map"},
			},
			want: 0,
		},
		{
			name: "malformed cap data - wrong type",
			src:  TailscaleSource{Capability: "example.com/cap/cetacean"},
			id: &auth.Identity{
				Subject: "alice",
				Raw: map[string]any{
					"caps": map[string]any{
						"example.com/cap/cetacean": "not-an-array",
					},
				},
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.src.GrantsFor(tt.id)
			if len(got) != tt.want {
				t.Errorf("GrantsFor() returned %d grants, want %d", len(got), tt.want)
			}
		})
	}
}

// --- OIDCSource ---

func TestOIDCSource(t *testing.T) {
	tests := []struct {
		name string
		src  OIDCSource
		id   *auth.Identity
		want int
	}{
		{
			name: "valid claim data",
			src:  OIDCSource{Claim: "cetacean_grants"},
			id: &auth.Identity{
				Subject: "alice",
				Raw: map[string]any{
					"cetacean_grants": []any{
						map[string]any{
							"resources":   []any{"service:*"},
							"permissions": []any{"read"},
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "missing claim key",
			src:  OIDCSource{Claim: "cetacean_grants"},
			id: &auth.Identity{
				Subject: "alice",
				Raw:     map[string]any{"other": "data"},
			},
			want: 0,
		},
		{
			name: "nil Raw",
			src:  OIDCSource{Claim: "cetacean_grants"},
			id:   &auth.Identity{Subject: "alice"},
			want: 0,
		},
		{
			name: "nil identity",
			src:  OIDCSource{Claim: "cetacean_grants"},
			id:   nil,
			want: 0,
		},
		{
			name: "empty claim",
			src:  OIDCSource{Claim: ""},
			id: &auth.Identity{
				Subject: "alice",
				Raw:     map[string]any{"cetacean_grants": []any{}},
			},
			want: 0,
		},
		{
			name: "flat grant array",
			src:  OIDCSource{Claim: "cetacean_grants"},
			id: &auth.Identity{
				Subject: "alice",
				Raw: map[string]any{
					"cetacean_grants": []any{
						map[string]any{
							"resources":   []any{"node:*"},
							"permissions": []any{"write"},
						},
						map[string]any{
							"resources":   []any{"service:api-*"},
							"permissions": []any{"read"},
						},
					},
				},
			},
			want: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.src.GrantsFor(tt.id)
			if len(got) != tt.want {
				t.Errorf("GrantsFor() returned %d grants, want %d", len(got), tt.want)
			}
		})
	}
}

// --- HeadersSource ---

func TestHeadersSource(t *testing.T) {
	tests := []struct {
		name string
		src  HeadersSource
		id   *auth.Identity
		want int
	}{
		{
			name: "valid JSON in header",
			src:  HeadersSource{Header: "header:X-ACL"},
			id: &auth.Identity{
				Subject: "alice",
				Raw: map[string]any{
					"header:X-ACL": `[{"resources":["service:*"],"permissions":["read"]}]`,
				},
			},
			want: 1,
		},
		{
			name: "missing header key",
			src:  HeadersSource{Header: "header:X-ACL"},
			id: &auth.Identity{
				Subject: "alice",
				Raw:     map[string]any{"other": "data"},
			},
			want: 0,
		},
		{
			name: "invalid JSON string",
			src:  HeadersSource{Header: "header:X-ACL"},
			id: &auth.Identity{
				Subject: "alice",
				Raw:     map[string]any{"header:X-ACL": "not-json"},
			},
			want: 0,
		},
		{
			name: "non-string value",
			src:  HeadersSource{Header: "header:X-ACL"},
			id: &auth.Identity{
				Subject: "alice",
				Raw:     map[string]any{"header:X-ACL": 42},
			},
			want: 0,
		},
		{
			name: "empty string value",
			src:  HeadersSource{Header: "header:X-ACL"},
			id: &auth.Identity{
				Subject: "alice",
				Raw:     map[string]any{"header:X-ACL": ""},
			},
			want: 0,
		},
		{
			name: "nil identity",
			src:  HeadersSource{Header: "header:X-ACL"},
			id:   nil,
			want: 0,
		},
		{
			name: "nil Raw",
			src:  HeadersSource{Header: "header:X-ACL"},
			id:   &auth.Identity{Subject: "alice"},
			want: 0,
		},
		{
			name: "empty header name",
			src:  HeadersSource{Header: ""},
			id: &auth.Identity{
				Subject: "alice",
				Raw:     map[string]any{"header:X-ACL": `[{"resources":["service:*"],"permissions":["read"]}]`},
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.src.GrantsFor(tt.id)
			if len(got) != tt.want {
				t.Errorf("GrantsFor() returned %d grants, want %d", len(got), tt.want)
			}
		})
	}
}
