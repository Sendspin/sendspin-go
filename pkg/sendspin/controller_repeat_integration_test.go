// ABOUTME: End-to-end test for controller repeat/shuffle (spec#81)
// ABOUTME: Real Server + ControllerGroupRole + real Receiver over WebSocket
package sendspin

import (
	"fmt"
	"testing"
	"time"
)

// TestController_RepeatShuffleEndToEnd is the full-stack scenario: a real
// Server with a ControllerGroupRole, a real Receiver connecting over a
// WebSocket, and a SetRepeat/SetShuffle call on the server that must show up
// in the client's merged metadata snapshot. This exercises the spec#81
// controller-state path across both implementations of the wire model in
// this module (server broadcast -> protocol client -> receiver merge).
func TestController_RepeatShuffleEndToEnd(t *testing.T) {
	server, err := NewServer(ServerConfig{
		Port:   0,
		Name:   "Repeat E2E Server",
		Source: NewTestTone(48000, 2),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctrl := NewControllerRole(ControllerConfig{
		SupportedCommands: []string{"repeat_off", "repeat_one", "repeat_all", "shuffle", "unshuffle"},
		ClockMicros:       server.getClockMicros,
	})
	server.Group().RegisterRole(ctrl)

	errChan := make(chan error, 1)
	go func() { errChan <- server.Start() }()
	defer func() {
		server.Stop()
		select {
		case <-errChan:
		case <-time.After(5 * time.Second):
			t.Error("server stop timeout")
		}
	}()

	port := waitForServerPort(t, server)

	mc := &metadataCapture{}
	recv, err := NewReceiver(ReceiverConfig{
		ServerAddr: fmt.Sprintf("localhost:%d", port),
		PlayerName: "Repeat E2E Client",
		ClientID:   "repeat-e2e-client",
		OnMetadata: mc.record,
	})
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	defer recv.Close()

	if err := recv.Connect(); err != nil {
		t.Fatalf("Receiver.Connect: %v", err)
	}

	// Drain decoded audio so the scheduler never blocks on a full channel.
	go func() {
		for range recv.Output() {
		}
	}()

	// Wait until the receiver has joined and we've seen the initial controller
	// state (default repeat "off") before changing it, so the change is the
	// observed transition rather than a race with the join snapshot.
	waitFor(t, 3*time.Second, func() bool {
		got, ok := mc.latest()
		return ok && got.Repeat == "off"
	}, "initial controller state (repeat=off) on join")

	ctrl.SetRepeat("all")
	ctrl.SetShuffle(true)

	waitFor(t, 3*time.Second, func() bool {
		got, ok := mc.latest()
		return ok && got.Repeat == "all" && got.Shuffle
	}, "controller repeat=all + shuffle=true after server SetRepeat/SetShuffle")
}

// waitFor polls cond until it returns true or the timeout elapses, failing
// the test with desc on timeout.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}
