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
