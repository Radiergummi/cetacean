package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/radiergummi/cetacean/internal/spec"
)

func runStatic(root string) error {
	reg, err := spec.Load()
	if err != nil {
		return err
	}

	claims, errs := Scan(root)
	errs = append(errs, reg.Validate()...)
	errs = append(errs, checkStatic(reg, claims)...)

	if len(errs) > 0 {
		report(errs)

		return fmt.Errorf("%d problem(s)", len(errs))
	}

	fmt.Fprintf(os.Stderr, "spec: %d requirements across %d documents, all accounted for\n",
		len(reg.All()), len(reg.Documents))

	return nil
}

func report(errs []error) {
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}

	slices.Sort(msgs)

	for _, m := range msgs {
		fmt.Fprintln(os.Stderr, "  "+m)
	}
}

// checkStatic reports every requirement with no claimant and no reason, every
// reason that is empty, every deferred requirement with nothing pinning it,
// every claim naming a requirement the registry does not have, and every lane
// declaration the claimants contradict.
func checkStatic(reg *spec.Registry, claims []Claim) []error {
	claimed, tagged, untagged := map[string]bool{}, map[string]bool{}, map[string]bool{}

	var errs []error

	for _, c := range claims {
		if _, ok := reg.Lookup(c.ID); !ok {
			errs = append(errs, fmt.Errorf(
				"%s:%d: claims %q, which the registry does not have",
				c.File, c.Line, c.ID,
			))

			continue
		}

		claimed[c.ID] = true

		if c.Tagged {
			tagged[c.ID] = true
		} else {
			untagged[c.ID] = true
		}
	}

	for _, q := range reg.All() {
		id := q.FullID()
		has := claimed[id]

		switch {
		case q.Deferred != "":
			if strings.TrimSpace(q.Deferred) == "" {
				errs = append(errs, fmt.Errorf("%s: deferred with an empty reason", id))
			}

			if !has {
				errs = append(errs, fmt.Errorf(
					"%s: deferred, but no test pins the current answer", id))
			}
		case q.Gap != "":
			if strings.TrimSpace(q.Gap) == "" {
				errs = append(errs, fmt.Errorf("%s: gap with an empty reason", id))
			}
		case !has:
			errs = append(errs, fmt.Errorf("%s: no test claims this requirement", id))
		}

		if err := checkLane(q, tagged[id], untagged[id]); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// checkLane holds the declaration to the claimants. The report counts a
// declared lane as not run rather than uncovered, so a declaration that is
// absent excuses nothing and one that is stale excuses a requirement the unit
// suite already reaches.
func checkLane(q *spec.Requirement, tagged, untagged bool) error {
	switch {
	case q.Lane == spec.LaneE2E && untagged:
		return fmt.Errorf(
			"%s: declares lane: e2e, but a claimant outside the e2e tag reaches it",
			q.FullID())
	case q.Lane == "" && tagged && !untagged:
		return fmt.Errorf(
			"%s: claimed only from behind the e2e tag; declare lane: e2e",
			q.FullID())
	}

	return nil
}
