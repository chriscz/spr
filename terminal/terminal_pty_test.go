//go:build !windows

package terminal

import (
	"os"
	"testing"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWidth_RealPTY(t *testing.T) {
	ptmx, tty, err := pty.Open()
	require.NoError(t, err)
	defer ptmx.Close()
	defer tty.Close()
	require.NoError(t, pty.Setsize(tty, &pty.Winsize{Rows: 24, Cols: 117}))

	orig := os.Stdin
	os.Stdin = tty
	defer func() { os.Stdin = orig }()

	w, err := Width()
	require.NoError(t, err)
	assert.Equal(t, 117, w)
}
