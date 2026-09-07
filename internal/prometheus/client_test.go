package prometheus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_InstantQuery(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [
					{"metric": {"container_label_com_docker_stack_namespace": "myapp"}, "value": [1234567890, "1073741824"]},
					{"metric": {"container_label_com_docker_stack_namespace": "monitoring"}, "value": [1234567890, "536870912"]}
				]
			}
		}`))
	}))
	defer prom.Close()

	pc := NewClient(prom.URL)
	results, err := pc.InstantQuery(
		context.Background(),
		`sum by (container_label_com_docker_stack_namespace)(container_memory_usage_bytes)`,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Labels["container_label_com_docker_stack_namespace"] != "myapp" {
		t.Errorf("unexpected label: %v", results[0].Labels)
	}
	if results[0].Value != 1073741824 {
		t.Errorf("unexpected value: %f", results[0].Value)
	}
}

func TestClient_InstantQuery_Unreachable(t *testing.T) {
	pc := NewClient("http://127.0.0.1:1")
	_, err := pc.InstantQuery(context.Background(), "up")
	if err == nil {
		t.Fatal("expected error for unreachable prometheus")
	}
}

func TestClient_InstantQuery_ErrorResponse(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status": "error", "errorType": "bad_data", "error": "invalid query"}`))
	}))
	defer prom.Close()

	pc := NewClient(prom.URL)
	_, err := pc.InstantQuery(context.Background(), "bad{")
	if err == nil {
		t.Fatal("expected error for error response")
	}
}

func TestRangeQueryRaw(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"up"},"values":[[1710000000,"1"],[1710000015,"1"]]}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "up" {
			t.Errorf("unexpected query: %s", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("start") == "" || r.URL.Query().Get("end") == "" ||
			r.URL.Query().Get("step") == "" {
			t.Error("missing start/end/step params")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	pc := NewClient(srv.URL)
	raw, err := pc.RangeQueryRaw(context.Background(), "up", "1710000000", "1710000015", "15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != body {
		t.Errorf("unexpected body: %s", string(raw))
	}
}

func TestInstantQueryRaw(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up"},"values":[]}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	defer srv.Close()

	pc := NewClient(srv.URL)
	raw, err := pc.InstantQueryRaw(context.Background(), "up")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != body {
		t.Errorf("unexpected body: %s", string(raw))
	}
}

func TestRangeQueryRaw_PrometheusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"invalid query"}`))
	}))
	defer srv.Close()

	pc := NewClient(srv.URL)
	_, err := pc.RangeQueryRaw(context.Background(), "bad{", "0", "1", "1")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestInstantQueryRaw_ConnectionRefused(t *testing.T) {
	pc := NewClient("http://127.0.0.1:1")
	_, err := pc.InstantQueryRaw(context.Background(), "up")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}
