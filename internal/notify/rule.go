package notify

import (
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
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
