package prometheus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	"github.com/radiergummi/cetacean/internal/prom"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (pc *Client) InstantQuery(ctx context.Context, query string) ([]prom.Result, error) {
	respBody, err := pc.open(ctx, "/api/v1/query", url.Values{"query": {query}})
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var body struct {
		Status    string `json:"status"`
		Error     string `json:"error"`
		ErrorType string `json:"errorType"`
		Data      struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string  `json:"metric"`
				Value  [2]json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	err = json.NewDecoder(io.LimitReader(respBody, maxResponseBytes)).Decode(&body)
	if err != nil {
		return nil, &redactedError{message: "prometheus response parse error", cause: err}
	}
	if body.Status != "success" {
		return nil, fmt.Errorf("prometheus error: %s: %s", body.ErrorType, body.Error)
	}

	results := make([]prom.Result, 0, len(body.Data.Result))
	for _, r := range body.Data.Result {
		var valStr string
		if err := json.Unmarshal(r.Value[1], &valStr); err != nil {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		results = append(results, prom.Result{
			Labels: r.Metric,
			Value:  val,
		})
	}
	return results, nil
}

// RangeQuery runs a range query and decodes the matrix it returns. The raw
// variant is for the proxy and the SSE stream, which forward Prometheus' own
// JSON verbatim; this one drops a sample it cannot parse rather than failing
// the series, since one malformed value should not blank a chart.
func (pc *Client) RangeQuery(
	ctx context.Context,
	query, start, end, step string,
) ([]prom.Series, error) {
	raw, err := pc.RangeQueryRaw(ctx, query, start, end, step)
	if err != nil {
		return nil, err
	}

	var body struct {
		Status    string `json:"status"`
		Error     string `json:"error"`
		ErrorType string `json:"errorType"`
		Data      struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string    `json:"metric"`
				Values [][2]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("prometheus response parse error: %w", err)
	}
	if body.Status != "success" {
		return nil, fmt.Errorf("prometheus error: %s: %s", body.ErrorType, body.Error)
	}

	series := make([]prom.Series, 0, len(body.Data.Result))
	for _, r := range body.Data.Result {
		points := make([]prom.Point, 0, len(r.Values))
		for _, sample := range r.Values {
			var timestamp float64
			if err := json.Unmarshal(sample[0], &timestamp); err != nil {
				continue
			}

			var raw string
			if err := json.Unmarshal(sample[1], &raw); err != nil {
				continue
			}

			value, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				continue
			}

			points = append(points, prom.Point{Timestamp: timestamp, Value: value})
		}

		series = append(series, prom.Series{Labels: r.Metric, Points: points})
	}

	return series, nil
}

func (pc *Client) RangeQueryRaw(
	ctx context.Context,
	query, start, end, step string,
) ([]byte, error) {
	return pc.get(ctx, "/api/v1/query_range", url.Values{
		"query": {query},
		"start": {start},
		"end":   {end},
		"step":  {step},
	})
}

func (pc *Client) InstantQueryRaw(ctx context.Context, query string) ([]byte, error) {
	return pc.get(ctx, "/api/v1/query", url.Values{"query": {query}})
}

func (pc *Client) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	respBody, err := pc.open(ctx, path, params)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()
	return readLimited(respBody)
}

// ErrResponseTooLarge is the cause of an error for a Prometheus answer longer
// than maxResponseBytes, which is refused rather than cut short.
var ErrResponseTooLarge = errors.New("prometheus response exceeds the size limit")

// readLimited reads a whole body of at most maxResponseBytes. It reads one
// byte past the limit, since a body of exactly the limit is not too large.
func readLimited(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxResponseBytes+1))
	if err != nil {
		return nil, unreachable(err)
	}
	if len(body) > maxResponseBytes {
		return nil, &redactedError{message: "prometheus response too large", cause: ErrResponseTooLarge}
	}

	return body, nil
}

// open sends a GET and returns the body of a 200 for the caller to read and
// close. Of any other status it reads only the preview statusError keeps.
func (pc *Client) open(ctx context.Context, path string, params url.Values) (io.ReadCloser, error) {
	u := pc.baseURL + path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, invalidRequest(err)
	}
	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, unreachable(err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, statusPreviewBytes))
		return nil, statusError(resp.StatusCode, preview)
	}
	return resp.Body, nil
}

// redactedError is what callers relay to clients: a fixed message, since the
// cause names the internal Prometheus address. Logging it records the cause.
type redactedError struct {
	message string
	cause   error
}

func (e *redactedError) Error() string { return e.message }

func (e *redactedError) Unwrap() error { return e.cause }

func (e *redactedError) LogValue() slog.Value {
	return slog.StringValue(e.message + ": " + e.cause.Error())
}

func unreachable(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &redactedError{message: "prometheus query timed out", cause: err}
	}

	return &redactedError{message: "prometheus unreachable", cause: err}
}

// statusError keeps the upstream body as the logged cause, since the message
// drops a 5xx's text.
func statusError(status int, body []byte) error {
	if len(body) > statusPreviewBytes {
		body = body[:statusPreviewBytes]
	}

	return &redactedError{
		message: prometheusErrorDetail(status, body),
		cause:   errors.New(string(body)),
	}
}

const statusPreviewBytes = 512

func invalidRequest(err error) error {
	return &redactedError{message: "failed to create prometheus request", cause: err}
}
