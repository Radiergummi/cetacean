package sse

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/metrics"
)

// ReplaySource provides access to recent history entries for SSE replay.
type ReplaySource interface {
	Since(afterID uint64) ([]cache.HistoryEntry, bool)
	Count() uint64
}

type sseClient struct {
	events chan cache.Event
	match  func(cache.Event) bool // nil means accept all
	done   chan struct{}
	owner  string

	// lagged marks a client an event it matched never reached. Its next frame
	// is a sync rather than an id past the gap, which a reconnect would skip.
	lagged atomic.Bool
}

// accepts reports whether the client's filter admits e.
func (c *sseClient) accepts(e cache.Event) bool {
	return c.match == nil || c.match(e)
}

const (
	MaxClients = 256

	// MaxClientsPerOwner is one caller's share of MaxClients, so no single
	// identity, or address under auth none, can hold every slot.
	MaxClientsPerOwner = MaxClients / 4
)

// writeTimeout bounds one write to a stream. The server sets no write timeout
// for streams to share, so without it a client that stops reading holds its
// slot, and shutdown, for as long as the connection lives.
var writeTimeout = 30 * time.Second

// ErrorWriter is a callback for writing HTTP error responses.
// This decouples the SSE package from the API error registry.
type ErrorWriter func(w http.ResponseWriter, r *http.Request, code, detail string)

type Broadcaster struct {
	mu                sync.RWMutex
	clients           map[*sseClient]struct{}
	owners            map[string]int
	closed            bool
	inbox             chan cache.Event
	stop              chan struct{}
	batchInterval     time.Duration
	keepaliveInterval time.Duration
	writeError        ErrorWriter
	replay            ReplaySource

	// basePath prefixes the @id every event carries. It is configuration, not
	// per-request state, and the stream has no request context to read once it
	// is running — an identifier without it addresses nothing on a deployment
	// served under a prefix.
	basePath string

	// epoch prefixes every event id, so a cursor from another process, whose
	// history counter restarted, reads as foreign rather than as a position.
	epoch string
}

func NewBroadcaster(
	batchInterval time.Duration,
	writeError ErrorWriter,
	replay ReplaySource,
) *Broadcaster {
	if batchInterval <= 0 {
		batchInterval = 100 * time.Millisecond
	}
	b := &Broadcaster{
		clients:           make(map[*sseClient]struct{}),
		owners:            make(map[string]int),
		inbox:             make(chan cache.Event, 256),
		stop:              make(chan struct{}),
		batchInterval:     batchInterval,
		keepaliveInterval: 15 * time.Second,
		writeError:        writeError,
		replay:            replay,
		epoch:             newEpoch(),
	}
	go b.fanOut()
	return b
}

// Broadcast enqueues an event for delivery to SSE clients.
// Non-blocking: drops the event if the internal buffer is full.
func (b *Broadcaster) Broadcast(e cache.Event) {
	select {
	case b.inbox <- e:
		metrics.RecordSSEBroadcast()
	default:
		metrics.RecordSSEDrop()
		slog.Warn("SSE broadcast buffer full, dropping event", "type", e.Type, "id", e.ID)

		b.mu.RLock()
		for c := range b.clients {
			if c.accepts(e) {
				c.lagged.Store(true)
			}
		}
		b.mu.RUnlock()
	}
}

// fanOut is the dedicated goroutine that delivers events to SSE clients.
func (b *Broadcaster) fanOut() {
	for {
		select {
		case e := <-b.inbox:
			b.mu.RLock()
			for c := range b.clients {
				if !c.accepts(e) {
					continue
				}
				select {
				case c.events <- e:
				default:
					// Slow client: drop the event rather than block the
					// broadcaster goroutine, and resync the client instead.
					metrics.RecordSSEClientDrop()
					c.lagged.Store(true)
				}
			}
			b.mu.RUnlock()
		case <-b.stop:
			return
		}
	}
}

func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.stop)
	for c := range b.clients {
		close(c.done)
	}
}

// ReplayAll is the replay type for a stream that carries every event type,
// as /events does: a reconnect replays history of any type its match admits.
const ReplayAll cache.EventType = "*"

// TypesFilter matches the event types a ?types= list names, or returns nil
// when there is none. Sync events always pass: they tell the client to refetch.
func TypesFilter(r *http.Request) func(cache.Event) bool {
	t := r.URL.Query().Get("types")
	if t == "" {
		return nil
	}

	types := make(map[string]bool)
	for typ := range strings.SplitSeq(t, ",") {
		types[strings.TrimSpace(typ)] = true
	}

	return func(e cache.Event) bool { return e.Type == cache.EventSync || types[string(e.Type)] }
}

func (b *Broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.ServeSSE(w, r, TypesFilter(r), ReplayAll)
}

func (b *Broadcaster) ServeSSE(
	w http.ResponseWriter,
	r *http.Request,
	match func(cache.Event) bool,
	replayType cache.EventType,
) {
	if _, ok := w.(http.Flusher); !ok {
		b.writeError(w, r, "API005", "streaming not supported")
		return
	}

	client := &sseClient{
		events: make(chan cache.Event, 64),
		match:  match,
		done:   make(chan struct{}),
		owner:  ownerOf(r),
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	if len(b.clients) >= MaxClients {
		b.mu.Unlock()
		w.Header().Set("Retry-After", RetryAfter())
		b.writeError(w, r, "SSE001", "too many SSE connections")
		return
	}
	if b.owners[client.owner] >= MaxClientsPerOwner {
		b.mu.Unlock()
		w.Header().Set("Retry-After", RetryAfter())
		b.writeError(w, r, "SSE001", "too many SSE connections from this client")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	b.clients[client] = struct{}{}
	b.owners[client.owner]++
	b.mu.Unlock()
	metrics.RecordSSEConnect()

	stream := newStreamWriter(w)
	defer func() { _ = stream.clearDeadline() }()

	defer func() {
		b.mu.Lock()
		delete(b.clients, client)
		if b.owners[client.owner]--; b.owners[client.owner] <= 0 {
			delete(b.owners, client.owner)
		}
		// Close the done channel if it wasn't already closed by Broadcaster.Close(),
		// so any goroutine selecting on it can unblock.
		select {
		case <-client.done:
		default:
			close(client.done)
		}
		b.mu.Unlock()
		metrics.RecordSSEDisconnect()
	}()

	if stream.send(func() error { return nil }) != nil {
		return
	}

	var skipBelow uint64
	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" && b.replay != nil {
		var err error
		if skipBelow, err = b.replayEvents(stream, lastID, replayType, client.match); err != nil {
			return
		}
	}

	// A lagged client's pending batch has a gap, so it is not written on the
	// way out: the client reconnects from its last whole cursor instead.
	finish := func(batch []cache.Event) {
		if len(batch) > 0 && !client.lagged.Load() {
			_ = b.writeFrame(stream, batch)
		}
	}

	batchTicker := time.NewTicker(b.batchInterval)
	defer batchTicker.Stop()
	keepalive := time.NewTicker(b.keepaliveInterval)
	defer keepalive.Stop()
	var batch []cache.Event

	for {
		select {
		case e, ok := <-client.events:
			if !ok {
				finish(batch)
				return
			}
			// Skip events already sent during replay. Only clear the dedup
			// window once we see a definitive event beyond it (HistoryID > 0).
			// A sync is never in a replay, and its HistoryID is the last entry
			// that preceded it, so the window would swallow a live one.
			if skipBelow > 0 && e.HistoryID > 0 && e.Type != cache.EventSync {
				if e.HistoryID <= skipBelow {
					continue
				}
				skipBelow = 0
			}
			batch = append(batch, e)
		case <-batchTicker.C:
			// The sync covers the events queued behind it, which are skipped.
			if client.lagged.Swap(false) {
				batch = batch[:0]
				skipBelow = b.replayPosition()
				if b.writeFrame(stream, []cache.Event{syncEvent(skipBelow)}) != nil {
					return
				}
				keepalive.Reset(b.keepaliveInterval)

				continue
			}
			if len(batch) > 0 {
				if b.writeFrame(stream, batch) != nil {
					return
				}
				batch = batch[:0]
				keepalive.Reset(b.keepaliveInterval)
			}
		case <-keepalive.C:
			err := stream.send(func() error {
				_, err := fmt.Fprint(stream, ": keepalive\n\n")
				return err
			})
			if err != nil {
				return
			}
		case <-r.Context().Done():
			finish(batch)
			return
		case <-client.done:
			finish(batch)
			return
		}
	}
}

func (b *Broadcaster) replayEvents(
	stream *streamWriter,
	lastID string,
	replayType cache.EventType,
	match func(cache.Event) bool,
) (uint64, error) {
	writeSync := func() (uint64, error) {
		count := b.replay.Count()
		return count, b.writeFrame(stream, []cache.Event{syncEvent(count)})
	}

	// Detail/stack streams are ineligible for replay — send sync.
	if replayType == "" {
		return writeSync()
	}

	afterID, ok := b.cursor(lastID)
	if !ok {
		return writeSync()
	}

	entries, ok := b.replay.Since(afterID)
	if !ok {
		return writeSync()
	}

	if len(entries) == 0 {
		return afterID, nil
	}

	// Filter entries by type and ACL, then convert to cache.Event (no Resource payload).
	var replay []cache.Event
	for _, e := range entries {
		if replayType != ReplayAll && e.Type != replayType {
			continue
		}

		ev := cache.Event{
			Type:      e.Type,
			Action:    e.Action,
			ID:        e.ResourceID,
			Name:      e.Name,
			HistoryID: e.ID,
			Service:   e.Service,
			Stack:     e.Stack,
		}

		if match != nil && !match(ev) {
			continue
		}

		replay = append(replay, ev)
	}

	if len(replay) == 0 {
		return afterID, nil
	}

	return replay[len(replay)-1].HistoryID, b.writeFrame(stream, replay)
}

// writeFrame writes events as one frame under the stream's write deadline.
func (b *Broadcaster) writeFrame(stream *streamWriter, events []cache.Event) error {
	return stream.send(func() error {
		return writeBatch(stream, stream, events, b.basePath, b.epoch)
	})
}

// syncEvent tells a client to refetch, at history position id.
func syncEvent(id uint64) cache.Event {
	return cache.Event{Type: cache.EventSync, Action: "full_sync", HistoryID: id}
}

// replayPosition is the newest history id, or zero without a history.
func (b *Broadcaster) replayPosition() uint64 {
	if b.replay == nil {
		return 0
	}

	return b.replay.Count()
}

// streamWriter writes one stream under a per-write deadline and keeps the
// first flush error, which http.Flusher would swallow.
type streamWriter struct {
	io.Writer
	controller *http.ResponseController
	err        error
}

func newStreamWriter(w http.ResponseWriter) *streamWriter {
	return &streamWriter{Writer: w, controller: http.NewResponseController(w)}
}

func (s *streamWriter) Flush() {
	if err := s.controller.Flush(); err != nil && s.err == nil {
		s.err = err
	}
}

// send runs write under a fresh deadline and flushes. A writer that cannot
// take a deadline is written to without one rather than refused. The deadline
// is lifted afterwards: HTTP/2 resets a stream whose deadline passes while idle.
func (s *streamWriter) send(write func() error) error {
	err := s.controller.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}

	if err := write(); err != nil {
		return err
	}
	s.Flush()
	if s.err != nil {
		return s.err
	}

	return s.clearDeadline()
}

// clearDeadline lifts the deadline, which would otherwise outlive the write.
func (s *streamWriter) clearDeadline() error {
	err := s.controller.SetWriteDeadline(time.Time{})
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}

	return err
}

// EventID is the id the stream writes for a history position.
func (b *Broadcaster) EventID(historyID uint64) string {
	return b.epoch + "-" + strconv.FormatUint(historyID, 10)
}

// cursor reads the history id out of a Last-Event-ID this process wrote.
func (b *Broadcaster) cursor(lastID string) (uint64, bool) {
	n, ok := strings.CutPrefix(lastID, b.epoch+"-")
	if !ok {
		return 0, false
	}

	id, err := strconv.ParseUint(n, 10, 64)

	return id, err == nil
}

func newEpoch() string {
	var nonce [4]byte
	_, _ = rand.Read(nonce[:])

	return hex.EncodeToString(nonce[:])
}

// ownerOf names whose share of the connection cap a request draws on: the
// authenticated identity, or the client address when every caller is anonymous.
func ownerOf(r *http.Request) string {
	if id := auth.IdentityFromContext(r.Context()); id != nil && id.Provider != "none" {
		return id.Provider + ":" + id.Subject
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	return "addr:" + host
}

// TypeMatcher returns a match function that accepts events of the given type.
// Sync events always pass through so clients can trigger a full refetch.
func TypeMatcher(typ cache.EventType) func(cache.Event) bool {
	return func(e cache.Event) bool {
		return e.Type == typ || e.Type == cache.EventSync
	}
}

// ResourceMatcher returns a match function for per-resource SSE streams.
// Sync events always pass through so clients can trigger a full refetch.
func ResourceMatcher(typ cache.EventType, id string) func(cache.Event) bool {
	switch typ {
	case cache.EventNode:
		return func(e cache.Event) bool {
			if e.Type == cache.EventSync {
				return true
			}
			if e.Type == cache.EventNode && e.ID == id {
				return true
			}
			if e.Type == cache.EventTask {
				if t, ok := e.Resource.(swarm.Task); ok {
					return t.NodeID == id
				}
			}
			return false
		}
	case cache.EventService:
		return func(e cache.Event) bool {
			if e.Type == cache.EventSync {
				return true
			}
			if e.Type == cache.EventService && e.ID == id {
				return true
			}
			if e.Type == cache.EventTask {
				if t, ok := e.Resource.(swarm.Task); ok {
					return t.ServiceID == id
				}
			}
			return false
		}
	case cache.EventTask:
		return func(e cache.Event) bool {
			return e.Type == cache.EventSync || (e.Type == cache.EventTask && e.ID == id)
		}
	default:
		// config, secret, network, volume — match by type+ID.
		// Cross-reference "ref_changed" events already have the correct type+ID.
		return func(e cache.Event) bool {
			return e.Type == cache.EventSync || (e.Type == typ && e.ID == id)
		}
	}
}

// NetworkMatcher returns a match function for a network's SSE stream: its own
// events, and those cluster.AddressNetworks says may move its address figure.
// Sync events always pass through so clients can trigger a full refetch.
func NetworkMatcher(c *cache.Cache, id string) func(cache.Event) bool {
	own := ResourceMatcher(cache.EventNetwork, id)

	return func(e cache.Event) bool {
		if own(e) {
			return true
		}

		ids, all := cluster.AddressNetworks(c, e)

		return all || slices.Contains(ids, id)
	}
}

// StackMatcher returns a match function for stack SSE streams.
// Looks up the current stack membership from the cache on every event so that
// newly added or removed services are immediately reflected in the stream.
// Sync events always pass through so clients can trigger a full refetch.
func StackMatcher(c *cache.Cache, name string) func(cache.Event) bool {
	return func(e cache.Event) bool {
		if e.Type == cache.EventSync {
			return true
		}
		// The cache has already taken a removed resource out of its stack.
		if e.Action == "remove" {
			return e.Stack == name
		}
		switch e.Type {
		case cache.EventService, cache.EventConfig, cache.EventSecret,
			cache.EventNetwork, cache.EventVolume:
			return c.StackContains(name, e.Type, e.ID)
		case cache.EventTask:
			// A task belongs to the stack its service does.
			if t, ok := e.Resource.(swarm.Task); ok {
				return c.StackContains(name, cache.EventService, t.ServiceID)
			}
			return false
		default:
			return false
		}
	}
}

// Event is the JSON-LD enriched wire format for SSE event payloads.
type Event struct {
	AtID     string `json:"@id,omitempty"`
	AtType   string `json:"@type,omitempty"`
	Type     string `json:"type"`
	Action   string `json:"action"`
	ID       string `json:"id"`
	Resource any    `json:"resource,omitempty"`
}

// SetBasePath sets the prefix every event's @id carries. Called once while the
// router is built, before any client connects.
func (b *Broadcaster) SetBasePath(p string) { b.basePath = p }

func ToSSEEvent(e cache.Event, basePath string) Event {
	path := ResourcePath(e.Type, e.ID)
	if path != "" {
		path = basePath + path
	}

	// A removed resource rides on its event for matching only.
	resource := e.Resource
	if e.Action == "remove" {
		resource = nil
	}

	return Event{
		AtID:     path,
		AtType:   ResourceType(e.Type),
		Type:     string(e.Type),
		Action:   e.Action,
		ID:       e.ID,
		Resource: resource,
	}
}

// ResourcePath returns the canonical API path for a resource.
func ResourcePath(typ cache.EventType, id string) string {
	switch typ {
	case cache.EventNode:
		return "/nodes/" + id
	case cache.EventService:
		return "/services/" + id
	case cache.EventTask:
		return "/tasks/" + id
	case cache.EventConfig:
		return "/configs/" + id
	case cache.EventSecret:
		return "/secrets/" + id
	case cache.EventNetwork:
		return "/networks/" + id
	case cache.EventVolume:
		return "/volumes/" + id
	case cache.EventStack:
		return "/stacks/" + id
	default:
		return ""
	}
}

// ResourceType returns the JSON-LD @type for a resource type string.
func ResourceType(typ cache.EventType) string {
	switch typ {
	case cache.EventNode:
		return "Node"
	case cache.EventService:
		return "Service"
	case cache.EventTask:
		return "Task"
	case cache.EventConfig:
		return "Config"
	case cache.EventSecret:
		return "Secret"
	case cache.EventNetwork:
		return "Network"
	case cache.EventVolume:
		return "Volume"
	case cache.EventStack:
		return "Stack"
	default:
		return ""
	}
}

// WriteBatch writes events as one frame.
func WriteBatch(
	w io.Writer,
	flusher http.Flusher,
	events []cache.Event,
	basePath, epoch string,
) {
	_ = writeBatch(w, flusher, events, basePath, epoch)
}

// writeBatch is WriteBatch, returning the first write error.
func writeBatch(
	w io.Writer,
	flusher http.Flusher,
	events []cache.Event,
	basePath, epoch string,
) error {
	var maxID uint64
	for _, e := range events {
		if e.HistoryID > maxID {
			maxID = e.HistoryID
		}
	}

	if len(events) == 1 {
		data, _ := json.Marshal(ToSSEEvent(events[0], basePath))
		_, err := fmt.Fprintf(
			w, "id: %s-%d\nevent: %s\ndata: %s\n\n", epoch, maxID, events[0].Type, data,
		)
		if err != nil {
			return err
		}
	} else {
		enriched := make([]Event, len(events))
		for i, e := range events {
			enriched[i] = ToSSEEvent(e, basePath)
		}
		data, _ := json.Marshal(enriched)
		_, err := fmt.Fprintf(w, "id: %s-%d\nevent: batch\ndata: %s\n\n", epoch, maxID, data)
		if err != nil {
			return err
		}
	}
	flusher.Flush()

	return nil
}
