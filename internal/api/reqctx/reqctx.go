// Package reqctx carries the request values every response writer reads, the
// problem writer included, so none of them needs the transport package.
package reqctx

import "context"

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
