// ABOUTME: High-level Player API for Sendspin streaming
// ABOUTME: Composes Receiver + audio output with optional ProcessCallback
package sendspin

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/audio"
	"github.com/Sendspin/sendspin-go/pkg/audio/decode"
	"github.com/Sendspin/sendspin-go/pkg/audio/output"
	"github.com/Sendspin/sendspin-go/pkg/protocol"
	"github.com/Sendspin/sendspin-go/pkg/sync"
)

// ReconnectConfig controls automatic reconnect behavior after the protocol
// connection drops. When Enabled is false (the zero value), Player behaves
// as a one-shot: a lost connection stays lost.
type ReconnectConfig struct {
	Enabled      bool
	InitialDelay time.Duration // default 500ms
	MaxDelay     time.Duration // default 30s
	Multiplier   float64       // default 2.0
	MaxAttempts  int           // 0 = infinite (default)

	// Rediscover is an optional callback invoked before each reconnect
	// attempt. Returning a non-empty address overrides the configured
	// ServerAddr for that attempt. Use this to re-run mDNS discovery when
	// the server may have moved. Errors are logged and the attempt falls
	// back to the last known address.
	Rediscover func(ctx context.Context) (string, error)
}

// PlayerConfig holds player configuration
type PlayerConfig struct {
	// ServerAddr is the server address (host:port)
	ServerAddr string

	PlayerName string

	// Volume is the initial volume (0-100)
	Volume int

	// BufferMs is the playback buffer size in milliseconds (default: 500)
	BufferMs int

	// StaticDelayMs shifts every scheduled play time forward by this many
	// milliseconds. Used to compensate for hardware that introduces a fixed
	// downstream latency (Bluetooth sinks, AVRs with DSP, some USB DACs).
	// Default 0 means no shift.
	StaticDelayMs int

	// PreferredCodec reorders the advertised format list so the server
	// picks this codec first. Values: "pcm" (default), "opus", "flac".
	PreferredCodec string

	// BufferCapacity is the buffer_capacity (bytes) advertised to the
	// server in client/hello. The server uses this to pace how far ahead
	// it sends audio. Default: 1048576 (1MB).
	BufferCapacity int

	DeviceInfo DeviceInfo

	OnMetadata func(Metadata)

	OnStateChange func(PlayerState)

	OnError func(error)

	// Output overrides the default audio output backend.
	// When nil, a malgo-backed output is created on stream start.
	Output output.Output

	// DecoderFactory overrides the default decoder selection.
	// When nil, the default codec switch (PCM, Opus, FLAC) is used.
	DecoderFactory func(audio.Format) (decode.Decoder, error)

	// ProcessCallback is called with decoded samples before they are written to output.
	// Must not block. Runs on the audio consumption goroutine.
	ProcessCallback func([]int32)

	// Reconnect controls automatic reconnection behavior when the protocol
	// connection drops. Disabled by default.
	Reconnect ReconnectConfig
}

type DeviceInfo struct {
	ProductName     string
	Manufacturer    string
	SoftwareVersion string
}

type Metadata struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	ArtworkURL  string
	Track       int
	Year        int
	Duration    int // seconds
}

type PlayerState struct {
	State      string // "idle", "playing", "paused"
	Volume     int
	Muted      bool
	Codec      string
	SampleRate int
	Channels   int
	BitDepth   int
	Connected  bool
}

type PlayerStats struct {
	Received    int64
	Played      int64
	Dropped     int64
	BufferDepth int // milliseconds
	SyncRTT     int64
	SyncQuality sync.Quality
}

// Player provides high-level audio playback from Sendspin servers.
// It composes a Receiver (connect/sync/decode/schedule) with an audio output backend.
type Player struct {
	config   PlayerConfig
	receiver *Receiver
	output   output.Output
	state    PlayerState
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewPlayer(config PlayerConfig) (*Player, error) {
	if config.Volume == 0 {
		config.Volume = 100
	}
	if config.BufferMs == 0 {
		config.BufferMs = 500
	}
	if config.Reconnect.Enabled {
		if config.Reconnect.InitialDelay <= 0 {
			config.Reconnect.InitialDelay = 500 * time.Millisecond
		}
		if config.Reconnect.MaxDelay <= 0 {
			config.Reconnect.MaxDelay = 30 * time.Second
		}
		if config.Reconnect.Multiplier <= 1.0 {
			config.Reconnect.Multiplier = 2.0
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Player{
		config: config,
		output: config.Output,
		ctx:    ctx,
		cancel: cancel,
		state: PlayerState{
			State:     "idle",
			Volume:    config.Volume,
			Muted:     false,
			Connected: false,
		},
	}, nil
}

func (p *Player) Connect() error {
	recv, err := p.buildReceiver(p.config.ServerAddr)
	if err != nil {
		return err
	}
	if err := recv.Connect(); err != nil {
		return err
	}

	p.receiver = recv
	p.state.Connected = true
	p.notifyStateChange()

	go p.consumeAudio(recv)

	if p.config.Reconnect.Enabled {
		go p.runReconnectLoop(recv)
	}

	return nil
}

func (p *Player) buildReceiver(addr string) (*Receiver, error) {
	return NewReceiver(ReceiverConfig{
		ServerAddr:     addr,
		PlayerName:     p.config.PlayerName,
		BufferMs:       p.config.BufferMs,
		StaticDelayMs:  p.config.StaticDelayMs,
		PreferredCodec: p.config.PreferredCodec,
		BufferCapacity: p.config.BufferCapacity,
		DeviceInfo:     p.config.DeviceInfo,
		DecoderFactory: p.config.DecoderFactory,
		OnMetadata:     p.config.OnMetadata,
		OnStreamStart:  p.onStreamStart,
		OnStreamEnd:    p.onStreamEnd,
		OnError:        p.config.OnError,
	})
}

// runReconnectLoop supervises the active receiver and rebuilds it with
// exponential backoff whenever its Done channel closes. Exits when the
// Player context is cancelled.
func (p *Player) runReconnectLoop(initial *Receiver) {
	current := initial
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-current.Done():
		}

		// Connection lost. Enter reconnecting state and back off.
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		p.state.Connected = false
		p.state.State = "reconnecting"
		p.notifyStateChange()

		next, ok := p.reconnectWithBackoff()
		if !ok {
			return
		}
		current = next

		p.receiver = current
		p.state.Connected = true
		p.notifyStateChange()

		go p.consumeAudio(current)
	}
}

func (p *Player) reconnectWithBackoff() (*Receiver, bool) {
	cfg := p.config.Reconnect
	delay := cfg.InitialDelay
	attempt := 0

	for {
		attempt++
		if cfg.MaxAttempts > 0 && attempt > cfg.MaxAttempts {
			p.notifyError(fmt.Errorf("reconnect: gave up after %d attempts", cfg.MaxAttempts))
			return nil, false
		}

		// Jittered sleep (±20%).
		jittered := jitter(delay, 0.2)
		log.Printf("Reconnect attempt %d in %v", attempt, jittered)
		select {
		case <-p.ctx.Done():
			return nil, false
		case <-time.After(jittered):
		}

		addr := p.config.ServerAddr
		if cfg.Rediscover != nil {
			discovered, err := cfg.Rediscover(p.ctx)
			if err != nil {
				log.Printf("Reconnect: rediscover failed: %v (using last known addr %s)", err, addr)
			} else if discovered != "" {
				addr = discovered
			}
		}

		recv, err := p.buildReceiver(addr)
		if err == nil {
			if err = recv.Connect(); err == nil {
				log.Printf("Reconnect: connected to %s on attempt %d", addr, attempt)
				return recv, true
			}
		}
		log.Printf("Reconnect attempt %d to %s failed: %v", attempt, addr, err)

		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}
}

func jitter(d time.Duration, frac float64) time.Duration {
	if d <= 0 {
		return d
	}
	delta := (rand.Float64()*2 - 1) * frac
	return time.Duration(float64(d) * (1 + delta))
}

func (p *Player) onStreamStart(format audio.Format) {
	if p.output == nil {
		p.output = output.NewMalgo()
	}

	if err := p.output.Open(format.SampleRate, format.Channels, format.BitDepth); err != nil {
		p.notifyError(fmt.Errorf("failed to initialize output: %w", err))
		return
	}

	p.output.SetVolume(p.state.Volume)
	p.output.SetMuted(p.state.Muted)

	p.state.Codec = format.Codec
	p.state.SampleRate = format.SampleRate
	p.state.Channels = format.Channels
	p.state.BitDepth = format.BitDepth
	p.state.State = "playing"
	p.notifyStateChange()
}

func (p *Player) onStreamEnd() {
	p.state.State = "idle"
	p.notifyStateChange()
}

func (p *Player) consumeAudio(recv *Receiver) {
	for {
		select {
		case buf, ok := <-recv.Output():
			if !ok {
				return
			}

			if p.config.ProcessCallback != nil {
				p.config.ProcessCallback(buf.Samples)
			}

			if p.output != nil {
				if err := p.output.Write(buf.Samples); err != nil {
					p.notifyError(fmt.Errorf("playback error: %w", err))
				}
			}

		case <-p.ctx.Done():
			return
		}
	}
}

func (p *Player) Play() error {
	if !p.state.Connected {
		return fmt.Errorf("not connected")
	}
	p.state.State = "playing"
	p.notifyStateChange()
	return p.sendState()
}

func (p *Player) Pause() error {
	if !p.state.Connected {
		return fmt.Errorf("not connected")
	}
	p.state.State = "paused"
	p.notifyStateChange()
	return p.sendState()
}

func (p *Player) Stop() error {
	if !p.state.Connected {
		return fmt.Errorf("not connected")
	}
	p.state.State = "idle"
	p.notifyStateChange()
	return p.sendState()
}

// SetVolume sets the volume (0-100)
func (p *Player) SetVolume(volume int) error {
	if volume < 0 {
		volume = 0
	}
	if volume > 100 {
		volume = 100
	}
	p.state.Volume = volume

	if p.output != nil {
		p.output.SetVolume(volume)
	}

	if p.receiver != nil && p.state.Connected {
		p.sendState()
	}

	p.notifyStateChange()
	return nil
}

func (p *Player) Mute(muted bool) error {
	p.state.Muted = muted

	if p.output != nil {
		p.output.SetMuted(muted)
	}

	if p.receiver != nil && p.state.Connected {
		p.sendState()
	}

	p.notifyStateChange()
	return nil
}

func (p *Player) Status() PlayerState {
	return p.state
}

func (p *Player) Stats() PlayerStats {
	stats := PlayerStats{}

	if p.receiver != nil {
		rs := p.receiver.Stats()
		stats.Received = rs.Received
		stats.Played = rs.Played
		stats.Dropped = rs.Dropped
		stats.BufferDepth = rs.BufferDepth
		stats.SyncRTT = rs.SyncRTT
		stats.SyncQuality = rs.SyncQuality
	}

	return stats
}

func (p *Player) Close() error {
	p.cancel()

	if p.receiver != nil {
		p.receiver.Close()
	}

	if p.output != nil {
		p.output.Close()
	}

	p.state.Connected = false
	p.state.State = "idle"
	p.notifyStateChange()

	return nil
}

// SendCommand sends a controller command to the server (e.g., "play",
// "pause", "next", "previous"). This is how a player requests playback
// control — the server decides whether to act on it.
func (p *Player) SendCommand(command string) error {
	if p.receiver == nil || p.receiver.client == nil {
		return fmt.Errorf("not connected")
	}
	payload := map[string]interface{}{
		"controller": map[string]interface{}{
			"command": command,
		},
	}
	return p.receiver.client.Send("client/command", payload)
}

func (p *Player) sendState() error {
	if p.receiver == nil || p.receiver.client == nil {
		return nil
	}
	return p.receiver.client.SendState(protocol.PlayerState{
		State:  "synchronized",
		Volume: p.state.Volume,
		Muted:  p.state.Muted,
	})
}

func (p *Player) notifyStateChange() {
	if p.config.OnStateChange != nil {
		p.config.OnStateChange(p.state)
	}
}

func (p *Player) notifyError(err error) {
	if p.config.OnError != nil {
		p.config.OnError(err)
	} else {
		log.Printf("Player error: %v", err)
	}
}

// Helper functions used by both player.go and receiver.go

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

func getDurationSeconds(p *protocol.ProgressState) int {
	if p == nil {
		return 0
	}
	return p.TrackDuration / 1000
}

func containsRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
