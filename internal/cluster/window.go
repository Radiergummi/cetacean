package cluster

import (
	"fmt"
	"strings"
	"time"
)

// TimeWindow is the span a timeline read covers. Since is exclusive and Until
// inclusive, so two windows sharing a boundary never both hold a moment; a zero
// bound is open. cache.HistoryQuery's After and Before are the same comparison.
type TimeWindow struct {
	Since time.Time
	Until time.Time
}

// ParseTimeWindow reads the since and until parameters both transports take;
// an empty value is an open bound.
func ParseTimeWindow(since, until string) (TimeWindow, error) {
	var (
		window TimeWindow
		err    error
	)

	if window.Since, err = ParseTimestamp("since", since); err != nil {
		return TimeWindow{}, err
	}

	if window.Until, err = ParseTimestamp("until", until); err != nil {
		return TimeWindow{}, err
	}

	return window, nil
}

// ParseTimestamp reads the RFC 3339 time a transport was given as name, or
// the zero time for an empty one. Its "T" and "Z" may be lower case (§5.6),
// which time.RFC3339 alone refuses.
func ParseTimestamp(name, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}

	parsed, err := time.Parse(time.RFC3339, strings.ToUpper(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%s: must be an RFC 3339 timestamp, such as 2026-09-05T14:02:00Z", name,
		)
	}

	return parsed, nil
}

// Overlaps reports whether an item spanning from to to is in the window, where
// a moment is from == to. A range is in it when any part of it is.
func (w TimeWindow) Overlaps(from, to time.Time) bool {
	return (w.Since.IsZero() || to.After(w.Since)) &&
		(w.Until.IsZero() || !from.After(w.Until))
}
