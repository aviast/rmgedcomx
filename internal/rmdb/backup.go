package rmdb

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EnsureBackup copies this database's source file to a timestamped backup
// in the same directory, the first time it's called for this *DB instance
// -- subsequent calls are no-ops that return the same result, via
// sync.Once, so every future write handler can call this defensively,
// unconditionally, before it does anything, without worrying about making
// redundant copies. Returns the backup file's path.
//
// This is meant to run once per server session, before the first real
// write (see SCOPE.md's "Write support" section for the staged plan this
// is part of) -- not on every write, and not at startup regardless of
// whether a write ever actually happens.
//
// This isn't a substitute for RootsMagic's own "Backup" feature, and
// doesn't try to be -- it's a narrower, automatic safety net specifically
// for changes made by this server, so a mistake (a bug here, a bad
// request, this server writing something RootsMagic doesn't expect) can
// always be undone by restoring this one file, without depending on the
// user having remembered to make their own backup first.
func (db *DB) EnsureBackup() (string, error) {
	db.backupOnce.Do(func() {
		db.backupPath, db.backupErr = copyToTimestampedBackup(db.path)
	})
	return db.backupPath, db.backupErr
}

func copyToTimestampedBackup(path string) (string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	backupPath := filepath.Join(dir, fmt.Sprintf("%s-backup-%s%s", stem, time.Now().Format("20060102-150405"), ext))

	src, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening %s to back it up: %w", path, err)
	}
	defer src.Close()

	// O_EXCL: refuse to overwrite an existing file at the backup path
	// rather than silently clobber a previous backup -- this should never
	// actually collide (the timestamp has second resolution and this only
	// runs once per DB instance), but if it somehow did, overwriting a
	// backup is exactly the kind of silent data loss this mechanism exists
	// to prevent.
	dst, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("creating backup file %s: %w", backupPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(backupPath) // don't leave a partial, corrupt-looking backup behind
		return "", fmt.Errorf("copying %s to %s: %w", path, backupPath, err)
	}

	return backupPath, nil
}
