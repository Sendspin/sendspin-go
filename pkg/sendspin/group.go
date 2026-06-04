// ABOUTME: Group owns the per-playback-group event bus
// ABOUTME: Publishes client-level events to subscribed GroupRole handlers
package sendspin

import (
	"log"
	"sync"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

// Event is the sealed interface implemented by every event published on a
// Group's event bus. Callers type-switch on the interface to handle the
// concrete types they care about.
//
// The isGroupEvent marker is unexported, so third-party packages cannot
// implement Event directly — all events must be defined in this package.
type Event interface {
	isGroupEvent()
}

// ClientJoinedEvent fires when a client has completed the handshake and
// been added to a Group. Handlers that need to send a greeting message
// or snapshot state to the new client should listen for this.
//
// Unlike ClientLeftEvent, this event carries a live *ServerClient
// pointer because at publish time the client has just completed its
// handshake and its connection is fully alive — handler calls like
// c.Send(...) are safe. ClientLeftEvent intentionally drops the
// pointer because by the time it fires, the client is mid-teardown.
type ClientJoinedEvent struct {
	Client *ServerClient
}

func (ClientJoinedEvent) isGroupEvent() {}

// ClientLeftEvent fires when a client has disconnected and been removed
// from a Group. The event carries only the ID and name of the departed
// client, not a pointer — by the time handlers see this event, the
// underlying ServerClient may already be mid-teardown and its methods
// are unsafe to call. Handlers that need per-client state must have
// captured it earlier (e.g. on the matching ClientJoinedEvent).
type ClientLeftEvent struct {
	ClientID   string
	ClientName string
}

func (ClientLeftEvent) isGroupEvent() {}

// ClientStateChangedEvent fires when a client's player state (state,
// volume, muted) has been updated from a client/state control message.
// The snapshot fields are captured at publish time; the Client pointer
// is provided for handlers that need to reply.
//
// Handlers should treat the embedded State/Volume/Muted fields as the
// authoritative value for this specific event; calling c.State() on
// the Client pointer may return a newer value written by a subsequent
// state update that raced this event's delivery.
type ClientStateChangedEvent struct {
	Client *ServerClient
	State  string
	Volume int
	Muted  bool
}

func (ClientStateChangedEvent) isGroupEvent() {}

// GroupPlaybackStateChangedEvent fires when the group's playback state
// transitions. Roles that need to react (e.g. re-broadcast metadata at
// stopped→playing) listen via the optional PlaybackStateChangedHandler
// interface dispatched by Group's role event loop.
//
// OldState may be empty for the initial transition out of the zero value.
// Same-state transitions are not published (no-op writes are silent).
type GroupPlaybackStateChangedEvent struct {
	OldState string
	NewState string
}

func (GroupPlaybackStateChangedEvent) isGroupEvent() {}

// Group owns the event bus and the set of clients currently attached to
// a playback group. For M2 there is exactly one Group per Server,
// auto-created in NewServer. Multi-group support is a post-#61 concern.
//
// Event ordering: events originating from a single source (e.g., all
// events from one client's read loop) are delivered to each subscriber
// in publish order. Events from different sources have no relative
// ordering guarantee — a handler that reacts to ClientJoinedEvent by
// publishing another event does not race the client's own subsequent
// events deterministically. M3 role handlers that need cross-source
// ordering must coordinate via their own synchronization.
//
// The zero value is not usable — construct via NewGroup.
type Group struct {
	id            string
	playbackState string

	mu      sync.RWMutex
	clients map[string]*ServerClient
	subs    map[int]chan Event
	nextSub int
	closed  bool

	roles               map[string]GroupRole
	roleDispatchStarted bool
}

// NewGroup constructs a Group with the given identifier. The ID is
// typically the server's UUID for the implicit default group.
func NewGroup(id string) *Group {
	return &Group{
		id:            id,
		playbackState: "playing",
		clients:       make(map[string]*ServerClient),
		subs:          make(map[int]chan Event),
	}
}

// ID returns the group identifier.
func (g *Group) ID() string { return g.id }

// Subscribe registers a new listener and returns the event channel plus
// an unsubscribe function. Each Subscribe call gets its own buffered
// channel (capacity 32). Calling unsubscribe closes the channel and
// removes it from the fan-out list; calling it more than once is safe.
//
// After Close() has been called, Subscribe returns a pre-closed channel
// and a no-op unsubscribe. A receive on the returned channel will yield
// the zero value with ok == false — callers ranging over the channel
// should treat this as "group has shut down" rather than "no events
// yet."
func (g *Group) Subscribe() (<-chan Event, func()) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		// Return a closed channel and a no-op unsubscribe so callers
		// don't have to check for this case.
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}

	id := g.nextSub
	g.nextSub++
	// Buffer size 32 is a heuristic: the bus carries low-rate control
	// events (joins, leaves, state changes), not audio. Slow handlers
	// drop events via the non-blocking publish path rather than stalling
	// the publisher.
	ch := make(chan Event, 32)
	g.subs[id] = ch

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			if existing, ok := g.subs[id]; ok {
				delete(g.subs, id)
				close(existing)
			}
		})
	}
	return ch, unsubscribe
}

// publish fans an event out to every active subscriber. This is the
// top-level entry point used by code that does not already hold g.mu.
// Sends are non-blocking: if a subscriber's buffer is full, the event
// is dropped and a warning is logged.
func (g *Group) publish(evt Event) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	g.publishLocked(evt)
}

// publishLocked fans an event out assuming the caller already holds
// g.mu (either Lock or RLock). Use this from code that mutates g.subs
// or g.clients and wants to publish under the same critical section
// to preserve ordering. Sends are non-blocking for the same reason
// publish is — slow subscribers drop events instead of stalling the
// publisher.
//
// Note: when invoked under a write Lock (as addClient/removeClient do),
// the non-blocking sends run while the writer lock is held. This is
// bounded by the number of subscribers and each send is select-default,
// so the critical section remains O(subscribers) with no blocking.
func (g *Group) publishLocked(evt Event) {
	if g.closed {
		return
	}

	for id, ch := range g.subs {
		select {
		case ch <- evt:
		default:
			log.Printf("Group %s: subscriber %d dropped event %T (buffer full)", g.id, id, evt)
		}
	}
}

// addClient attaches a ServerClient to the group and publishes a
// ClientJoinedEvent. Idempotent — adding the same client twice is a
// no-op on the second call.
func (g *Group) addClient(c *ServerClient) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return
	}
	if _, exists := g.clients[c.ID()]; exists {
		return
	}
	g.clients[c.ID()] = c

	// Send group/update to the joining client — group-level concern,
	// not role-specific. Sent before ClientJoinedEvent so role handlers
	// can assume the client already knows its group context.
	groupID := g.id
	playbackState := g.playbackState
	if playbackState == "" {
		playbackState = "playing"
	}
	c.Send("group/update", protocol.GroupUpdate{
		GroupID:       &groupID,
		PlaybackState: &playbackState,
	})

	g.publishLocked(ClientJoinedEvent{Client: c})
}

// removeClient detaches a ServerClient and publishes a ClientLeftEvent.
// Idempotent — removing an unknown client is a no-op.
func (g *Group) removeClient(c *ServerClient) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.clients[c.ID()]; !exists {
		return
	}
	delete(g.clients, c.ID())

	g.publishLocked(ClientLeftEvent{
		ClientID:   c.ID(),
		ClientName: c.Name(),
	})
}

// Clients returns a snapshot of the ServerClients currently attached to
// this group. The returned slice is a fresh copy; mutating it does not
// affect the group.
func (g *Group) Clients() []*ServerClient {
	g.mu.RLock()
	defer g.mu.RUnlock()

	out := make([]*ServerClient, 0, len(g.clients))
	for _, c := range g.clients {
		out = append(out, c)
	}
	return out
}

// Close releases all subscribers and marks the group as shut down.
// After Close, Subscribe returns a pre-closed channel and publish is a
// no-op. Close is safe to call multiple times.
func (g *Group) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed {
		return
	}
	g.closed = true

	for id, ch := range g.subs {
		close(ch)
		delete(g.subs, id)
	}
	clear(g.clients)
}

// SetPlaybackState updates the group's playback state. If the new state
// differs from the current value, a GroupPlaybackStateChangedEvent is
// published. Future clients joining the group will receive the new
// state in group/update. Same-state writes are a silent no-op.
func (g *Group) SetPlaybackState(state string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.playbackState == state {
		return
	}
	oldState := g.playbackState
	g.playbackState = state
	g.publishLocked(GroupPlaybackStateChangedEvent{
		OldState: oldState,
		NewState: state,
	})
}
