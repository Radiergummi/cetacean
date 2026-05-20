package mcp

import (
	"context"
	"sync"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/cache"
)

// NotificationManager tracks per-session resource subscriptions and matches
// cache events against them. It owns no goroutines; dispatch is driven by the
// cache's OnChange listener registered in StartNotifications.
type NotificationManager struct {
	mu sync.RWMutex
	// subscriptions[sessionID][uri] is the set of resource URIs each session
	// has subscribed to via resources/subscribe.
	subscriptions map[string]map[string]struct{}
}

// NewNotificationManager returns an empty manager. Subscriptions are populated
// via mcp-go's OnAfterSubscribe hook (wired in StartNotifications).
func NewNotificationManager() *NotificationManager {
	return &NotificationManager{
		subscriptions: make(map[string]map[string]struct{}),
	}
}

// Subscribe records that sessionID is interested in updates to uri. Idempotent.
func (nm *NotificationManager) Subscribe(sessionID, uri string) {
	if sessionID == "" || uri == "" {
		return
	}
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if nm.subscriptions[sessionID] == nil {
		nm.subscriptions[sessionID] = make(map[string]struct{})
	}
	nm.subscriptions[sessionID][uri] = struct{}{}
}

// Unsubscribe removes sessionID's interest in uri.
func (nm *NotificationManager) Unsubscribe(sessionID, uri string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if subs, ok := nm.subscriptions[sessionID]; ok {
		delete(subs, uri)
		if len(subs) == 0 {
			delete(nm.subscriptions, sessionID)
		}
	}
}

// RemoveSession drops every subscription for a disconnected session.
func (nm *NotificationManager) RemoveSession(sessionID string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	delete(nm.subscriptions, sessionID)
}

// MatchingURIs returns the subscribed URIs (for the given session) that match
// the cache event. Detail-level URIs match exactly; for a service-update event
// the manager also reports `cetacean://services/<id>/logs` so log-tail
// subscribers learn about new lines without polling.
func (nm *NotificationManager) MatchingURIs(sessionID string, event cache.Event) []string {
	prefix := eventTypeToURIPrefix(event.Type)
	if prefix == "" || event.ID == "" {
		return nil
	}

	nm.mu.RLock()
	defer nm.mu.RUnlock()

	subs := nm.subscriptions[sessionID]
	if len(subs) == 0 {
		return nil
	}

	var matches []string
	detail := prefix + event.ID
	if _, ok := subs[detail]; ok {
		matches = append(matches, detail)
	}
	if event.Type == cache.EventService {
		logURI := detail + "/logs"
		if _, ok := subs[logURI]; ok {
			matches = append(matches, logURI)
		}
	}
	return matches
}

// IsListChange reports whether the event represents a resource being created
// or removed. List subscribers (via notifications/resources/list_changed) only
// care about these, not in-place updates.
func (nm *NotificationManager) IsListChange(event cache.Event) bool {
	return event.Action == "create" || event.Action == "remove"
}

// sessionIDs returns a snapshot of all sessions currently holding at least one
// subscription. Used by tests; the dispatch path uses matchingDeliveries to
// avoid the N+1 RLock pattern.
func (nm *NotificationManager) sessionIDs() []string {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	if len(nm.subscriptions) == 0 {
		return nil
	}
	out := make([]string, 0, len(nm.subscriptions))
	for id := range nm.subscriptions {
		out = append(out, id)
	}
	return out
}

// matchingDeliveries walks every session's subscriptions under a single RLock
// and returns the (session, uri) pairs that need notification for this event.
// The returned slice may be nil when no session is interested. The lock is
// released before the caller dispatches notifications so a slow send cannot
// block subscribe/unsubscribe.
func (nm *NotificationManager) matchingDeliveries(event cache.Event) []delivery {
	prefix := eventTypeToURIPrefix(event.Type)
	if prefix == "" || event.ID == "" {
		return nil
	}
	detail := prefix + event.ID
	logURI := ""
	if event.Type == cache.EventService {
		logURI = detail + "/logs"
	}

	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var out []delivery
	for sessionID, subs := range nm.subscriptions {
		if _, ok := subs[detail]; ok {
			out = append(out, delivery{sessionID: sessionID, uri: detail})
		}
		if logURI != "" {
			if _, ok := subs[logURI]; ok {
				out = append(out, delivery{sessionID: sessionID, uri: logURI})
			}
		}
	}
	return out
}

type delivery struct {
	sessionID string
	uri       string
}

func eventTypeToURIPrefix(t cache.EventType) string {
	switch t {
	case cache.EventNode:
		return "cetacean://nodes/"
	case cache.EventService:
		return "cetacean://services/"
	case cache.EventTask:
		return "cetacean://tasks/"
	case cache.EventConfig:
		return "cetacean://configs/"
	case cache.EventSecret:
		return "cetacean://secrets/"
	case cache.EventNetwork:
		return "cetacean://networks/"
	case cache.EventVolume:
		return "cetacean://volumes/"
	case cache.EventStack:
		return "cetacean://stacks/"
	default:
		return ""
	}
}

// startNotifications attaches the manager to the cache's event stream and
// returns a cancel function the caller invokes at shutdown. Called from New
// after the mcp-go server is constructed.
//
// The cache listener fires synchronously on every cache mutation, so per-event
// work must stay cheap: a lock-protected map lookup and a non-blocking write
// onto each session's mcp-go notification channel.
func (s *Server) startNotifications() func() {
	if s.cache == nil || s.notifications == nil {
		return func() {}
	}
	return s.cache.AddOnChangeListener(func(event cache.Event) {
		s.dispatchCacheEvent(event)
	})
}

// dispatchCacheEvent fans an event out to subscribed sessions. Detail
// subscribers get notifications/resources/updated; list-relevant changes
// (create/remove) additionally broadcast notifications/resources/list_changed.
func (s *Server) dispatchCacheEvent(event cache.Event) {
	if event.Type == cache.EventSync {
		// The full-resync event isn't useful as a single notification.
		// Clients re-read on reconnect anyway.
		return
	}

	for _, d := range s.notifications.matchingDeliveries(event) {
		_ = s.mcpServer.SendNotificationToSpecificClient(d.sessionID, "notifications/resources/updated", map[string]any{
			"uri": d.uri,
		})
	}

	if s.notifications.IsListChange(event) {
		// list_changed is a coarse signal — broadcast to all initialized
		// clients and let each refilter on the next list call.
		s.mcpServer.SendNotificationToAllClients("notifications/resources/list_changed", nil)
	}
}

// installSubscriptionHooks returns the mcp-go Hooks that wire client subscribe
// / unsubscribe / session lifecycle into the NotificationManager. The hook
// callbacks read the session ID from the request context — mcp-go stamps a
// ClientSession on the ctx before invoking handlers.
func (s *Server) installSubscriptionHooks() *mcpserver.Hooks {
	h := &mcpserver.Hooks{}

	h.AddAfterSubscribe(func(ctx context.Context, _ any, msg *mcplib.SubscribeRequest, _ *mcplib.EmptyResult) {
		if sid := sessionIDFromContext(ctx); sid != "" {
			s.notifications.Subscribe(sid, msg.Params.URI)
		}
	})

	h.AddAfterUnsubscribe(func(ctx context.Context, _ any, msg *mcplib.UnsubscribeRequest, _ *mcplib.EmptyResult) {
		if sid := sessionIDFromContext(ctx); sid != "" {
			s.notifications.Unsubscribe(sid, msg.Params.URI)
		}
	})

	h.AddOnUnregisterSession(func(_ context.Context, session mcpserver.ClientSession) {
		s.notifications.RemoveSession(session.SessionID())
	})

	return h
}

func sessionIDFromContext(ctx context.Context) string {
	session := mcpserver.ClientSessionFromContext(ctx)
	if session == nil {
		return ""
	}
	return session.SessionID()
}
