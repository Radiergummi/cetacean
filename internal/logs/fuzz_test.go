package logs

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Exercises ParseDockerLogs against Docker's multiplexed framing, which Cetacean
// decodes straight off the daemon. The property is bounded allocation: the
// parser must never produce more message bytes, or more lines, than the input
// held — so a length prefix claiming more must not be allocated for.
func FuzzParseDockerLogs(f *testing.F) {
	frame := func(streamType byte, payload string) []byte {
		buf := make([]byte, 8+len(payload))
		buf[0] = streamType
		binary.BigEndian.PutUint32(buf[4:8], uint32(len(payload)))
		copy(buf[8:], payload)
		return buf
	}

	// A valid stdout frame.
	f.Add(frame(1, "hello world\n"))

	// A length prefix claiming 0xffffffff bytes with only one byte of
	// payload actually present.
	oversized := make([]byte, 9)
	oversized[0] = 1
	binary.BigEndian.PutUint32(oversized[4:8], 0xffffffff)
	oversized[8] = 'x'
	f.Add(oversized)

	// A header with no payload.
	f.Add([]byte{1, 0, 0, 0, 0, 0, 0, 0})

	f.Add([]byte{})
	f.Add([]byte("plain non-frame text with no length-prefixed header at all"))

	f.Fuzz(func(t *testing.T, data []byte) {
		lines, _ := ParseDockerLogs(bytes.NewReader(data))

		if len(lines) > len(data) {
			t.Fatalf("got %d lines from %d input bytes", len(lines), len(data))
		}

		var messageBytes int
		for _, line := range lines {
			messageBytes += len(line.Message)
		}
		if messageBytes > len(data) {
			t.Fatalf("got %d message bytes from %d input bytes", messageBytes, len(data))
		}
	})
}
