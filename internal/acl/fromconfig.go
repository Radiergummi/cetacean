package acl

import (
	"fmt"
	"log/slog"

	"github.com/radiergummi/cetacean/internal/config"
)

// FromConfig builds the evaluator an auth mode and its ACL settings describe,
// and the stop for the policy file's watch. Mode "none" resolves every caller
// to one anonymous identity, so it gets no evaluator and a policy is ignored.
func FromConfig(
	authMode string,
	cfg config.ACLConfig,
	resolver ResourceResolver,
) (*Evaluator, func(), error) {
	noop := func() {}

	if authMode == "none" {
		if cfg.Policy != "" || cfg.PolicyFile != "" {
			slog.Warn(
				"an ACL policy is configured but auth.mode is none, so no grant will be enforced; "+
					"set auth.mode to identify callers, or remove the policy",
				"policy_file", cfg.PolicyFile,
			)
		}

		return nil, noop, nil
	}

	e := NewEvaluator()
	e.SetResolver(resolver)

	stop := noop

	switch {
	case cfg.Policy != "":
		p, err := ParsePolicy([]byte(cfg.Policy))
		if err == nil {
			err = Validate(p)
		}
		if err != nil {
			return nil, noop, fmt.Errorf("invalid inline ACL policy: %w", err)
		}

		e.SetPolicy(p)
		slog.Info("loaded inline ACL policy", "grants", len(p.Grants))
	case cfg.PolicyFile != "":
		p, err := ParsePolicyFile(cfg.PolicyFile)
		if err == nil {
			err = Validate(p)
		}
		if err != nil {
			return nil, noop, fmt.Errorf("invalid ACL policy file %s: %w", cfg.PolicyFile, err)
		}

		e.SetPolicy(p)
		slog.Info("loaded ACL policy file", "path", cfg.PolicyFile, "grants", len(p.Grants))

		if stop, err = WatchPolicyFile(e, cfg.PolicyFile); err != nil {
			slog.Warn("failed to watch policy file for changes", "error", err)
			stop = noop
		}
	}

	switch authMode {
	case "tailscale":
		if cfg.TailscaleCapability != "" {
			e.SetSource(&TailscaleSource{Capability: cfg.TailscaleCapability})
		}
	case "oidc":
		if cfg.OIDCClaim != "" {
			e.SetSource(&OIDCSource{Claim: cfg.OIDCClaim})
		}

		if err := e.SetOIDCUserClaims(cfg.OIDCUserClaims); err != nil {
			stop()
			return nil, noop, fmt.Errorf("invalid acl.oidc_user_claims: %w", err)
		}
	case "headers":
		if cfg.HeadersACL != "" {
			e.SetSource(&HeadersSource{Header: cfg.HeadersACL, Capability: cfg.HeadersCapability})
		}
	}

	return e, stop, nil
}
