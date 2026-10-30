package api

import (
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// feedEntry is a format-agnostic representation of a single feed entry.
// Both Atom and JSON Feed renderers convert from this type.
type feedEntry struct {
	ID          string
	Title       string
	ContentHTML string
	URL         string
	Updated     time.Time
	Tags        []string

	// Author names who made the change, empty for one made outside Cetacean.
	Author string
}

// feedData holds the format-agnostic data needed to render a feed.
type feedData struct {
	Title      string
	Entries    []feedEntry
	Updated    time.Time
	BeforeID   uint64
	Limit      int
	LastItemID uint64 // numeric ID of last entry for pagination cursors

	// QueryParams names the query parameters this feed reads beyond the
	// pagination pair, and so the only ones its links may echo back.
	QueryParams []string
}

// feedRenderer writes a feed in a specific format (Atom, JSON Feed, etc.).
type feedRenderer func(w http.ResponseWriter, r *http.Request, data feedData)

// feedListHandler returns a handler that queries history for the given event
// type and renders it using the provided renderer.
func (h *Handlers) feedListHandler(
	title string,
	eventType cache.EventType,
	render feedRenderer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireAnyGrant(w, r) {
			return
		}

		entries, beforeID, limit, ok := h.queryHistoryFeed(
			w,
			r,
			cache.HistoryQuery{Type: eventType},
		)
		if !ok {
			return
		}

		render(w, r, historyFeedData(r, title, entries, beforeID, limit))
	}
}

// feedDetailHandler returns a handler that queries history for a single
// resource and renders it using the provided renderer.
func (h *Handlers) feedDetailHandler(
	eventType cache.EventType,
	idParam string,
	nameFunc func(id string) string,
	render feedRenderer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireAnyGrant(w, r) {
			return
		}

		resourceID := r.PathValue(idParam)

		entries, beforeID, limit, ok := h.queryHistoryFeed(w, r, cache.HistoryQuery{
			Type:       eventType,
			ResourceID: resourceID,
		})
		if !ok {
			return
		}

		// The title must not name what the caller cannot read. A task is keyed
		// by its ID, which is what its parent service's grant resolves from.
		title := nameFunc(resourceID)
		key := title
		if eventType == cache.EventTask {
			key = resourceID
		}
		canRead := h.acl.Checker(auth.IdentityFromContext(r.Context()), "read")
		if !canRead(string(eventType) + ":" + key) {
			title = resourceID
		}

		render(w, r, historyFeedData(r, title, entries, beforeID, limit))
	}
}

// handleFeedHistory queries global history and renders it using the given renderer.
func (h *Handlers) handleFeedHistory(
	w http.ResponseWriter,
	r *http.Request,
	render feedRenderer,
) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	entries, beforeID, limit, ok := h.queryHistoryFeed(w, r, cache.HistoryQuery{})
	if !ok {
		return
	}

	render(w, r, historyFeedData(r, "History", entries, beforeID, limit))
}

// queryHistoryFeed runs query for a feed page: the request's pagination, only
// what the caller may read, and tasks named. It answers a malformed page itself.
func (h *Handlers) queryHistoryFeed(
	w http.ResponseWriter,
	r *http.Request,
	query cache.HistoryQuery,
) ([]cache.HistoryEntry, uint64, int, bool) {
	beforeID, limit, err := parseFeedPagination(r)
	if err != nil {
		problem.Write(w, r, "API016", err.Error())
		return nil, 0, 0, false
	}

	query.BeforeID = beforeID
	query.Limit = limit
	query.Visible = h.readableHistory(r)
	canRead := h.acl.Checker(auth.IdentityFromContext(r.Context()), "read")
	entries := cluster.NameHistoryTasks(h.cache, h.cache.History().List(query), canRead)

	return entries, beforeID, limit, true
}

// handleFeedSearch validates the search query, queries matching history entries,
// and renders them using the given renderer.
func (h *Handlers) handleFeedSearch(
	w http.ResponseWriter,
	r *http.Request,
	render feedRenderer,
) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	q, ok := searchQuery(w, r)
	if !ok {
		return
	}

	entries, beforeID, limit, ok := h.queryHistoryFeed(w, r, cache.HistoryQuery{NameContains: q})
	if !ok {
		return
	}

	// This feed titles itself with ?q= verbatim beside ACL-filtered entries,
	// the same BREACH shape HandleSearch opts out of, so it opts out too.
	render(w, disableCompression(r), searchFeedData(
		r, fmt.Sprintf("Cetacean — Search: %s", q), entries, beforeID, limit,
	))
}

// searchFeedData is historyFeedData for the search feed, the one feed that
// reads ?q= and so the only one whose links may carry it back.
func searchFeedData(
	r *http.Request,
	title string,
	entries []cache.HistoryEntry,
	beforeID uint64,
	limit int,
) feedData {
	data := historyFeedData(r, title, entries, beforeID, limit)
	data.QueryParams = searchFeedParams

	return data
}

// handleFeedRecommendations queries recommendations and renders them using the
// given renderer.
func (h *Handlers) handleFeedRecommendations(
	w http.ResponseWriter,
	r *http.Request,
	render feedRenderer,
) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	results := recommendations.Readable(
		h.recEngine.Results(),
		h.acl.Checker(auth.IdentityFromContext(r.Context()), "read"),
	)

	lastTick := h.recEngine.LastTick()
	if lastTick.IsZero() {
		lastTick = time.Now()
	}

	entries := make([]feedEntry, 0, len(results))
	for _, rec := range results {
		entries = append(entries, recommendationToFeedEntry(r, rec, lastTick))
	}

	render(w, r, feedData{
		Title:   "Recommendations",
		Entries: entries,
		Updated: lastTick,
	})
}

// feedHistoryHandler serves /history's feeds, which read the time window its
// JSON does. /events serves the same feed through handleFeedHistory, unwindowed.
func (h *Handlers) feedHistoryHandler(render feedRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireAnyGrant(w, r) {
			return
		}

		q := r.URL.Query()

		window, err := cluster.ParseTimeWindow(q.Get("since"), q.Get("until"))
		if err != nil {
			problem.Write(w, r, "API016", err.Error())
			return
		}

		entries, beforeID, limit, ok := h.queryHistoryFeed(w, r, cache.HistoryQuery{
			After:  window.Since,
			Before: window.Until,
		})
		if !ok {
			return
		}

		data := historyFeedData(r, "History", entries, beforeID, limit)
		data.QueryParams = historyWindowParams

		render(w, r, data)
	}
}

// feedSearchHandler wraps handleFeedSearch as an http.HandlerFunc.
func (h *Handlers) feedSearchHandler(render feedRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.handleFeedSearch(w, r, render)
	}
}

// feedRecommendationsHandler wraps handleFeedRecommendations as an http.HandlerFunc.
func (h *Handlers) feedRecommendationsHandler(render feedRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.handleFeedRecommendations(w, r, render)
	}
}

// actionVerb maps history action strings to past-tense verbs for feed titles.
var actionVerb = map[string]string{
	"create":      "Added",
	"update":      "Updated",
	"remove":      "Removed",
	"ref_changed": "Updated",
}

// historyFeedData builds a feedData from history entries, including the
// last entry's numeric ID for pagination cursors.
func historyFeedData(
	r *http.Request,
	title string,
	entries []cache.HistoryEntry,
	beforeID uint64,
	limit int,
) feedData {
	var lastItemID uint64
	if len(entries) > 0 {
		lastItemID = entries[len(entries)-1].ID
	}

	return feedData{
		Title:      title,
		Entries:    historyToFeedEntries(r, entries),
		Updated:    historyUpdated(entries),
		BeforeID:   beforeID,
		Limit:      limit,
		LastItemID: lastItemID,
	}
}

// historyToFeedEntries converts cache history entries to format-agnostic feed entries.
func historyToFeedEntries(r *http.Request, entries []cache.HistoryEntry) []feedEntry {
	result := make([]feedEntry, 0, len(entries))

	for _, e := range entries {
		if e.Kind == cache.KindGap {
			result = append(result, gapFeedEntry(e))
			continue
		}

		verb := actionVerb[e.Action]
		if verb == "" && e.Action != "" {
			verb = strings.ToUpper(e.Action[:1]) + e.Action[1:]
		}

		var href string
		if path := sse.ResourcePath(e.Type, e.ResourceID); path != "" {
			href = absURL(r, path)
		}

		result = append(result, feedEntry{
			ID:          fmt.Sprintf("urn:cetacean:history:%d", e.ID),
			Title:       fmt.Sprintf("%s %s: %s", verb, e.Type, e.Name),
			ContentHTML: historyEntryHTML(e, href),
			URL:         href,
			Updated:     e.Timestamp,
			Tags:        []string{string(e.Type)},
			Author:      actorName(e.Actor),
		})
	}

	return result
}

// gapFeedEntry is a gap as a feed entry: it links nowhere, since it names no
// resource, and it was last updated when it was recorded, at its end.
func gapFeedEntry(e cache.HistoryEntry) feedEntry {
	from, until := e.Timestamp.UTC().Format(time.RFC3339), e.Until.UTC().Format(time.RFC3339)

	return feedEntry{
		ID:    fmt.Sprintf("urn:cetacean:history:%d", e.ID),
		Title: fmt.Sprintf("Nothing recorded from %s to %s", from, until),
		ContentHTML: "<p>Cetacean was not watching the cluster from " + from + " to " + until +
			", so changes made then are missing from this record.</p>",
		Updated: e.Until,
		Tags:    []string{cache.KindGap},
	}
}

// recommendationToFeedEntry converts a recommendation to a format-agnostic feed entry.
func recommendationToFeedEntry(
	r *http.Request,
	rec recommendations.Recommendation,
	updated time.Time,
) feedEntry {
	id := fmt.Sprintf(
		"urn:cetacean:recommendation:%s:%s:%s",
		rec.TargetID, rec.Category, rec.Resource,
	)

	var href string
	if rec.TargetID != "" {
		if path := recommendationTargetPath(rec); path != "" {
			href = absURL(r, path)
		}
	}

	content := "<p>" + html.EscapeString(rec.Message) + "</p>"
	if href != "" {
		content += `<p><a href="` + html.EscapeString(href) + `">View in Cetacean</a></p>`
	}

	return feedEntry{
		ID: id,
		Title: fmt.Sprintf(
			"[%s] %s: %s",
			rec.Severity, rec.Category, rec.TargetName,
		),
		ContentHTML: content,
		URL:         href,
		Updated:     updated,
		Tags:        []string{string(rec.Category), string(rec.Severity)},
	}
}

// historyEntryHTML renders a human-readable HTML snippet for a history entry.
func historyEntryHTML(e cache.HistoryEntry, href string) string {
	verb := actionVerb[e.Action]
	if verb == "" {
		verb = e.Action
	}

	var b strings.Builder
	b.WriteString("<p>")
	b.WriteString(html.EscapeString(verb))
	b.WriteString(" ")
	b.WriteString(html.EscapeString(string(e.Type)))
	b.WriteString(" <strong>")
	b.WriteString(html.EscapeString(e.Name))
	b.WriteString("</strong>")
	if e.Summary != "" && e.Summary != e.Action+" "+e.Name {
		b.WriteString(": ")
		b.WriteString(html.EscapeString(e.Summary))
	}
	if e.Actor != nil {
		b.WriteString(" by ")
		b.WriteString(html.EscapeString(e.Actor.DisplayName()))
		b.WriteString(" via ")
		b.WriteString(html.EscapeString(transportLabel[e.Actor.Transport]))
	}
	b.WriteString("</p>")

	if href != "" {
		b.WriteString(`<p><a href="`)
		b.WriteString(html.EscapeString(href))
		b.WriteString(`">View in Cetacean</a></p>`)
	}

	return b.String()
}

// transportLabel is how a reader names each transport.
var transportLabel = map[cache.Transport]string{
	cache.TransportDashboard: "the dashboard",
	cache.TransportREST:      "the API",
	cache.TransportMCP:       "MCP",
}

func actorName(actor *cache.Actor) string {
	if actor == nil {
		return ""
	}

	return actor.DisplayName()
}

// recommendationTargetPath maps a recommendation to the URL path of its target.
func recommendationTargetPath(rec recommendations.Recommendation) string {
	switch rec.Scope {
	case recommendations.ScopeService:
		return "/services/" + rec.TargetID
	case recommendations.ScopeNode:
		return "/nodes/" + rec.TargetID
	case recommendations.ScopeSecret:
		return "/secrets/" + rec.TargetID
	case recommendations.ScopeConfig:
		return "/configs/" + rec.TargetID
	case recommendations.ScopeNetwork:
		return "/networks/" + rec.TargetID
	default:
		return "/cluster"
	}
}

// readableHistory admits the history entries the caller of r may read.
func (h *Handlers) readableHistory(r *http.Request) func(cache.HistoryEntry) bool {
	return cluster.HistoryReadable(
		h.acl.Checker(auth.IdentityFromContext(r.Context()), "read"),
	)
}

// feedID builds a tag URI (RFC 4151) for the feed: tag:{host},{year}:{path}.
// The host is the one the feed's own links carry; only server.public_url makes
// it permanent, which is what RFC 4151 asks of it.
// The year 2026 is the date the tag namespace was minted and must remain constant.
func feedID(r *http.Request) string {
	authority, port := tagAuthority(originHostOf(r))
	specific := absPath(r.Context(), r.URL.Path)

	// Two deployments on one host are two feeds. The authority cannot say so
	// and the specific part is the only other thing that can.
	if port != "" {
		specific = port + ":" + specific
	}

	return fmt.Sprintf(
		"tag:%s,2026:%s",
		authority,
		specific,
	)
}

// tagAuthority splits a request host into an RFC 4151 authorityName and the
// port no such name may carry. It admits a DNS name or an email address, so
// neither a port nor an IPv6 literal's colons can stay: either ends the
// tagging entity early and leaves the authority empty. §2.1 wants lowercase.
func tagAuthority(host string) (authority, port string) {
	if bare, p, err := net.SplitHostPort(host); err == nil {
		host, port = bare, p
	}

	host = strings.Trim(host, "[]")

	return strings.ToLower(strings.ReplaceAll(host, ":", "-")), port
}

// parseFeedPagination reads ?before= and ?limit= from the query string.
// Default limit is 50, max is 200.
func parseFeedPagination(r *http.Request) (beforeID uint64, limit int, err error) {
	limit = 50

	if v := r.URL.Query().Get("before"); v != "" {
		if beforeID, err = strconv.ParseUint(v, 10, 64); err != nil {
			return 0, 0, errInvalidBefore
		}
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit <= 0 {
			return 0, 0, errInvalidLimit
		}
	}

	return beforeID, min(limit, 200), nil
}

// feedPaginationParams names the query parameters every feed reads: the
// cursor and its page size (parseFeedPagination). A feed reading anything
// beyond these declares it in feedData.QueryParams.
var feedPaginationParams = []string{"before", "limit"}

// searchFeedParams names the parameter only the search feed reads.
var searchFeedParams = []string{"q"}

// listCSVParams names what a list endpoint reads. The CSV comes off the list
// its JSON handler prepared, so the alternate link has to carry them.
var listCSVParams = []string{"search", "filter", "sort", "dir", "limit", "offset"}

// historyWindowParams names what /history's feeds read beyond the pagination
// pair.
var historyWindowParams = []string{"since", "until"}

// historyCSVParams names what HandleHistory reads.
var historyCSVParams = slices.Concat(
	[]string{"type", "resourceId", "search", "before", "limit"}, historyWindowParams,
)

// echoingFeedRequest opts r out of compression when the feed's links will echo
// a parameter beyond the pagination pair back: reflected input beside
// ACL-filtered entries is the BREACH shape.
func echoingFeedRequest(r *http.Request, data feedData) *http.Request {
	if len(keptQuery(r, data.QueryParams)) > 0 {
		return disableCompression(r)
	}

	return r
}

// feedQuery returns the subset of r's query a feed's links may carry: the
// pagination pair every feed reads, plus whatever else the caller declares.
func feedQuery(r *http.Request, extra []string) url.Values {
	return keptQuery(r, feedPaginationParams, extra)
}

// keptQuery returns the values of the named parameters, and nothing else:
// reflecting a raw query into a compressed response is the BREACH shape.
func keptQuery(r *http.Request, names ...[]string) url.Values {
	source := r.URL.Query()
	kept := url.Values{}

	for _, set := range names {
		for _, name := range set {
			if values, ok := source[name]; ok {
				kept[name] = values
			}
		}
	}

	return kept
}

// feedHref joins a base URL and a feed query, omitting the "?" for an empty
// one.
func feedHref(base string, query url.Values) string {
	if encoded := query.Encode(); encoded != "" {
		return base + "?" + encoded
	}

	return base
}

// emptyFeedEpoch is a stable timestamp for empty feeds so the ETag is
// deterministic (time.Now would produce a different ETag every request).
var emptyFeedEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// historyUpdated returns the timestamp of the most recent history entry,
// or a fixed epoch if the slice is empty.
func historyUpdated(entries []cache.HistoryEntry) time.Time {
	switch {
	case len(entries) == 0:
		return emptyFeedEpoch
	case entries[0].Kind == cache.KindGap:
		return entries[0].Until
	default:
		return entries[0].Timestamp
	}
}
