package api

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
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
