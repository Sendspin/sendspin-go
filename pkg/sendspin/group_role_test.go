// ABOUTME: Tests for GroupRole interface and role dispatch on Group
// ABOUTME: Verifies RegisterRole, event dispatch, and message routing
package sendspin

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// testRole is a minimal GroupRole implementation for testing.
type testRole struct {
	mu       sync.Mutex
	role     string
	joined   []*ServerClient
	left     []string
	messages []json.RawMessage
}

func (r *testRole) Role() string { return r.role }

func (r *testRole) OnClientJoin(c *ServerClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.joined = append(r.joined, c)
}

func (r *testRole) OnClientLeave(id string, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.left = append(r.left, id)
}

func (r *testRole) HandleMessage(c *ServerClient, payload json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, payload)
	return nil
}

func TestGroup_RegisterRole(t *testing.T) {
	g := NewGroup("test")
	defer g.Close()

	role := &testRole{role: "controller"}
	g.RegisterRole(role)

	got := g.GetRole("controller")
	if got != role {
		t.Errorf("GetRole returned %v, want registered role", got)
	}
	if g.GetRole("nonexistent") != nil {
		t.Error("GetRole for unregistered role should return nil")
	}
}

func TestGroup_RoleDispatchOnJoinLeave(t *testing.T) {
	g := NewGroup("test")
	defer g.Close()

	role := &testRole{role: "player"}
	g.RegisterRole(role)

	sc := &ServerClient{id: "c1", name: "Client 1", roles: []string{"player@v1"}}
	g.addClient(sc)

	// Give the dispatcher goroutine time to process.
	time.Sleep(50 * time.Millisecond)

	role.mu.Lock()
	joinedCount := len(role.joined)
	var joinedID string
	if joinedCount > 0 {
		joinedID = role.joined[0].ID()
	}
	role.mu.Unlock()

	if joinedCount != 1 || joinedID != "c1" {
		t.Errorf("OnClientJoin: got %d calls, want 1 with id=c1", joinedCount)
	}

	g.removeClient(sc)
	time.Sleep(50 * time.Millisecond)

	role.mu.Lock()
	leftCount := len(role.left)
	var leftID string
	if leftCount > 0 {
		leftID = role.left[0]
	}
	role.mu.Unlock()

	if leftCount != 1 || leftID != "c1" {
		t.Errorf("OnClientLeave: got %d calls, want 1 with id=c1", leftCount)
	}
}

func TestGroup_RouteMessage(t *testing.T) {
	g := NewGroup("test")
	defer g.Close()

	role := &testRole{role: "controller"}
	g.RegisterRole(role)

	sc := &ServerClient{id: "c1"}
	payload := json.RawMessage(`{"command":"next"}`)

	err := g.RouteMessage(sc, "controller", payload)
	if err != nil {
		t.Fatalf("RouteMessage: %v", err)
	}
	if len(role.messages) != 1 {
		t.Fatalf("HandleMessage called %d times, want 1", len(role.messages))
	}
}

func TestGroup_RouteMessageNoHandler(t *testing.T) {
	g := NewGroup("test")
	defer g.Close()

	err := g.RouteMessage(&ServerClient{id: "c1"}, "nonexistent", json.RawMessage(`{}`))
	if err == nil {
		t.Error("RouteMessage to unregistered role should return error")
	}
}

// roleWithoutMessageHandler implements GroupRole but NOT MessageHandler.
type roleWithoutMessageHandler struct {
	role string
}

func (r *roleWithoutMessageHandler) Role() string                         { return r.role }
func (r *roleWithoutMessageHandler) OnClientJoin(c *ServerClient)         {}
func (r *roleWithoutMessageHandler) OnClientLeave(id string, name string) {}

func TestGroup_RouteMessageRoleWithoutHandler(t *testing.T) {
	g := NewGroup("test")
	defer g.Close()

	g.RegisterRole(&roleWithoutMessageHandler{role: "metadata"})

	err := g.RouteMessage(&ServerClient{id: "c1"}, "metadata", json.RawMessage(`{}`))
	if err == nil {
		t.Error("RouteMessage to role without MessageHandler should return error")
	}
}
