package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWidth verifies the Width() function returns internally consistent results.
// In non-TTY environments (like test CI), os.Stdin is not a TTY and IoctlGetWinsize
// returns an error, so we expect (0, err). In TTY environments, we expect
// (w >= 0, nil). The test asserts this consistency regardless of environment.
func TestWidth(t *testing.T) {
	w, err := Width()

	// Assert internal consistency: either success with non-negative width,
	// or error with zero width.
	if err == nil {
		assert.GreaterOrEqual(t, w, 0, "Width should be non-negative on success")
	} else {
		assert.Equal(t, 0, w, "Width should be 0 on error")
	}
}
