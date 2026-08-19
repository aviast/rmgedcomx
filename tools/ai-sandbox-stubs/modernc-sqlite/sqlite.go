// Package sqlite is a stub that wraps the CGO-based mattn/go-sqlite3
// driver under modernc.org/sqlite's own import path, so code written
// against modernc.org/sqlite needs no changes. Registers itself under
// the same driver name ("sqlite") modernc.org/sqlite uses, and provides
// a MustRegisterCollationUtf8 shim (modernc.org/sqlite registers
// collations globally; mattn/go-sqlite3 registers them per-connection
// via a ConnectHook, so this shim applies every registered collation to
// each new connection as it's opened).
package sqlite

import (
	"database/sql"
	"sync"

	sqlite3 "github.com/mattn/go-sqlite3"
)

var (
	collationsMu sync.Mutex
	collations   = map[string]func(string, string) int{}
)

func MustRegisterCollationUtf8(name string, cmp func(string, string) int) {
	collationsMu.Lock()
	defer collationsMu.Unlock()
	collations[name] = cmp
}

func init() {
	sql.Register("sqlite", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			collationsMu.Lock()
			defer collationsMu.Unlock()
			for name, cmp := range collations {
				if err := conn.RegisterCollation(name, cmp); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
