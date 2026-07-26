package rmdb

import (
	"database/sql"
	"encoding/xml"
	"fmt"
	"strings"
)

// UniqueID returns RootsMagic's own <UniqueID> for this database: a
// GUID-like string RootsMagic assigns once, at file creation, and never
// changes -- unlike the Home Person (see RootPersonDisplayName), which a
// user can edit at any time. It's read from the same place: ConfigTable's
// Database Configuration record (RecType=1), a plain, human-readable XML
// blob.
//
// Returns ("", nil) -- not an error -- if this can't be determined for any
// reason that isn't itself a database error: no ConfigTable, no RecType=1
// row, or no <UniqueID> in it. This intentionally duplicates the
// ConfigTable read RootPersonDisplayName also does, rather than sharing a
// cached parse -- both are cheap, startup-only operations, and keeping
// them independent keeps each one a self-contained, minimal piece of code.
func (db *DB) UniqueID() (string, error) {
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
		XMLName  xml.Name `xml:"Root"`
		UniqueID string   `xml:"UniqueID"`
	}
	if err := xml.Unmarshal(blob, &cfg); err != nil {
		return "", nil
	}
	return strings.TrimSpace(cfg.UniqueID), nil
}
