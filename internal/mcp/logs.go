package mcp

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/logs"
)

// LogStreamer is the subset of the docker.Client log API the MCP server needs.
// Matches api.DockerLogStreamer so concrete docker.Client satisfies both.
type LogStreamer interface {
	Logs(
		ctx context.Context,
		kind docker.LogKind,
		id string,
		tail string,
		follow bool,
		since, until string,
	) (io.ReadCloser, error)
}

// LogResourceResponse is the body returned by reading
// cetacean://services/<id>/logs and the get_logs tool. Lines are time-ordered
// (oldest first). Cursor is opaque to clients but in practice is the
// timestamp of the newest line returned — pass it back as `since` on the next
// read to receive only lines newer than it.
type LogResourceResponse struct {
	Lines  []logs.LogLine `json:"lines"`
	Cursor string         `json:"cursor,omitempty"`

	// Errors names the services a scoped read could not reach, so a partial
	// answer says which part is missing rather than pretending to be whole.
	// Always an array, never null — `omitempty` is deliberately absent, since
	// dropping the field on the common path is exactly the "never null"
	// promise broken in the other direction.
	Errors []string `json:"errors"`

	// Oldest is the timestamp of the earliest line returned, so a caller can
	// compare the window it asked for against the one it got without parsing
	// the lines. Empty when nothing matched.
	Oldest string `json:"oldest,omitempty"`

	// Truncated reports that the read ran out of budget before it ran out of
	// log, so it covers less time than it was asked to. Docker ignores `since`
	// for service logs and knows nothing of `contains` or `level`, so all three
	// are enforced by filtering after a fetch that `tail` bounds — on a chatty
	// service that bound binds first, and a five-minute request comes back
	// holding seconds. A search is bounded the same way with no window to fall
	// short of, and a read across many services is cut again when their lines
	// are merged; all three set this. Without it the two indistinguishable
	// answers are "nothing happened" and "I did not look that far".
	Truncated bool `json:"truncated,omitempty"`

	// Note is a caveat about the read as a whole rather than about any one
	// line: a scope wider than one fan-out may cover, so far. It exists
	// because the cap is otherwise invisible in the payload — a cluster-wide
	// grep that read a quarter of the services and matched nothing is
	// indistinguishable from a clean cluster, and the model reasons over the
	// result, not over the tool description.
	Note string `json:"note,omitempty"`
}

const (
	defaultLogTail  = 100
	maxLogTail      = 1000
	logFetchTimeout = 5 * time.Second
)

// readLogsImpl drives the cetacean://services/{id}/logs read and the get_logs
// tool so they produce identical output. Returns an empty response (not an
// error) when no LogStreamer is wired — keeps unit tests that don't need a
// Docker client working.
//
// kind selects the Docker endpoint: a service merges the output of its live
// replicas, while a task reads one replica's own stream and is the only way to
// reach a replica that has already exited. Swarm keeps a dead task's output
// only until its record falls out of the history window
// (TaskHistoryRetentionLimit, 5 by default), so on a service that restarts in
// a loop that window is seconds deep.
func (s *Server) readLogsImpl(
	ctx context.Context,
	kind docker.LogKind,
	targetID string,
	opts logOptions,
) (LogResourceResponse, error) {
	if s.logs == nil {
		return LogResourceResponse{Lines: []logs.LogLine{}, Errors: []string{}}, nil
	}

	if opts.since != "" {
		if _, err := time.Parse(time.RFC3339Nano, opts.since); err != nil {
			return LogResourceResponse{}, fmt.Errorf("since: %w", err)
		}
	}

	wanted := boundLogTail(opts.tail)

	// Every narrowing below happens after the fetch — Docker ignores `since`
	// for service logs, and it knows nothing of `contains` or `level` at all —
	// so any of them has to pull a wider window than the caller asked for.
	// Without it a grep returns the matches among the newest `tail` lines
	// rather than the newest `tail` matches, which on a scoped read is 50
	// lines per service. The widening is an implementation detail: the caller
	// asked for `tail` lines and gets at most that many.
	tail := wanted
	widened := opts.since != "" || opts.contains != "" || opts.level != ""
	if widened {
		tail = min(tail*10, maxLogTail)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, logFetchTimeout)
	defer cancel()

	reader, err := s.logs.Logs(
		fetchCtx,
		kind,
		targetID,
		strconv.Itoa(tail),
		false,
		opts.since,
		"",
	)
	if err != nil {
		if kind == docker.TaskLog && isNotFound(err) {
			return LogResourceResponse{}, s.explainMissingTaskLogs(targetID, err)
		}

		return LogResourceResponse{}, fmt.Errorf("fetch logs: %w", err)
	}
	defer reader.Close()

	// Use the idle-cancel variant: Docker's ServiceLogs with Follow=false
	// often leaves the stream open until the deadline expires. Cancelling
	// the fetch context 250ms after the last frame returns control without
	// waiting out the full logFetchTimeout. Mirrors REST log_handlers.go.
	lines, err := logs.ParseDockerLogsWithIdleCancel(reader, cancel, 250*time.Millisecond)
	if err != nil {
		return LogResourceResponse{}, fmt.Errorf("parse logs: %w", err)
	}

	// Whether the fetch itself was bounded has to be decided before the
	// narrowings below discard the evidence: once `since` has filtered the
	// lines away, a ceiling-bound read and a quiet service look identical.
	ceilingHit := widened && hitFetchCeiling(lines, tail)

	// How far back the fetch actually reached, which is the limit of what the
	// answer can be evidence about. It is not resp.Oldest below: that is the
	// oldest line to *survive* the filters, and on a grep the two are worlds
	// apart — a search that read back an hour and matched once ten minutes ago
	// would otherwise claim to have looked only ten minutes.
	deepest := oldestTimestamp(lines)

	lines = filterLogLines(lines, opts.level)
	lines = filterLogContains(lines, opts.contains)
	lines = logs.FilterSince(lines, opts.since)

	resp := finishLogRead(lines, wanted, opts.since)
	if ceilingHit {
		resp.Truncated = true
		resp.Note = truncationNote(tail, deepest, opts.since)
	}

	return resp, nil
}

// hitFetchCeiling reports whether the fetch stopped because it ran out of
// budget rather than out of log.
//
// It counts per task, because that is how Docker spends the budget: `tail` is
// applied to each of a service's task streams and the results are interleaved,
// so a three-replica service comes back holding up to three full windows.
// Comparing the merged total against the bound called every such read
// truncated — a complete answer reported as a partial one, which is the same
// lie the field exists to prevent, told in the other direction. Lines carry
// their task in Attrs because the fetch asks Docker for Details.
//
// A stream that ends exactly on the bound is indistinguishable from one that
// was cut, and is reported as cut: an unnecessary "there may be more" costs a
// caller one wider read, where the reverse costs it a wrong conclusion.
func hitFetchCeiling(lines []logs.LogLine, tail int) bool {
	if tail <= 0 {
		return false
	}

	perTask := make(map[string]int, 4)
	for _, line := range lines {
		perTask[line.Attrs["taskId"]]++
	}

	for _, count := range perTask {
		if count >= tail {
			return true
		}
	}

	return false
}

// truncationNote says what a filled fetch means for the answer.
//
// The two cases really are different questions. With `since` the caller named
// a window and did not get all of it, so the note compares the two. Without
// one — a `contains` grep or a `level` filter, which widen the fetch just the
// same — there is no window to fall short of, and the miss is that lines older
// than the ceiling were never searched at all. That second case was silent
// until now, and it is the common shape of a grep, since `since` is optional:
// a cluster-wide search that filled its budget and matched nothing answered
// exactly like a clean cluster.
func truncationNote(tail int, deepest, since string) string {
	if since != "" {
		return fmt.Sprintf(
			"reached the %d-line-per-task fetch ceiling: lines were read back "+
				"only to %s, not to %s, so this answer covers less time than "+
				"asked for — narrow it with `contains` or `level`, or read a "+
				"shorter window",
			tail, orNone(deepest), since,
		)
	}

	return fmt.Sprintf(
		"reached the %d-line-per-task fetch ceiling: lines were read back only "+
			"to %s, so anything older was never searched and this is not "+
			"evidence that nothing older matched — bound the read with `since`, "+
			"or raise `tail`",
		tail, orNone(deepest),
	)
}

// oldestTimestamp is the earliest timestamp among lines, which is not lines[0]:
// Docker interleaves a service's task streams, so arrival order is not time
// order until finishLogRead sorts them.
func oldestTimestamp(lines []logs.LogLine) string {
	oldest := ""
	for _, line := range lines {
		if line.Timestamp == "" {
			continue
		}

		if oldest == "" || line.Timestamp < oldest {
			oldest = line.Timestamp
		}
	}

	return oldest
}

// orNone renders an empty timestamp as something a sentence can hold, for the
// case where no line carried a parseable one.
func orNone(timestamp string) string {
	if timestamp == "" {
		return "an unknown point"
	}

	return timestamp
}

// boundLogTail clamps a caller's requested tail to the defaults, so the two
// log reads cannot disagree about how many lines "unspecified" or "too many"
// means.
func boundLogTail(tail int) int {
	if tail <= 0 {
		return defaultLogTail
	}

	return min(tail, maxLogTail)
}

// finishLogRead is the tail every log read shares: newest-last ordering, the
// caller's cut, and the cursor.
//
// Docker interleaves the output of a service's tasks, so arrival order is not
// time order. Sorting first makes the truncation keep the truly newest lines
// and the cursor the truly newest timestamp — without it a late-arriving older
// line would push the cursor backwards, silently dropping everything between
// the two on the next read. Mirrors the REST path in api/log_handlers.go.
//
// The cursor comes from logs.ParseCursor rather than the raw Docker timestamp,
// falling back to the caller's own `since` when no line carries a parseable
// one. internal/logs is where cursor semantics live and logs.FilterSince is
// what receives this value on the next call, so a cursor minted any other way
// resumes a tail on something the rest of the codebase never produced.
func finishLogRead(lines []logs.LogLine, wanted int, since string) LogResourceResponse {
	slices.SortStableFunc(lines, func(a, b logs.LogLine) int {
		return strings.Compare(a.Timestamp, b.Timestamp)
	})

	if len(lines) > wanted {
		lines = lines[len(lines)-wanted:]
	}

	if lines == nil {
		lines = []logs.LogLine{}
	}

	resp := LogResourceResponse{Lines: lines, Cursor: since, Errors: []string{}}
	if len(lines) > 0 {
		resp.Oldest = lines[0].Timestamp
	}

	for _, line := range slices.Backward(lines) {
		if cursor, ok := logs.ParseCursor(line.Timestamp); ok {
			resp.Cursor = cursor

			break
		}
	}

	return resp
}

// logOptions holds the parsed arguments shared between the log resource read
// and the get_logs tool.
type logOptions struct {
	tail  int
	since string
	level string

	// contains narrows to lines holding this substring, case-insensitively.
	// Applied server-side because the point of a cluster-wide read is that the
	// caller pays for matches rather than for every line of every service.
	contains string
}

// filterLogLines drops lines below the given minimum log level. Empty level
// returns the input unchanged. The level comparison is best-effort: Docker
// log lines don't carry a structured level, so we match common prefixes
// (DEBUG/INFO/WARN/ERROR/FATAL) anywhere in the message text.
func filterLogLines(lines []logs.LogLine, level string) []logs.LogLine {
	if level == "" {
		return lines
	}
	minRank, ok := logLevelRank[strings.ToLower(level)]
	if !ok {
		return lines
	}
	out := make([]logs.LogLine, 0, len(lines))
	for _, l := range lines {
		if lineLevelRank(l.Message) >= minRank {
			out = append(out, l)
		}
	}
	return out
}

// logLevelRank orders the levels `level` accepts. Keyed in the lower case a
// caller writes and the schema advertises; filterLogLines lower-cases before
// looking one up, so either case works on the wire.
var logLevelRank = map[string]int{
	"debug": 0,
	"info":  1,
	"warn":  2,
	"error": 3,
	"fatal": 4,
}

// logLevelNames lists the levels `level` accepts, least severe first.
func logLevelNames() []string {
	return slices.SortedFunc(maps.Keys(logLevelRank), func(a, b string) int {
		return cmp.Compare(logLevelRank[a], logLevelRank[b])
	})
}

func lineLevelRank(msg string) int {
	upper := strings.ToUpper(msg)
	switch {
	case strings.Contains(upper, "FATAL"):
		return 4
	case strings.Contains(upper, "ERROR"):
		return 3
	case strings.Contains(upper, "WARN"):
		return 2
	case strings.Contains(upper, "INFO"):
		return 1
	case strings.Contains(upper, "DEBUG"):
		return 0
	default:
		// Lines without an obvious level are treated as INFO so they
		// pass any threshold of INFO or below.
		return 1
	}
}

// optsFromToolRequest extracts the shared log option arguments from a
// CallToolRequest.
func optsFromToolRequest(req mcplib.CallToolRequest) logOptions {
	return logOptions{
		tail:     req.GetInt("tail", defaultLogTail),
		since:    req.GetString("since", ""),
		level:    req.GetString("level", ""),
		contains: req.GetString("contains", ""),
	}
}
