// ABOUTME: Audio output interface tests
// ABOUTME: Verifies Output interface implementation
package output

import (
	"testing"
)

func TestOtoImplementsOutput(t *testing.T) {
	var _ Output = (*Oto)(nil)
}

func TestMalgoImplementsOutput(t *testing.T) {
	var _ Output = (*Malgo)(nil)
}

func TestNewOto(t *testing.T) {
	out := NewOto()
	if out == nil {
		t.Fatal("NewOto returned nil")
	}
}
