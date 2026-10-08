package api

import (
	"encoding/json"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// Drives forwardedNodes with attacker-controlled RFC 7239 header values. What
// must hold is nodeAddr's contract on whatever it is handed: never panic, an
// accepted address is a valid netip.Addr, and reparsing its own string form
// reproduces it — or a trust decision is made against a different address.
func FuzzForwardedNodes(f *testing.F) {
	seeds := []string{
		`for=192.0.2.1`,
		`for=unknown`,
		`for="[2001:db8::1]:4711"`,
		`for=192.0.2.1, for=198.51.100.2`,
		`for=::ffff:10.0.0.2`,
		`for=_hidden;by=_secret`,
		`for="192.0.2.1, evil"`,
		``,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, value string) {
		nodes := forwardedNodes([]string{value})

		for _, node := range nodes {
			addr, ok := nodeAddr(node)
			if !ok {
				continue
			}

			if !addr.IsValid() {
				t.Fatalf("nodeAddr(%q) reported ok with an invalid address", node)
			}

			again, ok2 := nodeAddr(addr.String())
			if !ok2 || again != addr {
				t.Fatalf(
					"nodeAddr(%q) = %v not stable on reparse: nodeAddr(%q) = %v, ok=%v",
					node, addr, addr.String(), again, ok2,
				)
			}
		}
	})
}

// Drives peerOf with a fuzzed Forwarded header, X-Forwarded-For and RemoteAddr
// against a fixed trusted-proxy set. peerOf takes the whole request, but its
// verdict must depend solely on RemoteAddr — a header is what a client sends —
// so it must agree with isTrusted applied to the parsed RemoteAddr alone.
func FuzzResolveClientIP(f *testing.F) {
	type seed struct{ forwarded, xff, remoteAddr string }

	seeds := []seed{
		{`for=192.0.2.1`, ``, `203.0.113.5:1234`},
		{``, `10.0.0.1, 203.0.113.9`, `10.0.0.1:1234`},
		{``, ``, `not-an-address`},
		{`for=unknown`, ``, `10.0.0.5:9999`},
		{`for="[2001:db8::1]:4711"`, ``, `[2001:db8::1]:80`},
		{`for=::ffff:10.0.0.2`, ``, `10.0.0.9:1`},
		{`for=_hidden;by=_secret`, `10.0.0.1, 203.0.113.9`, `203.0.113.5:1234`},
		{``, ``, ``},
	}
	for _, s := range seeds {
		f.Add(s.forwarded, s.xff, s.remoteAddr)
	}

	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}

	f.Fuzz(func(t *testing.T, forwarded, xff, remoteAddr string) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remoteAddr

		if forwarded != "" {
			r.Header.Set("Forwarded", forwarded)
		}

		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}

		peer, _ := peerOf(r, trusted)

		wantTrusted := false
		if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
			if addr, err := netip.ParseAddr(host); err == nil {
				wantTrusted = isTrusted(addr, trusted)
			}
		}

		if peer.Trusted != wantTrusted {
			t.Fatalf(
				"peerOf trusted=%v, want %v (remoteAddr=%q forwarded=%q xff=%q)",
				peer.Trusted, wantTrusted, remoteAddr, forwarded, xff,
			)
		}
	})
}

// Drives applyJSONPatch with fuzzed RFC 6902 operations over a fuzzed flat
// string map, both as JSON so the fuzzer can mutate structure. Malformed JSON is
// skipped. A failed patch must leave the input untouched, and the function must
// never return both an error and a non-nil result.
func FuzzApplyJSONPatch(f *testing.F) {
	type seed struct{ mapJSON, opsJSON string }

	seeds := []seed{
		{`{"a":"1"}`, `[{"op":"test","path":"a","value":"1"},{"op":"add","path":"b","value":"2"}]`},
		{`{"a":"1"}`, `[{"op":"remove","path":"missing"}]`},
		{`{"a":"1"}`, `[{"op":"add","path":"","value":"x"}]`},
		{`{"a":"1"}`, `[]`},
		{`{"a":"1"}`, `not json`},
		{`not json`, `[{"op":"add","path":"a","value":"1"}]`},
	}
	for _, s := range seeds {
		f.Add(s.mapJSON, s.opsJSON)
	}

	f.Fuzz(func(t *testing.T, mapJSON, opsJSON string) {
		var m map[string]string
		if err := json.Unmarshal([]byte(mapJSON), &m); err != nil {
			return
		}

		var ops []PatchOp
		if err := json.Unmarshal([]byte(opsJSON), &ops); err != nil {
			return
		}

		before := maps.Clone(m)

		result, err := applyJSONPatch(m, ops)

		if !maps.Equal(m, before) {
			t.Fatalf("applyJSONPatch mutated its input map: before=%v after=%v", before, m)
		}

		if err != nil && result != nil {
			t.Fatalf(
				"applyJSONPatch returned both an error (%v) and a non-nil result (%v)",
				err,
				result,
			)
		}

		if err == nil && result == nil {
			t.Fatalf("applyJSONPatch returned neither an error nor a result")
		}
	})
}
