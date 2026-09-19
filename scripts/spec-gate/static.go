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

	claims, observations, errs := Scan(root)
	errs = append(errs, reg.Validate()...)
	errs = append(errs, unknown(reg, claims)...)
	errs = append(errs, unknown(reg, observations)...)
	errs = append(errs, checkStatic(reg, claims)...)
	errs = append(errs, checkObservations(claims, observations)...)

	if len(errs) > 0 {
		report(errs)

		return fmt.Errorf("%d problem(s)", len(errs))
	}

	// What this gate knows is that a literal was typed in a file it can parse,
	// or that a reason was written down instead. Not that the test compiles,
	// runs, passes, or asserts anything — spec-gate report answers the first
	// three and the mutant catalog is the only answer to the fourth.
	fmt.Fprintf(os.Stderr,
		"spec: %d requirements across %d documents have a claimant or a recorded reason\n",
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
// and every lane declaration the claimants contradict.
func checkStatic(reg *spec.Registry, claims []Claim) []error {
	claimed, tagged, untagged := map[string]bool{}, map[string]bool{}, map[string]bool{}

	var errs []error

	for _, c := range claims {
		if _, ok := reg.Lookup(c.ID); !ok {
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

			// The mirror of the deferred rule, and the only thing that closes
			// this hatch again: a gap outlives its reason silently, and the
			// report goes on counting a requirement its test now exercises as
			// one nothing has been written for.
			if has {
				errs = append(errs, fmt.Errorf(
					"%s: gap, but a test claims it; the gap is what is stale, not the test", id))
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

// checkObservations reports an observation its own test never claimed. The
// report silently drops one — it has no claim to hang on — so without this the
// evidence a test meant to publish would simply not appear.
func checkObservations(claims, observations []Claim) []error {
	claimed := make(map[string]bool, len(claims))
	for _, c := range claims {
		claimed[site(c)] = true
	}

	var errs []error

	for _, o := range observations {
		if claimed[site(o)] {
			continue
		}

		errs = append(errs, fmt.Errorf(
			"%s:%d: observes %q, which %s does not claim; the observation is dropped",
			o.File, o.Line, o.ID, o.Func,
		))
	}

	return errs
}

// site identifies the one test a record was written in.
func site(c Claim) string { return c.File + "\t" + c.Func + "\t" + c.ID }

// unknown reports every record naming a requirement the registry does not
// have. Run over claims and observations alike: a typo is a typo either way.
func unknown(reg *spec.Registry, records []Claim) []error {
	var errs []error

	for _, c := range records {
		if _, ok := reg.Lookup(c.ID); !ok {
			errs = append(errs, fmt.Errorf(
				"%s:%d: names %q, which the registry does not have",
				c.File, c.Line, c.ID,
			))
		}
	}

	return errs
}
