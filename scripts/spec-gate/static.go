package main

import (
	"fmt"
	"os"
	"slices"

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
// deferred requirement with nothing pinning it, and every lane declaration the
// claimants contradict.
func checkStatic(reg *spec.Registry, claims []Claim) []error {
	tagged, untagged := map[string]bool{}, map[string]bool{}

	var errs []error

	for _, c := range claims {
		if _, ok := reg.Lookup(c.ID); !ok {
			continue
		}

		if c.Tagged {
			tagged[c.ID] = true
		} else {
			untagged[c.ID] = true
		}
	}

	for _, q := range reg.All() {
		id := q.FullID()
		has := tagged[id] || untagged[id]

		switch {
		case q.Deferred != "":
			if !has {
				errs = append(errs, fmt.Errorf(
					"%s: deferred, but no test pins the current answer", id))
			}
		case q.Gap != "":
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

// checkObservations reports an observation no claim in its own test reaches,
// which the report would otherwise drop without a word. At run time a claim
// reaches its own t and that t's subtests: lexically, the function it is in.
func checkObservations(claims, observations []Claim) []error {
	var errs []error

	for _, o := range observations {
		if slices.ContainsFunc(claims, func(c Claim) bool { return reaches(c, o) }) {
			continue
		}

		errs = append(errs, fmt.Errorf(
			"%s:%d: observes %q, which no claim in %s reaches; the observation is dropped",
			o.File, o.Line, o.ID, o.Func,
		))
	}

	return errs
}

func reaches(c, o Claim) bool {
	return c.File == o.File && c.Func == o.Func && c.ID == o.ID &&
		c.scope[0] <= o.pos && o.pos <= c.scope[1]
}

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
