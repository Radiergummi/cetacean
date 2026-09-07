package prometheus

import (
	"context"
	"fmt"
	"io"
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
	u := pc.baseURL + "/api/v1/query?query=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prometheus query failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf(
			"prometheus returned HTTP %d for %s: %s",
			resp.StatusCode,
			u,
			string(preview),
		)
	}

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
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("prometheus response parse error: %w", err)
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

// RangeQuery runs a range query and decodes the matrix it returns.
//
// The raw variant exists for the proxy and the SSE stream, which forward
// Prometheus' own JSON verbatim; this one is for callers that need the samples
// themselves, and it drops a sample it cannot parse rather than failing the
// whole series — one malformed value should not blank a chart.
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
	u := pc.baseURL + "/api/v1/query_range?query=" + url.QueryEscape(query) +
		"&start=" + url.QueryEscape(start) + "&end=" + url.QueryEscape(end) +
		"&step=" + url.QueryEscape(step)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (pc *Client) InstantQueryRaw(ctx context.Context, query string) ([]byte, error) {
	u := pc.baseURL + "/api/v1/query?query=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
