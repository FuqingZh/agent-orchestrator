package gitworktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// shrinkRemoveAllRetry makes the retry budget test-fast and forces the retry on
// regardless of platform, restoring the production values afterwards. Without
// the enable override every assertion below would silently pass by falling
// through to a single os.RemoveAll on the non-Windows machines CI runs.
func shrinkRemoveAllRetry(t *testing.T, attempts int) {
	t.Helper()
	origAttempts, origBackoff, origCap := removeAllAttempts, removeAllBackoff, removeAllBackoffCap
	origEnabled := removeAllRetryEnabled
	removeAllAttempts, removeAllBackoff, removeAllBackoffCap = attempts, time.Millisecond, time.Millisecond
	removeAllRetryEnabled = true
	t.Cleanup(func() {
		removeAllAttempts, removeAllBackoff, removeAllBackoffCap = origAttempts, origBackoff, origCap
		removeAllRetryEnabled = origEnabled
	})
}

// stubRemoveAll replaces the real os.RemoveAll for the duration of a test.
func stubRemoveAll(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := removeAll
	removeAll = fn
	t.Cleanup(func() { removeAll = orig })
}

func TestRemoveAllWithRetryRemovesPopulatedDirectory(t *testing.T) {
	shrinkRemoveAllRetry(t, 3)
	dir := t.TempDir()
	target := filepath.Join(dir, "worktree")
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "nested", "file.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := removeAllWithRetry(context.Background(), target); err != nil {
		t.Fatalf("removeAllWithRetry: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("stat after remove = %v, want not-exist", err)
	}
}

// TestRemoveAllWithRetryRepairsOwnerReadOnlyDirectories is the regression for
// renv's sandbox layout: renv intentionally creates owner-owned 0500 package
// directories containing symlinks into the system R library. AO owns the
// worktree and must be able to remove that layout without following or
// modifying the symlink target.
func TestRemoveAllWithRetryRepairsOwnerReadOnlyDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce Unix directory permission bits")
	}

	root := t.TempDir()
	target := filepath.Join(root, "worktree")
	restricted := filepath.Join(target, "renv", "sandbox", "linux", "R-4.5", "hash")
	if err := os.MkdirAll(restricted, 0o750); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(root, "system-library")
	if err := os.MkdirAll(outside, 0o750); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outside, "base-package")
	if err := os.WriteFile(marker, []byte("must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(outside, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(outside, 0o700) })
	if err := os.Symlink(outside, filepath.Join(restricted, "base")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(restricted, 0o500); err != nil {
		t.Fatal(err)
	}
	// Restore permissions if the assertion fails so testing.TempDir can clean up.
	t.Cleanup(func() { _ = os.Chmod(restricted, 0o700) })

	if err := removeAllWithRetry(context.Background(), target); err != nil {
		t.Fatalf("removeAllWithRetry on renv sandbox: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("worktree stat after remove = %v, want not-exist", err)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "must survive" {
		t.Fatalf("symlink target changed: content=%q err=%v", got, err)
	}
	info, err := os.Stat(outside)
	if err != nil {
		t.Fatalf("stat symlink target: %v", err)
	}
	if info.Mode().Perm() != 0o500 {
		t.Fatalf("symlink target mode = %v, want 0500", info.Mode().Perm())
	}
}

// A path that was never there is success, matching os.RemoveAll's own
// semantics — teardown must be idempotent.
func TestRemoveAllWithRetryTreatsMissingPathAsSuccess(t *testing.T) {
	shrinkRemoveAllRetry(t, 3)
	if err := removeAllWithRetry(context.Background(), filepath.Join(t.TempDir(), "never-existed")); err != nil {
		t.Fatalf("removeAllWithRetry on a missing path: %v", err)
	}
}

// TestRemoveAllWithRetrySucceedsAfterTransientFailure is the regression for the
// Windows sharing violation this retry exists for: `ao session kill` destroys a
// session's scoped shell terminal and then removes its worktree in the same
// request, but the PTY's handle on that directory can outlive the call that
// killed it, so the first removal fails and a moment later succeeds.
func TestRemoveAllWithRetrySucceedsAfterTransientFailure(t *testing.T) {
	shrinkRemoveAllRetry(t, 5)
	sharingViolation := errors.New("The process cannot access the file because it is being used by another process")
	var calls int
	stubRemoveAll(t, func(string) error {
		calls++
		if calls < 3 {
			return sharingViolation
		}
		return nil
	})

	if err := removeAllWithRetry(context.Background(), "/managed/worktrees/demo/demo-1"); err != nil {
		t.Fatalf("removeAllWithRetry did not recover from a transient lock: %v", err)
	}
	if calls != 3 {
		t.Errorf("removeAll calls = %d, want 3 (two failures then success)", calls)
	}
}

// A path that never becomes removable must surface the error rather than
// retrying forever — a permanently wedged directory has to fail loudly.
func TestRemoveAllWithRetryGivesUpAndReturnsError(t *testing.T) {
	shrinkRemoveAllRetry(t, 4)
	wedged := errors.New("still locked")
	var calls int
	stubRemoveAll(t, func(string) error {
		calls++
		return wedged
	})

	err := removeAllWithRetry(context.Background(), "/managed/worktrees/demo/demo-1")
	if !errors.Is(err, wedged) {
		t.Fatalf("error = %v, want the underlying removal error", err)
	}
	if calls != 4 {
		t.Errorf("removeAll calls = %d, want the full budget of 4", calls)
	}
}

// os.ErrNotExist must short-circuit immediately: a worktree already gone is
// not a failure to retry against.
func TestRemoveAllWithRetryDoesNotRetryMissingPath(t *testing.T) {
	shrinkRemoveAllRetry(t, 5)
	var calls int
	stubRemoveAll(t, func(string) error {
		calls++
		return os.ErrNotExist
	})

	if err := removeAllWithRetry(context.Background(), "/managed/worktrees/demo/demo-1"); err != nil {
		t.Fatalf("removeAllWithRetry: %v", err)
	}
	if calls != 1 {
		t.Errorf("removeAll calls = %d, want 1 — a missing path must not be retried", calls)
	}
}

// TestRemoveAllWithRetrySkipsRetryOffWindows: the sharing violation is a
// Windows handle-release artifact, so elsewhere a removal failure is real and
// immediate. Sleeping out the budget there would only make every genuine
// failure ~7s slower before returning the identical error.
func TestRemoveAllWithRetrySkipsRetryOffWindows(t *testing.T) {
	shrinkRemoveAllRetry(t, 5)
	removeAllRetryEnabled = false // stand in for a non-Windows GOOS
	wedged := errors.New("EACCES")
	var calls int
	stubRemoveAll(t, func(string) error {
		calls++
		return wedged
	})

	err := removeAllWithRetry(context.Background(), "/managed/worktrees/demo/demo-1")
	if !errors.Is(err, wedged) {
		t.Fatalf("error = %v, want the underlying removal error", err)
	}
	if calls != 1 {
		t.Errorf("removeAll calls = %d, want 1 — no retry off Windows", calls)
	}
}

// TestRemoveAllWithRetryStopsOnContextCancellation: time.Sleep is
// uninterruptible, so without a ctx check a caller that has already given up
// still pays out the remaining budget — and Session Manager tears a workspace
// project's repos down serially, so that cost repeats per repo while the
// session's shell-terminal gate is held.
func TestRemoveAllWithRetryStopsOnContextCancellation(t *testing.T) {
	shrinkRemoveAllRetry(t, 50)
	removeAllBackoff, removeAllBackoffCap = 20*time.Millisecond, 20*time.Millisecond
	wedged := errors.New("still locked")
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	stubRemoveAll(t, func(string) error {
		calls++
		if calls == 2 {
			cancel()
		}
		return wedged
	})

	err := removeAllWithRetry(ctx, "/managed/worktrees/demo/demo-1")
	if !errors.Is(err, wedged) {
		t.Fatalf("error = %v, want the underlying removal error, not the ctx error", err)
	}
	if calls > 3 {
		t.Errorf("removeAll calls = %d, want the loop to stop right after cancellation, not run all 50", calls)
	}
}
