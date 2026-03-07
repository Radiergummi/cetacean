package api

import (
	"encoding/binary"
	"io"
	"strings"
)

// LogLine represents a single parsed Docker log line.
type LogLine struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Stream    string `json:"stream"`
}

// ParseDockerLogs reads Docker multiplexed log output and returns parsed lines.
// Docker multiplex frame: [stream_type(1)][padding(3)][size(4 big-endian)][payload].
// Stream types: 1=stdout, 2=stderr.
func ParseDockerLogs(r io.Reader) ([]LogLine, error) {
	var lines []LogLine
	header := make([]byte, 8)

	for {
		_, err := io.ReadFull(r, header)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return lines, err
		}

		streamType := header[0]
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			continue
		}

		payload := make([]byte, size)
		if _, err := io.ReadFull(r, payload); err != nil {
			return lines, err
		}

		stream := "stdout"
		if streamType == 2 {
			stream = "stderr"
		}

		raw := strings.TrimRight(string(payload), "\n")
		for _, line := range strings.Split(raw, "\n") {
			if line == "" {
				continue
			}
			lines = append(lines, parseLine(line, stream))
		}
	}

	return lines, nil
}

func parseLine(line, stream string) LogLine {
	// Docker timestamps: 2024-01-01T00:00:00.000000000Z <message>
	if len(line) > 31 && line[4] == '-' && line[10] == 'T' {
		spaceIdx := strings.IndexByte(line, ' ')
		if spaceIdx > 0 {
			return LogLine{
				Timestamp: line[:spaceIdx],
				Message:   line[spaceIdx+1:],
				Stream:    stream,
			}
		}
	}
	return LogLine{Timestamp: "", Message: line, Stream: stream}
}

// StreamDockerLogs reads Docker multiplexed log frames and sends parsed lines to ch.
// Returns nil on EOF. The caller must close ch after this returns.
func StreamDockerLogs(r io.Reader, ch chan<- LogLine) error {
	header := make([]byte, 8)

	for {
		_, err := io.ReadFull(r, header)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil
		}
		if err != nil {
			return err
		}

		streamType := header[0]
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			continue
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
		for _, line := range strings.Split(raw, "\n") {
			if line == "" {
				continue
			}
			ch <- parseLine(line, stream)
		}
	}
}
