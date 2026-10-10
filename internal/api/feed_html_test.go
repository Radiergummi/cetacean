package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

func TestHistoryEntryHTMLEscapesNames(t *testing.T) {
	got := historyEntryHTML(cache.HistoryEntry{
		Type:    "node",
		Action:  "update",
		Name:    `<img src=x onerror=alert(1)>`,
		Summary: `<script>steal()</script>`,
	}, "")

	if strings.Contains(got, "<img") || strings.Contains(got, "<script>") {
		t.Errorf("unescaped markup in feed HTML: %s", got)
	}
}

func TestRecommendationFeedEntryEscapesTheMessage(t *testing.T) {
	entry := recommendationToFeedEntry(
		httptest.NewRequest("GET", "/recommendations.atom", nil),
		recommendations.Recommendation{Message: `node <img src=x onerror=alert(1)> is full`},
		time.Now(),
	)

	if strings.Contains(entry.ContentHTML, "<img") {
		t.Errorf("unescaped markup in feed HTML: %s", entry.ContentHTML)
	}
}

// A trusted proxy's X-Forwarded-Host passes isAuthority with quotes and angle
// brackets in it, so the origin is as untrusted as any other value here.
func TestFeedEntryHTMLEscapesTheLink(t *testing.T) {
	req := httptest.NewRequest("GET", "/recommendations.atom", nil)
	req.Host = `a"><b`

	history := historyEntryHTML(
		cache.HistoryEntry{Type: "node", Name: "n"},
		absURL(req, "/nodes/x"),
	)
	rec := recommendationToFeedEntry(
		req,
		recommendations.Recommendation{Scope: recommendations.ScopeNode, TargetID: "x"},
		time.Now(),
	)

	for _, got := range []string{history, rec.ContentHTML} {
		if strings.Contains(got, "<b") {
			t.Errorf("unescaped markup in feed link: %s", got)
		}
	}
}
