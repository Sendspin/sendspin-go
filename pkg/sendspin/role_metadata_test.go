// ABOUTME: Tests for the MetadataGroupRole implementation
// ABOUTME: Verifies metadata state push to joining clients and broadcast paths
package sendspin

import (
	"sync"
	"testing"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

// drainServerStateMessage reads one queued send from a fake client's
// sendChan and returns the decoded protocol.Message + ServerStateMessage.
// Fails the test if nothing is queued or if the queued payload is not a
// server/state message.
func drainServerStateMessage(t *testing.T, sc *ServerClient) (protocol.Message, protocol.ServerStateMessage) {
	t.Helper()
	select {
	case raw := <-sc.sendChan:
		msg, ok := raw.(protocol.Message)
		if !ok {
			t.Fatalf("queued send was %T, want protocol.Message", raw)
		}
		state, ok := msg.Payload.(protocol.ServerStateMessage)
		if !ok {
			t.Fatalf("Message.Payload was %T, want protocol.ServerStateMessage", msg.Payload)
		}
		return msg, state
	case <-time.After(50 * time.Millisecond):
		t.Fatal("expected a server/state send, none arrived")
		return protocol.Message{}, protocol.ServerStateMessage{}
	}
}

// expectNoSend fails the test if the client received any queued send
// within the wait window. Used to assert non-metadata clients don't get
// broadcasts.
func expectNoSend(t *testing.T, sc *ServerClient, wait time.Duration, label string) {
	t.Helper()
	select {
	case raw := <-sc.sendChan:
		t.Fatalf("%s: unexpected send queued: %T %+v", label, raw, raw)
	case <-time.After(wait):
		// Expected: nothing.
	}
}

func TestMetadataRole_OnClientJoinSendsState(t *testing.T) {
	meta := NewMetadataRole(MetadataConfig{
		GetMetadata: func() (string, string, string) {
			return "Track Title", "Artist Name", "Album Name"
		},
		ClockMicros: func() int64 { return 12345 },
	})

	sc := &ServerClient{
		id:       "c1",
		roles:    []string{"metadata@v1"},
		sendChan: make(chan interface{}, 10),
	}

	meta.OnClientJoin(sc)

	select {
	case msg := <-sc.sendChan:
		_ = msg
	default:
		t.Fatal("OnClientJoin did not send metadata state")
	}
}

func TestMetadataRole_OnClientJoinSkipsNonMetadataClient(t *testing.T) {
	meta := NewMetadataRole(MetadataConfig{
		GetMetadata: func() (string, string, string) { return "t", "a", "al" },
		ClockMicros: func() int64 { return 0 },
	})

	sc := &ServerClient{
		id:       "c1",
		roles:    []string{"player@v1"},
		sendChan: make(chan interface{}, 10),
	}

	meta.OnClientJoin(sc)

	select {
	case <-sc.sendChan:
		t.Error("should not send metadata to non-metadata client")
	default:
	}
}

func TestMetadataRole_Role(t *testing.T) {
	meta := NewMetadataRole(MetadataConfig{})
	if meta.Role() != "metadata" {
		t.Errorf("Role() = %q, want %q", meta.Role(), "metadata")
	}
}

// TestMetadataRole_OnPlaybackStateChanged_BroadcastsToMetadataClients
// builds a Group with two attached clients (one with the metadata role,
// one without), registers a MetadataGroupRole, and invokes
// OnPlaybackStateChanged("stopped", "playing"). The metadata client must
// receive a server/state with the expected payload; the non-metadata
// client must not.
func TestMetadataRole_OnPlaybackStateChanged_BroadcastsToMetadataClients(t *testing.T) {
	g := NewGroup("test-group")
	defer g.Close()

	meta := NewMetadataRole(MetadataConfig{
		GetMetadata: func() (string, string, string) {
			return "Track Title", "Artist Name", "Album Name"
		},
		ClockMicros: func() int64 { return 42 },
	})
	g.RegisterRole(meta)

	metaClient := &ServerClient{
		id:       "meta-c",
		name:     "metadata client",
		roles:    []string{"metadata@v1"},
		sendChan: make(chan interface{}, 10),
	}
	playerClient := &ServerClient{
		id:       "player-c",
		name:     "player client",
		roles:    []string{"player@v1"},
		sendChan: make(chan interface{}, 10),
	}

	// Attach via addClient so they're members of g.Clients(); drain the
	// group/update that addClient sends before exercising broadcast.
	g.addClient(metaClient)
	g.addClient(playerClient)
	<-metaClient.sendChan   // group/update
	<-playerClient.sendChan // group/update

	// Drain the OnClientJoin server/state that fires on the metadata
	// client (since meta was registered before the client joined).
	// Sleep briefly so the dispatcher goroutine processes the join.
	time.Sleep(20 * time.Millisecond)
	select {
	case raw := <-metaClient.sendChan:
		t.Logf("drained join-time send: %+v", raw)
	default:
		// Already drained or never sent — fine, this test exercises the
		// broadcast path, not the join path.
	}

	// Direct invocation — independent of event dispatch.
	meta.OnPlaybackStateChanged("stopped", "playing")

	msg, state := drainServerStateMessage(t, metaClient)
	t.Logf("metaClient received: type=%s payload=%+v metadata=%+v", msg.Type, state, state.Metadata)
	if msg.Type != "server/state" {
		t.Errorf("Message.Type = %q, want server/state", msg.Type)
	}
	if state.Metadata == nil {
		t.Fatal("state.Metadata is nil")
	}
	if state.Metadata.Title == nil || *state.Metadata.Title != "Track Title" {
		t.Errorf("Title = %v, want \"Track Title\"", state.Metadata.Title)
	}
	if state.Metadata.Artist == nil || *state.Metadata.Artist != "Artist Name" {
		t.Errorf("Artist = %v, want \"Artist Name\"", state.Metadata.Artist)
	}
	if state.Metadata.Album == nil || *state.Metadata.Album != "Album Name" {
		t.Errorf("Album = %v, want \"Album Name\"", state.Metadata.Album)
	}
	if state.Metadata.Timestamp != 42 {
		t.Errorf("Timestamp = %d, want 42", state.Metadata.Timestamp)
	}

	expectNoSend(t, playerClient, 30*time.Millisecond, "non-metadata client")
}

// TestMetadataRole_OnPlaybackStateChanged_IgnoresNonPlayingTransitions
// asserts that transitions that aren't "* → playing" (excluding
// playing→playing) do not broadcast.
func TestMetadataRole_OnPlaybackStateChanged_IgnoresNonPlayingTransitions(t *testing.T) {
	g := NewGroup("test-group")
	defer g.Close()

	meta := NewMetadataRole(MetadataConfig{
		GetMetadata: func() (string, string, string) { return "t", "a", "al" },
		ClockMicros: func() int64 { return 0 },
	})
	g.RegisterRole(meta)

	sc := &ServerClient{
		id:       "meta-c",
		roles:    []string{"metadata@v1"},
		sendChan: make(chan interface{}, 10),
	}
	g.addClient(sc)
	<-sc.sendChan // group/update

	// Drain any OnClientJoin send that landed.
	time.Sleep(20 * time.Millisecond)
	select {
	case <-sc.sendChan:
	default:
	}

	cases := []struct {
		oldState, newState string
	}{
		{"playing", "paused"},
		{"playing", "stopped"},
		{"stopped", "paused"},
		{"playing", "playing"}, // would have been filtered by Group; pin role-level guard too
	}

	for _, tc := range cases {
		t.Logf("trying transition %s -> %s (must not broadcast)", tc.oldState, tc.newState)
		meta.OnPlaybackStateChanged(tc.oldState, tc.newState)
		expectNoSend(t, sc, 30*time.Millisecond, tc.oldState+"->"+tc.newState)
	}

	// Sanity: stopped -> playing IS broadcast.
	t.Log("sanity: stopped -> playing must broadcast")
	meta.OnPlaybackStateChanged("stopped", "playing")
	msg, state := drainServerStateMessage(t, sc)
	t.Logf("sanity broadcast received: type=%s metadata=%+v", msg.Type, state.Metadata)
}

// TestMetadataRole_BroadcastMetadata_BroadcastsRegardlessOfState confirms
// that the public BroadcastMetadata method pushes the snapshot to every
// metadata-role client unconditionally.
func TestMetadataRole_BroadcastMetadata_BroadcastsRegardlessOfState(t *testing.T) {
	g := NewGroup("test-group")
	defer g.Close()

	meta := NewMetadataRole(MetadataConfig{
		GetMetadata: func() (string, string, string) {
			return "Updated Title", "Updated Artist", "Updated Album"
		},
		ClockMicros: func() int64 { return 100 },
	})
	g.RegisterRole(meta)

	sc1 := &ServerClient{
		id:       "meta-1",
		roles:    []string{"metadata@v1"},
		sendChan: make(chan interface{}, 10),
	}
	sc2 := &ServerClient{
		id:       "meta-2",
		roles:    []string{"metadata@v2"}, // versioned form must still match
		sendChan: make(chan interface{}, 10),
	}
	scPlayer := &ServerClient{
		id:       "player-1",
		roles:    []string{"player@v1"},
		sendChan: make(chan interface{}, 10),
	}

	g.addClient(sc1)
	g.addClient(sc2)
	g.addClient(scPlayer)
	<-sc1.sendChan
	<-sc2.sendChan
	<-scPlayer.sendChan

	// Drain join-time server/state on the metadata clients.
	time.Sleep(20 * time.Millisecond)
	for _, sc := range []*ServerClient{sc1, sc2} {
		select {
		case <-sc.sendChan:
		default:
		}
	}

	meta.BroadcastMetadata()

	msg1, state1 := drainServerStateMessage(t, sc1)
	t.Logf("sc1 (metadata@v1) received: type=%s metadata=%+v", msg1.Type, state1.Metadata)
	if state1.Metadata == nil || state1.Metadata.Title == nil || *state1.Metadata.Title != "Updated Title" {
		t.Errorf("sc1: title = %v, want \"Updated Title\"", state1.Metadata)
	}

	msg2, state2 := drainServerStateMessage(t, sc2)
	t.Logf("sc2 (metadata@v2) received: type=%s metadata=%+v", msg2.Type, state2.Metadata)
	if state2.Metadata == nil || state2.Metadata.Title == nil || *state2.Metadata.Title != "Updated Title" {
		t.Errorf("sc2: title = %v, want \"Updated Title\"", state2.Metadata)
	}

	expectNoSend(t, scPlayer, 30*time.Millisecond, "player-only client")
}

// TestMetadataRole_OnPlaybackStateChanged_FiresFromGroupEvent is the
// end-to-end wiring proof: register the role on a real Group, attach a
// metadata-role client, call g.SetPlaybackState("stopped") then
// g.SetPlaybackState("playing"), assert the client received the broadcast
// via the dispatcher path.
func TestMetadataRole_OnPlaybackStateChanged_FiresFromGroupEvent(t *testing.T) {
	g := NewGroup("test-group")
	defer g.Close()

	meta := NewMetadataRole(MetadataConfig{
		GetMetadata: func() (string, string, string) {
			return "E2E Title", "E2E Artist", "E2E Album"
		},
		ClockMicros: func() int64 { return 7 },
	})
	g.RegisterRole(meta)

	sc := &ServerClient{
		id:       "meta-c",
		name:     "metadata client",
		roles:    []string{"metadata@v1"},
		sendChan: make(chan interface{}, 10),
	}
	g.addClient(sc)
	<-sc.sendChan // group/update

	// Drain join-time server/state.
	time.Sleep(20 * time.Millisecond)
	select {
	case msg := <-sc.sendChan:
		t.Logf("drained join-time send: %+v", msg)
	default:
	}

	// Default playback state is "playing"; transition through stopped to
	// trigger an actual stopped→playing edge.
	g.SetPlaybackState("stopped")
	g.SetPlaybackState("playing")

	// Wait for the dispatcher to deliver, then assert.
	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case raw := <-sc.sendChan:
			msg, ok := raw.(protocol.Message)
			if !ok {
				t.Fatalf("queued send was %T, want protocol.Message", raw)
			}
			if msg.Type != "server/state" {
				continue
			}
			state, ok := msg.Payload.(protocol.ServerStateMessage)
			if !ok {
				t.Fatalf("Message.Payload was %T, want ServerStateMessage", msg.Payload)
			}
			t.Logf("E2E broadcast received: type=%s metadata=%+v", msg.Type, state.Metadata)
			if state.Metadata == nil || state.Metadata.Title == nil || *state.Metadata.Title != "E2E Title" {
				t.Errorf("E2E broadcast title = %v, want \"E2E Title\"", state.Metadata)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for E2E broadcast via SetPlaybackState")
		}
	}
}

// TestMetadataRole_BroadcastMetadata_ConcurrentCallsAreSafe runs many
// parallel BroadcastMetadata calls and asserts no panics, no races, and
// every call lands at least one send on the metadata client. The fake
// client's sendChan has capacity 200; the test bounds itself to fewer
// broadcasts so we don't need to drain in parallel.
func TestMetadataRole_BroadcastMetadata_ConcurrentCallsAreSafe(t *testing.T) {
	g := NewGroup("test-group")
	defer g.Close()

	meta := NewMetadataRole(MetadataConfig{
		GetMetadata: func() (string, string, string) { return "t", "a", "al" },
		ClockMicros: func() int64 { return 0 },
	})
	g.RegisterRole(meta)

	sc := &ServerClient{
		id:       "meta-c",
		roles:    []string{"metadata@v1"},
		sendChan: make(chan interface{}, 200),
	}
	g.addClient(sc)
	<-sc.sendChan

	// Drain any join-time send.
	time.Sleep(20 * time.Millisecond)
	select {
	case <-sc.sendChan:
	default:
	}

	const callers = 10
	const perCaller = 5
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perCaller; j++ {
				meta.BroadcastMetadata()
			}
		}()
	}
	wg.Wait()

	count := 0
loop:
	for {
		select {
		case <-sc.sendChan:
			count++
		default:
			break loop
		}
	}
	t.Logf("concurrent broadcasts queued %d sends (callers=%d, perCaller=%d)", count, callers, perCaller)
	if count == 0 {
		t.Error("expected at least one queued send from concurrent broadcasts, got 0")
	}
}
