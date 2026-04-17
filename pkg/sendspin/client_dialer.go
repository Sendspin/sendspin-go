// ABOUTME: Outbound client discovery dialer for server-initiated mode
// ABOUTME: Converts discovery.ClientInfo into dialed WebSocket connections

package sendspin

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Sendspin/sendspin-go/internal/discovery"
	"github.com/gorilla/websocket"
)

// clientInfoURL builds a ws:// URL from a discovered ClientInfo.
// Normalizes Path to ensure a single leading slash.
func clientInfoURL(info *discovery.ClientInfo) string {
	path := info.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("ws://%s:%d%s", info.Host, info.Port, path)
}

// dialAndHandle opens a WebSocket to a discovered client and hands the
// connection to the provided handler. The handler is expected to block
// until the connection is fully drained (e.g. handleConnection).
func dialAndHandle(ctx context.Context, info *discovery.ClientInfo, handle func(*websocket.Conn)) error {
	url := clientInfoURL(info)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}

	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", url, err)
	}
	log.Printf("Dialed discovered client %s at %s", info.Name, url)
	handle(conn)
	return nil
}

// dialFunc dials a discovered client and returns when the resulting
// connection has been fully handled. Returning nil means the connection
// completed normally; returning an error means the dial or handshake
// failed and the slot should be released for retry.
type dialFunc func(ctx context.Context, info *discovery.ClientInfo) error

// clientDialer consumes discovery events and dispatches one goroutine
// per unique mDNS instance, deduping so we never open two sockets to
// the same advertisement concurrently.
type clientDialer struct {
	in   <-chan *discovery.ClientInfo
	dial dialFunc

	baseBackoff time.Duration
	maxBackoff  time.Duration

	mu       sync.Mutex
	active   map[string]bool      // currently dialing/connected
	cooldown map[string]time.Time // instance -> earliest retry time
	failures map[string]int       // consecutive failure count per instance
}

// newClientDialer constructs a clientDialer that reads discovery events
// from in and dispatches them through dial.
func newClientDialer(in <-chan *discovery.ClientInfo, dial dialFunc) *clientDialer {
	return &clientDialer{
		in:          in,
		dial:        dial,
		baseBackoff: 1 * time.Second,
		maxBackoff:  30 * time.Second,
		active:      make(map[string]bool),
		cooldown:    make(map[string]time.Time),
		failures:    make(map[string]int),
	}
}

// run pumps discovery events until ctx is cancelled. It returns after
// all in-flight dial goroutines have completed.
func (d *clientDialer) run(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case info, ok := <-d.in:
			if !ok {
				return
			}
			if info == nil {
				continue
			}
			if !d.claim(info.Instance) {
				continue
			}
			wg.Add(1)
			go func(info *discovery.ClientInfo) {
				defer wg.Done()
				err := d.dial(ctx, info)
				d.release(info.Instance, err)
				if err != nil {
					log.Printf("dial client %s: %v", info.Instance, err)
				}
			}(info)
		}
	}
}

// claim returns true if the instance slot was free, not in cooldown,
// and is now owned by the caller. Returns false otherwise.
func (d *clientDialer) claim(instance string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.active[instance] {
		return false
	}
	if until, ok := d.cooldown[instance]; ok && time.Now().Before(until) {
		return false
	}
	d.active[instance] = true
	return true
}

// release updates the instance slot after a dial attempt completes.
// On success (dialErr == nil) the instance stays latched in the active
// set — we never re-dial a successfully-connected instance. On error
// it frees the slot and schedules an exponentially-backed-off cooldown
// before the next retry.
func (d *clientDialer) release(instance string, dialErr error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if dialErr == nil {
		// Latch: keep active[instance] = true so future discovery
		// events for this instance are silently ignored. The server
		// already handled the connection; re-dialing would create a
		// duplicate session.
		delete(d.failures, instance)
		delete(d.cooldown, instance)
		return
	}

	// Dial failed — release the slot so the instance can be retried
	// after the backoff period.
	delete(d.active, instance)
	d.failures[instance]++
	backoff := d.baseBackoff * time.Duration(1<<(d.failures[instance]-1))
	if backoff > d.maxBackoff || backoff <= 0 {
		backoff = d.maxBackoff
	}
	d.cooldown[instance] = time.Now().Add(backoff)
}
