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

// frameEvent is an event on its way to a client. sync numbers a sync event
// among every one broadcast, so a cursor can say which it has seen; data is
// its payload, encoded once for every client, or nil to encode on write.
type frameEvent struct {
	cache.Event
	sync uint64
	data []byte
}

// syncMark is the newest sync broadcast: its history position and number.
type syncMark struct {
	pos, seq uint64
}

type sseClient struct {
	events chan frameEvent
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
	inbox             chan frameEvent
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

	// lastSync is the newest sync broadcast, nil before the first. A resync is
	// not in the history, so replay checks a cursor against it; see needsSync.
	lastSync atomic.Pointer[syncMark]
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
		inbox:             make(chan frameEvent, 256),
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
	queued := frameEvent{Event: e}
	if e.Type == cache.EventSync {
		queued.sync = b.recordSync(e.HistoryID)
	}

	select {
	case b.inbox <- queued:
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

// recordSync marks a sync at history position pos and returns its number. It
// runs before the sync is queued, so a reconnect cannot miss one dropped there.
func (b *Broadcaster) recordSync(pos uint64) uint64 {
	for {
		prev := b.lastSync.Load()
		next := &syncMark{pos: pos, seq: 1}
		if prev != nil {
			next.seq = prev.seq + 1
		}
		if b.lastSync.CompareAndSwap(prev, next) {
			return next.seq
		}
	}
}

// syncFrame is a sync at the current history position, covering every sync
// recorded so far. The position is read first, so no sync it covers can be
// recorded after its number is read and then arrive live as a second frame.
func (b *Broadcaster) syncFrame() frameEvent {
	e := frameEvent{Event: syncEvent(b.replayPosition())}
	if mark := b.lastSync.Load(); mark != nil {
		e.sync = mark.seq
	}

	return e
}

// needsSync reports whether a sync landed after a cursor. One that names a sync
// has seen that one; one that does not has seen none at or after its position,
// since a frame ending in a sync carries its number.
func (b *Broadcaster) needsSync(afterID, seen uint64) bool {
	mark := b.lastSync.Load()
	switch {
	case mark == nil:
		return false
	case seen > 0:
		return seen != mark.seq
	default:
		return mark.pos >= afterID
	}
}

// fanOut is the dedicated goroutine that delivers events to SSE clients.
// A client's events channel is never closed, so a send to one that has
// just left, outside the lock, lands in a buffer nobody reads.
func (b *Broadcaster) fanOut() {
	var recipients []*sseClient

	for {
		select {
		case e := <-b.inbox:
			recipients = recipients[:0]
			b.mu.RLock()
			for c := range b.clients {
				if c.accepts(e.Event) {
					recipients = append(recipients, c)
				}
			}
			b.mu.RUnlock()

			if len(recipients) == 0 {
				continue
			}

			e.data = encodeEvent(e.Event, b.basePath)
			for _, c := range recipients {
				select {
				case c.events <- e:
				default:
					// Slow client: drop the event rather than block the
					// broadcaster goroutine, and resync the client instead.
					metrics.RecordSSEClientDrop()
					c.lagged.Store(true)
				}
			}
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
		events: make(chan frameEvent, 64),
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

	// syncedThrough is the newest sync a frame written here covered; a live
	// one it covered is skipped rather than sent as a second refetch.
	var skipBelow, syncedThrough uint64
	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" && b.replay != nil {
		var err error
		skipBelow, syncedThrough, err = b.replayEvents(stream, lastID, replayType, client.match)
		if err != nil {
			return
		}
	}

	// A lagged client's pending batch has a gap, so it is not written on the
	// way out: the client reconnects from its last whole cursor instead.
	finish := func(batch []frameEvent) {
		if len(batch) > 0 && !client.lagged.Load() {
			_ = b.writeFrame(stream, batch)
		}
	}

	batchTicker := time.NewTicker(b.batchInterval)
	defer batchTicker.Stop()
	keepalive := time.NewTicker(b.keepaliveInterval)
	defer keepalive.Stop()
	var batch []frameEvent

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
			if e.Type == cache.EventSync && e.sync <= syncedThrough {
				continue
			}
			batch = append(batch, e)
		case <-batchTicker.C:
			// The sync covers the events queued behind it, which are skipped.
			if client.lagged.Swap(false) {
				batch = batch[:0]
				frame := b.syncFrame()
				skipBelow, syncedThrough = frame.HistoryID, max(syncedThrough, frame.sync)
				if b.writeFrame(stream, []frameEvent{frame}) != nil {
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
) (position, synced uint64, err error) {
	writeSync := func() (uint64, uint64, error) {
		frame := b.syncFrame()
		return frame.HistoryID, frame.sync, b.writeFrame(stream, []frameEvent{frame})
	}

	// Detail/stack streams are ineligible for replay — send sync.
	if replayType == "" {
		return writeSync()
	}

	afterID, seen, ok := b.cursor(lastID)
	if !ok || b.needsSync(afterID, seen) {
		return writeSync()
	}

	entries, ok := b.replay.Since(afterID)
	if !ok {
		return writeSync()
	}

	if len(entries) == 0 {
		return afterID, 0, nil
	}

	// Filter entries by type and ACL, then convert to cache.Event (no Resource payload).
	var replay []frameEvent
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

		replay = append(replay, frameEvent{Event: ev})
	}

	if len(replay) == 0 {
		return afterID, 0, nil
	}

	return replay[len(replay)-1].HistoryID, 0, b.writeFrame(stream, replay)
}

// writeFrame writes events as one frame under the stream's write deadline.
func (b *Broadcaster) writeFrame(stream *streamWriter, events []frameEvent) error {
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

// cursor reads a Last-Event-ID this process wrote: the history id, and the
// number of the sync the frame ended in, zero if it did not.
func (b *Broadcaster) cursor(lastID string) (id, seen uint64, ok bool) {
	n, ok := strings.CutPrefix(lastID, b.epoch+"-")
	if !ok {
		return 0, 0, false
	}

	n, seq, synced := strings.Cut(n, ".")
	id, err := strconv.ParseUint(n, 10, 64)
	if err != nil {
		return 0, 0, false
	}

	if synced {
		if seen, err = strconv.ParseUint(seq, 10, 64); err != nil || seen == 0 {
			return 0, 0, false
		}
	}

	return id, seen, true
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

		return slices.Contains(cluster.AddressNetworks(c, e), id)
	}
}

// VersionMatcher returns a match function for a config's or secret's SSE
// stream: its own events, and those that change its versions, by the rule
// cluster.VersionIndex.TouchesDigest gives MCP subscribers too.
func VersionMatcher(
	versions *cluster.VersionIndex,
	typ cache.EventType,
	id string,
) func(cache.Event) bool {
	own := ResourceMatcher(typ, id)

	return func(e cache.Event) bool {
		return own(e) || versions.TouchesDigest(e, typ, id)
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

// encodeEvent is an event's payload as every client receives it.
func encodeEvent(e cache.Event, basePath string) []byte {
	data, _ := json.Marshal(ToSSEEvent(e, basePath))
	return data
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

// WriteBatch writes events as one frame, encoding each. It is the seam the
// serialization benchmarks in internal/api measure; streams use writeFrame.
func WriteBatch(
	w io.Writer,
	flusher http.Flusher,
	events []cache.Event,
	basePath, epoch string,
) {
	frame := make([]frameEvent, len(events))
	for i, e := range events {
		frame[i] = frameEvent{Event: e}
	}

	_ = writeBatch(w, flusher, frame, basePath, epoch)
}

// writeBatch is WriteBatch, returning the first write error.
func writeBatch(
	w io.Writer,
	flusher http.Flusher,
	events []frameEvent,
	basePath, epoch string,
) error {
	var maxID, synced uint64
	for _, e := range events {
		if e.HistoryID > maxID {
			maxID, synced = e.HistoryID, 0
		}
		if e.Type == cache.EventSync && e.HistoryID == maxID {
			synced = e.sync
		}
	}

	// A frame no change follows a sync in says which sync, for needsSync.
	id := epoch + "-" + strconv.FormatUint(maxID, 10)
	if synced > 0 {
		id += "." + strconv.FormatUint(synced, 10)
	}

	encoded := func(e frameEvent) []byte {
		if e.data != nil {
			return e.data
		}

		return encodeEvent(e.Event, basePath)
	}

	if len(events) == 1 {
		_, err := fmt.Fprintf(
			w, "id: %s\nevent: %s\ndata: %s\n\n", id, events[0].Type, encoded(events[0]),
		)
		if err != nil {
			return err
		}
	} else {
		data := []byte{'['}
		for i, e := range events {
			if i > 0 {
				data = append(data, ',')
			}
			data = append(data, encoded(e)...)
		}
		data = append(data, ']')

		_, err := fmt.Fprintf(w, "id: %s\nevent: batch\ndata: %s\n\n", id, data)
		if err != nil {
			return err
		}
	}
	flusher.Flush()

	return nil
}
