package logs

import "testing"

// =============================================================================
// Log line parsing
// =============================================================================

func BenchmarkParseLine(b *testing.B) {
	b.Run("with_details", func(b *testing.B) {
		line := "2026-03-12T10:30:45.123456789Z com.docker.swarm.node.id=abc123,com.docker.swarm.service.id=svc456,com.docker.swarm.task.id=task789 INFO: request processed successfully"
		for b.Loop() {
			parseLine(line, "stdout")
		}
	})
	b.Run("plain_message", func(b *testing.B) {
		line := "2026-03-12T10:30:45.123456789Z INFO: request processed successfully"
		for b.Loop() {
			parseLine(line, "stdout")
		}
	})
	b.Run("no_timestamp", func(b *testing.B) {
		line := "some raw log output without timestamp"
		for b.Loop() {
			parseLine(line, "stderr")
		}
	})
}
