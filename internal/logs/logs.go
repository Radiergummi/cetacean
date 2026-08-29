package logs

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const maxLogFrameSize = 1 << 20 // 1 MiB

// LogLine represents a single parsed Docker log line.
type LogLine struct {
	Timestamp string            `json:"timestamp"`
	Message   string            `json:"message"`
	Stream    string            `json:"stream"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

// readDockerLogFrames reads Docker multiplexed log frames and calls emit for each parsed line.
// Docker multiplex frame: [stream_type(1)][padding(3)][size(4 big-endian)][payload].
// Stream types: 1=stdout, 2=stderr.
//
// Docker's ServiceLogs with Follow=false may not close the stream after
// sending all data. To avoid blocking for the full context timeout, we
// use an idle-timeout wrapper: after receiving the first frame, if no
// new data arrives within 2 seconds, the wrapper closes the stream so
// the read returns immediately.
func readDockerLogFrames(r io.Reader, emit func(LogLine)) error {
	header := make([]byte, 8)

	for {
		_, err := io.ReadFull(r, header)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil
		}
		if err != nil {
			// Docker's log stream with Follow=false may not close
			// promptly after sending all data. Treat context
			// deadline/cancellation as EOF — all complete frames
			// have already been emitted.
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}

		streamType := header[0]
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			continue
		}
		if size > maxLogFrameSize {
			return fmt.Errorf("log frame too large: %d bytes (max %d)", size, maxLogFrameSize)
		}

		payload := make([]byte, size)
		if _, err := io.ReadFull(r, payload); err != nil {
			return err
		}

		stream := "stdout"
		if streamType == 2 {
			stream = "stderr"
		}

		// Docker prefixes only the first line of a multi-line message with a
		// timestamp and its detail labels; the continuation lines of a stack
		// trace or a pretty-printed JSON object carry neither. They belong to
		// the message they continue, so they inherit its identity — without
		// one they cannot be ordered, cursored, or attributed to a task.
		// Inheritance stops at the frame boundary, which is where Docker's
		// message boundary is: the next frame may well be another task's.
		var parent LogLine

		raw := strings.TrimRight(string(payload), "\n")
		for line := range strings.SplitSeq(raw, "\n") {
			if line == "" {
				continue
			}

			parsed := parseLine(line, stream)
			if parsed.Timestamp == "" {
				parsed.Timestamp = parent.Timestamp
				parsed.Attrs = parent.Attrs
			} else {
				parent = parsed
			}

			emit(parsed)
		}
	}
}

// ParseDockerLogs reads Docker multiplexed log output and returns parsed lines.
func ParseDockerLogs(r io.Reader) ([]LogLine, error) {
	var lines []LogLine
	err := readDockerLogFrames(r, func(l LogLine) {
		lines = append(lines, l)
	})
	return lines, err
}

// detailKeyMap maps Docker swarm label keys to short attribute names.
var detailKeyMap = map[string]string{
	"com.docker.swarm.node.id":    "nodeId",
	"com.docker.swarm.service.id": "serviceId",
	"com.docker.swarm.task.id":    "taskId",
}

func parseLine(line, stream string) LogLine {
	// Docker log format with Timestamps+Details: "TIMESTAMP DETAILS MESSAGE"
	// Extract timestamp first, then parse details from the remainder.
	var timestamp, rest string
	if len(line) > 31 && line[4] == '-' && line[10] == 'T' {
		if spaceIdx := strings.IndexByte(line, ' '); spaceIdx > 0 {
			timestamp = line[:spaceIdx]
			rest = line[spaceIdx+1:]
		}
	}
	if rest == "" {
		rest = line
	}

	attrs, msg := parseDetails(rest)
	return LogLine{Timestamp: timestamp, Message: msg, Stream: stream, Attrs: attrs}
}

// parseDetails extracts the comma-separated key=value prefix that Docker
// prepends when Details=true. Returns the attributes and the remaining line.
func parseDetails(line string) (map[string]string, string) {
	// Details are comma-separated key=value pairs before a space + timestamp.
	// Quick check: details always start with "com.docker." in swarm mode.
	if !strings.HasPrefix(line, "com.docker.") {
		return nil, line
	}

	// Find the end of the details section: first space followed by a timestamp
	// or message content.
	before, after, ok := strings.Cut(line, " ")
	if !ok {
		return nil, line
	}

	attrs := make(map[string]string)
	for pair := range strings.SplitSeq(before, ",") {
		before, after, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		key, val := before, after
		if short, ok := detailKeyMap[key]; ok {
			attrs[short] = val
		} else {
			attrs[key] = val
		}
	}
	if len(attrs) == 0 {
		return nil, line
	}
	return attrs, after
}

// StreamDockerLogs reads Docker multiplexed log frames and sends parsed lines to ch.
// Returns nil on EOF. The caller must close ch after this returns.
func StreamDockerLogs(r io.Reader, ch chan<- LogLine) error {
	return readDockerLogFrames(r, func(l LogLine) {
		ch <- l
	})
}

// ParseDockerLogsWithIdleCancel is like ParseDockerLogs but cancels the
// context (via cancelFn) if no new frames arrive within idleTimeout after
// the first frame. This works around Docker's ServiceLogs not closing the
// stream after sending all data with Follow=false.
func ParseDockerLogsWithIdleCancel(
	r io.Reader,
	cancelFn context.CancelFunc,
	idleTimeout time.Duration,
) ([]LogLine, error) {
	var lines []LogLine
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	err := readDockerLogFrames(r, func(l LogLine) {
		lines = append(lines, l)
		// Reset the idle timer on every frame received.
		if timer == nil {
			timer = time.AfterFunc(idleTimeout, cancelFn)
		} else {
			timer.Reset(idleTimeout)
		}
	})
	return lines, err
}

// dockerTimeFormat is how Docker stamps its log lines: UTC, nanosecond
// precision, fixed width. Normalising to it makes two timestamps comparable as
// strings; RFC3339Nano cannot be used because it trims trailing zeros, which
// sorts a truncated timestamp above genuinely newer ones.
const dockerTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// Canonical normalises a log timestamp to Docker's own format so it can be
// compared against another one as a string. Docker's own timestamps already
// are in that format, which is the fast path; a client-supplied cursor may be
// at second precision or in a non-UTC offset, and gets parsed and reformatted.
func Canonical(timestamp string) (string, bool) {
	if len(timestamp) == len("2006-01-02T15:04:05.000000000Z") &&
		timestamp[len(timestamp)-1] == 'Z' {
		return timestamp, true
	}

	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return "", false
	}

	return t.UTC().Format(dockerTimeFormat), true
}

// ParseCursor resolves a log cursor to Docker's canonical timestamp format.
//
// Cursors reach us as RFC 3339 timestamps at any precision and in any offset,
// or as Go durations meaning "this long ago" — all three documented and
// accepted. Comparing any of them against a Docker timestamp as a raw string
// is wrong: "30m" sorts below every timestamp, "…T12:00:00+02:00" above the
// same instant in UTC, and a second-precision cursor above the sub-second
// lines that follow it. Returns false when the cursor is empty or unusable,
// meaning no filtering should happen at all.
func ParseCursor(cursor string) (string, bool) {
	if cursor == "" {
		return "", false
	}

	if timestamp, ok := Canonical(cursor); ok {
		return timestamp, true
	}

	if d, err := time.ParseDuration(cursor); err == nil {
		return time.Now().Add(-d.Abs()).UTC().Format(dockerTimeFormat), true
	}

	return "", false
}

// Newer reports whether a log line's timestamp is strictly newer than a
// cursor produced by ParseCursor. A line without a usable timestamp counts as
// newer: it cannot be placed against the cursor, and dropping it would lose
// output for good.
func Newer(timestamp, cursor string) bool {
	normalized, ok := Canonical(timestamp)
	if !ok {
		return true
	}

	return normalized > cursor
}

// Older reports whether a log line's timestamp is strictly older than a cursor
// produced by ParseCursor. As in Newer, a line without a usable timestamp
// counts as included rather than dropped.
func Older(timestamp, cursor string) bool {
	normalized, ok := Canonical(timestamp)
	if !ok {
		return true
	}

	return normalized < cursor
}

// FilterSince returns the lines strictly newer than the given cursor.
//
// Docker ignores the Since option for service logs, so every caller that
// offers a cursor has to enforce it after parsing. This is that one place.
func FilterSince(lines []LogLine, since string) []LogLine {
	cursor, ok := ParseCursor(since)
	if !ok {
		return lines
	}

	filtered := lines[:0]
	for _, line := range lines {
		if !Newer(line.Timestamp, cursor) {
			continue
		}
		filtered = append(filtered, line)
	}

	return filtered
}

// BacklogFilter drops the lines a resumed follow stream has already delivered.
//
// A follow stream opens with a backlog — the lines Docker replays out of its
// tail before it catches up to live output — and only that backlog can hold
// lines the client already has. So only the backlog is filtered; live output
// is passed through whatever its timestamp says. That distinction matters
// because a task's timestamps come from the clock of the node running it,
// which need not agree with the clock that produced the cursor, and because a
// cursor can legitimately be expressed relative to now.
//
// One task's backlog is chronologically ordered, so the first of its lines to
// clear the cursor ends it: everything that task emits afterwards is newer
// still. Tasks are tracked apart because Docker interleaves them.
type BacklogFilter struct {
	cursor string
	caught map[string]bool
}

// NewBacklogFilter returns a filter for a stream resuming from the given
// cursor, or nil when there is no cursor to resume from — a nil filter keeps
// every line, so a fresh stream needs no special case.
func NewBacklogFilter(since string) *BacklogFilter {
	cursor, ok := ParseCursor(since)
	if !ok {
		return nil
	}

	return &BacklogFilter{cursor: cursor, caught: make(map[string]bool)}
}

// Keep reports whether the line should be delivered to the client.
func (f *BacklogFilter) Keep(line LogLine) bool {
	if f == nil {
		return true
	}

	task := line.Attrs["taskId"]
	if f.caught[task] {
		return true
	}

	if !Newer(line.Timestamp, f.cursor) {
		return false
	}

	// A line with no timestamp is delivered, but says nothing about where
	// the backlog ends, so it does not end it.
	if line.Timestamp != "" {
		f.caught[task] = true
	}

	return true
}
