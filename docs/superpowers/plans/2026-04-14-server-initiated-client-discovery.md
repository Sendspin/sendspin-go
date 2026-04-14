# Server-Initiated Client Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a Sendspin server browse `_sendspin._tcp.local.` mDNS advertisements, dial out to each discovered player via WebSocket, and funnel the resulting connections into the existing `handleConnection` code path with zero changes below the transport layer.

**Architecture:** A new `ClientDialer` component owned by `pkg/sendspin.Server` subscribes to a new `discovery.Manager.BrowseClients()` stream, dedupes by mDNS instance name, dials each advertised `ws://host:port{path}` endpoint with exponential backoff, and hands the upgraded `*websocket.Conn` to `s.handleConnection` — the same entry point inbound connections already use. Dedupe-by-`ClientID` at the protocol layer (`server.go:394`-ish) provides a second safety net for races against inbound connections.

**Tech Stack:** Go 1.24, `github.com/hashicorp/mdns` v1.0.6 (browsing + TXT parsing), `github.com/gorilla/websocket` v1.5.3 (dialing), standard library `testing`.

---

## Spec Reference

From https://www.sendspin-audio.com/spec/ (server-initiated discovery mode):

- Service type: `_sendspin._tcp.local.` (note: client-advertised, not server-advertised)
- Recommended port: `8928`
- TXT records: `path=<ws endpoint>` (typically `/sendspin`), `name=<friendly name>` (optional)
- After dial: player still sends `client/hello` first; server responds `server/hello`
- Mutual exclusion is per-client: a player must NOT both advertise `_sendspin._tcp` AND manually connect to a server

The server side we're building can freely run both modes in parallel — dedupe handles collisions.

---

## File Structure

**Modified:**
- `internal/discovery/mdns.go` — add `ClientInfo`, `BrowseClients()`, TXT parsing helper, `parseClientEntry()`
- `internal/discovery/mdns_test.go` — add tests for TXT parsing and entry conversion
- `pkg/sendspin/server.go` — add `DiscoverClients` config field, wire `ClientDialer` in `Start()` / `Stop()`
- `pkg/sendspin/server_test.go` — add integration test with a fake mDNS advertiser
- `cmd/sendspin-server/main.go` — add `-discover-clients` flag

**Created:**
- `pkg/sendspin/client_dialer.go` — `ClientDialer` struct, goroutine loop, backoff, instance-name dedupe
- `pkg/sendspin/client_dialer_test.go` — unit tests with injected dial func
- `internal/discovery/txt.go` — pure TXT parsing helper (no mDNS dependency, easier to test)
- `internal/discovery/txt_test.go` — table-driven tests for TXT parsing

Each file has one responsibility: discovery emits `ClientInfo`, the dialer turns `ClientInfo` into `*websocket.Conn`, and `handleConnection` remains untouched.

---

## Task 1: TXT Record Parsing Helper

**Files:**
- Create: `internal/discovery/txt.go`
- Create: `internal/discovery/txt_test.go`

Why a standalone file: `hashicorp/mdns` stores TXT records as `[]string` of raw `key=value` entries in `ServiceEntry.InfoFields`. Parsing is a pure function with no mDNS types, so we isolate and test it in isolation before touching the browser loop.

- [ ] **Step 1: Write failing tests for `parseTXT`**

```go
// internal/discovery/txt_test.go
// ABOUTME: Tests for TXT record parsing
// ABOUTME: Pure parser, no mDNS dependency

package discovery

import (
	"reflect"
	"testing"
)

func TestParseTXT(t *testing.T) {
	tests := []struct {
		name   string
		fields []string
		want   map[string]string
	}{
		{
			name:   "empty",
			fields: nil,
			want:   map[string]string{},
		},
		{
			name:   "single key",
			fields: []string{"path=/sendspin"},
			want:   map[string]string{"path": "/sendspin"},
		},
		{
			name:   "multiple keys",
			fields: []string{"path=/sendspin", "name=Living Room"},
			want:   map[string]string{"path": "/sendspin", "name": "Living Room"},
		},
		{
			name:   "key without value",
			fields: []string{"flag"},
			want:   map[string]string{"flag": ""},
		},
		{
			name:   "value contains equals",
			fields: []string{"token=abc=def=ghi"},
			want:   map[string]string{"token": "abc=def=ghi"},
		},
		{
			name:   "empty string ignored",
			fields: []string{"", "path=/sendspin"},
			want:   map[string]string{"path": "/sendspin"},
		},
		{
			name:   "duplicate key: last wins",
			fields: []string{"path=/old", "path=/new"},
			want:   map[string]string{"path": "/new"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTXT(tt.fields)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTXT(%v) = %v, want %v", tt.fields, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -run TestParseTXT -v`
Expected: FAIL with `undefined: parseTXT`.

- [ ] **Step 3: Implement `parseTXT`**

```go
// internal/discovery/txt.go
// ABOUTME: Pure TXT record parsing for mDNS service entries
// ABOUTME: Converts []string of "key=value" entries to map[string]string

package discovery

import "strings"

// parseTXT converts an mDNS TXT record slice (each element "key=value")
// into a map. Empty strings are ignored. Keys without '=' are stored
// with an empty value. When a key appears multiple times, the last
// occurrence wins.
func parseTXT(fields []string) map[string]string {
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		if f == "" {
			continue
		}
		if idx := strings.Index(f, "="); idx >= 0 {
			out[f[:idx]] = f[idx+1:]
		} else {
			out[f] = ""
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/discovery/ -run TestParseTXT -v`
Expected: PASS for all 7 subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/txt.go internal/discovery/txt_test.go
git commit -m "feat(discovery): add TXT record parsing helper"
```

---

## Task 2: ClientInfo Type and Entry Conversion

**Files:**
- Modify: `internal/discovery/mdns.go` (add types + function near `ServerInfo` at lines 33-38)
- Modify: `internal/discovery/mdns_test.go` (add tests)

Why separate from Task 3: converting a `*mdns.ServiceEntry` into a `ClientInfo` is pure logic that we can test without spinning up an mDNS loop. Isolating it keeps the browse goroutine in Task 3 trivial.

- [ ] **Step 1: Write failing test for `clientInfoFromEntry`**

Add to `internal/discovery/mdns_test.go`:

```go
import (
	"net"
	"testing"

	"github.com/hashicorp/mdns"
)

func TestClientInfoFromEntry(t *testing.T) {
	tests := []struct {
		name  string
		entry *mdns.ServiceEntry
		want  *ClientInfo
	}{
		{
			name: "full entry with path and name",
			entry: &mdns.ServiceEntry{
				Name:       "living-room._sendspin._tcp.local.",
				Host:       "living-room.local.",
				AddrV4:     net.ParseIP("192.168.1.42"),
				Port:       8928,
				InfoFields: []string{"path=/sendspin", "name=Living Room"},
			},
			want: &ClientInfo{
				Instance: "living-room._sendspin._tcp.local.",
				Name:     "Living Room",
				Host:     "192.168.1.42",
				Port:     8928,
				Path:     "/sendspin",
			},
		},
		{
			name: "missing path defaults to /sendspin",
			entry: &mdns.ServiceEntry{
				Name:       "kitchen._sendspin._tcp.local.",
				AddrV4:     net.ParseIP("192.168.1.43"),
				Port:       8928,
				InfoFields: []string{"name=Kitchen"},
			},
			want: &ClientInfo{
				Instance: "kitchen._sendspin._tcp.local.",
				Name:     "Kitchen",
				Host:     "192.168.1.43",
				Port:     8928,
				Path:     "/sendspin",
			},
		},
		{
			name: "missing name falls back to entry.Name",
			entry: &mdns.ServiceEntry{
				Name:       "bedroom._sendspin._tcp.local.",
				AddrV4:     net.ParseIP("192.168.1.44"),
				Port:       8928,
				InfoFields: []string{"path=/sendspin"},
			},
			want: &ClientInfo{
				Instance: "bedroom._sendspin._tcp.local.",
				Name:     "bedroom._sendspin._tcp.local.",
				Host:     "192.168.1.44",
				Port:     8928,
				Path:     "/sendspin",
			},
		},
		{
			name: "no IPv4 address returns nil",
			entry: &mdns.ServiceEntry{
				Name:       "ipv6-only._sendspin._tcp.local.",
				Port:       8928,
				InfoFields: []string{"path=/sendspin"},
			},
			want: nil,
		},
		{
			name: "zero port returns nil",
			entry: &mdns.ServiceEntry{
				Name:       "bad._sendspin._tcp.local.",
				AddrV4:     net.ParseIP("192.168.1.45"),
				Port:       0,
				InfoFields: []string{"path=/sendspin"},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clientInfoFromEntry(tt.entry)
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("clientInfoFromEntry nil-ness mismatch: got %+v, want %+v", got, tt.want)
			}
			if got == nil {
				return
			}
			if *got != *tt.want {
				t.Errorf("clientInfoFromEntry = %+v, want %+v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -run TestClientInfoFromEntry -v`
Expected: FAIL with `undefined: ClientInfo` and `undefined: clientInfoFromEntry`.

- [ ] **Step 3: Add `ClientInfo` and `clientInfoFromEntry` to `mdns.go`**

Insert after the `ServerInfo` struct (currently `internal/discovery/mdns.go:33-38`):

```go
// ClientInfo describes a discovered client (player) advertised via
// _sendspin._tcp.local.
type ClientInfo struct {
	Instance string // fully-qualified mDNS instance name (stable dedupe key)
	Name     string // friendly name from TXT "name=" or falls back to Instance
	Host     string // IPv4 address as a string
	Port     int
	Path     string // WebSocket path from TXT "path=" (default "/sendspin")
}

// clientInfoFromEntry converts an mdns.ServiceEntry into a ClientInfo.
// Returns nil when the entry lacks a usable IPv4 address or port.
func clientInfoFromEntry(entry *mdns.ServiceEntry) *ClientInfo {
	if entry == nil || entry.AddrV4 == nil || entry.Port == 0 {
		return nil
	}
	txt := parseTXT(entry.InfoFields)

	path := txt["path"]
	if path == "" {
		path = "/sendspin"
	}
	name := txt["name"]
	if name == "" {
		name = entry.Name
	}

	return &ClientInfo{
		Instance: entry.Name,
		Name:     name,
		Host:     entry.AddrV4.String(),
		Port:     entry.Port,
		Path:     path,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/discovery/ -run TestClientInfoFromEntry -v`
Expected: PASS for all 5 subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/mdns.go internal/discovery/mdns_test.go
git commit -m "feat(discovery): add ClientInfo type and entry conversion"
```

---

## Task 3: BrowseClients Method

**Files:**
- Modify: `internal/discovery/mdns.go` (add channel field, method, loop)
- Modify: `internal/discovery/mdns_test.go` (add sanity test that the channel is exposed)

- [ ] **Step 1: Write failing test that `Clients()` returns a readable channel**

Add to `internal/discovery/mdns_test.go`:

```go
func TestManagerExposesClientsChannel(t *testing.T) {
	mgr := NewManager(Config{ServiceName: "Test", Port: 8928})
	ch := mgr.Clients()
	if ch == nil {
		t.Fatal("Clients() returned nil channel")
	}
	// channel must be receive-only or bidirectional — just confirm we can select on it
	select {
	case <-ch:
		t.Fatal("unexpected value on empty clients channel")
	default:
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -run TestManagerExposesClientsChannel -v`
Expected: FAIL with `mgr.Clients undefined`.

- [ ] **Step 3: Add `clients` channel and `Clients()` / `BrowseClients()` methods**

In `internal/discovery/mdns.go`, modify the `Manager` struct (currently lines 26-31) to add a clients channel:

```go
type Manager struct {
	config  Config
	ctx     context.Context
	cancel  context.CancelFunc
	servers chan *ServerInfo
	clients chan *ClientInfo
}
```

Update `NewManager` (currently lines 41-50) to initialize the new channel:

```go
func NewManager(config Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		config:  config,
		ctx:     ctx,
		cancel:  cancel,
		servers: make(chan *ServerInfo, 10),
		clients: make(chan *ClientInfo, 10),
	}
}
```

Add these methods at the bottom of `mdns.go` (before `getLocalIPs`):

```go
// BrowseClients searches for Sendspin clients advertising _sendspin._tcp.
func (m *Manager) BrowseClients() error {
	go m.browseClientsLoop()
	return nil
}

// Clients returns the channel of discovered clients.
func (m *Manager) Clients() <-chan *ClientInfo {
	return m.clients
}

// browseClientsLoop continuously browses for clients.
func (m *Manager) browseClientsLoop() {
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		entries := make(chan *mdns.ServiceEntry, 10)

		go func() {
			for entry := range entries {
				info := clientInfoFromEntry(entry)
				if info == nil {
					continue
				}
				log.Printf("Discovered client: %s at %s:%d%s",
					info.Name, info.Host, info.Port, info.Path)
				select {
				case m.clients <- info:
				case <-m.ctx.Done():
					return
				}
			}
		}()

		params := &mdns.QueryParam{
			Service: "_sendspin._tcp",
			Domain:  "local",
			Timeout: 3,
			Entries: entries,
			Logger:  silentLogger,
		}

		if err := mdns.Query(params); err != nil {
			log.Printf("mdns query error: %v", err)
		}
		close(entries)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/discovery/ -v`
Expected: PASS for all discovery tests including the new `TestManagerExposesClientsChannel`.

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/mdns.go internal/discovery/mdns_test.go
git commit -m "feat(discovery): browse for clients advertising _sendspin._tcp"
```

---

## Task 4: WebSocket Dial Helper

**Files:**
- Create: `pkg/sendspin/client_dialer.go` (just the helper function for now)
- Create: `pkg/sendspin/client_dialer_test.go`

Why a thin helper before the full dialer: we want one place that turns a `discovery.ClientInfo` into a dialed URL string, so the tests for URL construction don't need a real network or an mDNS stack.

- [ ] **Step 1: Write failing test for `clientInfoURL`**

```go
// pkg/sendspin/client_dialer_test.go
// ABOUTME: Tests for outbound client discovery and dialing
// ABOUTME: Uses injected dial functions — no real network I/O

package sendspin

import (
	"testing"

	"github.com/Sendspin/sendspin-go/internal/discovery"
)

func TestClientInfoURL(t *testing.T) {
	tests := []struct {
		name string
		info *discovery.ClientInfo
		want string
	}{
		{
			name: "ipv4 host",
			info: &discovery.ClientInfo{Host: "192.168.1.42", Port: 8928, Path: "/sendspin"},
			want: "ws://192.168.1.42:8928/sendspin",
		},
		{
			name: "path missing leading slash is normalized",
			info: &discovery.ClientInfo{Host: "192.168.1.42", Port: 8928, Path: "sendspin"},
			want: "ws://192.168.1.42:8928/sendspin",
		},
		{
			name: "custom path preserved",
			info: &discovery.ClientInfo{Host: "10.0.0.5", Port: 9000, Path: "/custom/endpoint"},
			want: "ws://10.0.0.5:9000/custom/endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clientInfoURL(tt.info)
			if got != tt.want {
				t.Errorf("clientInfoURL = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/sendspin/ -run TestClientInfoURL -v`
Expected: FAIL with `undefined: clientInfoURL`.

- [ ] **Step 3: Create `client_dialer.go` with the helper**

```go
// pkg/sendspin/client_dialer.go
// ABOUTME: Outbound client discovery dialer for server-initiated mode
// ABOUTME: Converts discovery.ClientInfo into dialed WebSocket connections

package sendspin

import (
	"fmt"
	"strings"

	"github.com/Sendspin/sendspin-go/internal/discovery"
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/sendspin/ -run TestClientInfoURL -v`
Expected: PASS for all 3 subtests.

- [ ] **Step 5: Commit**

```bash
git add pkg/sendspin/client_dialer.go pkg/sendspin/client_dialer_test.go
git commit -m "feat(sendspin): add URL builder for discovered client dialing"
```

---

## Task 5: ClientDialer Struct with Instance Dedupe

**Files:**
- Modify: `pkg/sendspin/client_dialer.go`
- Modify: `pkg/sendspin/client_dialer_test.go`

The dialer owns a goroutine that reads `*discovery.ClientInfo` from an input channel and calls a `dialFunc` for each new instance. The dedupe map is keyed by `Instance` (the mDNS fully-qualified name — stable across browse cycles). We inject the dial function so tests never touch the network.

- [ ] **Step 1: Write failing test for dedupe behavior**

Add to `pkg/sendspin/client_dialer_test.go`:

```go
import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

func TestClientDialerDedupesByInstance(t *testing.T) {
	in := make(chan *discovery.ClientInfo, 10)

	var dialCalls int32
	var mu sync.Mutex
	var seen []string

	dial := func(ctx context.Context, info *discovery.ClientInfo) error {
		atomic.AddInt32(&dialCalls, 1)
		mu.Lock()
		seen = append(seen, info.Instance)
		mu.Unlock()
		return nil // success — slot stays occupied
	}

	d := newClientDialer(in, dial)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		d.run(ctx)
		close(done)
	}()

	// Emit the same instance 3 times and a different instance once
	in <- &discovery.ClientInfo{Instance: "a._sendspin._tcp.local.", Host: "1.1.1.1", Port: 8928, Path: "/sendspin"}
	in <- &discovery.ClientInfo{Instance: "a._sendspin._tcp.local.", Host: "1.1.1.1", Port: 8928, Path: "/sendspin"}
	in <- &discovery.ClientInfo{Instance: "b._sendspin._tcp.local.", Host: "1.1.1.2", Port: 8928, Path: "/sendspin"}
	in <- &discovery.ClientInfo{Instance: "a._sendspin._tcp.local.", Host: "1.1.1.1", Port: 8928, Path: "/sendspin"}

	// Wait for the dialer to drain
	deadline := time.After(500 * time.Millisecond)
	for {
		if atomic.LoadInt32(&dialCalls) >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for dial calls, got %d", atomic.LoadInt32(&dialCalls))
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Give any extra (incorrect) calls a chance to fire
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	if got := atomic.LoadInt32(&dialCalls); got != 2 {
		t.Errorf("dialCalls = %d, want 2 (one per unique instance)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("seen = %v, want 2 entries", seen)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/sendspin/ -run TestClientDialerDedupesByInstance -v`
Expected: FAIL with `undefined: newClientDialer`.

- [ ] **Step 3: Add `clientDialer` struct and `run` loop to `client_dialer.go`**

Append to `pkg/sendspin/client_dialer.go`:

```go
import (
	"context"
	"log"
	"sync"
)

// dialFunc dials a discovered client and returns when the resulting
// connection has been fully handled (e.g. handleConnection returned).
// Returning nil means the connection completed normally; returning an
// error means the dial or handshake failed and the slot should be
// released for retry.
type dialFunc func(ctx context.Context, info *discovery.ClientInfo) error

// clientDialer consumes discovery events and dispatches one goroutine
// per unique mDNS instance, deduping so we never open two sockets to
// the same advertisement concurrently.
type clientDialer struct {
	in   <-chan *discovery.ClientInfo
	dial dialFunc

	mu     sync.Mutex
	active map[string]bool // instance name -> currently dialing/connected
}

func newClientDialer(in <-chan *discovery.ClientInfo, dial dialFunc) *clientDialer {
	return &clientDialer{
		in:     in,
		dial:   dial,
		active: make(map[string]bool),
	}
}

// run pumps discovery events until ctx is cancelled.
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
				defer d.release(info.Instance)
				if err := d.dial(ctx, info); err != nil {
					log.Printf("dial client %s: %v", info.Instance, err)
				}
			}(info)
		}
	}
}

// claim returns true if the instance slot was free and is now owned by
// the caller. Returns false if another goroutine already owns it.
func (d *clientDialer) claim(instance string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.active[instance] {
		return false
	}
	d.active[instance] = true
	return true
}

func (d *clientDialer) release(instance string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.active, instance)
}
```

Note: the existing top of the file will now need its imports consolidated. After editing, the import block should read:

```go
import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/Sendspin/sendspin-go/internal/discovery"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/sendspin/ -run TestClientDialer -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/sendspin/client_dialer.go pkg/sendspin/client_dialer_test.go
git commit -m "feat(sendspin): add clientDialer with instance-name dedupe"
```

---

## Task 6: Exponential Backoff on Dial Failures

**Files:**
- Modify: `pkg/sendspin/client_dialer.go`
- Modify: `pkg/sendspin/client_dialer_test.go`

When a dial fails, we want to back off before accepting another discovery event for the same instance. The backoff is tracked per-instance and resets after a successful dial.

- [ ] **Step 1: Write failing test for backoff**

Add to `client_dialer_test.go`:

```go
import "errors"

func TestClientDialerBackoffOnFailure(t *testing.T) {
	in := make(chan *discovery.ClientInfo, 10)

	var dialCalls int32
	dial := func(ctx context.Context, info *discovery.ClientInfo) error {
		atomic.AddInt32(&dialCalls, 1)
		return errors.New("boom")
	}

	d := newClientDialer(in, dial)
	// Override backoff clock to keep the test fast
	d.baseBackoff = 20 * time.Millisecond
	d.maxBackoff = 80 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		d.run(ctx)
		close(done)
	}()

	info := &discovery.ClientInfo{Instance: "a._sendspin._tcp.local.", Host: "1.1.1.1", Port: 8928, Path: "/sendspin"}

	// First event — should dial immediately
	in <- info

	// Wait for first dial
	waitForCalls(t, &dialCalls, 1, 200*time.Millisecond)

	// Immediately re-emit — should be rejected by backoff
	in <- info
	time.Sleep(5 * time.Millisecond)
	if got := atomic.LoadInt32(&dialCalls); got != 1 {
		t.Errorf("dialCalls after immediate retry = %d, want 1 (blocked by backoff)", got)
	}

	// Wait past base backoff, re-emit — should dial again
	time.Sleep(30 * time.Millisecond)
	in <- info
	waitForCalls(t, &dialCalls, 2, 200*time.Millisecond)

	cancel()
	<-done
}

func waitForCalls(t *testing.T, counter *int32, want int32, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(counter) >= want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d calls, got %d", want, atomic.LoadInt32(counter))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/sendspin/ -run TestClientDialerBackoff -v`
Expected: FAIL with `d.baseBackoff undefined` (or the test blows past its assertion because backoff isn't implemented).

- [ ] **Step 3: Add backoff state and logic**

Modify the `clientDialer` struct in `pkg/sendspin/client_dialer.go`:

```go
type clientDialer struct {
	in   <-chan *discovery.ClientInfo
	dial dialFunc

	baseBackoff time.Duration
	maxBackoff  time.Duration

	mu      sync.Mutex
	active  map[string]bool          // currently dialing/connected
	cooldown map[string]time.Time    // instance -> earliest retry time
	failures map[string]int          // consecutive failure count per instance
}
```

Update the constructor:

```go
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
```

Add `"time"` to the imports.

Replace `claim` with a version that checks cooldown:

```go
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
```

Replace `release` with a version that records failures and sets cooldown:

```go
func (d *clientDialer) release(instance string, dialErr error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.active, instance)

	if dialErr == nil {
		delete(d.failures, instance)
		delete(d.cooldown, instance)
		return
	}

	d.failures[instance]++
	backoff := d.baseBackoff * time.Duration(1<<(d.failures[instance]-1))
	if backoff > d.maxBackoff {
		backoff = d.maxBackoff
	}
	d.cooldown[instance] = time.Now().Add(backoff)
}
```

Update the `run` goroutine to pass the error into `release`:

```go
wg.Add(1)
go func(info *discovery.ClientInfo) {
	defer wg.Done()
	err := d.dial(ctx, info)
	d.release(info.Instance, err)
	if err != nil {
		log.Printf("dial client %s: %v", info.Instance, err)
	}
}(info)
```

Important: the Task 5 test `TestClientDialerDedupesByInstance` calls `d.release(instance)` implicitly via the `run` loop. That test used a `dial` that returns `nil`, so with the new signature it still works — just update the goroutine call site once.

- [ ] **Step 4: Run all dialer tests to verify they pass**

Run: `go test ./pkg/sendspin/ -run TestClientDialer -v`
Expected: PASS for `TestClientDialerDedupesByInstance`, `TestClientDialerBackoffOnFailure`, `TestClientInfoURL`.

- [ ] **Step 5: Commit**

```bash
git add pkg/sendspin/client_dialer.go pkg/sendspin/client_dialer_test.go
git commit -m "feat(sendspin): add exponential backoff on dial failures"
```

---

## Task 7: Real Dial Function + Integration with handleConnection

**Files:**
- Modify: `pkg/sendspin/client_dialer.go` (add real websocket dial)
- Modify: `pkg/sendspin/server.go` (wire the dialer into `Start`/`Stop`)

The real dial function opens a WebSocket, then calls `s.handleConnection(conn)` synchronously. When `handleConnection` returns, the dial function returns nil (handled normally). Dial failures return an error so backoff kicks in.

- [ ] **Step 1: Add `DiscoverClients` config field**

Modify `pkg/sendspin/server.go` `ServerConfig` (currently lines 43-58):

```go
type ServerConfig struct {
	Port       int
	Name       string
	Source     AudioSource
	EnableMDNS bool
	Debug      bool

	// DiscoverClients enables server-initiated discovery: browse for
	// clients advertising _sendspin._tcp and dial out to them.
	// See https://www.sendspin-audio.com/spec/ — "server-initiated" mode.
	DiscoverClients bool
}
```

Also add a field to the `Server` struct (currently lines 61-92) to hold the dialer's cancel:

```go
// client dialer (server-initiated discovery)
dialerCancel context.CancelFunc
```

- [ ] **Step 2: Add the real dial function and `dialClient` helper to `client_dialer.go`**

Append to `pkg/sendspin/client_dialer.go`:

```go
import "github.com/gorilla/websocket"

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
```

Consolidated import block for `client_dialer.go`:

```go
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
```

- [ ] **Step 3: Wire the dialer into `Server.Start()`**

In `pkg/sendspin/server.go`, inside `Start()`, directly after the existing mDNS advertisement block (currently `server.go:172-185`), add:

```go
if s.config.DiscoverClients {
	if s.mdnsManager == nil {
		// If mDNS isn't running for advertising, start a manager just for browsing.
		s.mdnsManager = discovery.NewManager(discovery.Config{
			ServiceName: s.config.Name,
			Port:        s.config.Port,
			ServerMode:  true,
		})
	}

	if err := s.mdnsManager.BrowseClients(); err != nil {
		log.Printf("Failed to start client discovery: %v", err)
	} else {
		log.Printf("Browsing for clients advertising _sendspin._tcp")

		dialCtx, cancel := context.WithCancel(context.Background())
		s.dialerCancel = cancel

		dialer := newClientDialer(s.mdnsManager.Clients(), func(ctx context.Context, info *discovery.ClientInfo) error {
			return dialAndHandle(ctx, info, s.handleConnection)
		})

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			dialer.run(dialCtx)
		}()
	}
}
```

Add `"context"` to the `pkg/sendspin/server.go` imports if it isn't already present.

- [ ] **Step 4: Wire the dialer shutdown into `Server.Stop()` / shutdown path**

Find the shutdown sequence in `pkg/sendspin/server.go` (look for `s.mdnsManager.Stop()`). Directly before the existing `if s.mdnsManager != nil { s.mdnsManager.Stop() }` call, add:

```go
if s.dialerCancel != nil {
	s.dialerCancel()
}
```

This guarantees in-flight dials see context cancellation before the mDNS manager stops emitting.

- [ ] **Step 5: Run full test suite to verify nothing broke**

Run: `go test ./...`
Expected: PASS. No new tests in this task — the new wiring is exercised by the integration test in Task 9.

- [ ] **Step 6: Commit**

```bash
git add pkg/sendspin/client_dialer.go pkg/sendspin/server.go
git commit -m "feat(sendspin): wire ClientDialer into server start/stop"
```

---

## Task 8: CLI Flag

**Files:**
- Modify: `cmd/sendspin-server/main.go`

- [ ] **Step 1: Add `-discover-clients` flag**

In `cmd/sendspin-server/main.go`, add alongside the existing flag declarations (currently lines 20-26):

```go
discoverClients = flag.Bool("discover-clients", false,
	"Enable server-initiated discovery: browse _sendspin._tcp and dial out to clients")
```

- [ ] **Step 2: Pass the flag into `ServerConfig`**

Modify the `sendspin.NewServer(sendspin.ServerConfig{...})` call (currently lines 73-79) to include the new field:

```go
srv, err := sendspin.NewServer(sendspin.ServerConfig{
	Port:            *port,
	Name:            serverName,
	Source:          source,
	EnableMDNS:      !*noMDNS,
	Debug:           *debug,
	DiscoverClients: *discoverClients,
})
```

- [ ] **Step 3: Build to verify the CLI compiles**

Run: `make server`
Expected: build succeeds, `bin/sendspin-server` (or equivalent) produced.

- [ ] **Step 4: Smoke test the flag is wired**

Run: `./bin/sendspin-server -h 2>&1 | grep discover-clients`
Expected: output shows `-discover-clients` flag line.

- [ ] **Step 5: Commit**

```bash
git add cmd/sendspin-server/main.go
git commit -m "feat(cli): add -discover-clients flag to sendspin-server"
```

---

## Task 9: End-to-End Integration Test

**Files:**
- Create: `pkg/sendspin/client_discovery_integration_test.go`

This test spins up a fake mDNS advertiser for `_sendspin._tcp` pointing at a `httptest.Server` that upgrades to WebSocket and sends a `client/hello`. With `DiscoverClients: true`, the server should dial out, complete the handshake, and register the player in `s.clients`.

- [ ] **Step 1: Write failing end-to-end test**

```go
// pkg/sendspin/client_discovery_integration_test.go
// ABOUTME: Integration test for server-initiated client discovery
// ABOUTME: Fakes an mDNS advertisement and verifies the server dials + handshakes

package sendspin

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/mdns"
)

func TestServerDiscoversAndDialsClient(t *testing.T) {
	// 1. Stand up a fake "player" HTTP server that upgrades to WS and
	// sends client/hello.
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	playerReady := make(chan string, 1) // receives serverID via server/hello

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		hello := protocol.Message{
			Type: "client/hello",
			Payload: protocol.ClientHello{
				ClientID:       "test-player-1",
				Name:           "Test Player",
				SupportedRoles: []string{"player@v1"},
			},
		}
		data, _ := json.Marshal(hello)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			t.Errorf("write hello: %v", err)
			return
		}

		_, resp, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg protocol.Message
		if err := json.Unmarshal(resp, &msg); err != nil {
			return
		}
		if msg.Type == "server/hello" {
			playerReady <- "ok"
		}

		// Block until the server closes us
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatalf("split host: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	// 2. Advertise an mDNS _sendspin._tcp record pointing at httptest.
	ips := []net.IP{net.ParseIP("127.0.0.1")}
	svc, err := mdns.NewMDNSService(
		"test-player-instance",
		"_sendspin._tcp",
		"",
		"",
		port,
		ips,
		[]string{"path=/", "name=Test Player"},
	)
	if err != nil {
		t.Fatalf("mdns service: %v", err)
	}
	mdnsServer, err := mdns.NewServer(&mdns.Config{Zone: svc})
	if err != nil {
		t.Fatalf("mdns server: %v", err)
	}
	defer mdnsServer.Shutdown()

	// 3. Start a Sendspin server with DiscoverClients=true.
	source := NewTestTone(48000, 2)
	srv, err := NewServer(ServerConfig{
		Port:            findFreePort(t),
		Name:            "Test Server",
		Source:          source,
		EnableMDNS:      false,
		DiscoverClients: true,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	serverDone := make(chan error, 1)
	go func() { serverDone <- srv.Start() }()
	defer srv.Stop()

	// 4. Wait for the player to confirm it received server/hello.
	select {
	case <-playerReady:
		// success
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for dialed handshake")
	}
}

// findFreePort returns a TCP port that is free at call time.
func findFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

```

Note: `NewTestTone` is the existing public test fixture used throughout `pkg/sendspin/server_test.go` (see e.g. line 16). It implements `AudioSource` with a sine test tone and is the same helper the other integration tests use.

- [ ] **Step 2: Run the test to verify it fails if wiring is broken**

Run: `go test ./pkg/sendspin/ -run TestServerDiscoversAndDialsClient -v -timeout 30s`
Expected (if previous tasks are implemented correctly): PASS. If it fails, the failure points at real wiring bugs — fix them in the appropriate earlier task and re-run.

If mDNS doesn't work reliably in the CI sandbox (some environments block multicast), mark the test with `t.Skip("requires multicast")` behind an env var gate like `if os.Getenv("SENDSPIN_MULTICAST_TESTS") == "" { t.Skip(...) }`. Leave the gate off by default so local developers running `go test ./...` aren't blocked by environment issues.

- [ ] **Step 3: Run the full suite**

Run: `go test ./... -timeout 60s`
Expected: PASS.

- [ ] **Step 4: Run lint**

Run: `make lint`
Expected: no new warnings.

- [ ] **Step 5: Commit**

```bash
git add pkg/sendspin/client_discovery_integration_test.go
git commit -m "test(sendspin): end-to-end test for server-initiated discovery"
```

---

## Task 10: Documentation Update

**Files:**
- Modify: `README.md` (if it mentions server modes)
- Modify: `pkg/sendspin/doc.go` (package doc)

- [ ] **Step 1: Add a short section to `pkg/sendspin/doc.go`**

Locate the existing package doc and append a paragraph:

```go
// Server-Initiated Discovery
//
// When ServerConfig.DiscoverClients is true, the server browses mDNS
// for clients advertising _sendspin._tcp.local. and dials each one
// using the advertised path (TXT "path=", default "/sendspin"). The
// dialed connection is handed to the same handleConnection funnel
// inbound clients use, so the protocol handshake and audio streaming
// paths are unchanged. Per the Sendspin spec, a player must either
// advertise _sendspin._tcp OR connect directly — never both.
```

- [ ] **Step 2: Update `README.md` if it lists server flags**

Check: `grep -n "no-mdns\|EnableMDNS" README.md`
If matches found, add a line mentioning `-discover-clients` in the same section.
If no matches, skip.

- [ ] **Step 3: Commit**

```bash
git add pkg/sendspin/doc.go README.md
git commit -m "docs: document server-initiated client discovery"
```

---

## Done Criteria

- [ ] `go test ./...` passes
- [ ] `make lint` clean
- [ ] `make server` builds
- [ ] `./bin/sendspin-server -discover-clients` starts and logs `Browsing for clients advertising _sendspin._tcp`
- [ ] Integration test confirms a fake mDNS advertisement triggers a real WebSocket dial and handshake
- [ ] `handleConnection` in `pkg/sendspin/server.go` is *unmodified* by this plan — grep the final diff and confirm
