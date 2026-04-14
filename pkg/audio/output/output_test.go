// ABOUTME: Audio output interface tests
// ABOUTME: Verifies Output interface implementation
package output

import "testing"

func TestMalgoImplementsOutput(t *testing.T) {
	var _ Output = (*Malgo)(nil)
}
