// ABOUTME: Tests for the ControllerGroupRole implementation
// ABOUTME: Verifies command dispatch and controller state push on join
package sendspin

import (
	"encoding/json"
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

// drainSendChan empties a client's send channel without blocking, returning
// the messages read. Used to discard setup messages (group/update, join
// state) before asserting on a later broadcast.
func drainSendChan(ch chan interface{}) []interface{} {
	var out []interface{}
	for {
		select {
		case m := <-ch:
			out = append(out, m)
		default:
			return out
		}
	}
}

// controllerStateFrom extracts the ControllerState from a queued server/state
// message, or nil if the message is not a controller server/state.
func controllerStateFrom(t *testing.T, msg interface{}) *protocol.ControllerState {
	t.Helper()
	m, ok := msg.(protocol.Message)
	if !ok || m.Type != "server/state" {
		return nil
	}
	state, ok := m.Payload.(protocol.ServerStateMessage)
	if !ok {
		return nil
	}
	return state.Controller
}

func TestControllerRole_HandleCommand(t *testing.T) {
	var received string
	var receivedClient *ServerClient

	ctrl := NewControllerRole(ControllerConfig{
		SupportedCommands: []string{"next", "previous"},
		OnCommand: func(c *ServerClient, command string) {
			receivedClient = c
			received = command
		},
	})

	sc := &ServerClient{id: "c1", sendChan: make(chan interface{}, 10)}
	payload := json.RawMessage(`{"command":"next"}`)

	err := ctrl.HandleMessage(sc, payload)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if received != "next" {
		t.Errorf("received command = %q, want %q", received, "next")
	}
	if receivedClient == nil || receivedClient.ID() != "c1" {
		t.Error("OnCommand did not receive the correct client")
	}
}

func TestControllerRole_HandleCommandNoCallback(t *testing.T) {
	ctrl := NewControllerRole(ControllerConfig{
		SupportedCommands: []string{"next"},
	})

	err := ctrl.HandleMessage(&ServerClient{id: "c1"}, json.RawMessage(`{"command":"next"}`))
	if err != nil {
		t.Errorf("HandleMessage without callback should not error, got %v", err)
	}
}

func TestControllerRole_HandleCommandMissingField(t *testing.T) {
	ctrl := NewControllerRole(ControllerConfig{})

	err := ctrl.HandleMessage(&ServerClient{id: "c1"}, json.RawMessage(`{}`))
	if err == nil {
		t.Error("HandleMessage with empty command should return error")
	}
}

func TestControllerRole_OnClientJoinSendsState(t *testing.T) {
	ctrl := NewControllerRole(ControllerConfig{
		SupportedCommands: []string{"next", "previous", "play", "pause"},
	})

	sc := &ServerClient{
		id:       "c1",
		roles:    []string{"controller@v1"},
		sendChan: make(chan interface{}, 10),
	}

	ctrl.OnClientJoin(sc)

	select {
	case msg := <-sc.sendChan:
		_ = msg // Verifies a message was enqueued
	default:
		t.Fatal("OnClientJoin did not send controller state to client")
	}
}

func TestControllerRole_OnClientJoinSkipsNonControllerClient(t *testing.T) {
	ctrl := NewControllerRole(ControllerConfig{
		SupportedCommands: []string{"next"},
	})

	sc := &ServerClient{
		id:       "c1",
		roles:    []string{"player@v1"},
		sendChan: make(chan interface{}, 10),
	}

	ctrl.OnClientJoin(sc)

	select {
	case <-sc.sendChan:
		t.Error("OnClientJoin should not send state to non-controller client")
	default:
		// Correct — nothing sent.
	}
}

func TestControllerRole_Role(t *testing.T) {
	ctrl := NewControllerRole(ControllerConfig{})
	if ctrl.Role() != "controller" {
		t.Errorf("Role() = %q, want %q", ctrl.Role(), "controller")
	}
}

func TestControllerRole_DefaultRepeatOff(t *testing.T) {
	ctrl := NewControllerRole(ControllerConfig{})
	if got := ctrl.Repeat(); got != "off" {
		t.Errorf("default Repeat() = %q, want %q", got, "off")
	}
	if ctrl.Shuffle() {
		t.Error("default Shuffle() = true, want false")
	}
}

func TestControllerRole_OnClientJoinIncludesRepeatShuffle(t *testing.T) {
	ctrl := NewControllerRole(ControllerConfig{SupportedCommands: []string{"repeat_all", "shuffle"}})
	ctrl.SetRepeat("all") // no group wired → state updates, no broadcast
	ctrl.SetShuffle(true)

	sc := &ServerClient{id: "c1", roles: []string{"controller@v1"}, sendChan: make(chan interface{}, 10)}
	ctrl.OnClientJoin(sc)

	msgs := drainSendChan(sc.sendChan)
	if len(msgs) == 0 {
		t.Fatal("OnClientJoin sent nothing")
	}
	cs := controllerStateFrom(t, msgs[0])
	if cs == nil {
		t.Fatalf("first message is not a controller server/state: %#v", msgs[0])
	}
	if cs.Repeat != "all" {
		t.Errorf("join controller state Repeat = %q, want %q", cs.Repeat, "all")
	}
	if !cs.Shuffle {
		t.Error("join controller state Shuffle = false, want true")
	}
}

func TestControllerRole_RegisterRoleInjectsGroup(t *testing.T) {
	g := NewGroup("g")
	defer g.Close()

	ctrl := NewControllerRole(ControllerConfig{})
	g.RegisterRole(ctrl)

	if ctrl.group != g {
		t.Error("RegisterRole did not inject the group into the controller role")
	}
}

func TestControllerRole_SetRepeatBroadcastsToControllerMembers(t *testing.T) {
	g := NewGroup("g")
	defer g.Close()

	ctrl := NewControllerRole(ControllerConfig{SupportedCommands: []string{"repeat_all"}})
	ctrl.attachToGroup(g)

	c1 := &ServerClient{id: "c1", roles: []string{"controller@v1"}, sendChan: make(chan interface{}, 10)}
	c2 := &ServerClient{id: "c2", roles: []string{"controller@v1"}, sendChan: make(chan interface{}, 10)}
	g.addClient(c1)
	g.addClient(c2)
	drainSendChan(c1.sendChan) // discard group/update
	drainSendChan(c2.sendChan)

	ctrl.SetRepeat("one")

	for _, c := range []*ServerClient{c1, c2} {
		msgs := drainSendChan(c.sendChan)
		if len(msgs) != 1 {
			t.Fatalf("client %s got %d messages, want 1", c.id, len(msgs))
		}
		cs := controllerStateFrom(t, msgs[0])
		if cs == nil || cs.Repeat != "one" {
			t.Errorf("client %s did not receive controller state with Repeat=one: %#v", c.id, msgs[0])
		}
	}
}

func TestControllerRole_SetRepeatDedup(t *testing.T) {
	g := NewGroup("g")
	defer g.Close()

	ctrl := NewControllerRole(ControllerConfig{})
	ctrl.attachToGroup(g)

	c1 := &ServerClient{id: "c1", roles: []string{"controller@v1"}, sendChan: make(chan interface{}, 10)}
	g.addClient(c1)
	drainSendChan(c1.sendChan)

	ctrl.SetRepeat("all")
	drainSendChan(c1.sendChan) // discard first broadcast

	ctrl.SetRepeat("all") // same value — must not re-broadcast
	if msgs := drainSendChan(c1.sendChan); len(msgs) != 0 {
		t.Errorf("setting Repeat to the same value re-broadcast %d messages, want 0", len(msgs))
	}
}

func TestControllerRole_SetShuffleBroadcasts(t *testing.T) {
	g := NewGroup("g")
	defer g.Close()

	ctrl := NewControllerRole(ControllerConfig{})
	ctrl.attachToGroup(g)

	c1 := &ServerClient{id: "c1", roles: []string{"controller@v1"}, sendChan: make(chan interface{}, 10)}
	g.addClient(c1)
	drainSendChan(c1.sendChan)

	ctrl.SetShuffle(true)

	msgs := drainSendChan(c1.sendChan)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	cs := controllerStateFrom(t, msgs[0])
	if cs == nil || !cs.Shuffle {
		t.Errorf("did not receive controller state with Shuffle=true: %#v", msgs[0])
	}
}

func TestControllerRole_MirrorsToMetadataOnlyClient(t *testing.T) {
	g := NewGroup("g")
	defer g.Close()

	ctrl := NewControllerRole(ControllerConfig{
		ClockMicros: func() int64 { return 12345 },
	})
	ctrl.attachToGroup(g)

	// Pure v1 client: metadata role only, no controller role.
	md := &ServerClient{id: "m1", roles: []string{"metadata@v1"}, sendChan: make(chan interface{}, 10)}
	g.addClient(md)
	drainSendChan(md.sendChan)

	ctrl.SetRepeat("one")

	msgs := drainSendChan(md.sendChan)
	if len(msgs) != 1 {
		t.Fatalf("metadata-only client got %d messages, want 1", len(msgs))
	}
	m, ok := msgs[0].(protocol.Message)
	if !ok || m.Type != "server/state" {
		t.Fatalf("expected server/state, got %#v", msgs[0])
	}
	state := m.Payload.(protocol.ServerStateMessage)
	if state.Metadata == nil || state.Metadata.Repeat == nil || *state.Metadata.Repeat != "one" {
		t.Errorf("metadata mirror missing Repeat=one: %#v", state.Metadata)
	}
	if state.Metadata.Timestamp != 12345 {
		t.Errorf("metadata mirror Timestamp = %d, want 12345", state.Metadata.Timestamp)
	}
	if state.Controller != nil {
		t.Error("metadata-only client should not receive controller state")
	}
}
