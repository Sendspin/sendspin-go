// ABOUTME: Tests for the pure capsFromFormats / formatBits helpers
package output

import (
	"testing"

	"github.com/gen2brain/malgo"
)

func TestFormatBits(t *testing.T) {
	tests := []struct {
		format malgo.FormatType
		want   int
	}{
		{malgo.FormatS16, 16},
		{malgo.FormatS24, 24},
		{malgo.FormatS32, 32},
		{malgo.FormatF32, 32},
		{malgo.FormatUnknown, 0},
		{malgo.FormatU8, 0}, // we don't currently advertise 8-bit formats
	}

	for _, tt := range tests {
		if got := formatBits(tt.format); got != tt.want {
			t.Errorf("formatBits(%v) = %d, want %d", tt.format, got, tt.want)
		}
	}
}

func TestCapsFromFormats_Empty(t *testing.T) {
	rate, depth := capsFromFormats(nil)
	if rate != 0 || depth != 0 {
		t.Errorf("expected (0, 0) for empty input, got (%d, %d)", rate, depth)
	}
}

func TestCapsFromFormats_TakesMaxAcrossEntries(t *testing.T) {
	// Mixed-rate / mixed-depth list. Rate and depth caps come from
	// different entries — neither field's max needs to come from the same row.
	formats := []malgo.DataFormat{
		{Format: malgo.FormatS16, Channels: 2, SampleRate: 192000},
		{Format: malgo.FormatS24, Channels: 2, SampleRate: 48000},
	}
	rate, depth := capsFromFormats(formats)
	if rate != 192000 {
		t.Errorf("expected max rate 192000, got %d", rate)
	}
	if depth != 24 {
		t.Errorf("expected max depth 24, got %d", depth)
	}
}

func TestCapsFromFormats_IgnoresUnknownFormat(t *testing.T) {
	// An entry with an unknown FormatType must not contribute to either cap,
	// even if its SampleRate is huge — we'd be advertising a rate we couldn't
	// actually drive in any of our supported formats.
	formats := []malgo.DataFormat{
		{Format: malgo.FormatUnknown, Channels: 2, SampleRate: 384000},
		{Format: malgo.FormatS16, Channels: 2, SampleRate: 48000},
	}
	rate, depth := capsFromFormats(formats)
	if rate != 48000 {
		t.Errorf("expected unknown-format entry to be ignored; got rate %d", rate)
	}
	if depth != 16 {
		t.Errorf("expected depth 16, got %d", depth)
	}
}

func TestCapsFromFormats_AllUnknownReturnsZero(t *testing.T) {
	formats := []malgo.DataFormat{
		{Format: malgo.FormatUnknown, Channels: 2, SampleRate: 192000},
		{Format: malgo.FormatU8, Channels: 2, SampleRate: 48000},
	}
	rate, depth := capsFromFormats(formats)
	if rate != 0 || depth != 0 {
		t.Errorf("expected (0, 0) when no entry has a known bit depth, got (%d, %d)", rate, depth)
	}
}

func TestCapsFromFormats_F32MapsToThirtyTwo(t *testing.T) {
	formats := []malgo.DataFormat{
		{Format: malgo.FormatF32, Channels: 2, SampleRate: 96000},
	}
	_, depth := capsFromFormats(formats)
	if depth != 32 {
		t.Errorf("FormatF32 should map to 32 bits; got %d", depth)
	}
}
