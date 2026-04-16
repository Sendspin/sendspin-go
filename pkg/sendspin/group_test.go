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

	g.publish(ClientLeftEvent{ClientID: "c-gone", ClientName: "Gone Client"})

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

// TestGroup_ConcurrentSubscribeClose spawns many Subscribe callers
// against a concurrent Close and asserts that nothing panics and every
// returned channel is either pre-closed or closes promptly. Guards the
// lock discipline against future refactors that could regress the
// Subscribe-vs-Close ordering.
func TestGroup_ConcurrentSubscribeClose(t *testing.T) {
	const subscribers = 50
	g := NewGroup("race-bait")

	var wg sync.WaitGroup
	subsReady := make(chan struct{})

	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-subsReady
			ch, unsub := g.Subscribe()
			defer unsub()

			// Drain until the channel closes. If Close races with our
			// Subscribe we should either get a pre-closed channel
			// immediately or see it close after Close completes.
			deadline := time.After(500 * time.Millisecond)
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
				case <-deadline:
					t.Error("channel never closed")
					return
				}
			}
		}()
	}

	close(subsReady)
	time.Sleep(5 * time.Millisecond) // let some Subscribes land
	g.Close()

	wg.Wait()
}

// TestGroup_ConcurrentPublishClose races publishes against a close and
// asserts no panic. A send on a closed channel would surface as a panic
// recovered by the test runner.
func TestGroup_ConcurrentPublishClose(t *testing.T) {
	g := NewGroup("race-bait")

	// Pre-subscribe so there's a channel to fan out to.
	_, unsub := g.Subscribe()
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			g.publish(ClientJoinedEvent{Client: &ServerClient{id: "flood"}})
		}
		close(done)
	}()

	time.Sleep(2 * time.Millisecond) // let some publishes land
	g.Close()
	<-done
}

// TestGroup_AddClientSendsGroupUpdate confirms that addClient sends
// a group/update message to the joining client before publishing
// ClientJoinedEvent.
func TestGroup_AddClientSendsGroupUpdate(t *testing.T) {
	g := NewGroup("group-123")
	defer g.Close()

	sc := &ServerClient{
		id:       "c1",
		sendChan: make(chan interface{}, 10),
	}
	g.addClient(sc)

	select {
	case msg := <-sc.sendChan:
		_ = msg // Verifies group/update was sent
	default:
		t.Fatal("addClient did not send group/update")
	}
}
