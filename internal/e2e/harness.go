// Package e2e provides test-support helpers for end-to-end tests that exercise
// the real spr binaries as subprocesses.
//
// The binaries are built instrumented (`go build -cover`) so that, when run with
// GOCOVERDIR set, they emit Go binary coverage data ("covdata"). The make/CI
// pipeline merges that e2e covdata with the unit-test covdata so that code paths
// reachable only by running the real binary (e.g. os.Exit branches) are credited.
//
// This package is test support, not tests themselves; it must not be a _test.go
// file because later e2e scenario tests import it. It is excluded from the
// coverage gate (see .testcoverage.yml).
package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// repoRoot returns the absolute path of the repository root (the directory that
// contains go.mod). Tests in this package run with CWD = internal/e2e, so we walk
// up from this source file's directory to locate go.mod. Computing it from the
// source file (via runtime.Caller) is robust regardless of the test's CWD.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("e2e: unable to determine source file location")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("e2e: could not find go.mod walking up from %s", filepath.Dir(thisFile))
		}
		dir = parent
	}
}

// binCache caches built instrumented binaries by package import path so repeated
// BuildBinary calls within a `go test` process do not rebuild.
var (
	binCacheMu sync.Mutex
	binCache   = map[string]string{}
)

// BuildBinary builds an instrumented (coverage-enabled) binary for the given Go
// package and returns the absolute path to the built executable. pkg should be a
// full import path such as "github.com/ejoffe/spr/cmd/spr"; for convenience a
// leading-"./" relative path like "./cmd/spr" is also accepted and resolved
// against the module root.
//
// The build runs `go build -cover -covermode=atomic` from the repo root so it is
// independent of the test's working directory. Results are cached per package for
// the lifetime of the test binary process: the binary is written into a
// process-lifetime temp dir (os.MkdirTemp, NOT t.TempDir) so a cached path stays
// valid for every test in the process, even after the first caller's test ends.
// The OS reclaims the dir on process exit.
func BuildBinary(t *testing.T, pkg string) string {
	t.Helper()

	binCacheMu.Lock()
	defer binCacheMu.Unlock()
	if path, ok := binCache[pkg]; ok {
		return path
	}

	root := repoRoot(t)

	// Normalise a leading "./" relative package into a full import path so the
	// build is unaffected by CWD. "../" paths are not supported (no caller needs
	// them and they cannot be resolved into a module import path here).
	buildPkg := pkg
	if strings.HasPrefix(pkg, "../") {
		t.Fatalf("e2e: BuildBinary does not support %q; pass a full import path or a leading-\"./\" path", pkg)
	}
	if strings.HasPrefix(pkg, "./") {
		buildPkg = "github.com/ejoffe/spr/" + filepath.ToSlash(filepath.Clean(strings.TrimPrefix(pkg, "./")))
	}

	name := filepath.Base(buildPkg)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	// Use a process-lifetime dir (not t.TempDir) so cached binaries outlive the
	// test that first built them.
	binDir, err := os.MkdirTemp("", "e2e-bin-")
	if err != nil {
		t.Fatalf("e2e: creating bin dir: %v", err)
	}
	out := filepath.Join(binDir, name)

	cmd := exec.Command("go", "build", "-cover", "-covermode=atomic", "-o", out, buildPkg)
	cmd.Dir = root
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("e2e: building %s failed: %v\n%s", buildPkg, err, combined)
	}

	binCache[pkg] = out
	return out
}

// coverDir returns the GOCOVERDIR to use for a single subprocess run.
//
//   - If SPR_E2E_COVERDIR is set and non-empty (as the make/CI pipeline does), a
//     unique subdirectory under it is created and returned, so concurrent or
//     repeated runs never clobber each other's covdata.
//   - Otherwise t.TempDir() is returned, so a plain `go test ./...` still runs
//     the scenarios and asserts behaviour; the covdata is simply discarded with
//     the temp dir.
func coverDir(t *testing.T) string {
	t.Helper()
	base := os.Getenv("SPR_E2E_COVERDIR")
	if base == "" {
		return t.TempDir()
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("e2e: creating SPR_E2E_COVERDIR %s: %v", base, err)
	}
	dir, err := os.MkdirTemp(base, "run-")
	if err != nil {
		t.Fatalf("e2e: creating unique coverdir under %s: %v", base, err)
	}
	return dir
}

// Run executes the given instrumented binary with the supplied arguments and an
// environment that has GOCOVERDIR injected (so coverage data is emitted). extra
// env entries are appended to the current process environment.
//
// It captures stdout and stderr and returns them as strings along with the
// process exit code. A non-zero exit does NOT fail the test: scenarios assert
// specific exit codes themselves.
func Run(t *testing.T, bin string, env []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return RunInDir(t, "", bin, env, args...)
}

// RunInDir is like Run but runs the subprocess with its working directory set to
// dir (so scenarios can run the binary inside a specific git repo, or inside a
// non-repo directory). If dir is empty the subprocess inherits the test's CWD,
// preserving Run's original behaviour. GOCOVERDIR is injected exactly as Run does.
func RunInDir(t *testing.T, dir, bin string, env []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "GOCOVERDIR="+coverDir(t))

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("e2e: running %s %v failed to start: %v", bin, args, err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// InitTempRepo creates a temporary git repository suitable for e2e tests and
// returns its path. It mirrors git/realgit/realcmd_test.go's initTempRepo (init +
// user config + an initial commit so HEAD exists) and additionally adds an
// "origin" remote pointing at a GitHub URL so spr's owner/name auto-detection
// succeeds.
func InitTempRepo(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = tmp
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("e2e: git %v: %v\n%s", args, err, out)
		}
	}

	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(tmp, "README"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("e2e: writing README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "init")

	// Add a GitHub-shaped remote so owner/name auto-detection succeeds.
	run("remote", "add", "origin", "git@github.com:acme/widgets.git")

	return tmp
}
