package mcp

import (
	"context"
	"sync"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// NotificationManager tracks per-session resource subscriptions and matches
// cache events against them. It owns no goroutines; dispatch is driven by the
// cache's OnChange listener. Sessions are keyed by ClientSession value, since
// 2026-07-28 has no session IDs and every client would collapse onto "".
type NotificationManager struct {
	mu sync.RWMutex
	// sessions holds per-session subscription state: the subscribed URIs, the
	// *auth.Identity captured at subscribe time so dispatch can ACL-filter, and
	// the notification filter the subscriptions/listen stream established.
	sessions map[mcpserver.ClientSession]*sessionState

	// resolve turns a subscribed URI into what it matches. Nil matches every
	// URI literally, by the ID it names; the server installs one that resolves
	// names and follows a resource's children as its REST stream does.
	resolve func(uri string, identity *auth.Identity) subscription
}

type sessionState struct {
	identity *auth.Identity
	uris     map[string]subscription

	// filter is the opt-in a subscriptions/listen stream established, and is
	// nil for legacy sessions. 2026-07-28 makes every notification type
	// opt-in, so a modern client only receives what its filter names; legacy
	// clients have no such filter and keep the old always-on semantics.
	filter *mcplib.SubscriptionFilter
}

// NewNotificationManager returns an empty manager. Subscriptions are populated
// via mcp-go's OnAfterSubscribe hook (wired in StartNotifications).
func NewNotificationManager() *NotificationManager {
	return &NotificationManager{
		sessions: make(map[mcpserver.ClientSession]*sessionState),
	}
}

// Subscribe records that session is interested in updates to uri. The identity
// is captured for later ACL re-checks at dispatch time. Idempotent; a later
// call updates the stored identity (matching token refresh behaviour).
func (nm *NotificationManager) Subscribe(
	session mcpserver.ClientSession,
	uri string,
	identity *auth.Identity,
) {
	if session == nil || uri == "" {
		return
	}

	// Resolved before taking the lock, since resolving reads the cache.
	resolve := nm.resolve
	if resolve == nil {
		resolve = literalSubscription
	}
	sub := resolve(uri, identity)

	nm.mu.Lock()
	defer nm.mu.Unlock()

	st := nm.sessions[session]
	if st == nil {
		st = &sessionState{uris: make(map[string]subscription)}
		nm.sessions[session] = st
	}

	st.identity = identity
	st.uris[uri] = sub
}

// SetFilter records the notification opt-in a subscriptions/listen stream
// established, so dispatch can honour it. The identity is recorded here as
// well as in Subscribe: the filter's four fields are independent, so a stream
// opting into resourcesListChanged alone never reaches Subscribe.
func (nm *NotificationManager) SetFilter(
	session mcpserver.ClientSession,
	identity *auth.Identity,
	filter mcplib.SubscriptionFilter,
) {
	if session == nil {
		return
	}

	nm.mu.Lock()
	defer nm.mu.Unlock()

	st := nm.sessions[session]
	if st == nil {
		st = &sessionState{uris: make(map[string]subscription)}
		nm.sessions[session] = st
	}

	st.identity = identity
	st.filter = &filter
}

// Unsubscribe removes session's interest in uri.
func (nm *NotificationManager) Unsubscribe(session mcpserver.ClientSession, uri string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	st, ok := nm.sessions[session]
	if !ok {
		return
	}

	delete(st.uris, uri)
	if len(st.uris) == 0 {
		delete(nm.sessions, session)
	}
}

// RemoveSession drops every subscription for a disconnected session.
func (nm *NotificationManager) RemoveSession(session mcpserver.ClientSession) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	delete(nm.sessions, session)
}

// IdentityFor returns the identity associated with session, or nil when the
// session has no live subscription (or was never recorded).
func (nm *NotificationManager) IdentityFor(session mcpserver.ClientSession) *auth.Identity {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	st, ok := nm.sessions[session]
	if !ok {
		return nil
	}

	return st.identity
}

// MatchingURIs returns the subscribed URIs (for the given session) that match
// the cache event.
func (nm *NotificationManager) MatchingURIs(
	session mcpserver.ClientSession,
	event cache.Event,
) []string {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	st, ok := nm.sessions[session]
	if !ok {
		return nil
	}

	var matches []string
	for uri, sub := range st.uris {
		if sub.matches(event) {
			matches = append(matches, uri)
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

// listChangedTargets returns the sessions that should receive a
// resources/list_changed notification, paired with the identity to ACL-check
// against. Only those whose filter opted in: 2026-07-28 makes notification
// types opt-in, and a server MUST NOT deliver one that was not requested.
func (nm *NotificationManager) listChangedTargets() []delivery {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var out []delivery
	for session, st := range nm.sessions {
		if st.filter == nil || !st.filter.ResourcesListChanged {
			continue
		}

		out = append(out, delivery{session: session, identity: st.identity})
	}

	return out
}

// matchingDeliveries walks every session's subscriptions under a single RLock
// and returns the tuples needing notification for this event. The identity
// rides along so the dispatcher can ACL-check without re-acquiring the lock,
// which is released before dispatch so a slow send cannot block subscribe.
func (nm *NotificationManager) matchingDeliveries(event cache.Event) []delivery {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var out []delivery
	for session, st := range nm.sessions {
		for uri, sub := range st.uris {
			if sub.matches(event) {
				out = append(out, delivery{
					session:  session,
					uri:      uri,
					target:   sub.target,
					identity: st.identity,
				})
			}
		}
	}

	return out
}

type delivery struct {
	session  mcpserver.ClientSession
	uri      string
	target   subscriptionTarget
	identity *auth.Identity
}

// isResourceEvent reports whether an event type names a kind of resource, as
// every type but sync does. Each such type is its ACL type as written.
func isResourceEvent(t cache.EventType) bool {
	switch t {
	case cache.EventNode, cache.EventService, cache.EventTask, cache.EventConfig,
		cache.EventSecret, cache.EventNetwork, cache.EventVolume, cache.EventStack:
		return true
	default:
		return false
	}
}

func eventTypeToURIPrefix(t cache.EventType) string {
	if !isResourceEvent(t) {
		return ""
	}

	return "cetacean://" + string(t) + "s/"
}

// startNotifications attaches the manager to the cache's event stream and
// returns a cancel function for shutdown. The listener fires synchronously on
// every mutation, so per-event work stays a map lookup and a non-blocking
// send.
func (s *Server) startNotifications() func() {
	if s.cache == nil || s.notifications == nil {
		return func() {}
	}
	return s.cache.AddOnChangeListener(func(event cache.Event) {
		s.dispatchCacheEvent(event)
	})
}

// dispatchCacheEvent fans an event out to subscribed sessions. Detail
// subscribers get resources/updated, on a resync too; create and remove additionally broadcast
// resources/list_changed, which carries no URI. Deliveries are ACL-checked, so
// a policy tightened after a subscription still applies.
func (s *Server) dispatchCacheEvent(event cache.Event) {
	hasResource := eventNamesResource(event)
	for _, d := range s.notifications.matchingDeliveries(event) {
		canRead := func(resource string) bool { return s.canRead(d.identity, resource) }

		// A resync names no resource, so the subscribed one is checked instead:
		// any of them may have changed, and the client must re-read.
		if event.Type == cache.EventSync {
			if !s.targetReadable(d.identity, d.target) {
				continue
			}
		} else if hasResource && !cluster.CanReadEvent(event, canRead) {
			continue
		}

		s.notify(d.session, "notifications/resources/updated", map[string]any{
			"uri": d.uri,
		})
	}

	if s.notifications.IsListChange(event) {
		// list_changed is a coarse "go refetch" signal, delivered only to the
		// streams that opted in. When ACL is in play, skip sessions whose
		// identity cannot read any resource of the affected type — otherwise
		// the notification leaks activity timing across tenancy boundaries.
		aclType := ""
		if isResourceEvent(event.Type) {
			aclType = string(event.Type)
		}

		for _, d := range s.notifications.listChangedTargets() {
			if aclType != "" && s.acl != nil && !s.canReadAnyOfType(d.identity, aclType) {
				continue
			}

			s.notify(d.session, "notifications/resources/list_changed", nil)
		}
	}
}

// notify delivers a notification to one session. 2026-07-28 sessions are
// ephemeral and unregistered, so the session is written to directly rather
// than looked up by ID. A full channel is dropped, as mcp-go does; the client
// re-reads when it reconnects.
func (s *Server) notify(session mcpserver.ClientSession, method string, params map[string]any) {
	if session == nil {
		return
	}

	if upgradable, ok := session.(mcpserver.SessionWithStreamableHTTPConfig); ok {
		upgradable.UpgradeToSSEWhenReceiveNotification()
	}

	notification := mcplib.JSONRPCNotification{
		JSONRPC: mcplib.JSONRPC_VERSION,
		Method:  method,
		Params:  mcplib.NotificationParams{AdditionalFields: params},
	}

	select {
	case session.NotificationChannel() <- notification:
	default:
	}
}

// canReadAnyOfType returns true when the identity has at least one read grant
// on a resource of the type. Shares acl.TypeGrants with toolVisibilityFor, so
// the two cannot disagree. list_changed carries no resource data, but its
// timing still says one of that type changed.
func (s *Server) canReadAnyOfType(identity *auth.Identity, resourceType string) bool {
	if s.acl == nil {
		return true
	}

	return s.acl.TypeGrants(identity).Can("read", resourceType)
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

// eventNamesResource reports whether an event names the resource it is checked
// against: a task by ID, anything else by the Name the cache fills via
// ExtractName. Sync, or a ref to a resource already gone, names none.
func eventNamesResource(event cache.Event) bool {
	if !isResourceEvent(event.Type) {
		return false
	}

	if event.Type == cache.EventTask {
		return event.ID != ""
	}

	return event.Name != ""
}

// installSubscriptionHooks wires subscribe / unsubscribe / session lifecycle
// into the NotificationManager, on both the legacy resources/subscribe path
// and the 2026-07-28 subscriptions/listen path.
func (s *Server) installSubscriptionHooks() *mcpserver.Hooks {
	h := &mcpserver.Hooks{}

	s.installTaskTTLHook(h)
	s.installTaskOwnerHooks(h)
	installLegacyInitializeHooks(h)

	// mcp-go tracks subscriptions by type-asserting the session to
	// SessionWithResourceSubscriptions, which its streamable-HTTP session does
	// not implement — so tracking happens here, or a client subscribes
	// successfully and is never notified. The filter rides along: types opt in.
	h.AddBeforeSubscriptionsListen(
		func(ctx context.Context, _ any, msg *mcplib.SubscriptionsListenRequest) {
			session := mcpserver.ClientSessionFromContext(ctx)
			if session == nil {
				return
			}

			identity := auth.IdentityFromContext(ctx)

			s.notifications.SetFilter(session, identity, msg.Params.Notifications)

			for _, uri := range msg.Params.Notifications.ResourceSubscriptions {
				s.notifications.Subscribe(session, uri, identity)
			}
		},
	)

	// The listen handler blocks until the client disconnects, so the after-hook
	// is our stream-closed signal. The session is ephemeral — it exists only
	// for this request — so drop the whole record rather than the individual
	// URIs.
	h.AddAfterSubscriptionsListen(
		func(ctx context.Context, _ any, _ *mcplib.SubscriptionsListenRequest, _ *mcplib.SubscriptionsListenResult) {
			if session := mcpserver.ClientSessionFromContext(ctx); session != nil {
				s.notifications.RemoveSession(session)
			}
		},
	)

	return h
}
