package rmdb

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// sqliteHeader is the fixed 16-byte magic string every valid SQLite
// database file begins with -- confirmed directly against a real
// RootsMagic file (`xxd`'d, not just taken from SQLite's own file format
// documentation): 53 51 4c 69 74 65 20 66 6f 72 6d 61 74 20 33 00.
var sqliteHeader = []byte("SQLite format 3\x00")

// validateDatabaseFile checks that path exists, is a regular file (not a
// directory), and starts with SQLite's standard file header, before this
// package attempts to open it as a database.
//
// This exists because SQLite's own errors for these specific cases are
// unhelpful or actively misleading, not because Open otherwise lacked
// error handling. Concretely, the case that prompted this: pointing -db at
// a directory (an easy mistake with shell tab-completion, since a
// directory and a same-prefixed file both complete) doesn't fail with
// anything resembling "that's a directory" -- it fails with "unable to
// open database file: out of memory (14)", which sends a person hunting
// for a memory problem that doesn't exist. Likewise, a non-SQLite file (a
// renamed .rmtree that's actually something else, a corrupted download,
// an accidental GEDCOM export where a database export was meant) produces
// SQLite-internal errors that don't name the actual problem either.
func validateDatabaseFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a RootsMagic database file", path)
	}
	if info.Size() < int64(len(sqliteHeader)) {
		return fmt.Errorf("%s is only %d bytes -- too small to be a SQLite database", path, info.Size())
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	header := make([]byte, len(sqliteHeader))
	if _, err := io.ReadFull(f, header); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if !bytes.Equal(header, sqliteHeader) {
		return fmt.Errorf("%s doesn't look like a SQLite database file (missing the standard SQLite file header) -- is this actually a RootsMagic .rmtree/.rmgc file?", path)
	}
	return nil
}
