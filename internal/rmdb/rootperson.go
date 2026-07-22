package rmdb

import (
	"database/sql"
	"encoding/xml"
	"fmt"
	"strings"
)

// RootPersonDisplayName returns the display name of this database's "Home
// Person" -- RootsMagic's own nominated primary/starting person, stored as
// <RootPerson> in ConfigTable's Database Configuration record (RecType=1),
// which is otherwise a plain, human-readable XML blob (confirmed by
// inspection, not documented in the data dictionary -- see SCOPE.md's
// "Multiple databases / Collections" section).
//
// Returns ("", nil) -- not an error -- if this can't be determined for any
// reason that isn't itself a database error: no ConfigTable, no
// <RootPerson> set, or no name on record for that person. This is used
// only to build a human-recognizable Collection id/title (see
// internal/collectionid), so an inability to determine it is never fatal
// to opening the database; callers should treat a genuine error the same
// way (fall back to something else) rather than refuse to start.
func (db *DB) RootPersonDisplayName() (string, error) {
	if !db.hasTable("ConfigTable") {
		return "", nil
	}

	var blob []byte
	row := db.sql.QueryRow(`SELECT DataRec FROM ConfigTable WHERE RecType = 1 LIMIT 1`)
	if err := row.Scan(&blob); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("reading database configuration: %w", err)
	}

	var cfg struct {
		XMLName    xml.Name `xml:"Root"`
		RootPerson int64    `xml:"RootPerson"`
	}
	if err := xml.Unmarshal(blob, &cfg); err != nil || cfg.RootPerson == 0 {
		return "", nil
	}

	name, err := db.PrimaryName(cfg.RootPerson)
	if err != nil {
		return "", fmt.Errorf("looking up root person %d: %w", cfg.RootPerson, err)
	}
	if name == nil {
		return "", nil
	}

	var parts []string
	for _, p := range []string{name.Prefix, name.Given, name.Surname, name.Suffix} {
		if s := strings.TrimSpace(p); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " "), nil
}
