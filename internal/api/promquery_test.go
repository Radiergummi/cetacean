package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPromClient_InstantQuery(t *testing.T) {
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

	pc := NewPromClient(prom.URL)
	results, err := pc.InstantQuery(context.Background(), `sum by (container_label_com_docker_stack_namespace)(container_memory_usage_bytes)`)
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

func TestPromClient_InstantQuery_Unreachable(t *testing.T) {
	pc := NewPromClient("http://127.0.0.1:1")
	_, err := pc.InstantQuery(context.Background(), "up")
	if err == nil {
		t.Fatal("expected error for unreachable prometheus")
	}
}

func TestPromClient_InstantQuery_ErrorResponse(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status": "error", "errorType": "bad_data", "error": "invalid query"}`))
	}))
	defer prom.Close()

	pc := NewPromClient(prom.URL)
	_, err := pc.InstantQuery(context.Background(), "bad{")
	if err == nil {
		t.Fatal("expected error for error response")
	}
}
