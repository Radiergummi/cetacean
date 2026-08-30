package mcp

import (
	"context"
	"strings"
	"sync"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// NotificationManager tracks per-session resource subscriptions and matches
// cache events against them. It owns no goroutines; dispatch is driven by the
// cache's OnChange listener registered in StartNotifications.
type NotificationManager struct {
	mu sync.RWMutex
	// sessions holds per-session subscription state. Each session record stores
	// both the subscribed URIs and the *auth.Identity captured at subscribe
	// time — used by dispatch to ACL-filter notifications per-session.
	sessions map[string]*sessionState
}

type sessionState struct {
	identity *auth.Identity
	uris     map[string]struct{}
}

// NewNotificationManager returns an empty manager. Subscriptions are populated
// via mcp-go's OnAfterSubscribe hook (wired in StartNotifications).
func NewNotificationManager() *NotificationManager {
	return &NotificationManager{
		sessions: make(map[string]*sessionState),
	}
}

// Subscribe records that sessionID is interested in updates to uri. The
// identity is captured for later ACL re-checks at dispatch time. Idempotent;
// a later call updates the stored identity (matching token refresh behaviour).
func (nm *NotificationManager) Subscribe(sessionID, uri string, identity *auth.Identity) {
	if sessionID == "" || uri == "" {
		return
	}
	nm.mu.Lock()
	defer nm.mu.Unlock()
	st := nm.sessions[sessionID]
	if st == nil {
		st = &sessionState{uris: make(map[string]struct{})}
		nm.sessions[sessionID] = st
	}
	st.identity = identity
	st.uris[uri] = struct{}{}
}

// Unsubscribe removes sessionID's interest in uri.
func (nm *NotificationManager) Unsubscribe(sessionID, uri string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	st, ok := nm.sessions[sessionID]
	if !ok {
		return
	}
	delete(st.uris, uri)
	if len(st.uris) == 0 {
		delete(nm.sessions, sessionID)
	}
}

// RemoveSession drops every subscription for a disconnected session.
func (nm *NotificationManager) RemoveSession(sessionID string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	delete(nm.sessions, sessionID)
}

// SubscribedURIs returns the URIs sessionID is currently subscribed to. Order
// is unspecified. Used by tests to assert subscription bookkeeping.
func (nm *NotificationManager) SubscribedURIs(sessionID string) []string {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	st := nm.sessions[sessionID]
	if st == nil {
		return nil
	}

	uris := make([]string, 0, len(st.uris))
	for uri := range st.uris {
		uris = append(uris, uri)
	}

	return uris
}

// IdentityFor returns the identity associated with sessionID, or nil when the
// session has no live subscription (or was never recorded). Exposed for tests
// and for the list_changed dispatch path, which iterates known sessions.
func (nm *NotificationManager) IdentityFor(sessionID string) *auth.Identity {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	st, ok := nm.sessions[sessionID]
	if !ok {
		return nil
	}
	return st.identity
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

	st, ok := nm.sessions[sessionID]
	if !ok || len(st.uris) == 0 {
		return nil
	}

	var matches []string
	detail := prefix + event.ID
	if _, ok := st.uris[detail]; ok {
		matches = append(matches, detail)
	}
	if event.Type == cache.EventService {
		logURI := detail + "/logs"
		if _, ok := st.uris[logURI]; ok {
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
	if len(nm.sessions) == 0 {
		return nil
	}
	out := make([]string, 0, len(nm.sessions))
	for id := range nm.sessions {
		out = append(out, id)
	}
	return out
}

// matchingDeliveries walks every session's subscriptions under a single RLock
// and returns the (session, uri, identity) tuples that need notification for
// this event. The identity is included so the dispatcher can ACL-check without
// re-acquiring the lock. The lock is released before the caller dispatches so
// a slow send cannot block subscribe/unsubscribe.
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
	for sessionID, st := range nm.sessions {
		if _, ok := st.uris[detail]; ok {
			out = append(out, delivery{sessionID: sessionID, uri: detail, identity: st.identity})
		}
		if logURI != "" {
			if _, ok := st.uris[logURI]; ok {
				out = append(
					out,
					delivery{sessionID: sessionID, uri: logURI, identity: st.identity},
				)
			}
		}
	}
	return out
}

type delivery struct {
	sessionID string
	uri       string
	identity  *auth.Identity
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
//
// Notifications are ACL-checked per-delivery: a session whose stored identity
// can't read the affected resource never receives the notification, even if it
// subscribed before a policy tightening. The list_changed broadcast carries no
// resource URI — it's a "go refetch" signal whose payload becomes empty after
// the next per-resource list call's ACL filter, so it stays a broadcast.
func (s *Server) dispatchCacheEvent(event cache.Event) {
	if event.Type == cache.EventSync {
		// The full-resync event isn't useful as a single notification.
		// Clients re-read on reconnect anyway.
		return
	}

	aclResource := eventACLResource(event)
	for _, d := range s.notifications.matchingDeliveries(event) {
		if aclResource != "" && !s.canRead(d.identity, aclResource) {
			continue
		}
		_ = s.mcpServer.SendNotificationToSpecificClient(
			d.sessionID,
			"notifications/resources/updated",
			map[string]any{
				"uri": d.uri,
			},
		)
	}

	if s.notifications.IsListChange(event) {
		// list_changed is a coarse "go refetch" signal. When ACL is in play,
		// don't broadcast it to sessions whose identity can't read any
		// resource of the affected type — otherwise the notification leaks
		// activity timing across tenancy boundaries.
		aclType := eventTypeToACLPrefix(event.Type)
		aclType = strings.TrimSuffix(aclType, ":")
		if aclType == "" || s.acl == nil {
			s.mcpServer.SendNotificationToAllClients("notifications/resources/list_changed", nil)
			return
		}
		for _, sid := range s.notifications.sessionIDs() {
			id := s.notifications.IdentityFor(sid)
			if !s.canReadAnyOfType(id, aclType) {
				continue
			}
			_ = s.mcpServer.SendNotificationToSpecificClient(
				sid,
				"notifications/resources/list_changed",
				nil,
			)
		}
	}
}

// canReadAnyOfType returns true when the identity has at least one read grant
// on a resource of the given type. Mirrors the logic in filterToolsForIdentity
// — a write grant implies read.
func (s *Server) canReadAnyOfType(identity *auth.Identity, resourceType string) bool {
	if s.acl == nil {
		return true
	}
	perms := s.acl.PermissionsFor(identity)
	if perms == nil {
		return true
	}
	for pattern, granted := range perms {
		resType, _, ok := splitResourceType(pattern)
		if !ok {
			continue
		}
		if resType != "*" && resType != resourceType {
			continue
		}
		for _, p := range granted {
			if p == "read" || p == "write" {
				return true
			}
		}
	}
	return false
}

// canRead returns true when identity is allowed to read aclResource (or when
// ACL is disabled). Lookup-only counterpart of checkRead for the dispatch hot
// path where we don't want an error return.
func (s *Server) canRead(identity *auth.Identity, aclResource string) bool {
	if s.acl == nil {
		return true
	}
	return s.acl.Can(identity, "read", aclResource)
}

// eventACLResource returns the ACL resource key for a cache event, or "" when
// the event carries no resource (sync, ref_changed, etc). Mirrors the ACL key
// convention used by lookupResource and tools.go.
func eventACLResource(event cache.Event) string {
	prefix := eventTypeToACLPrefix(event.Type)
	if prefix == "" {
		return ""
	}
	// Tasks key on ID; everything else keys on Name (which the cache fills via
	// ExtractName, matching the convention used by REST handlers).
	if event.Type == cache.EventTask {
		if event.ID == "" {
			return ""
		}
		return prefix + event.ID
	}
	if event.Name == "" {
		return ""
	}
	return prefix + event.Name
}

func eventTypeToACLPrefix(t cache.EventType) string {
	switch t {
	case cache.EventNode:
		return "node:"
	case cache.EventService:
		return "service:"
	case cache.EventTask:
		return "task:"
	case cache.EventConfig:
		return "config:"
	case cache.EventSecret:
		return "secret:"
	case cache.EventNetwork:
		return "network:"
	case cache.EventVolume:
		return "volume:"
	case cache.EventStack:
		return "stack:"
	default:
		return ""
	}
}

// installSubscriptionHooks returns the mcp-go Hooks that wire client subscribe
// / unsubscribe / session lifecycle into the NotificationManager, on both the
// legacy resources/subscribe path and the 2026-07-28 subscriptions/listen path.
// The hook callbacks read the session ID from the request context — mcp-go
// stamps a ClientSession on the ctx before invoking handlers, for modern and
// legacy clients alike.
func (s *Server) installSubscriptionHooks() *mcpserver.Hooks {
	h := &mcpserver.Hooks{}

	h.AddAfterSubscribe(
		func(ctx context.Context, _ any, msg *mcplib.SubscribeRequest, _ *mcplib.EmptyResult) {
			if sid := sessionIDFromContext(ctx); sid != "" {
				s.notifications.Subscribe(sid, msg.Params.URI, auth.IdentityFromContext(ctx))
			}
		},
	)

	h.AddAfterUnsubscribe(
		func(ctx context.Context, _ any, msg *mcplib.UnsubscribeRequest, _ *mcplib.EmptyResult) {
			if sid := sessionIDFromContext(ctx); sid != "" {
				s.notifications.Unsubscribe(sid, msg.Params.URI)
			}
		},
	)

	// 2026-07-28 replaced resources/subscribe with subscriptions/listen, which
	// does not fire the subscribe hooks above: mcp-go records those URIs by
	// type-asserting the session to SessionWithResourceSubscriptions, and its
	// streamable-HTTP session does not implement that interface. Track them
	// here instead, or modern clients would subscribe successfully and then
	// never receive a single notification.
	//
	// The requested URIs are what mcp-go establishes verbatim: its
	// allowedSubscriptions only drops them when resource subscription
	// capability is off, and we advertise it unconditionally.
	h.AddBeforeSubscriptionsListen(
		func(ctx context.Context, _ any, msg *mcplib.SubscriptionsListenRequest) {
			sid := sessionIDFromContext(ctx)
			if sid == "" {
				return
			}

			identity := auth.IdentityFromContext(ctx)
			for _, uri := range msg.Params.Notifications.ResourceSubscriptions {
				s.notifications.Subscribe(sid, uri, identity)
			}
		},
	)

	// The listen handler blocks until the client disconnects, so the after-hook
	// is our stream-closed signal. Drop exactly what this request established;
	// the session may still hold subscriptions from another listen stream.
	h.AddAfterSubscriptionsListen(
		func(ctx context.Context, _ any, msg *mcplib.SubscriptionsListenRequest, _ *mcplib.SubscriptionsListenResult) {
			sid := sessionIDFromContext(ctx)
			if sid == "" {
				return
			}

			for _, uri := range msg.Params.Notifications.ResourceSubscriptions {
				s.notifications.Unsubscribe(sid, uri)
			}
		},
	)

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
