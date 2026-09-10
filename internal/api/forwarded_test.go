package api

import (
	"slices"
	"testing"
)

func TestForwardedNodes(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   []string
	}{
		{
			name:   "single element",
			values: []string{"for=192.0.2.1"},
			want:   []string{"192.0.2.1"},
		},
		{
			name:   "parameter names are case-insensitive",
			values: []string{"FOR=192.0.2.1"},
			want:   []string{"192.0.2.1"},
		},
		{
			name:   "quoted IPv6 with port",
			values: []string{`for="[2001:db8::1]:8080"`},
			want:   []string{"[2001:db8::1]:8080"},
		},
		{
			name:   "elements keep their order, first proxy first",
			values: []string{"for=192.0.2.1, for=198.51.100.1"},
			want:   []string{"192.0.2.1", "198.51.100.1"},
		},
		{
			name:   "other parameters are ignored",
			values: []string{`by=203.0.113.43;for=192.0.2.1;proto=https;host=example.com`},
			want:   []string{"192.0.2.1"},
		},
		// The three quoting cases below hide a "for" inside another
		// parameter's value: a proxy reflecting attacker-controlled input into
		// host or by must not be able to inject a node into the chain.
		{
			name:   "a comma inside a quoted string does not start a new element",
			values: []string{`for=192.0.2.1;host="x, for=203.0.113.99"`},
			want:   []string{"192.0.2.1"},
		},
		{
			name:   "a semicolon inside a quoted string does not start a new pair",
			values: []string{`for=192.0.2.1;host="x; for=203.0.113.99"`},
			want:   []string{"192.0.2.1"},
		},
		{
			name:   "an escaped quote does not end the quoted string",
			values: []string{`host="x\", for=203.0.113.99";for=192.0.2.1`},
			want:   []string{"192.0.2.1"},
		},
		{
			name:   "empty forwarded-pairs are tolerated",
			values: []string{"for=192.0.2.1;;"},
			want:   []string{"192.0.2.1"},
		},
		{
			name:   "an element without a for parameter contributes nothing",
			values: []string{"proto=https, for=192.0.2.1"},
			want:   []string{"192.0.2.1"},
		},
		{
			name:   "unknown and obfuscated nodes are reported as they stand",
			values: []string{"for=unknown, for=_hidden"},
			want:   []string{"unknown", "_hidden"},
		},
		{
			name:   "repeated header lines are one list",
			values: []string{"for=192.0.2.1", "for=198.51.100.1"},
			want:   []string{"192.0.2.1", "198.51.100.1"},
		},
		{
			name:   "no header",
			values: nil,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := forwardedNodes(tt.values)
			if !slices.Equal(got, tt.want) {
				t.Errorf("forwardedNodes(%q) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}

func TestNodeAddr(t *testing.T) {
	tests := []struct {
		node string
		want string // empty means the node yields no address
	}{
		{"192.0.2.1", "192.0.2.1"},
		{"192.0.2.1:8080", "192.0.2.1"},
		{"[2001:db8::1]", "2001:db8::1"},
		{"[2001:db8::1]:8080", "2001:db8::1"},
		// Unbracketed IPv6 violates the grammar, but is unambiguous and
		// common enough in the wild to read rather than discard.
		{"2001:db8::1", "2001:db8::1"},
		{"unknown", ""},
		{"UNKNOWN", ""},
		{"_hidden", ""},
		{"[2001:db8::1", ""},
		{"[not-an-address]", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.node, func(t *testing.T) {
			addr, ok := nodeAddr(tt.node)
			if ok != (tt.want != "") {
				t.Fatalf("nodeAddr(%q) ok = %v, want %v", tt.node, ok, tt.want != "")
			}
			if ok && addr.String() != tt.want {
				t.Errorf("nodeAddr(%q) = %s, want %s", tt.node, addr, tt.want)
			}
		})
	}
}
