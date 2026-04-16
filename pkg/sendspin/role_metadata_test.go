// ABOUTME: Tests for the MetadataGroupRole implementation
// ABOUTME: Verifies metadata state push to joining clients
package sendspin

import (
	"testing"
)

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
