package rmdb

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestOpenRejectsPreRM8Database confirms Open refuses a database missing
// UTCModDate on the tables this server's write support actually depends
// on -- see requiredTablesAndColumns's own comment for the full account
// of why RM7 was dropped, and TestOpenAcceptsRM8Database below for the
// corresponding positive case.
//
// Built by taking a real copy of royal92.rmtree (RM8+) and dropping
// UTCModDate from each of the five tables that gained it in RM8, rather
// than a synthetic from-scratch schema -- this way the test is exercising
// the actual real-world failure mode (an otherwise-valid, fully
// real database that merely predates UTCModDate), not a hypothetical one.
func TestOpenRejectsPreRM8Database(t *testing.T) {
	dbPath := t.TempDir() + "/fake_rm7.rmtree"
	data, err := os.ReadFile("../../royal92.rmtree")
	if err != nil {
		t.Fatalf("reading royal92.rmtree: %v", err)
	}
	if err := os.WriteFile(dbPath, data, 0o644); err != nil {
		t.Fatalf("writing test copy: %v", err)
	}

	// Direct connection, not rmdb.Open -- registerCollation is called
	// here explicitly, matching what Open itself does, since ALTER TABLE
	// needs RMNOCASE available to rebuild PlaceTable's/SourceTable's own
	// indexes during the column drop.
	registerCollation()
	raw, err := sql.Open("sqlite", "file:"+dbPath+"?mode=rw")
	if err != nil {
		t.Fatalf("opening raw connection: %v", err)
	}
	for _, table := range []string{"PlaceTable", "SourceTable", "MultimediaTable", "MediaLinkTable", "ConfigTable"} {
		if _, err := raw.Exec("ALTER TABLE " + table + " DROP COLUMN UTCModDate"); err != nil {
			t.Fatalf("dropping UTCModDate from %s: %v", table, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(dbPath, true)
	if err == nil {
		t.Fatal("expected Open to reject a database missing UTCModDate, but it succeeded")
	}
	if !strings.Contains(err.Error(), "RootsMagic 8 or later") {
		t.Errorf("expected error to mention \"RootsMagic 8 or later\", got: %v", err)
	}
	// All five tables' missing columns should be named, not just the
	// first one encountered -- a person fixing this should see the full
	// picture in one error, not have to re-run Open five times.
	for _, table := range []string{"PlaceTable", "SourceTable", "MultimediaTable", "MediaLinkTable", "ConfigTable"} {
		if !strings.Contains(err.Error(), table+".UTCModDate") {
			t.Errorf("expected error to mention %s.UTCModDate, got: %v", table, err)
		}
	}
}

// TestOpenAcceptsRM8Database confirms a real, unmodified RM8+ database
// (royal92.rmtree) is accepted, and that SchemaHint no longer reports a
// range starting below 8 now that anything older is rejected outright --
// see SchemaHint's own comment.
func TestOpenAcceptsRM8Database(t *testing.T) {
	db, err := Open("../../royal92.rmtree", true)
	if err != nil {
		t.Fatalf("expected a real RM8+ database to be accepted, got: %v", err)
	}
	defer db.Close()

	hint := db.SchemaHint()
	if strings.Contains(hint, "7") {
		t.Errorf("SchemaHint should never mention RootsMagic 7 now that it's rejected outright, got: %q", hint)
	}
}
