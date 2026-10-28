package auth

import "testing"

func TestHostSetAdmits(t *testing.T) {
	set := NewHostSet(
		"https://Swarm.Example.com./",
		"", "  ", "swarm.lan.", "cetacean.tailnet.ts.net",
	)

	for host, want := range map[string]bool{
		"swarm.example.com":          true,
		"SWARM.example.com.:443":     true,
		"swarm.lan:9000":             true,
		"cetacean.tailnet.ts.net":    true,
		"localhost:9000":             true,
		"127.0.0.1":                  true,
		"[::1]:9000":                 true,
		"10.0.0.5:9000":              true,
		"[fd00::5]":                  true,
		"":                           false,
		":9000":                      false,
		"rebind.attacker.example":    false,
		"swarm.example.com.evil.net": false,
	} {
		if got := set.Admits(host); got != want {
			t.Errorf("Admits(%q) = %v, want %v", host, got, want)
		}
	}

	if (HostSet{}).Admits("") {
		t.Error("the empty set admits a request with no Host")
	}
}
