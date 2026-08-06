package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
)

// checkRootsMagicNotRunning refuses to proceed if RootsMagic.exe appears to
// be running, when write mode is enabled -- RootsMagic and rmgedcomx
// writing to the same file at the same time is a real risk of corrupting
// it, not a hypothetical one (see SCOPE.md's "Write support" section).
//
// This only checks at startup. It does not, and cannot, protect against
// someone opening RootsMagic *after* rmgedcomx has already started with
// -write -- that's a real, remaining gap, not something this check closes.
//
// Only meaningful on Windows, where RootsMagic actually runs; a no-op
// everywhere else. Uses `tasklist`, a standard built-in Windows command,
// rather than a new dependency, to avoid the running-process list.
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
func checkRootsMagicNotRunning() error {
	if runtime.GOOS != "windows" {
		return nil
	}

	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq RootsMagic.exe", "/NH").Output()
	if err != nil {
		// Couldn't run the check at all -- e.g. tasklist missing from PATH,
		// which shouldn't happen on any real Windows install, but isn't
		// this server's own problem to be blocked by if it does. Warn
		// loudly and proceed, rather than refuse to start over a failure
		// unrelated to the actual risk being checked for.
		log.Printf("warning: couldn't check whether RootsMagic is running (%v) -- proceeding, but please make sure RootsMagic is closed before using -write", err)
		return nil
	}

	if strings.Contains(strings.ToLower(string(out)), "rootsmagic.exe") {
		return fmt.Errorf("RootsMagic.exe appears to be running -- close RootsMagic before starting rmgedcomx with -write (running both against the same file at once risks corrupting it)")
	}
	return nil
}
