// Package cliexit provides a single top-level panic handler shared by the spr
// command binaries (cmd/spr, cmd/amend).
//
// The command logic in spr/ and git/ reports unrecoverable problems by
// panicking (git.GitInterface.MustGit, git/helpers.go check/panic, and the
// SPR_DEBUG-gated spr.check). Most of these "panics" are really *ordinary*
// expected failures — e.g. running `git spr amend` with nothing staged makes
// git exit non-zero, MustGit panics, and the user sees a full Go goroutine
// stack trace that buries the real git error message.
//
// HandlePanic, installed as a deferred call at the very top of each main(),
// turns those panics into a clean one-line error plus a non-zero exit code.
// The full stack trace is still available on demand, gated behind a verbose
// signal so real bugs in spr remain debuggable.
package cliexit

import (
	"fmt"
	"os"
)

// verbose reports whether the user asked for verbose/debug output, in which
// case HandlePanic re-raises the panic so the original Go stack trace is
// printed. It honours the SPR_DEBUG=1 environment variable (mirroring the
// existing convention in spr.check) so the stack trace can be recovered even
// for failures that happen before CLI flags are parsed.
func verbose() bool {
	if os.Getenv("SPR_DEBUG") == "1" {
		return true
	}
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--verbose", "-verbose", "--debug", "-debug":
			return true
		}
	}
	return false
}

// HandlePanic recovers a panic escaping a command's main(), prints a clean
// error message, and exits with a non-zero status. It is meant to be installed
// with `defer cliexit.HandlePanic()` at the top of main().
//
// When verbose/debug output is requested (--verbose, --debug or SPR_DEBUG=1)
// the panic is re-raised instead so the original Go stack trace is shown — real
// bugs in spr stay debuggable.
//
// The underlying git error text is already written to stderr by realgit before
// the panic propagates here, so HandlePanic only needs to add a concise summary
// line and the non-zero exit code; it does not swallow that earlier output.
func HandlePanic() {
	r := recover()
	if r == nil {
		return
	}
	if verbose() {
		// Re-raise so the runtime prints the full stack trace.
		panic(r)
	}
	if err, ok := r.(error); ok {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "error: %v\n", r)
	}
	os.Exit(1)
}
