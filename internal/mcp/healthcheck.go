package mcp

import (
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
)

// defaultHealthcheckMode is the file mode Swarm gives a probe's exit handling;
// Docker's own default retry count when a caller sets none.
const defaultHealthcheckRetries = 3

// healthcheckValue is the healthcheck section's argument.
//
// Durations arrive as strings ("10s") rather than the nanosecond integers
// container.HealthConfig carries, for the same reason ServiceDetails reports
// them that way: 10000000000 is a value a caller has to decode, and one they
// will sooner or later write in the wrong scale. Parse failures name the
// offending field, because a caller that guessed the format has to be told
// which of three it got wrong.
type healthcheckValue struct {
	Test        []string `json:"test"`
	Interval    string   `json:"interval,omitempty"`
	Timeout     string   `json:"timeout,omitempty"`
	StartPeriod string   `json:"startPeriod,omitempty"`
	Retries     int      `json:"retries,omitempty"`
}

// toHealthConfig converts the wire shape to Docker's.
//
// An empty Test clears the healthcheck: a probe that runs no command is not a
// thing Swarm can execute, so the only sensible reading of "no test" is
// "remove it". A nil result with a nil error says exactly that, and
// UpdateServiceHealthcheck takes a nil config to mean the same.
func (v healthcheckValue) toHealthConfig() (*container.HealthConfig, error) {
	if len(v.Test) == 0 {
		return nil, nil
	}

	retries := v.Retries
	if retries <= 0 {
		retries = defaultHealthcheckRetries
	}

	hc := &container.HealthConfig{Test: v.Test, Retries: retries}

	for _, field := range []struct {
		name string
		raw  string
		into *time.Duration
	}{
		{"interval", v.Interval, &hc.Interval},
		{"timeout", v.Timeout, &hc.Timeout},
		{"startPeriod", v.StartPeriod, &hc.StartPeriod},
	} {
		if field.raw == "" {
			continue
		}

		parsed, err := time.ParseDuration(field.raw)
		if err != nil {
			return nil, fmt.Errorf(
				"healthcheck %s: %q is not a duration; write it like \"10s\", \"1m30s\" or \"500ms\"",
				field.name,
				field.raw,
			)
		}

		*field.into = parsed
	}

	return hc, nil
}

// commandValue is the command section's argument.
//
// Command is the entrypoint and Args what follows it, the split Docker itself
// makes and the one ServiceDetails reports — a caller must be able to write
// back what a describe just showed them.
type commandValue struct {
	Command []string `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

// applyTo writes both fields onto a container spec, leaving everything else
// alone. It replaces rather than merges — the section semantics every
// wholesale section here follows — so omitting one clears it.
func (v commandValue) applyTo(spec *swarm.ContainerSpec) {
	spec.Command = v.Command
	spec.Args = v.Args
}
