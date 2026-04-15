// ABOUTME: Tests for the Group event bus plumbing
// ABOUTME: Fan-out, unsubscribe, and slow-subscriber drop behavior
package sendspin

import (
	"sync"
	"testing"
	"time"
)

// TestGroup_PublishSubscribe confirms the basic contract: an event
// published after Subscribe is delivered to the subscriber's channel.
func TestGroup_PublishSubscribe(t *testing.T) {
	g := NewGroup("test-group")
	defer g.Close()

	events, unsubscribe := g.Subscribe()
	defer unsubscribe()

	g.publish(ClientJoinedEvent{Client: &ServerClient{id: "c1"}})

	select {
	case evt := <-events:
		joined, ok := evt.(ClientJoinedEvent)
		if !ok {
			t.Fatalf("got %T, want ClientJoinedEvent", evt)
		}
		if joined.Client.ID() != "c1" {
			t.Errorf("Client.ID() = %q, want %q", joined.Client.ID(), "c1")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for published event")
	}
}

// TestGroup_FanOut confirms one publish reaches every active subscriber.
func TestGroup_FanOut(t *testing.T) {
	g := NewGroup("test-group")
	defer g.Close()

	const subscribers = 3
	chans := make([]<-chan Event, subscribers)
	for i := 0; i < subscribers; i++ {
		ch, unsub := g.Subscribe()
		defer unsub()
		chans[i] = ch
	}

	g.publish(ClientLeftEvent{Client: &ServerClient{id: "c-gone"}})

	var wg sync.WaitGroup
	for i, ch := range chans {
		wg.Add(1)
		go func(idx int, c <-chan Event) {
			defer wg.Done()
			select {
			case evt := <-c:
				if _, ok := evt.(ClientLeftEvent); !ok {
					t.Errorf("subscriber %d got %T, want ClientLeftEvent", idx, evt)
				}
			case <-time.After(100 * time.Millisecond):
				t.Errorf("subscriber %d timed out", idx)
			}
		}(i, ch)
	}
	wg.Wait()
}

// TestGroup_Unsubscribe confirms that after calling the unsubscribe func,
// further publishes do NOT reach the channel.
func TestGroup_Unsubscribe(t *testing.T) {
	g := NewGroup("test-group")
	defer g.Close()

	events, unsubscribe := g.Subscribe()

	g.publish(ClientJoinedEvent{Client: &ServerClient{id: "c1"}})
	<-events // drain the first event

	unsubscribe()

	// Publish a second event — the unsubscribed channel should not receive it.
	g.publish(ClientJoinedEvent{Client: &ServerClient{id: "c2"}})

	select {
	case evt, ok := <-events:
		if ok {
			t.Errorf("received event %v after unsubscribe", evt)
		}
		// Channel closed — also acceptable.
	case <-time.After(50 * time.Millisecond):
		// No event arrived within the window — correct behavior.
	}
}

// TestGroup_SlowSubscriberDoesNotBlockPublisher pins the non-blocking
// fan-out contract: a subscriber that never reads must not stall events
// for other subscribers or the publisher itself.
func TestGroup_SlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	g := NewGroup("test-group")
	defer g.Close()

	// slow subscriber — we intentionally never read from this.
	_, _ = g.Subscribe()

	fast, unsubscribeFast := g.Subscribe()
	defer unsubscribeFast()

	// Overflow the slow subscriber's buffer.
	for i := 0; i < 100; i++ {
		g.publish(ClientJoinedEvent{Client: &ServerClient{id: "flood"}})
	}

	// The fast subscriber should still have received events.
	received := 0
	timeout := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case <-fast:
			received++
			if received >= 32 {
				break loop
			}
		case <-timeout:
			break loop
		}
	}
	if received == 0 {
		t.Error("fast subscriber received zero events while slow one blocked")
	}
}

// TestGroup_IDReturnsConstructorValue just pins the GroupID accessor.
func TestGroup_IDReturnsConstructorValue(t *testing.T) {
	g := NewGroup("my-group-id")
	defer g.Close()
	if got := g.ID(); got != "my-group-id" {
		t.Errorf("ID() = %q, want %q", got, "my-group-id")
	}
}
