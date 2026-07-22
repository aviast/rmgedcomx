// Package rmdb provides read-only access to a RootsMagic SQLite database. It
// discovers the actual columns present in each table at startup (rather than
// hard-coding a single schema version) so that it works unmodified across
// RootsMagic 7-11 files; see SCOPE.md at the repository root for details.
// RootsMagic 6 and earlier are out of scope and are rejected at startup.
package rmdb

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"modernc.org/sqlite"
)

// RootsMagic declares several indexed text columns (PlaceTable.Name,
// SourceTable.Name, etc.) with "COLLATE RMNOCASE", a custom collation
// RootsMagic itself implements (see https://github.com/mooredan/unifuzz,
// which documents and reimplements it as a loadable SQLite extension) to
// emulate Windows' Unicode case-insensitive string comparison. SQLite needs
// a collation named RMNOCASE registered on the connection for any query
// that touches those columns (including implicitly, via ORDER BY or an
// index), or it fails with "no such collation sequence: RMNOCASE".
//
// We approximate it with Go's Unicode-aware case-insensitive comparison
// (strings.ToLower already case-folds beyond ASCII). This is sufficient for
// correct query results and a reasonable sort order. What it does NOT
// reproduce is Windows' accent/diacritic-insensitivity (unifuzz emulates
// that more precisely via Wine's collation logic) -- so "café" and "cafe"
// will sort as distinct here where RootsMagic on Windows may treat them as
// equal. That only affects sort order and place/source name matching, not
// correctness of which rows are returned, and can be refined later (e.g.
// with golang.org/x/text/unicode/norm to strip combining marks) if it
// matters for a given file.
var registerCollationOnce sync.Once

func registerCollation() {
	registerCollationOnce.Do(func() {
		sqlite.MustRegisterCollationUtf8("RMNOCASE", func(a, b string) int {
			return strings.Compare(strings.ToLower(a), strings.ToLower(b))
		})
	})
}

// requiredTablesAndColumns lists, for each table this server reads, the
// columns it actually depends on. RootsMagic 7 is the minimum supported
// version (see SCOPE.md); if a required table or column is missing, Open
// fails with a clear error rather than silently returning incomplete data.
var requiredTablesAndColumns = map[string][]string{
	"PersonTable":       {"PersonID", "Sex", "Living", "ParentID", "SpouseID"},
	"NameTable":         {"NameID", "OwnerID", "Surname", "Given", "Prefix", "Suffix", "Nickname", "NameType", "IsPrimary", "SortDate", "BirthYear", "DeathYear", "Date"},
	"FamilyTable":       {"FamilyID", "FatherID", "MotherID"},
	"ChildTable":        {"RecID", "ChildID", "FamilyID", "RelFather", "RelMother", "ChildOrder"},
	"EventTable":        {"EventID", "EventType", "OwnerType", "OwnerID", "FamilyID", "PlaceID", "Date", "IsPrimary", "Details", "Note"},
	"FactTypeTable":     {"FactTypeID", "Name", "GedcomTag", "OwnerType"},
	"PlaceTable":        {"PlaceID", "PlaceType", "Name", "Latitude", "Longitude", "Note"},
	"SourceTable":       {"SourceID", "Name", "RefNumber", "ActualText", "Comments"},
	"CitationTable":     {"CitationID", "SourceID"},
	"CitationLinkTable": {"LinkID", "CitationID", "OwnerType", "OwnerID"},
	"MultimediaTable":   {"MediaID", "MediaType", "MediaPath", "MediaFile", "Caption", "RefNumber", "Date", "Description"},
	"MediaLinkTable":    {"LinkID", "MediaID", "OwnerType", "OwnerID"},
}

// optionalMarkerTables are used only to produce a best-effort, informational
// "which RootsMagic version is this" hint; nothing depends on them being
// present.
var optionalMarkerTables = []string{"DNATable", "FamilySearchTable", "AncestryTable", "LinkAncestryTable", "HealthTable"}

// DB is a read-only handle to a RootsMagic SQLite database.
type DB struct {
	sql     *sql.DB
	columns map[string]map[string]bool // table -> column set, lower-cased
}

// Open opens the RootsMagic file at path read-only and verifies it has the
// tables/columns this server requires.
//
// This is true, OS-enforced read-only access: SQLite's own URI filename
// parser (part of the actual SQLite C engine, which modernc.org/sqlite
// transpiles rather than reimplements) recognizes the standard "mode=ro"
// query parameter and restricts the connection accordingly -- confirmed
// empirically (see SCOPE.md's "SQLite driver" section) that this holds
// regardless of the programmatic open flags the Go driver layer passes in.
// A write attempt fails with SQLITE_READONLY, and a missing file fails to
// open at all rather than being silently created.
func Open(path string) (*DB, error) {
	return open(path, true)
}

// open is the single place that decides whether the connection is
// read-only. Today it's always called with readOnly=true from Open --
// write support isn't implemented yet (see SCOPE.md) -- but when a
// `-write` flag is added later, it should thread a bool through to here
// (e.g. an OpenForWriting, or an Open variant that takes readOnly) rather
// than have read/write behavior decided in more than one place.
func open(path string, readOnly bool) (*DB, error) {
	registerCollation()

	mode := "rw"
	if readOnly {
		mode = "ro"
	}
	dsn := fmt.Sprintf("file:%s?mode=%s", path, mode)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("connecting to database at %s: %w", path, err)
	}

	db := &DB{sql: sqlDB, columns: map[string]map[string]bool{}}
	if err := db.loadColumns(); err != nil {
		return nil, err
	}
	if err := db.checkCapabilities(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error { return db.sql.Close() }

func (db *DB) loadColumns() error {
	rows, err := db.sql.Query(`SELECT name FROM sqlite_master WHERE type IN ('table','view')`)
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, table := range tables {
		cols, err := db.tableColumns(table)
		if err != nil {
			return fmt.Errorf("inspecting table %s: %w", table, err)
		}
		set := make(map[string]bool, len(cols))
		for _, c := range cols {
			set[strings.ToLower(c)] = true
		}
		db.columns[table] = set
	}
	return nil
}

func (db *DB) tableColumns(table string) ([]string, error) {
	// PRAGMA table_info doesn't support bound parameters; table names come
	// only from sqlite_master itself (never user input), so this is safe.
	rows, err := db.sql.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

// hasTable reports whether the named table exists in the database.
func (db *DB) hasTable(table string) bool {
	_, ok := db.columns[table]
	return ok
}

// hasColumn reports whether the named table has the named column
// (case-insensitive).
func (db *DB) hasColumn(table, column string) bool {
	cols, ok := db.columns[table]
	if !ok {
		return false
	}
	return cols[strings.ToLower(column)]
}

func (db *DB) checkCapabilities() error {
	var missing []string
	for table, cols := range requiredTablesAndColumns {
		if !db.hasTable(table) {
			missing = append(missing, table+" (table)")
			continue
		}
		for _, col := range cols {
			if !db.hasColumn(table, col) {
				missing = append(missing, table+"."+col)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"this file doesn't look like a RootsMagic 7 or later database: missing %s "+
				"(see SCOPE.md)",
			strings.Join(missing, ", "),
		)
	}
	return nil
}

// SchemaHint returns a short, best-effort, informational description of
// which RootsMagic version range produced this file. It never affects query
// behavior.
func (db *DB) SchemaHint() string {
	present := map[string]bool{}
	for _, t := range optionalMarkerTables {
		present[t] = db.hasTable(t)
	}
	switch {
	case present["FamilySearchTable"] && present["DNATable"]:
		return "RootsMagic 9-11 (or compatible)"
	case present["DNATable"]:
		return "RootsMagic 8-11 (or compatible)"
	default:
		return "RootsMagic 7-11 (or compatible)"
	}
}
