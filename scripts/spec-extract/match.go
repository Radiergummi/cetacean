package main

import (
	"strings"

	"github.com/radiergummi/cetacean/internal/spec"
)

// minOverlap is the share of a statement's words a registry entry must carry,
// in order, to count as holding it. Not an exact comparison: the registry
// reflows what it quotes and elides with "[...]", so a statement and the entry
// quoting it agree on their words and never on their bytes.
const minOverlap = 0.7

// words normalises for comparison. Case and punctuation are how two copies of
// one sentence differ most and mean least.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

// shortest is the fewest words that mean anything spread across a sentence.
// "error REQUIRED" is a subsequence of almost any paragraph, so below this a
// statement is anchored to the opening of the entry instead.
const shortest = 5

// overlap scores the shorter side's containment in the longer one. Measured
// that way round because an entry elides — "[...]" and a dropped citation are
// both common — and never adds, so the quote is the subsequence and the
// document's sentence is what it is a subsequence of.
func overlap(a, b []string) float64 {
	if len(b) < len(a) {
		a, b = b, a
	}

	if len(a) == 0 {
		return 0
	}

	// A parameter declaration is two or three words. An entry quoting one
	// opens with it, so that is where it has to be found.
	if len(a) < shortest {
		b = b[:min(len(a)+3, len(b))]
	}

	matched, j := 0, 0

	for _, w := range a {
		for j < len(b) && b[j] != w {
			j++
		}

		if j < len(b) {
			matched++
			j++
		}
	}

	return float64(matched) / float64(len(a))
}

// Reconcile reports both ways round: the statements no requirement quotes, and
// the requirements no statement reaches. A dismissal carries no text to match,
// so a statement only a dismissal accounts for comes back unheld.
func Reconcile(statements []string, doc *spec.Document) (unheld, unquoted []string) {
	entries := make([][]string, len(doc.Requirements))
	for i := range doc.Requirements {
		entries[i] = words(doc.Requirements[i].Text)
	}

	reached := make([]bool, len(entries))

	for _, s := range statements {
		got, quoted := words(s), false

		for i := range entries {
			if overlap(got, entries[i]) >= minOverlap {
				reached[i], quoted = true, true
			}
		}

		if !quoted {
			unheld = append(unheld, s)
		}
	}

	for i, ok := range reached {
		if !ok {
			unquoted = append(unquoted, doc.Requirements[i].ID)
		}
	}

	return unheld, unquoted
}
