package main

import (
	"fmt"
	"log/slog"
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
// (found=false, err=nil) everywhere else. On Windows it uses the native
// process snapshot API, rather than spawning `tasklist`: that continues to
// work in restricted environments which prohibit child processes.
func isRootsMagicRunning() (found bool, err error) {
	return rootsMagicRunning()
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
		slog.Warn("couldn't check whether RootsMagic is running -- proceeding, but please make sure RootsMagic is closed before using -write", "error", err)
		return nil
	}
	if found {
		return fmt.Errorf("RootsMagic.exe appears to be running -- close RootsMagic before starting rmgedcomx with -write (running both against the same file at once risks corrupting it)")
	}
	return nil
}
