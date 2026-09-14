//go:build e2e

package e2e_test

import (
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file covers the `tailscale` auth mode, which had none. Reserves port
// 19012 (see README.md's reserved-ports table).
//
// # What this lane can reach, and what it cannot
//
// A *successful* Tailscale authentication is out of reach here, and saying so
// precisely matters more than the cases below. TailscaleProvider.Authenticate
// asks the local daemon's WhoIs API who owns the peer address, so a pass
// requires a real tailnet: a joined node, an auth key, and outbound access to
// Tailscale's control plane. tsnet mode needs the same, and brings up an
// embedded node at process start. Neither belongs in a suite that must run
// offline against a throwaway Docker-in-Docker swarm. So group extraction from
// the CapMap (extractCapGroups), acl.TailscaleSource, and the tsnet dual-listener
// topology stay covered by internal/auth's unit tests alone, and this file says
// so rather than leaving the gap to be rediscovered.
//
// What *is* reachable is everything the provider decides before it ever
// consults the daemon, and that half is where the security property lives:
// validateTailscaleAddr refuses any peer outside Tailscale's CGNAT
// (100.64.0.0/10) and ULA (fd7a:115c:a1e0::/48) ranges, as defence in depth
// for a server bound to 0.0.0.0. These cases drive that boundary from a real
// client, and they distinguish "refused on the range" from "refused by the
// daemon" through the binary's own log — at the HTTP layer both are an
// indistinguishable 401/AUT001, which is exactly why a status-only assertion
// would prove nothing about the ordering.
//
// Startup validation for tsnet mode is reachable too, and is driven against
// the real binary rather than only against config.LoadAuth.

const tailscalePort = 19012

// tailnetULABase is the base address of Tailscale's IPv6 ULA prefix. It is
// inside the range validateTailscaleAddr accepts, while being the one address
// in it that a tailnet never assigns to a node — assigned Tailscale IPv6
// addresses carry a non-zero host portion. That matters because these cases
// run on a developer's machine, which may well have a real tailscaled: an
// address the local daemon could actually vouch for would make the outcome
// depend on whose laptop the suite runs on.
const tailnetULABase = "fd7a:115c:a1e0::"

// startTailscale brings up a SUT in tailscale local mode. The provider is
// constructed without touching the daemon (auth.NewTailscaleLocalProvider only
// builds a client), so the binary comes up cleanly on a machine with no
// Tailscale at all — which is the premise of every case here.
func startTailscale(t *testing.T, env *harness.Env, extra map[string]string) *sut.Process {
	t.Helper()

	environment := map[string]string{
		"CETACEAN_AUTH_MODE":           "tailscale",
		"CETACEAN_AUTH_TAILSCALE_MODE": "local",
	}

	maps.Copy(environment, extra)

	return sut.Start(t, sut.Config{
		Port:       tailscalePort,
		DockerHost: env.DockerHost,
		Env:        environment,
	})
}

// getAs issues a GET carrying arbitrary headers, which is how the forged
// forwarding headers below are delivered.
func getWithHeaders(
	t *testing.T,
	proc *sut.Process,
	path string,
	headers map[string]string,
) httpOutcome {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", path, err)
	}

	req.Header.Set("Accept", "application/json")

	for name, value := range headers {
		req.Header.Set(name, value)
	}

	return send(t, proc, req)
}

// awaitLog waits for want to appear in everything the binary logged after
// mark, and returns that slice of the log.
//
// The wait is not incidental: the child's stdout reaches sut.Process through a
// pipe a goroutine copies, so a record written before the HTTP response was
// flushed can still arrive after the client has read it. Reading once would
// make every log assertion here intermittently wrong.
func awaitLog(t *testing.T, proc *sut.Process, mark int, want string) string {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		written := logSince(proc, mark)
		if strings.Contains(written, want) {
			return written
		}

		if time.Now().After(deadline) {
			t.Fatalf(
				"the binary never logged %q\n--- log written during this request ---\n%s",
				want, written,
			)

			return ""
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func logSince(proc *sut.Process, mark int) string {
	logs := proc.Logs()
	if mark >= len(logs) {
		return ""
	}

	return logs[mark:]
}

// TestTailscaleRefusesEveryPeerOutsideTheTailnet drives the address boundary
// that runs before the daemon is consulted.
func TestTailscaleRefusesEveryPeerOutsideTheTailnet(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	proc := startTailscale(t, env, nil)

	t.Run("meta_endpoints_stay_open", func(t *testing.T) {
		// /-/* is exempt from the auth middleware. It has to be: a health
		// check comes from the orchestrator, which is not on the tailnet, and
		// a readiness probe that 401s takes the service down.
		outcome := getWithHeaders(t, proc, "/-/health", nil)

		if outcome.status != http.StatusOK {
			t.Errorf("GET /-/health: status = %d, want 200; body: %s",
				outcome.status, outcome.body)
		}
	})

	t.Run("a_loopback_peer_is_refused_before_the_daemon", func(t *testing.T) {
		mark := len(proc.Logs())

		outcome := getWithHeaders(t, proc, "/services", nil)

		if outcome.status != http.StatusUnauthorized {
			t.Fatalf("GET /services: status = %d, want 401; body: %s",
				outcome.status, outcome.body)
		}

		if !strings.Contains(outcome.body, "AUT001") {
			t.Errorf("401 body does not name AUT001: %s", outcome.body)
		}

		written := awaitLog(t, proc, mark, "not in tailnet range")

		if !strings.Contains(written, "127.0.0.1") {
			t.Errorf("the refusal does not name the peer it refused:\n%s", written)
		}

		// The ordering is the property. A peer off the tailnet must be turned
		// away by the range check, not handed to the daemon — that is what
		// makes the check defence in depth for a server bound to 0.0.0.0
		// rather than a redundant pre-filter.
		if strings.Contains(written, "tailscale whois") {
			t.Errorf(
				"a non-tailnet peer reached the WhoIs daemon; validateTailscaleAddr "+
					"is meant to refuse first:\n%s",
				written,
			)
		}
	})

	t.Run("whoami_is_not_an_identity_oracle", func(t *testing.T) {
		// /auth/* is exempt from the middleware, so WhoamiHandler calls
		// Authenticate itself. If it did not, the one route that reports an
		// identity would be the one route that hands it out unchecked.
		outcome := getWithHeaders(t, proc, "/auth/whoami", nil)

		if outcome.status != http.StatusUnauthorized {
			t.Fatalf("GET /auth/whoami: status = %d, want 401; body: %s",
				outcome.status, outcome.body)
		}

		if strings.Contains(outcome.body, "tailscale") {
			t.Errorf("the refusal leaks provider detail to an unauthenticated caller: %s",
				outcome.body)
		}
	})

	t.Run("x_forwarded_for_cannot_forge_a_tailnet_peer", func(t *testing.T) {
		// With no trusted proxies configured, realIP records the verdict and
		// leaves RemoteAddr alone, so the provider still sees loopback. A
		// forwarding header is a claim by whoever sent it; honouring it here
		// would let any client on the box assert a tailnet address and put
		// the daemon to work on it.
		mark := len(proc.Logs())

		outcome := getWithHeaders(t, proc, "/services", map[string]string{
			"X-Forwarded-For": "100.64.0.5",
		})

		if outcome.status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body: %s", outcome.status, outcome.body)
		}

		written := awaitLog(t, proc, mark, "not in tailnet range")

		if !strings.Contains(written, "127.0.0.1") {
			t.Errorf(
				"the refusal names %s rather than the real peer; an untrusted "+
					"X-Forwarded-For moved the address the provider checked:\n%s",
				"100.64.0.5", written,
			)
		}
	})

	t.Run("forwarded_cannot_forge_a_tailnet_peer", func(t *testing.T) {
		// RFC 7239's Forwarded is preferred over X-Forwarded-For by realIP,
		// so it needs its own case: preferring a header is no use if the
		// preference is where the trust check was lost.
		mark := len(proc.Logs())

		outcome := getWithHeaders(t, proc, "/services", map[string]string{
			"Forwarded": `for="[` + tailnetULABase + `]"`,
		})

		if outcome.status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body: %s", outcome.status, outcome.body)
		}

		written := awaitLog(t, proc, mark, "not in tailnet range")

		if !strings.Contains(written, "127.0.0.1") {
			t.Errorf(
				"the refusal names a forwarded address rather than the real peer:\n%s",
				written,
			)
		}
	})
}

// TestTailscaleBehindATrustedProxyDefersToTheDaemon records what changes when
// an operator configures server.trusted_proxies in tailscale mode: realIP then
// rewrites RemoteAddr to the address the proxy named, so a peer the proxy calls
// a tailnet address clears validateTailscaleAddr and the daemon becomes the
// only thing deciding whether that address is real.
//
// That is the documented behaviour of the trusted-proxy allowlist rather than a
// defect — realIP makes the decision once, on the peer the connection actually
// arrived from — but it is worth pinning: it is the configuration in which the
// range check stops being a boundary, and nothing else in the suite says so.
func TestTailscaleBehindATrustedProxyDefersToTheDaemon(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	proc := startTailscale(t, env, map[string]string{
		"CETACEAN_TRUSTED_PROXIES": "127.0.0.1/32",
	})

	mark := len(proc.Logs())

	outcome := getWithHeaders(t, proc, "/services", map[string]string{
		"X-Forwarded-For": tailnetULABase,
	})

	if outcome.status == http.StatusOK {
		// Only reachable if a real tailscaled on this machine vouched for the
		// ULA base address. Skipping is the honest outcome: the case would be
		// asserting something about the developer's tailnet rather than about
		// Cetacean.
		t.Skipf(
			"a local Tailscale daemon authenticated %s; this case needs an address no "+
				"tailnet has assigned",
			tailnetULABase,
		)
	}

	if outcome.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", outcome.status, outcome.body)
	}

	// Past the range check and refused by the daemon instead — which is the
	// whole point of the case. On a machine with no tailscaled this is a dial
	// failure; on one with a tailnet it is a WhoIs that matched nothing. Both
	// carry the same wrapper, and neither is the range refusal.
	written := awaitLog(t, proc, mark, "tailscale whois")

	if strings.Contains(written, "not in tailnet range") {
		t.Errorf(
			"a trusted proxy's forwarded tailnet address was still refused on the "+
				"range check; realIP did not rewrite RemoteAddr:\n%s",
			written,
		)
	}
}

// TestTailscaleTsnetStartupValidation drives the configuration refusals against
// the real binary. internal/config has unit tests for the same rules; what this
// adds is that main.go actually acts on them — a validated-and-ignored error
// would leave the binary serving with no working provider.
func TestTailscaleTsnetStartupValidation(t *testing.T) {
	env := harness.Up(t)

	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "tsnet_without_an_auth_key",
			env: map[string]string{
				"CETACEAN_AUTH_MODE":           "tailscale",
				"CETACEAN_AUTH_TAILSCALE_MODE": "tsnet",
			},
			want: "auth.tailscale.authkey",
		},
		{
			name: "an_unknown_tailscale_mode",
			env: map[string]string{
				"CETACEAN_AUTH_MODE":           "tailscale",
				"CETACEAN_AUTH_TAILSCALE_MODE": "sidecar",
			},
			want: `tailscale mode must be "local" or "tsnet"`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			code, logs := sut.StartExpectingExit(t, sut.Config{
				Port:       tailscalePort,
				DockerHost: env.DockerHost,
				Env:        testCase.env,
			})

			if code == 0 {
				t.Errorf("the binary exited 0 on a configuration it must refuse\n%s", logs)
			}

			if !strings.Contains(logs, testCase.want) {
				t.Errorf(
					"the refusal does not name %q, so an operator cannot tell which "+
						"setting is wrong\n%s",
					testCase.want, logs,
				)
			}
		})
	}
}
