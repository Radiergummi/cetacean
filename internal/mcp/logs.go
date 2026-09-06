package mcp

import (
	"context"
	"fmt"
	"io"
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
	if opts.since != "" || opts.contains != "" || opts.level != "" {
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

	lines = filterLogLines(lines, opts.level)
	lines = filterLogContains(lines, opts.contains)
	lines = logs.FilterSince(lines, opts.since)

	return finishLogRead(lines, wanted, opts.since), nil
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
	minRank, ok := logLevelRank[strings.ToUpper(level)]
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

var logLevelRank = map[string]int{
	"DEBUG": 0,
	"INFO":  1,
	"WARN":  2,
	"ERROR": 3,
	"FATAL": 4,
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
