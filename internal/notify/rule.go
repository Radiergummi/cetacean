package notify

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"cetacean/internal/cache"
	"cetacean/internal/filter"
)

type Rule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Match    Match  `json:"match"`
	Webhook  string `json:"webhook"`
	Cooldown string `json:"cooldown"`

	nameRe      *regexp.Regexp
	cooldownDur time.Duration
	condProg    filter.Program
}

type Match struct {
	Type      string `json:"type,omitempty"`
	Action    string `json:"action,omitempty"`
	NameRegex string `json:"nameRegex,omitempty"`
	Condition string `json:"condition,omitempty"`
}

func (r *Rule) compile() error {
	u, err := url.Parse(r.Webhook)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("webhook must be an http or https URL, got %q", r.Webhook)
	}
	if u.Host == "" {
		return fmt.Errorf("webhook URL must have a host, got %q", r.Webhook)
	}
	if err := rejectInternalHost(u.Hostname()); err != nil {
		return fmt.Errorf("webhook URL rejected: %w", err)
	}
	if r.Match.NameRegex != "" {
		re, err := regexp.Compile(r.Match.NameRegex)
		if err != nil {
			return err
		}
		r.nameRe = re
	}
	if r.Cooldown != "" {
		d, err := time.ParseDuration(r.Cooldown)
		if err != nil {
			return err
		}
		r.cooldownDur = d
	}
	if r.Match.Condition != "" {
		prog, err := filter.Compile(r.Match.Condition)
		if err != nil {
			return err
		}
		r.condProg = prog
	}
	return nil
}

func (r *Rule) matches(e cache.Event, resourceName string) bool {
	if !r.Enabled {
		return false
	}
	if r.Match.Type != "" && r.Match.Type != e.Type {
		return false
	}
	if r.Match.Action != "" && r.Match.Action != e.Action {
		return false
	}
	if r.nameRe != nil && !r.nameRe.MatchString(resourceName) {
		return false
	}
	if r.condProg != nil && !r.matchesCondition(e.Resource) {
		return false
	}
	return true
}

// allowLoopback disables loopback checks for testing with local HTTP servers.
var allowLoopback bool

// rejectInternalHost returns an error if hostname resolves to a loopback,
// link-local, or cloud metadata address.
func rejectInternalHost(hostname string) error {
	lower := strings.ToLower(hostname)
	if lower == "localhost" && !allowLoopback {
		return fmt.Errorf("localhost is not allowed")
	}
	// Metadata endpoints (AWS, GCP, Azure)
	if lower == "metadata.google.internal" {
		return fmt.Errorf("cloud metadata endpoint is not allowed")
	}

	ip := net.ParseIP(hostname)
	if ip == nil {
		// Not an IP literal — resolve it.
		addrs, err := net.LookupHost(hostname)
		if err != nil {
			// Can't resolve at compile time; allow it (will fail at fire time).
			return nil
		}
		for _, addr := range addrs {
			if parsed := net.ParseIP(addr); parsed != nil {
				if err := checkIP(parsed); err != nil {
					return fmt.Errorf("%s resolves to %s: %w", hostname, addr, err)
				}
			}
		}
		return nil
	}
	return checkIP(ip)
}

func checkIP(ip net.IP) error {
	if ip.IsLoopback() && !allowLoopback {
		return fmt.Errorf("%s is a loopback address", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("%s is a link-local address", ip)
	}
	// AWS/cloud metadata: 169.254.169.254
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("%s is a cloud metadata address", ip)
	}
	return nil
}

func (r *Rule) matchesCondition(resource any) bool {
	env := filter.ResourceEnv(resource)
	if env == nil {
		return false
	}
	ok, err := filter.Evaluate(r.condProg, env)
	if err != nil {
		slog.Warn("notify: condition evaluation failed", "rule", r.ID, "condition", r.Match.Condition, "error", err)
		return false
	}
	return ok
}
