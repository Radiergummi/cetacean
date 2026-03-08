package notify

import (
	"regexp"
	"strings"
	"time"

	"cetacean/internal/cache"

	"github.com/docker/docker/api/types/swarm"
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
}

type Match struct {
	Type      string `json:"type,omitempty"`
	Action    string `json:"action,omitempty"`
	NameRegex string `json:"nameRegex,omitempty"`
	Condition string `json:"condition,omitempty"`
}

func (r *Rule) compile() error {
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
	if r.Match.Condition != "" && !r.matchesCondition(e.Resource) {
		return false
	}
	return true
}

func (r *Rule) matchesCondition(resource interface{}) bool {
	// Parse "field == value"
	parts := strings.SplitN(r.Match.Condition, "==", 2)
	if len(parts) != 2 {
		return false
	}
	field := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	switch field {
	case "state":
		return extractState(resource) == value
	default:
		return false
	}
}

func extractState(resource interface{}) string {
	switch r := resource.(type) {
	case swarm.Task:
		return string(r.Status.State)
	case swarm.Node:
		return string(r.Status.State)
	default:
		return ""
	}
}
