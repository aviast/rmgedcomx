package main

import (
	"log"
	"sync"
	"time"
)

// writeGuard re-checks whether RootsMagic is running before every write,
// not just once at server startup the way checkRootsMagicNotRunning was
// originally (and still is) used in main(). The startup-only check is
// fine for what it protects: it stops -write from ever starting alongside
// a currently-running RootsMagic. It says nothing about ten minutes
// later, once the server's already running -- and this server is meant
// to run for a long time, serving a client (GEDAM) that could be making
// writes at any point across that whole lifetime. RootsMagic being opened
// *after* startup is a real scenario, not a hypothetical one, and a
// startup-only check has no way to ever notice it.
//
// The actual risk being protected against, worth stating precisely:
// SQLite's own locking already prevents two processes from corrupting the
// file through genuinely concurrent writes -- that part was never in
// question. The real risk is RootsMagic itself receiving a SQLITE_BUSY
// error in a code path it was never written to expect, with unknown
// consequences. This guard exists to make sure RootsMagic never sees
// that, not to add a second layer of protection against corruption
// SQLite already prevents on its own.
//
// Two deliberate design choices, both by explicit request rather than
// this project's own default assumption:
//
//   - Rate-limited to once per checkInterval, not checked on every single
//     write. Repeatedly shelling out to tasklist on every request would
//     be wasteful, and the condition being watched for (a human opening
//     RootsMagic) doesn't change faster than a human can act -- infrequent
//     checking is enough to catch it before RootsMagic itself is actually
//     ready to attempt a write of its own. Triggered by a write attempt,
//     not run on a background timer regardless of activity -- a server
//     that's never asked to write should never need to check at all.
//   - Latches permanently once tripped, rather than re-checking and
//     recovering automatically once RootsMagic closes again. The
//     simpler of two reasonable designs, chosen deliberately: once
//     tripped, every write attempt fails with the same clear reason
//     until this server process is restarted, so a person is never left
//     wondering whether a write might silently start working again on
//     its own while they're still unsure what happened. Auto-recovery
//     (re-checking on the same interval and un-tripping once RootsMagic's
//     gone) is a plausible future refinement, not implemented here.
type writeGuard struct {
	mu            sync.Mutex
	tripped       bool
	trippedReason string
	lastChecked   time.Time
	checkInterval time.Duration
	// checkFunc is isRootsMagicRunning in production (set by
	// newWriteGuard); injectable so the rate-limiting/latching state
	// machine below can be unit-tested deterministically, without a real
	// Windows machine or RootsMagic to check against -- see
	// writeguard_test.go.
	checkFunc func() (found bool, err error)
}

func newWriteGuard() *writeGuard {
	return &writeGuard{checkInterval: 10 * time.Second, checkFunc: isRootsMagicRunning}
}

// Allow implements api.WriteGuard.
func (g *writeGuard) Allow() (ok bool, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.tripped {
		return false, g.trippedReason
	}

	if !g.lastChecked.IsZero() && time.Since(g.lastChecked) < g.checkInterval {
		return true, ""
	}
	g.lastChecked = time.Now()

	found, err := g.checkFunc()
	if err != nil {
		// Same reasoning as checkRootsMagicNotRunning's own handling of
		// this: not this server's problem to be blocked by if the check
		// itself can't run. Log it, but don't trip the guard over a
		// failure unrelated to the actual risk being checked for.
		log.Printf("warning: couldn't check whether RootsMagic is running (%v)", err)
		return true, ""
	}
	if found {
		g.tripped = true
		g.trippedReason = "RootsMagic.exe was detected running, so this server has switched to read-only for the rest of this session -- close RootsMagic and restart rmgedcomx to resume write support"
		log.Printf("write disabled: %s", g.trippedReason)
		return false, g.trippedReason
	}
	return true, ""
}
