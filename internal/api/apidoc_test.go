package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The playground loads Scalar from a file; nothing on it needs inline script.
func TestAPIPlaygroundPolicyAllowsNoInlineScript(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	newAPIDocRouter(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	policy := rec.Header().Get("Content-Security-Policy")
	for directive := range strings.SplitSeq(policy, ";") {
		name, sources, _ := strings.Cut(strings.TrimSpace(directive), " ")
		if name == "script-src" && strings.Contains(sources, "'unsafe-inline'") {
			t.Errorf("script-src = %q, want no 'unsafe-inline'", sources)
		}
	}
}
