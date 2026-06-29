package cliexit

import (
	"os"
	"testing"
)

// withArgs swaps os.Args for the duration of fn and restores it afterwards.
func withArgs(t *testing.T, args []string, fn func()) {
	t.Helper()
	saved := os.Args
	os.Args = args
	defer func() { os.Args = saved }()
	fn()
}

func TestVerbose_SPRDebugEnv(t *testing.T) {
	t.Setenv("SPR_DEBUG", "1")
	// Args without a verbose flag: the env var alone must trigger verbose.
	withArgs(t, []string{"spr"}, func() {
		if !verbose() {
			t.Fatal("verbose() = false; want true when SPR_DEBUG=1")
		}
	})
}

func TestVerbose_SPRDebugEnvNotOne(t *testing.T) {
	// A non-"1" value must NOT enable verbose via the env path.
	t.Setenv("SPR_DEBUG", "0")
	withArgs(t, []string{"spr"}, func() {
		if verbose() {
			t.Fatal("verbose() = true; want false when SPR_DEBUG=0 and no flag")
		}
	})
}

func TestVerbose_Flags(t *testing.T) {
	// Make sure the env path is off so we exercise the flag scan.
	t.Setenv("SPR_DEBUG", "")
	for _, flag := range []string{"--verbose", "-verbose", "--debug", "-debug"} {
		flag := flag
		t.Run(flag, func(t *testing.T) {
			withArgs(t, []string{"spr", "status", flag}, func() {
				if !verbose() {
					t.Fatalf("verbose() = false; want true when %q present in os.Args", flag)
				}
			})
		})
	}
}

func TestVerbose_Default(t *testing.T) {
	t.Setenv("SPR_DEBUG", "")
	withArgs(t, []string{"spr", "update", "--unrelated"}, func() {
		if verbose() {
			t.Fatal("verbose() = true; want false with no debug env and no verbose flag")
		}
	})
}

func TestHandlePanic_NilRecover(t *testing.T) {
	// Called with no active panic, recover() is nil so HandlePanic must return
	// normally without calling os.Exit or re-raising. If it did either, the test
	// process would die / fail.
	t.Setenv("SPR_DEBUG", "")
	withArgs(t, []string{"spr"}, func() {
		HandlePanic()
	})
}
