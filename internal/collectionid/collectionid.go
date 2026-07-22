// Package collectionid derives human-recognizable Collection ids and
// titles for RootsMagic database files. It makes no uniqueness or
// stability promise beyond what Dedupe provides within a single server
// invocation -- see SCOPE.md's "Multiple databases / Collections" section
// for why that's a deliberate choice, not an oversight: this server can
// never guarantee a database is represented by the same Collection id
// across restarts (the Home Person can be changed by the user, files get
// renamed/copied/restored from backup), so it doesn't try to. Instead it
// aims for ids/titles a human can recognize at a glance, and the server
// prints an id-to-file table at startup so a person can always verify
// which Collection is which for the session about to start.
package collectionid

import (
	"fmt"
	"regexp"
	"strings"
)

// nonSlugChars matches any run of characters that aren't ASCII letters or
// digits, so they can be collapsed into a single hyphen.
var nonSlugChars = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// Slugify converts s into a lowercase, URL-safe slug: runs of anything
// that isn't an ASCII letter or digit become a single hyphen, and
// leading/trailing hyphens are trimmed.
//
// Non-ASCII letters (accented characters, non-Latin scripts) are dropped
// rather than transliterated. That's a real, deliberate limitation, not an
// oversight -- proper transliteration needs Unicode tables this project
// doesn't otherwise depend on (see the project's general preference for
// the standard library where practical) -- and it means a name like "José
// García" slugifies to something like "jos-garc-a" rather than
// "jose-garcia". The id is still unique-ish and non-empty, just not
// pretty, for names outside ASCII; the human-readable title (see Derive)
// keeps the original characters, so nothing is lost, only the id.
func Slugify(s string) string {
	slug := nonSlugChars.ReplaceAllString(s, "-")
	return strings.ToLower(strings.Trim(slug, "-"))
}

// Derive computes a Collection id and title from a database's Home
// Person's display name (as returned by rmdb.RootPersonDisplayName; may be
// "" if undeterminable) and the database file's path.
//
// The id combines both the person's name and the filename when both are
// available, deliberately, rather than preferring one: the person's name
// is what a human actually recognizes a family tree by, but the filename
// is what disambiguates between multiple exports/backups of the *same*
// tree over time, where the Home Person is identical between them and
// only the filename differs (RootsMagic's own auto-backup naming embeds a
// timestamp in the filename for exactly this reason). See SCOPE.md for
// the concrete example this is designed around.
func Derive(rootPersonName, dbPath string) (id, title string) {
	stem := fileStem(dbPath)

	personName := strings.TrimSpace(rootPersonName)
	personSlug := Slugify(personName)
	fileSlug := Slugify(stem)

	switch {
	case personSlug != "" && fileSlug != "":
		id = personSlug + "-" + fileSlug
	case personSlug != "":
		id = personSlug
	case fileSlug != "":
		id = fileSlug
	default:
		id = "collection"
	}

	if personName != "" {
		title = personName + " (" + stem + ")"
	} else {
		title = stem
	}
	return id, title
}

// fileStem returns the filename (no directory, no extension) from a path,
// normalizing backslashes to forward slashes first so this behaves the
// same regardless of the host OS's path convention -- RootsMagic databases
// are frequently referenced by a Windows-style path even when this server
// isn't running on Windows. Mirrors the same normalization approach used
// for MediaPath resolution in internal/rmdb/mediapath.go.
func fileStem(path string) string {
	normalized := strings.ReplaceAll(path, `\`, "/")
	base := normalized
	if i := strings.LastIndex(normalized, "/"); i >= 0 {
		base = normalized[i+1:]
	}
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	return base
}

// Dedupe returns a copy of ids with a numeric suffix ("-2", "-3", ...)
// appended to any id after the first occurrence of a duplicate, so every
// returned id is unique. Order is preserved; the first occurrence of any
// given id is left unsuffixed.
//
// This is a last-resort safety net, not the primary disambiguation
// mechanism -- Derive's person+filename combination already makes
// collisions rare in practice. It exists only to guarantee the server
// never starts with two Collections silently sharing one id (e.g. the
// same file passed twice by accident, or two unrelated trees that happen
// to share both a Home Person name and a filename).
func Dedupe(ids []string) []string {
	out := make([]string, len(ids))
	seen := map[string]int{}
	for i, id := range ids {
		seen[id]++
		if n := seen[id]; n == 1 {
			out[i] = id
		} else {
			out[i] = fmt.Sprintf("%s-%d", id, n)
		}
	}
	return out
}
