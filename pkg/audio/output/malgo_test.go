// ABOUTME: Tests for the pure matchDevice selection logic used by Open
package output

import (
	"strings"
	"testing"

	"github.com/gen2brain/malgo"
)

// newDevice builds a PlaybackDevice with a unique sentinel ID so tests can
// assert the correct entry was returned. The actual ID bytes are opaque to
// miniaudio at this layer; we only check that matchDevice returns the right
// slice element.
func newDevice(name string, isDefault bool, marker byte) PlaybackDevice {
	var id malgo.DeviceID
	id[0] = marker
	return PlaybackDevice{Name: name, IsDefault: isDefault, ID: id}
}

func TestMatchDevice_EmptyRequest(t *testing.T) {
	tests := []struct {
		name       string
		devices    []PlaybackDevice
		wantNil    bool
		wantName   string
		wantMarker byte
	}{
		{
			name:    "empty catalog returns nil",
			devices: nil,
			wantNil: true,
		},
		{
			name: "prefers the device flagged IsDefault",
			devices: []PlaybackDevice{
				newDevice("First", false, 0x01),
				newDevice("DefaultSink", true, 0x02),
				newDevice("Third", false, 0x03),
			},
			wantName:   "DefaultSink",
			wantMarker: 0x02,
		},
		{
			name: "falls back to first device when none flagged default",
			devices: []PlaybackDevice{
				newDevice("Alpha", false, 0x0A),
				newDevice("Beta", false, 0x0B),
			},
			wantName:   "Alpha",
			wantMarker: 0x0A,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := matchDevice(tt.devices, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected a device, got nil")
			}
			if got.Name != tt.wantName {
				t.Errorf("name = %q, want %q", got.Name, tt.wantName)
			}
			if got.ID[0] != tt.wantMarker {
				t.Errorf("id[0] = 0x%x, want 0x%x (wrong slice element returned)", got.ID[0], tt.wantMarker)
			}
		})
	}
}

func TestMatchDevice_ExactNameMatch(t *testing.T) {
	devices := []PlaybackDevice{
		newDevice("HDA Intel PCH: ALC257 Analog", true, 0x10),
		newDevice("HDMI 0", false, 0x11),
		newDevice("USB Audio Device", false, 0x12),
	}

	got, err := matchDevice(devices, "USB Audio Device")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Name != "USB Audio Device" {
		t.Errorf("got %+v, want USB Audio Device", got)
	}
	if got.ID[0] != 0x12 {
		t.Errorf("wrong device matched: id[0] = 0x%x, want 0x12", got.ID[0])
	}
}

func TestMatchDevice_NoMatchListsAvailable(t *testing.T) {
	devices := []PlaybackDevice{
		newDevice("Charlie", false, 0x01),
		newDevice("Alpha", true, 0x02),
		newDevice("Bravo", false, 0x03),
	}

	got, err := matchDevice(devices, "DoesNotExist")
	if got != nil {
		t.Errorf("expected nil device, got %+v", got)
	}
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"DoesNotExist"`) {
		t.Errorf("error should name the missing device: %q", msg)
	}
	// Available names must be listed, sorted, so users can copy/paste the right one.
	for _, want := range []string{"Alpha", "Bravo", "Charlie"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should list %q; got %q", want, msg)
		}
	}
	alphaIdx := strings.Index(msg, "Alpha")
	bravoIdx := strings.Index(msg, "Bravo")
	charlieIdx := strings.Index(msg, "Charlie")
	if !(alphaIdx < bravoIdx && bravoIdx < charlieIdx) {
		t.Errorf("available names should be sorted alphabetically; got %q", msg)
	}
}

func TestMatchDevice_NoMatchEmptyCatalogGivesDistinctError(t *testing.T) {
	got, err := matchDevice(nil, "Anything")
	if got != nil {
		t.Errorf("expected nil device, got %+v", got)
	}
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no playback devices available") {
		t.Errorf("error should distinguish empty-catalog case: %q", msg)
	}
}

// TestMatchDevice_ShortNameMatch covers miniaudio's Linux/ALSA naming where
// device.name is "<card-short>, <stream-description>" — users typing just
// the short prefix should match unambiguously when only one device has that
// prefix. Reproduces the HiFiBerry case from the field bug.
func TestMatchDevice_ShortNameMatch(t *testing.T) {
	devices := []PlaybackDevice{
		newDevice("Default Audio Device", true, 0x01),
		newDevice("vc4-hdmi-0, MAI PCM i2s-hifi-0", false, 0x02),
		newDevice("vc4-hdmi-1, MAI PCM i2s-hifi-0", false, 0x03),
		newDevice("PDP Audio Device, USB Audio", false, 0x04),
	}

	got, err := matchDevice(devices, "vc4-hdmi-0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID[0] != 0x02 {
		t.Errorf("short-name %q should resolve to id 0x02; got %+v", "vc4-hdmi-0", got)
	}

	got, err = matchDevice(devices, "PDP Audio Device")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID[0] != 0x04 {
		t.Errorf("short-name %q should resolve to id 0x04; got %+v", "PDP Audio Device", got)
	}
}

// TestMatchDevice_ExactNameWinsOverShortName guards the precedence: if a
// device's full name happens to equal someone else's short prefix, the
// exact match takes priority over the short-name search.
func TestMatchDevice_ExactNameWinsOverShortName(t *testing.T) {
	devices := []PlaybackDevice{
		newDevice("Foo, long description", false, 0x01),
		newDevice("Foo", false, 0x02),
	}

	got, err := matchDevice(devices, "Foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID[0] != 0x02 {
		t.Errorf("exact match should win: expected id 0x02; got %+v", got)
	}
}

// TestMatchDevice_ShortNameAmbiguousReturnsError covers the two-HiFiBerry
// case: the same short prefix matches multiple devices. We must not silently
// pick one.
func TestMatchDevice_ShortNameAmbiguousReturnsError(t *testing.T) {
	devices := []PlaybackDevice{
		newDevice("HiFiBerry, card 0", false, 0x01),
		newDevice("HiFiBerry, card 1", false, 0x02),
	}

	got, err := matchDevice(devices, "HiFiBerry")
	if got != nil {
		t.Errorf("expected nil on ambiguous short-name match, got %+v", got)
	}
	if err == nil {
		t.Fatal("expected an error on ambiguous short-name match")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ambiguous") {
		t.Errorf("error should mention ambiguity: %q", msg)
	}
	if !strings.Contains(msg, `"HiFiBerry, card 0"`) || !strings.Contains(msg, `"HiFiBerry, card 1"`) {
		t.Errorf("ambiguity error should list both candidates quoted with %%q: %q", msg)
	}
}

// TestMatchDevice_NoMatchQuotesNames ensures names with embedded commas are
// distinguishable from the list separator in the error output.
func TestMatchDevice_NoMatchQuotesNames(t *testing.T) {
	devices := []PlaybackDevice{
		newDevice("vc4-hdmi-0, MAI PCM i2s-hifi-0", false, 0x01),
	}

	_, err := matchDevice(devices, "nonexistent")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"vc4-hdmi-0, MAI PCM i2s-hifi-0"`) {
		t.Errorf("name should appear quoted in error so embedded comma is unambiguous: %q", msg)
	}
}
