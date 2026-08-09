package main

import (
	"errors"
	"testing"
	"time"
)

func newTestGuard(checkFunc func() (bool, error)) *writeGuard {
	return &writeGuard{checkInterval: 10 * time.Second, checkFunc: checkFunc}
}

func TestWriteGuardAllowsWhenRootsMagicNotRunning(t *testing.T) {
	calls := 0
	g := newTestGuard(func() (bool, error) {
		calls++
		return false, nil
	})

	ok, reason := g.Allow()
	if !ok {
		t.Fatalf("expected allowed, got denied with reason %q", reason)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 check call, got %d", calls)
	}
}

func TestWriteGuardRateLimitsRepeatedChecks(t *testing.T) {
	calls := 0
	g := newTestGuard(func() (bool, error) {
		calls++
		return false, nil
	})

	// Three calls in quick succession should only actually check once --
	// the other two land within checkInterval of the first.
	g.Allow()
	g.Allow()
	ok, _ := g.Allow()
	if !ok {
		t.Fatal("expected allowed")
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 underlying check across 3 rapid Allow() calls, got %d -- rate limiting isn't working", calls)
	}
}

func TestWriteGuardRechecksAfterInterval(t *testing.T) {
	calls := 0
	g := newTestGuard(func() (bool, error) {
		calls++
		return false, nil
	})
	g.checkInterval = 10 * time.Millisecond // short, so the test doesn't have to wait 10 real seconds

	g.Allow()
	time.Sleep(20 * time.Millisecond)
	g.Allow()

	if calls != 2 {
		t.Fatalf("expected 2 underlying checks after waiting past checkInterval, got %d", calls)
	}
}

func TestWriteGuardLatchesOnceTripped(t *testing.T) {
	calls := 0
	g := newTestGuard(func() (bool, error) {
		calls++
		return true, nil // RootsMagic "found running" every time
	})
	g.checkInterval = 10 * time.Millisecond

	ok, reason := g.Allow()
	if ok {
		t.Fatal("expected denied on first check where RootsMagic is found running")
	}
	if reason == "" {
		t.Fatal("expected a non-empty reason")
	}

	// Wait past checkInterval and try again -- a non-latching design
	// would re-check (and get "still running" again, so the *outcome*
	// would look the same); latching means it must NOT re-check the
	// second time at all, proving the trip is permanent, not just
	// "still currently true."
	time.Sleep(20 * time.Millisecond)
	ok2, reason2 := g.Allow()
	if ok2 {
		t.Fatal("expected still denied after tripping")
	}
	if reason2 != reason {
		t.Fatalf("expected the same latched reason on a later call, got a different one: %q vs %q", reason, reason2)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 underlying check total -- latching should prevent any further checks once tripped, got %d checks", calls)
	}
}

func TestWriteGuardFailsOpenWhenCheckItselfErrors(t *testing.T) {
	g := newTestGuard(func() (bool, error) {
		return false, errors.New("tasklist: command not found")
	})

	ok, _ := g.Allow()
	if !ok {
		t.Fatal("expected allowed when the check itself fails to run -- a broken check shouldn't block writes over a problem unrelated to whether RootsMagic is actually running")
	}
}
