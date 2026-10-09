package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The fetch is shared by every caller, so one that disconnects must not fail
// it, or the failure is cached for everyone.
func TestDockerVersionFetchOutlivesTheCaller(t *testing.T) {
	c := newDockerVersionCache()
	c.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(
				strings.NewReader(
					`{"tag_name":"docker-v28.1.0","html_url":"https://example.test"}`,
				),
			),
		}, nil
	})}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	v, err := c.get(ctx)
	if err != nil || v == nil || v.Version != "28.1.0" {
		t.Fatalf("get with a cancelled caller = %+v, %v; want the fetched version", v, err)
	}
}
