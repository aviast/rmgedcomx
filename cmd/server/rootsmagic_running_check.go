package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
)

// isRootsMagicRunning is the shared detection primitive behind both
// checkRootsMagicNotRunning (the startup-time check) and writeGuard (the
// ongoing, periodic check for a long-running server) -- deliberately
// factored out so each caller can build its own contextually correct
// message (checkRootsMagicNotRunning's is about refusing to *start*;
// writeGuard's is about *already running* and switching to read-only
// mid-session) rather than sharing one message written for only one of
// those two situations.
//
// Only meaningful on Windows, where RootsMagic actually runs; a no-op
// (found=false, err=nil) everywhere else. Uses `tasklist`, a standard
// built-in Windows command, rather than a new dependency, to read the
// running-process list.
//
// A note on how this was verified: everything else in this project has
// been checked empirically, against real data, on the actual platform
// involved -- this piece is the exception. It was developed and tested on
// Linux, which cannot run `tasklist` or RootsMagic at all, so the
// Windows-specific behavior below is implemented carefully against
// `tasklist`'s well-documented output format, but has NOT been run against
// a real Windows machine with RootsMagic actually open. Please verify it
// does what you expect (start rmgedcomx -write with RootsMagic open, and
// confirm it refuses; close RootsMagic, and confirm it proceeds) before
// relying on it.
func isRootsMagicRunning() (found bool, err error) {
	if runtime.GOOS != "windows" {
		return false, nil
	}

	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq RootsMagic.exe", "/NH").Output()
	if err != nil {
		// Couldn't run the check at all -- e.g. tasklist missing from
		// PATH, which shouldn't happen on any real Windows install.
		// Returned as an error so each caller can decide how to react
		// (checkRootsMagicNotRunning warns and proceeds; see its own
		// comment for why failing open here, rather than failing closed,
		// is the deliberate choice for both callers).
		return false, err
	}

	return strings.Contains(strings.ToLower(string(out)), "rootsmagic.exe"), nil
}

// checkRootsMagicNotRunning refuses to proceed if RootsMagic.exe appears to
// be running, when write mode is enabled -- RootsMagic and rmgedcomx
// writing to the same file at the same time is a real risk of corrupting
// it, not a hypothetical one (see SCOPE.md's "Write support" section).
//
// This is the startup-time check only -- called once, from main(), before
// this server starts accepting any requests at all. It does not, and
// cannot, protect against someone opening RootsMagic *after* rmgedcomx has
// already started with -write; that gap is what writeGuard exists to
// close, checked on an ongoing basis rather than once.
func checkRootsMagicNotRunning() error {
	found, err := isRootsMagicRunning()
	if err != nil {
		// Not this server's own problem to be blocked by if the check
		// itself can't run. Warn loudly and proceed, rather than refuse
		// to start over a failure unrelated to the actual risk being
		// checked for.
		log.Printf("warning: couldn't check whether RootsMagic is running (%v) -- proceeding, but please make sure RootsMagic is closed before using -write", err)
		return nil
	}
	if found {
		return fmt.Errorf("RootsMagic.exe appears to be running -- close RootsMagic before starting rmgedcomx with -write (running both against the same file at once risks corrupting it)")
	}
	return nil
}
