// ABOUTME: Group owns the per-playback-group event bus
// ABOUTME: Publishes client-level events to subscribed GroupRole handlers
package sendspin

import (
	"log"
	"sync"
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
type ClientJoinedEvent struct {
	Client *ServerClient
}

func (ClientJoinedEvent) isGroupEvent() {}

// ClientLeftEvent fires when a client has disconnected and been removed
// from a Group. Handlers should release any per-client resources on this.
type ClientLeftEvent struct {
	Client *ServerClient
}

func (ClientLeftEvent) isGroupEvent() {}

// ClientStateChangedEvent fires when a client's player state (state,
// volume, muted) has been updated from a client/state control message.
// The snapshot fields are captured at publish time; the Client pointer
// is provided for handlers that need to reply.
type ClientStateChangedEvent struct {
	Client *ServerClient
	State  string
	Volume int
	Muted  bool
}

func (ClientStateChangedEvent) isGroupEvent() {}

// Group owns the event bus and the set of clients currently attached to
// a playback group. For M2 there is exactly one Group per Server,
// auto-created in NewServer. Multi-group support is a post-#61 concern.
//
// The zero value is not usable — construct via NewGroup.
type Group struct {
	id string

	mu      sync.RWMutex
	clients map[string]*ServerClient
	subs    map[int]chan Event
	nextSub int
	closed  bool
}

// NewGroup constructs a Group with the given identifier. The ID is
// typically the server's UUID for the implicit default group.
func NewGroup(id string) *Group {
	return &Group{
		id:      id,
		clients: make(map[string]*ServerClient),
		subs:    make(map[int]chan Event),
	}
}

// ID returns the group identifier.
func (g *Group) ID() string { return g.id }

// Subscribe registers a new listener and returns the event channel plus
// an unsubscribe function. Each Subscribe call gets its own buffered
// channel (capacity 32). Calling unsubscribe closes the channel and
// removes it from the fan-out list; calling it more than once is safe.
//
// The event bus fans out with non-blocking sends — if a subscriber's
// buffer is full when an event is published, the event is dropped for
// that subscriber and a warning is logged. Subscribers should drain
// their channel promptly or accept that they may miss events.
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

// publish fans an event out to every active subscriber. Sends are
// non-blocking: if a subscriber's buffer is full, the event is dropped
// and a warning is logged. publish is unexported — events are only
// published by other code in package sendspin.
func (g *Group) publish(evt Event) {
	g.mu.RLock()
	defer g.mu.RUnlock()

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
	if g.closed {
		g.mu.Unlock()
		return
	}
	if _, exists := g.clients[c.ID()]; exists {
		g.mu.Unlock()
		return
	}
	g.clients[c.ID()] = c
	g.mu.Unlock()

	g.publish(ClientJoinedEvent{Client: c})
}

// removeClient detaches a ServerClient and publishes a ClientLeftEvent.
// Idempotent — removing an unknown client is a no-op.
func (g *Group) removeClient(c *ServerClient) {
	g.mu.Lock()
	if _, exists := g.clients[c.ID()]; !exists {
		g.mu.Unlock()
		return
	}
	delete(g.clients, c.ID())
	g.mu.Unlock()

	g.publish(ClientLeftEvent{Client: c})
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
}
