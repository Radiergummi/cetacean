package reqctx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestLogHandlerNamesTheRequest(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(LogHandler(slog.NewTextHandler(&out, nil))).With("component", "api")

	logger.InfoContext(WithRequestID(context.Background(), "abc123"), "inside")
	logger.InfoContext(context.Background(), "outside")

	inside, outside, _ := strings.Cut(out.String(), "\n")
	if !strings.Contains(inside, "req_id=abc123") || !strings.Contains(inside, "component=api") {
		t.Errorf(
			"a record logged inside a request = %q, want req_id=abc123 beside its attrs",
			inside,
		)
	}

	if strings.Contains(outside, "req_id") {
		t.Errorf("a record logged outside a request = %q, want no req_id", outside)
	}
}
