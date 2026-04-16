// ABOUTME: Tests for the PlayerGroupRole implementation
// ABOUTME: Verifies codec negotiation, encoder setup, and stream/start on join
package sendspin

import (
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
)

func TestPlayerRole_OnClientJoinSendsStreamStart(t *testing.T) {
	role := NewPlayerRole(PlayerRoleConfig{
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   24,
	})

	sc := &ServerClient{
		id:    "c1",
		roles: []string{"player@v1"},
		capabilities: &protocol.PlayerV1Support{
			SupportedFormats: []protocol.AudioFormat{
				{Codec: "pcm", SampleRate: 48000, Channels: 2, BitDepth: 24},
			},
		},
		sendChan: make(chan interface{}, 10),
	}

	role.OnClientJoin(sc)

	select {
	case <-sc.sendChan:
		// stream/start was sent
	default:
		t.Fatal("OnClientJoin did not send stream/start")
	}

	if sc.Codec() != "pcm" {
		t.Errorf("client codec = %q, want %q", sc.Codec(), "pcm")
	}
}

func TestPlayerRole_OnClientJoinSkipsNonPlayerClient(t *testing.T) {
	role := NewPlayerRole(PlayerRoleConfig{
		SampleRate: 48000,
		Channels:   2,
		BitDepth:   24,
	})

	sc := &ServerClient{
		id:       "c1",
		roles:    []string{"controller@v1"},
		sendChan: make(chan interface{}, 10),
	}

	role.OnClientJoin(sc)

	select {
	case <-sc.sendChan:
		t.Error("should not send stream/start to non-player client")
	default:
	}
}

func TestPlayerRole_Role(t *testing.T) {
	role := NewPlayerRole(PlayerRoleConfig{})
	if role.Role() != "player" {
		t.Errorf("Role() = %q, want %q", role.Role(), "player")
	}
}
