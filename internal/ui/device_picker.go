// ABOUTME: Modal picker for audio output devices — shown over the main view
// ABOUTME: Save-and-restart model: selection writes audio_device to player.yaml
package ui

import (
	"fmt"
	"strings"

	"github.com/Sendspin/sendspin-go/pkg/audio/output"
)

// pickerAction is what handlePickerKey tells the Model to do next. The
// picker itself never writes to disk; the Model layer owns side effects so
// the pure state machine here stays trivially testable.
type pickerAction int

const (
	pickerNoop      pickerAction = iota // key was consumed, nothing further to do
	pickerClose                         // close without saving
	pickerSave                          // save the currently-selected device and close
	pickerPropagate                     // key was not handled by the picker; caller may handle it
)

type devicePickerState struct {
	devices     []output.PlaybackDevice
	index       int    // currently-highlighted row
	current     string // device name currently in effect (for the "(current)" annotation)
	loadErr     error  // non-nil when ListPlaybackDevices failed
	saveEnabled bool   // false when no config path is available — save is disabled, Esc still works
}

// newDevicePicker builds the state shown when the user first opens the
// modal. listFn is the enumerator — parameterised so tests can stub it.
func newDevicePicker(listFn func() ([]output.PlaybackDevice, error), current string, saveEnabled bool) devicePickerState {
	devices, err := listFn()
	s := devicePickerState{
		current:     current,
		loadErr:     err,
		saveEnabled: saveEnabled,
	}
	if err != nil {
		return s
	}
	s.devices = devices
	// Seed selection: current device if matched; else default; else first.
	for i, d := range devices {
		if d.Name == current {
			s.index = i
			return s
		}
	}
	for i, d := range devices {
		if d.IsDefault {
			s.index = i
			return s
		}
	}
	return s
}

// handlePickerKey is a pure state transition used by both production and
// tests. It returns the next state and an action the caller performs (close,
// save, etc.). The Model translates actions into side effects.
func (s devicePickerState) handlePickerKey(key string) (devicePickerState, pickerAction) {
	switch key {
	case "up":
		if len(s.devices) > 0 && s.index > 0 {
			s.index--
		}
		return s, pickerNoop
	case "down":
		if s.index < len(s.devices)-1 {
			s.index++
		}
		return s, pickerNoop
	case "esc", "q":
		return s, pickerClose
	case "enter":
		if !s.saveEnabled || len(s.devices) == 0 {
			return s, pickerNoop
		}
		return s, pickerSave
	}
	return s, pickerPropagate
}

// selected returns the device the user would save if they pressed Enter
// right now, or nil when the list is empty or failed to load.
func (s devicePickerState) selected() *output.PlaybackDevice {
	if s.loadErr != nil || len(s.devices) == 0 {
		return nil
	}
	if s.index < 0 || s.index >= len(s.devices) {
		return nil
	}
	return &s.devices[s.index]
}

// renderPicker draws the modal. width is the TUI width; the picker sizes
// itself accordingly. If the list failed to load, the error is surfaced
// inside the modal so the user knows why the picker is empty.
func renderPicker(s devicePickerState, width int) string {
	if width < 60 {
		width = 60
	}
	inner := width - 4
	var b strings.Builder

	b.WriteString("\u250c\u2500 Select playback device " + repeatString("\u2500", width-26) + "\u2510\n")

	if s.loadErr != nil {
		line := fmt.Sprintf("  %s", truncateRunes(s.loadErr.Error(), inner-2))
		b.WriteString(fmt.Sprintf("\u2502 %-*s \u2502\n", inner, line))
	} else if len(s.devices) == 0 {
		b.WriteString(fmt.Sprintf("\u2502 %-*s \u2502\n", inner, "  (no playback devices found)"))
	} else {
		for i, d := range s.devices {
			marker := " "
			if i == s.index {
				marker = ">"
			}
			annotations := []string{}
			if d.IsDefault {
				annotations = append(annotations, "default")
			}
			if d.Name == s.current {
				annotations = append(annotations, "current")
			}
			name := d.Name
			if len(annotations) > 0 {
				name = fmt.Sprintf("%s (%s)", d.Name, strings.Join(annotations, ", "))
			}
			line := fmt.Sprintf("%s %s", marker, truncateRunes(name, inner-2))
			// Highlight the selected row with reverse video so it reads even
			// when the user's terminal doesn't do dim/bright styling well.
			if i == s.index {
				line = ansiReverse + padRight(line, inner) + ansiReset
				b.WriteString(fmt.Sprintf("\u2502 %s \u2502\n", line))
			} else {
				b.WriteString(fmt.Sprintf("\u2502 %-*s \u2502\n", inner, line))
			}
		}
	}

	// Blank row then the keybinding hints.
	b.WriteString(fmt.Sprintf("\u2502 %-*s \u2502\n", inner, ""))
	hints := "  " + hotkey(KeyUpDown, "move") + "   " + hotkey(KeyEnter, "save") + "   " + hotkey(KeyEsc, "cancel")
	if !s.saveEnabled {
		hints = "  " + hotkey(KeyUpDown, "move") + "   save unavailable (no writable config path)   " + hotkey(KeyEsc, "cancel")
	}
	// Padding accounts for the fact that hints contains ANSI escape codes
	// that don't count toward display width.
	displayLen := displayLen(hints)
	pad := inner - displayLen
	if pad < 0 {
		pad = 0
	}
	b.WriteString(fmt.Sprintf("\u2502 %s%s \u2502\n", hints, strings.Repeat(" ", pad)))

	b.WriteString("\u2514" + repeatString("\u2500", width-2) + "\u2518\n")
	return b.String()
}

// displayLen approximates how many terminal columns a string takes, ignoring
// ANSI escape sequences. Close enough for alignment padding; we're not
// supporting combining characters or East Asian width here.
func displayLen(s string) int {
	count := 0
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		count++
	}
	return count
}

// truncateRunes returns s clipped to maxCols runes, appending an ellipsis
// when truncation actually happens. Rune-safe variant of model.go's
// byte-based truncate — device names may contain non-ASCII characters.
func truncateRunes(s string, maxCols int) string {
	if len([]rune(s)) <= maxCols {
		return s
	}
	if maxCols <= 1 {
		return "\u2026"
	}
	runes := []rune(s)
	return string(runes[:maxCols-1]) + "\u2026"
}

// padRight pads s with spaces up to width display columns. Used for the
// reverse-video row so the highlight extends across the full row.
func padRight(s string, width int) string {
	d := displayLen(s)
	if d >= width {
		return s
	}
	return s + strings.Repeat(" ", width-d)
}
