// ABOUTME: Tests for the device-picker state machine — pure functions, no TUI loop required
package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/Sendspin/sendspin-go/pkg/audio/output"
)

func fixtureDevices() []output.PlaybackDevice {
	return []output.PlaybackDevice{
		{Name: "USB Audio"},
		{Name: "Built-in Analog", IsDefault: true},
		{Name: "HDMI"},
	}
}

func listOK() ([]output.PlaybackDevice, error) {
	return fixtureDevices(), nil
}

func TestNewDevicePicker_SeedsIndexFromCurrent(t *testing.T) {
	s := newDevicePicker(listOK, "HDMI", true)
	if s.index != 2 {
		t.Errorf("index = %d, want 2 (HDMI)", s.index)
	}
}

func TestNewDevicePicker_SeedsIndexFromDefaultWhenCurrentMissing(t *testing.T) {
	s := newDevicePicker(listOK, "No such device", true)
	if s.index != 1 {
		t.Errorf("index = %d, want 1 (the IsDefault entry)", s.index)
	}
}

func TestNewDevicePicker_SeedsZeroWhenNoDefaultAndNoCurrent(t *testing.T) {
	listNoDefault := func() ([]output.PlaybackDevice, error) {
		return []output.PlaybackDevice{
			{Name: "One"},
			{Name: "Two"},
		}, nil
	}
	s := newDevicePicker(listNoDefault, "", true)
	if s.index != 0 {
		t.Errorf("index = %d, want 0", s.index)
	}
}

func TestNewDevicePicker_PropagatesLoadError(t *testing.T) {
	listErr := func() ([]output.PlaybackDevice, error) {
		return nil, errors.New("no audio stack")
	}
	s := newDevicePicker(listErr, "", true)
	if s.loadErr == nil {
		t.Error("loadErr should be set when enumerator fails")
	}
	if s.selected() != nil {
		t.Error("selected() should be nil when load failed")
	}
}

func TestHandlePickerKey_UpDownClamp(t *testing.T) {
	s := newDevicePicker(listOK, "USB Audio", true) // index 0

	// Up at the top clamps.
	s, act := s.handlePickerKey("up")
	if s.index != 0 || act != pickerNoop {
		t.Errorf("up at top: index=%d act=%d", s.index, act)
	}

	// Two downs.
	s, _ = s.handlePickerKey("down")
	s, _ = s.handlePickerKey("down")
	if s.index != 2 {
		t.Fatalf("after 2 downs, index = %d, want 2", s.index)
	}

	// Down at the bottom clamps.
	s, _ = s.handlePickerKey("down")
	if s.index != 2 {
		t.Errorf("down at bottom: index = %d, want 2", s.index)
	}

	// One up.
	s, _ = s.handlePickerKey("up")
	if s.index != 1 {
		t.Errorf("up from 2: index = %d, want 1", s.index)
	}
}

func TestHandlePickerKey_EnterSavesWhenEnabled(t *testing.T) {
	s := newDevicePicker(listOK, "", true)
	_, act := s.handlePickerKey("enter")
	if act != pickerSave {
		t.Errorf("enter action = %d, want pickerSave", act)
	}
}

func TestHandlePickerKey_EnterNoopWhenSaveDisabled(t *testing.T) {
	s := newDevicePicker(listOK, "", false)
	_, act := s.handlePickerKey("enter")
	if act != pickerNoop {
		t.Errorf("enter with save disabled action = %d, want pickerNoop", act)
	}
}

func TestHandlePickerKey_EnterNoopOnEmptyList(t *testing.T) {
	s := newDevicePicker(func() ([]output.PlaybackDevice, error) { return nil, nil }, "", true)
	_, act := s.handlePickerKey("enter")
	if act != pickerNoop {
		t.Errorf("enter on empty action = %d, want pickerNoop", act)
	}
}

func TestHandlePickerKey_EscAndQClose(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		s := newDevicePicker(listOK, "", true)
		_, act := s.handlePickerKey(key)
		if act != pickerClose {
			t.Errorf("%q action = %d, want pickerClose", key, act)
		}
	}
}

func TestHandlePickerKey_UnknownPropagates(t *testing.T) {
	s := newDevicePicker(listOK, "", true)
	_, act := s.handlePickerKey("x")
	if act != pickerPropagate {
		t.Errorf("unknown key action = %d, want pickerPropagate", act)
	}
}

func TestSelected_ReturnsCurrentRow(t *testing.T) {
	s := newDevicePicker(listOK, "HDMI", true)
	got := s.selected()
	if got == nil {
		t.Fatal("selected() = nil")
	}
	if got.Name != "HDMI" {
		t.Errorf("selected().Name = %q, want HDMI", got.Name)
	}
}

func TestRenderPicker_ShowsDefaultAndCurrentAnnotations(t *testing.T) {
	s := newDevicePicker(listOK, "HDMI", true)
	out := renderPicker(s, 80)
	// Built-in Analog carries (default), HDMI carries (current).
	if !strings.Contains(out, "Built-in Analog (default)") {
		t.Errorf("expected '(default)' annotation; got:\n%s", out)
	}
	if !strings.Contains(out, "HDMI (current)") {
		t.Errorf("expected '(current)' annotation on HDMI; got:\n%s", out)
	}
}

func TestRenderPicker_AnnotatesBothWhenCurrentIsDefault(t *testing.T) {
	s := newDevicePicker(listOK, "Built-in Analog", true)
	out := renderPicker(s, 80)
	if !strings.Contains(out, "Built-in Analog (default, current)") {
		t.Errorf("expected combined annotation; got:\n%s", out)
	}
}

func TestRenderPicker_EmptyAndErrorStates(t *testing.T) {
	empty := newDevicePicker(func() ([]output.PlaybackDevice, error) { return nil, nil }, "", true)
	emptyOut := renderPicker(empty, 80)
	if !strings.Contains(emptyOut, "no playback devices found") {
		t.Errorf("empty state did not render expected message; got:\n%s", emptyOut)
	}

	errState := newDevicePicker(func() ([]output.PlaybackDevice, error) {
		return nil, errors.New("enumeration boom")
	}, "", true)
	errOut := renderPicker(errState, 80)
	if !strings.Contains(errOut, "enumeration boom") {
		t.Errorf("error state did not render error; got:\n%s", errOut)
	}
}

func TestRenderPicker_SaveUnavailableHint(t *testing.T) {
	s := newDevicePicker(listOK, "", false)
	out := renderPicker(s, 80)
	if !strings.Contains(out, "save unavailable") {
		t.Errorf("expected 'save unavailable' hint when saveEnabled=false; got:\n%s", out)
	}
}

// The two tests below integrate picker state with the Model's key dispatch
// to verify the end-to-end "press o → navigate → save" flow without
// standing up a full bubbletea runtime.

func TestModel_OKeyOpensPicker(t *testing.T) {
	m := NewModel(Config{ConfigPath: "/tmp/test-player.yaml"})
	m.listPlaybackDevices = listOK
	m.width = 80

	next, _ := m.handleKey(fakeKeyMsg("o"))
	nm := next.(Model)
	if nm.mode != modeDevicePicker {
		t.Errorf("mode = %v, want modeDevicePicker", nm.mode)
	}
	if len(nm.picker.devices) != 3 {
		t.Errorf("picker.devices len = %d, want 3", len(nm.picker.devices))
	}
}

func TestModel_EnterInPickerSavesAndClosesModal(t *testing.T) {
	var savedPath, savedKey, savedValue string
	writer := func(path, key, value string) error {
		savedPath, savedKey, savedValue = path, key, value
		return nil
	}

	// Seed the current device so the picker opens highlighted on "USB Audio"
	// (index 0). One "down" then lands on the IsDefault entry at index 1.
	m := NewModel(Config{ConfigPath: "/tmp/test-player.yaml", AudioDevice: "USB Audio"})
	m.listPlaybackDevices = listOK
	m.writeConfigKey = writer
	m.width = 80

	// Open picker.
	next, _ := m.handleKey(fakeKeyMsg("o"))
	m = next.(Model)

	// Navigate down to Built-in Analog (index 1).
	next, _ = m.handleKey(fakeKeyMsg("down"))
	m = next.(Model)

	// Enter saves. Returns a Cmd (the 3s expire tick); we don't invoke it.
	next, cmd := m.handleKey(fakeKeyMsg("enter"))
	m = next.(Model)

	if m.mode != modeNormal {
		t.Errorf("mode after save = %v, want modeNormal", m.mode)
	}
	if savedPath != "/tmp/test-player.yaml" || savedKey != "audio_device" || savedValue != "Built-in Analog" {
		t.Errorf("write recorded (%q, %q, %q); want (/tmp/test-player.yaml, audio_device, Built-in Analog)",
			savedPath, savedKey, savedValue)
	}
	if m.transientMsg == "" {
		t.Error("transient banner should be set after save")
	}
	if cmd == nil {
		t.Error("save should schedule a transient-expire tick")
	}
}

func TestModel_EnterWhenSelectionMatchesSkipsWrite(t *testing.T) {
	writeCalled := false
	writer := func(path, key, value string) error {
		writeCalled = true
		return nil
	}

	// Current is the HDMI entry; picker seeds selection there.
	m := NewModel(Config{ConfigPath: "/tmp/test.yaml", AudioDevice: "HDMI"})
	m.listPlaybackDevices = listOK
	m.writeConfigKey = writer
	m.width = 80

	next, _ := m.handleKey(fakeKeyMsg("o"))
	m = next.(Model)
	next, _ = m.handleKey(fakeKeyMsg("enter"))
	m = next.(Model)

	if writeCalled {
		t.Error("writeConfigKey should not be called when selection matches current")
	}
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", m.mode)
	}
}
