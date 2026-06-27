package pretty

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// testObject is a simple struct used across multiple tests.
type testObject struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// --- PrettyWriter ---

func TestPrettyWriter_SimpleStruct(t *testing.T) {
	var buf bytes.Buffer
	obj := testObject{Name: "alice", Value: 42}
	PrettyWriter(obj, &buf)

	got := buf.String()
	require.Contains(t, got, `"name": "alice"`)
	require.Contains(t, got, `"value": 42`)
	// MarshalIndent output uses two-space indent.
	require.Contains(t, got, "  ")
}

func TestPrettyWriter_Map(t *testing.T) {
	var buf bytes.Buffer
	obj := map[string]int{"x": 1}
	PrettyWriter(obj, &buf)

	got := buf.String()
	require.Contains(t, got, `"x"`)
	require.Contains(t, got, `1`)
}

func TestPrettyWriter_PanicOnUnencodable(t *testing.T) {
	var buf bytes.Buffer
	// A channel cannot be JSON-marshalled; check() should panic.
	require.Panics(t, func() {
		PrettyWriter(make(chan int), &buf)
	})
}

// --- PrefixPrettyWriter ---

func TestPrefixPrettyWriter_WithPrefix(t *testing.T) {
	var buf bytes.Buffer
	obj := testObject{Name: "bob", Value: 7}
	PrefixPrettyWriter(&buf, "myprefix", obj)

	got := buf.String()
	require.True(t, strings.HasPrefix(got, "myprefix: "), "expected output to start with 'myprefix: ', got: %q", got)
	require.Contains(t, got, `"name": "bob"`)
	require.Contains(t, got, `"value": 7`)
}

func TestPrefixPrettyWriter_EmptyPrefix(t *testing.T) {
	var buf bytes.Buffer
	obj := testObject{Name: "carol", Value: 3}
	PrefixPrettyWriter(&buf, "", obj)

	got := buf.String()
	// With empty prefix the output should start with the JSON directly (no "prefix: " leader).
	require.False(t, strings.HasPrefix(got, ": "), "unexpected colon-space prefix in output: %q", got)
	require.Contains(t, got, `"name": "carol"`)
}

func TestPrefixPrettyWriter_PanicOnUnencodable(t *testing.T) {
	var buf bytes.Buffer
	require.Panics(t, func() {
		PrefixPrettyWriter(&buf, "p", func() {})
	})
}

// --- PrettyString ---

func TestPrettyString_ReturnsJSON(t *testing.T) {
	obj := testObject{Name: "dave", Value: 99}
	got := PrettyString(obj)

	require.Contains(t, got, `"name": "dave"`)
	require.Contains(t, got, `"value": 99`)
	// Should end with a newline (pretty library adds one).
	require.True(t, strings.HasSuffix(got, "\n"), "expected trailing newline, got: %q", got)
}

func TestPrettyString_Map(t *testing.T) {
	obj := map[string]string{"key": "val"}
	got := PrettyString(obj)
	require.Contains(t, got, `"key"`)
	require.Contains(t, got, `"val"`)
}

// --- PrefixPretty (writes to os.Stdout) ---

func TestPrefixPretty_WithPrefix(t *testing.T) {
	// Redirect os.Stdout to /dev/null so output doesn't leak to test stream.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = devNull
	defer func() {
		os.Stdout = orig
		_ = devNull.Close()
	}()

	// Should not panic.
	require.NotPanics(t, func() {
		PrefixPretty("section", testObject{Name: "eve", Value: 5})
	})
}

func TestPrefixPretty_EmptyPrefix(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = devNull
	defer func() {
		os.Stdout = orig
		_ = devNull.Close()
	}()

	require.NotPanics(t, func() {
		PrefixPretty("", testObject{Name: "frank", Value: 0})
	})
}

// --- PrettyPrint (delegates to PrefixPretty with empty prefix) ---

func TestPrettyPrint(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = devNull
	defer func() {
		os.Stdout = orig
		_ = devNull.Close()
	}()

	require.NotPanics(t, func() {
		PrettyPrint(testObject{Name: "grace", Value: 1})
	})
}

// --- check (via PrefixPretty with unencodable, captured) ---

// TestCheckNilIsNoop verifies that check(nil) does not panic.
// This is exercised indirectly by every successful encoding test, but we make
// the nil path explicit here for documentation.
func TestCheckNilIsNoop(t *testing.T) {
	// check is unexported; exercise it by calling PrettyString on a simple value.
	require.NotPanics(t, func() {
		_ = PrettyString(42)
	})
}
