// Package reqctx carries the request values every response writer and log line
// reads, the problem writer included, so none of them needs the transport package.
package reqctx

import (
	"context"
	"log/slog"
)

type basePathKey struct{}

type requestIDKey struct{}

// WithBasePath returns ctx carrying the base path the request was served under.
func WithBasePath(ctx context.Context, basePath string) context.Context {
	return context.WithValue(ctx, basePathKey{}, basePath)
}

// BasePath is the base path ctx carries, or "" when there is none.
func BasePath(ctx context.Context) string {
	v, _ := ctx.Value(basePathKey{}).(string)
	return v
}

// AbsPath prepends the base path ctx carries to path.
func AbsPath(ctx context.Context, path string) string {
	return BasePath(ctx) + path
}

// WithRequestID returns ctx carrying the request's ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID is the request ID ctx carries, or "" when there is none.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}

// LogHandler adds the request ID to every record logged with a context that
// carries one, so a requestId a client was given finds its log lines.
func LogHandler(next slog.Handler) slog.Handler { return logHandler{next} }

type logHandler struct{ slog.Handler }

func (h logHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := RequestID(ctx); id != "" {
		record.AddAttrs(slog.String("req_id", id))
	}

	return h.Handler.Handle(ctx, record)
}

func (h logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return logHandler{h.Handler.WithAttrs(attrs)}
}

func (h logHandler) WithGroup(name string) slog.Handler {
	return logHandler{h.Handler.WithGroup(name)}
}
