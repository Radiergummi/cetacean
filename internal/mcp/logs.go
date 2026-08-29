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
}

const (
	defaultLogTail  = 100
	maxLogTail      = 1000
	logFetchTimeout = 5 * time.Second
)

// readServiceLogsImpl drives both the cetacean://services/{id}/logs read and
// the get_logs tool so they produce identical output. Returns an empty
// response (not an error) when no LogStreamer is wired — keeps unit tests
// that don't need a Docker client working.
func (s *Server) readServiceLogsImpl(
	ctx context.Context,
	serviceID string,
	opts logOptions,
) (LogResourceResponse, error) {
	if s.logs == nil {
		return LogResourceResponse{Lines: []logs.LogLine{}}, nil
	}

	if opts.since != "" {
		if _, err := time.Parse(time.RFC3339Nano, opts.since); err != nil {
			return LogResourceResponse{}, fmt.Errorf("since: %w", err)
		}
	}

	wanted := opts.tail
	if wanted <= 0 {
		wanted = defaultLogTail
	}
	if wanted > maxLogTail {
		wanted = maxLogTail
	}

	// Docker ignores `since` for service logs, so a cursored read has to pull
	// a wider window and narrow it here. The widening is an implementation
	// detail: the caller asked for `tail` lines and gets at most that many.
	tail := wanted
	if opts.since != "" {
		tail = min(tail*10, maxLogTail)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, logFetchTimeout)
	defer cancel()

	reader, err := s.logs.Logs(
		fetchCtx,
		docker.ServiceLog,
		serviceID,
		strconv.Itoa(tail),
		false,
		opts.since,
		"",
	)
	if err != nil {
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
	lines = logs.FilterSince(lines, opts.since)

	// Docker interleaves the output of a service's tasks, so arrival order is
	// not time order. Sorting first makes the truncation below keep the truly
	// newest lines and the cursor the truly newest timestamp — without it a
	// late-arriving older line would push the cursor backwards, silently
	// dropping everything between the two on the next read. Mirrors the REST
	// path in api/log_handlers.go.
	slices.SortStableFunc(lines, func(a, b logs.LogLine) int {
		return strings.Compare(a.Timestamp, b.Timestamp)
	})

	if len(lines) > wanted {
		lines = lines[len(lines)-wanted:]
	}

	resp := LogResourceResponse{Lines: lines, Cursor: opts.since}
	for _, line := range slices.Backward(lines) {
		if cursor, ok := logs.ParseCursor(line.Timestamp); ok {
			resp.Cursor = cursor
			break
		}
	}
	return resp, nil
}

// logOptions holds the parsed arguments shared between the log resource read
// and the get_logs tool.
type logOptions struct {
	tail  int
	since string
	level string
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
		tail:  req.GetInt("tail", defaultLogTail),
		since: req.GetString("since", ""),
		level: req.GetString("level", ""),
	}
}
