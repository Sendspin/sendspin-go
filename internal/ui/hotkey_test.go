// ABOUTME: Tests for the hotkey label helper — in-place reverse highlighting
package ui

import "testing"

func TestHotkey_LetterInLabel(t *testing.T) {
	tests := []struct {
		name    string
		trigger rune
		label   string
		want    string
	}{
		{
			name:    "first letter match",
			trigger: 'o',
			label:   "Output",
			want:    ansiReverse + "O" + ansiReset + "utput",
		},
		{
			name:    "case-insensitive — lowercase trigger, uppercase in label",
			trigger: 'q',
			label:   "Quit",
			want:    ansiReverse + "Q" + ansiReset + "uit",
		},
		{
			name:    "case-insensitive — uppercase trigger, lowercase in label",
			trigger: 'E',
			label:   "enter",
			want:    ansiReverse + "e" + ansiReset + "nter",
		},
		{
			name:    "trigger matches a non-initial letter",
			trigger: 'u',
			label:   "Quit",
			want:    "Q" + ansiReverse + "u" + ansiReset + "it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hotkey(tt.trigger, tt.label)
			if got != tt.want {
				t.Errorf("hotkey(%q, %q) =\n  %q\nwant\n  %q", tt.trigger, tt.label, got, tt.want)
			}
		})
	}
}

func TestHotkey_LetterNotInLabel(t *testing.T) {
	got := hotkey('x', "Bye")
	want := "[" + ansiReverse + "X" + ansiReset + "] Bye"
	if got != want {
		t.Errorf("missing-letter fallback =\n  %q\nwant\n  %q", got, want)
	}
}

func TestHotkey_SpecialKeys(t *testing.T) {
	tests := []struct {
		name    string
		trigger rune
		label   string
		want    string
	}{
		{
			name:    "space",
			trigger: KeySpace,
			label:   "Play/Pause",
			want:    "[" + ansiReverse + "Space" + ansiReset + "] Play/Pause",
		},
		{
			name:    "up-down combined",
			trigger: KeyUpDown,
			label:   "Volume",
			want:    "[" + ansiReverse + "\u2191\u2193" + ansiReset + "] Volume",
		},
		{
			name:    "enter",
			trigger: KeyEnter,
			label:   "Save",
			want:    "[" + ansiReverse + "Enter" + ansiReset + "] Save",
		},
		{
			name:    "esc",
			trigger: KeyEsc,
			label:   "Cancel",
			want:    "[" + ansiReverse + "Esc" + ansiReset + "] Cancel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hotkey(tt.trigger, tt.label)
			if got != tt.want {
				t.Errorf("hotkey(%q) =\n  %q\nwant\n  %q", tt.trigger, got, tt.want)
			}
		})
	}
}

func TestHotkey_EmptyLabel(t *testing.T) {
	// Letter with empty label uses the bracket fallback since nothing to match.
	got := hotkey('q', "")
	want := "[" + ansiReverse + "Q" + ansiReset + "]"
	if got != want {
		t.Errorf("empty label with letter =\n  %q\nwant\n  %q", got, want)
	}

	// Special key with empty label uses bracket without trailing space.
	got = hotkey(KeyEsc, "")
	want = "[" + ansiReverse + "Esc" + ansiReset + "]"
	if got != want {
		t.Errorf("empty label with special =\n  %q\nwant\n  %q", got, want)
	}
}

func TestHotkey_Symbol(t *testing.T) {
	// Symbols aren't letters; they use the bracket prefix with the literal rune.
	got := hotkey('/', "Slash")
	want := "[" + ansiReverse + "/" + ansiReset + "] Slash"
	if got != want {
		t.Errorf("symbol trigger =\n  %q\nwant\n  %q", got, want)
	}
}
