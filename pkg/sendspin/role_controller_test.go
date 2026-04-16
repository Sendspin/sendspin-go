// ABOUTME: Tests for the ControllerGroupRole implementation
// ABOUTME: Verifies command dispatch and controller state push on join
package sendspin

import (
	"encoding/json"
	"testing"
)

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
