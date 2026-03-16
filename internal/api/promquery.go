package api

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
)

type PromClient struct {
	baseURL string
	client  *http.Client
}

func NewPromClient(baseURL string) *PromClient {
	return &PromClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

type PromResult struct {
	Labels map[string]string
	Value  float64
}

func (pc *PromClient) InstantQuery(ctx context.Context, query string) ([]PromResult, error) {
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

	results := make([]PromResult, 0, len(body.Data.Result))
	for _, r := range body.Data.Result {
		var valStr string
		if err := json.Unmarshal(r.Value[1], &valStr); err != nil {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		results = append(results, PromResult{
			Labels: r.Metric,
			Value:  val,
		})
	}
	return results, nil
}

func (pc *PromClient) RangeQueryRaw(ctx context.Context, query, start, end, step string) ([]byte, error) {
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

func (pc *PromClient) InstantQueryRaw(ctx context.Context, query string) ([]byte, error) {
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
