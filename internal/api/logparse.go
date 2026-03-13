package api

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
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

		raw := strings.TrimRight(string(payload), "\n")
		for line := range strings.SplitSeq(raw, "\n") {
			if line == "" {
				continue
			}
			emit(parseLine(line, stream))
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
