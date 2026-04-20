// ABOUTME: Hotkey label rendering helper — marks the trigger character with reverse video
// ABOUTME: Used by all TUI labels that advertise a keybinding so the cue is uniform
package ui

import (
	"strings"
	"unicode"
)

// ANSI escape sequences for SGR reverse-video. Kept here rather than in a
// styling library because we only need two codes — pulling in lipgloss for
// this would be overkill.
const (
	ansiReverse = "\x1b[7m"
	ansiReset   = "\x1b[0m"
)

// hotkey renders a human-readable label with its trigger key reverse-
// highlighted. The visual cue — a single inverted character embedded in the
// label — is uniform across every keybinding in the TUI so users learn the
// pattern once.
//
// Resolution rules:
//   - Printable letter trigger with a case-insensitive match in the label:
//     highlight the first occurrence in place.
//     hotkey('o', "Output") -> "\x1b[7mO\x1b[0mutput"
//   - Printable letter trigger NOT present in the label:
//     fall back to a bracket prefix.
//     hotkey('q', "Bye") -> "[\x1b[7mQ\x1b[0m] Bye"
//   - Non-letter triggers (space, arrow sentinels, etc.) always use the
//     bracket form with a friendly name for the key.
//     hotkey(KeySpace, "Play/Pause") -> "[\x1b[7mSpace\x1b[0m] Play/Pause"
//
// Empty labels still render the bracket prefix so the binding is visible.
func hotkey(trigger rune, label string) string {
	if name, ok := specialKeyName(trigger); ok {
		return bracketPrefix(name, label)
	}
	if !unicode.IsLetter(trigger) && !unicode.IsDigit(trigger) {
		// Punctuation or symbols — use bracket form with the literal rune.
		return bracketPrefix(string(trigger), label)
	}
	if idx := firstCaseFoldIndex(label, trigger); idx >= 0 {
		r, width := runeAt(label, idx)
		return label[:idx] + ansiReverse + string(r) + ansiReset + label[idx+width:]
	}
	return bracketPrefix(strings.ToUpper(string(trigger)), label)
}

// Special-key sentinels. These aren't valid Unicode code points that could
// appear in a label, so overloading a rune-sized value is safe. Any value
// outside the printable range works; using specific private-use-area code
// points keeps them self-documenting in stack traces.
const (
	KeySpace rune = '\uE000' // "Space"
	KeyUp    rune = '\uE001' // "↑"
	KeyDown  rune = '\uE002' // "↓"
	KeyUpDown rune = '\uE003' // "↑↓"
	KeyEnter rune = '\uE004' // "Enter"
	KeyEsc   rune = '\uE005' // "Esc"
)

func specialKeyName(r rune) (string, bool) {
	switch r {
	case KeySpace:
		return "Space", true
	case KeyUp:
		return "\u2191", true
	case KeyDown:
		return "\u2193", true
	case KeyUpDown:
		return "\u2191\u2193", true
	case KeyEnter:
		return "Enter", true
	case KeyEsc:
		return "Esc", true
	}
	return "", false
}

// bracketPrefix renders "[<highlighted keyName>] label" with the key name
// reverse-highlighted. When label is empty, emits just the bracket (still
// useful so the binding is visible in compact hint rows).
func bracketPrefix(keyName, label string) string {
	prefix := "[" + ansiReverse + keyName + ansiReset + "]"
	if label == "" {
		return prefix
	}
	return prefix + " " + label
}

// firstCaseFoldIndex returns the byte index of the first rune in s that
// case-folds equal to needle, or -1 if not found. Works for any letter —
// multi-byte ones included, though this codebase only uses ASCII triggers.
func firstCaseFoldIndex(s string, needle rune) int {
	target := unicode.ToLower(needle)
	for i, r := range s {
		if unicode.ToLower(r) == target {
			return i
		}
	}
	return -1
}

// runeAt returns the rune at byte index i and its encoded byte width. Only
// called after firstCaseFoldIndex has confirmed a match, so the index is
// guaranteed to be on a rune boundary.
func runeAt(s string, i int) (rune, int) {
	for j, r := range s {
		if j == i {
			return r, len(string(r))
		}
	}
	return 0, 0
}
