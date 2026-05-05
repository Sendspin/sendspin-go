// ABOUTME: GroupRole interface and role dispatch on Group
// ABOUTME: Roles register with the Group and receive client events + messages
package sendspin

import (
	"encoding/json"
	"fmt"
)

// GroupRole is the interface implemented by per-role handlers. Each
// role family (controller, metadata, player, artwork) registers one
// GroupRole with the Group. The Group dispatches client lifecycle
// events to all registered roles.
type GroupRole interface {
	Role() string
	OnClientJoin(c *ServerClient)
	OnClientLeave(id string, name string)
}

// MessageHandler is an optional interface that a GroupRole may
// implement to receive role-specific messages (e.g., client/command
// payloads routed by role key). Roles that don't handle messages
// don't need to implement this.
type MessageHandler interface {
	HandleMessage(c *ServerClient, payload json.RawMessage) error
}

// PlaybackStateChangedHandler is an optional interface that a GroupRole
// may implement to receive notifications when the group's playback state
// transitions. The Group dispatches GroupPlaybackStateChangedEvent to
// roles that implement this interface; same-state writes are not
// dispatched (Group.SetPlaybackState filters them out).
//
// Note: oldState may be empty for the very first transition out of the
// zero value.
type PlaybackStateChangedHandler interface {
	OnPlaybackStateChanged(oldState, newState string)
}

// RegisterRole registers a GroupRole with this group. The role is
// stored by its Role() family name. Duplicate registrations for the
// same family overwrite the previous one.
//
// After registration, the role receives OnClientJoin / OnClientLeave
// calls for clients that join or leave the group, dispatched from
// the group's internal event subscriber.
func (g *Group) RegisterRole(role GroupRole) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.roles == nil {
		g.roles = make(map[string]GroupRole)
	}
	g.roles[role.Role()] = role

	// Optional attach-to-group hook: roles that need to iterate the
	// group's clients on broadcast (e.g. MetadataGroupRole) implement
	// the unexported attachToGroup method to receive a back-reference.
	// Kept unexported so callers can't bypass RegisterRole.
	if a, ok := role.(interface{ attachToGroup(*Group) }); ok {
		a.attachToGroup(g)
	}

	if g.roleDispatchStarted {
		return
	}
	g.roleDispatchStarted = true

	events, _ := g.subscribeInternal()
	go g.dispatchRoleEvents(events)
}

// GetRole returns the registered GroupRole for the given family name,
// or nil if none is registered.
func (g *Group) GetRole(family string) GroupRole {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.roles == nil {
		return nil
	}
	return g.roles[family]
}

// RouteMessage routes a role-specific message payload to the
// registered GroupRole for the given family. Returns an error if no
// role is registered for the family or if the role doesn't implement
// MessageHandler.
func (g *Group) RouteMessage(c *ServerClient, roleFamily string, payload json.RawMessage) error {
	g.mu.RLock()
	role, ok := g.roles[roleFamily]
	g.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no role registered for %q", roleFamily)
	}

	handler, ok := role.(MessageHandler)
	if !ok {
		return fmt.Errorf("role %q does not handle messages", roleFamily)
	}

	return handler.HandleMessage(c, payload)
}

// dispatchRoleEvents reads from the group's event bus and calls
// the appropriate lifecycle method on every registered role.
func (g *Group) dispatchRoleEvents(events <-chan Event) {
	for evt := range events {
		g.mu.RLock()
		roles := make([]GroupRole, 0, len(g.roles))
		for _, r := range g.roles {
			roles = append(roles, r)
		}
		g.mu.RUnlock()

		switch e := evt.(type) {
		case ClientJoinedEvent:
			for _, r := range roles {
				r.OnClientJoin(e.Client)
			}
		case ClientLeftEvent:
			for _, r := range roles {
				r.OnClientLeave(e.ClientID, e.ClientName)
			}
		case GroupPlaybackStateChangedEvent:
			for _, r := range roles {
				if h, ok := r.(PlaybackStateChangedHandler); ok {
					h.OnPlaybackStateChanged(e.OldState, e.NewState)
				}
			}
		default:
			// ClientStateChangedEvent and future event types are
			// dispatched to roles that subscribe to them via other
			// mechanisms (e.g., the event bus directly). The role
			// dispatcher only handles lifecycle events.
		}
	}
}

// subscribeInternal creates a subscription while the caller already
// holds g.mu.Lock. This avoids the deadlock that would occur if we
// called the public Subscribe (which also takes Lock).
func (g *Group) subscribeInternal() (<-chan Event, func()) {
	if g.closed {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}

	id := g.nextSub
	g.nextSub++
	ch := make(chan Event, 32)
	g.subs[id] = ch

	return ch, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if existing, ok := g.subs[id]; ok {
			delete(g.subs, id)
			close(existing)
		}
	}
}
