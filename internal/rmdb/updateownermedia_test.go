package rmdb

import (
	"database/sql"
	"errors"
	"os"
	"testing"
)

// These tests exercise UpdateOwnerMedia's core behavior directly. Full
// end-to-end coverage (the HTTP layer, request parsing, backup-before-write)
// lives in cmd/server's TestWriteOperations; these are specifically about
// the diffing/SQL logic itself.

func setupMediaLinkTestDB(t *testing.T) *DB {
	t.Helper()
	dbPath := t.TempDir() + "/test.rmtree"
	data, err := os.ReadFile("../../royal92.rmtree")
	if err != nil {
		t.Fatalf("reading royal92.rmtree: %v", err)
	}
	if err := os.WriteFile(dbPath, data, 0o644); err != nil {
		t.Fatalf("writing test copy: %v", err)
	}
	db, err := Open(dbPath, false)
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func ownerHasMediaLink(db *DB, ownerType int, ownerID, mediaID int64) (bool, error) {
	var exists int
	err := db.sql.QueryRow(
		"SELECT 1 FROM MediaLinkTable WHERE OwnerType = ? AND OwnerID = ? AND MediaID = ?",
		ownerType, ownerID, mediaID,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func TestUpdateOwnerMediaAttachAndDetach(t *testing.T) {
	db := setupMediaLinkTestDB(t)

	// M1 exists in royal92.rmtree; PersonID 1 (Victoria) exists and has
	// no direct media links to begin with (M1 is only linked to the
	// marriage event, EventID 5049).
	if err := db.UpdateOwnerMedia(OwnerTypePerson, 1, []int64{1}); err != nil {
		t.Fatalf("attaching M1: %v", err)
	}

	linked, err := ownerHasMediaLink(db, OwnerTypePerson, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !linked {
		t.Fatal("expected M1 to be linked to person 1 after attaching")
	}

	// The pre-existing link (M1 -> Event 5049) must survive untouched.
	eventLinked, err := ownerHasMediaLink(db, OwnerTypeEvent, 5049, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !eventLinked {
		t.Fatal("expected the pre-existing M1 -> Event 5049 link to survive untouched")
	}

	if err := db.UpdateOwnerMedia(OwnerTypePerson, 1, nil); err != nil {
		t.Fatalf("detaching M1: %v", err)
	}
	linked, err = ownerHasMediaLink(db, OwnerTypePerson, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if linked {
		t.Fatal("expected M1 to be unlinked from person 1 after detaching")
	}
}

func TestUpdateOwnerMediaRejectsNonexistentArtifact(t *testing.T) {
	db := setupMediaLinkTestDB(t)

	err := db.UpdateOwnerMedia(OwnerTypePerson, 2, []int64{1, 999})
	if !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("expected ErrArtifactNotFound, got: %v", err)
	}

	// Must be fully atomic: the valid M1 link must NOT have been created
	// even though it would have succeeded on its own.
	linked, err := ownerHasMediaLink(db, OwnerTypePerson, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if linked {
		t.Fatal("expected no link to have been created when the request was rejected")
	}
}

func TestUpdateOwnerMediaNewLinkDefaults(t *testing.T) {
	db := setupMediaLinkTestDB(t)

	if err := db.UpdateOwnerMedia(OwnerTypePerson, 3, []int64{1}); err != nil {
		t.Fatalf("attaching M1: %v", err)
	}

	var isPrimary, include1 int
	err := db.sql.QueryRow(
		"SELECT IsPrimary, Include1 FROM MediaLinkTable WHERE OwnerType = ? AND OwnerID = ? AND MediaID = ?",
		OwnerTypePerson, 3, 1,
	).Scan(&isPrimary, &include1)
	if err != nil {
		t.Fatal(err)
	}
	if isPrimary != 0 {
		t.Errorf("IsPrimary = %d, want 0 (new links never default to primary)", isPrimary)
	}
	if include1 != 0 {
		t.Errorf("Include1 = %d, want 0 (new links never default to Scrapbook-included)", include1)
	}
}

func TestUpdateOwnerMediaRemovesAllDuplicates(t *testing.T) {
	db := setupMediaLinkTestDB(t)

	// Manually create two link rows for the same (MediaID, OwnerType,
	// OwnerID) -- MediaLinkTable has no uniqueness constraint preventing
	// this, so it's a real state to handle, not a hypothetical one.
	insert := `INSERT INTO MediaLinkTable
		(MediaID, OwnerType, OwnerID, IsPrimary, Include1, Include2, Include3, Include4,
		 SortOrder, RectLeft, RectTop, RectRight, RectBottom, Comments, UTCModDate)
		VALUES (1, ?, ?, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, '', 0)`
	if _, err := db.sql.Exec(insert, OwnerTypePerson, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(insert, OwnerTypePerson, 5); err != nil {
		t.Fatal(err)
	}

	if err := db.UpdateOwnerMedia(OwnerTypePerson, 5, nil); err != nil {
		t.Fatalf("removing duplicate links: %v", err)
	}

	var count int
	err := db.sql.QueryRow(
		"SELECT COUNT(*) FROM MediaLinkTable WHERE OwnerType = ? AND OwnerID = ? AND MediaID = ?",
		OwnerTypePerson, 5, 1,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected all duplicate links removed, %d remain", count)
	}
}

// TestUpdateOwnerMediaWorksForEventOwners confirms the shared diffing core
// works identically for a different owner type, not just Person -- the
// same M1 that's already linked to Event 5049 (the marriage) in real
// data is moved to a different, previously-unlinked event, and the
// original event's own link is independently detached, confirming both
// that different owner types don't interfere with each other and that
// the core logic isn't accidentally Person-specific.
func TestUpdateOwnerMediaWorksForEventOwners(t *testing.T) {
	db := setupMediaLinkTestDB(t)

	linked, err := ownerHasMediaLink(db, OwnerTypeEvent, 5049, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !linked {
		t.Fatal("expected M1 to already be linked to event 5049 in royal92.rmtree")
	}

	if err := db.UpdateOwnerMedia(OwnerTypeEvent, 2, []int64{1}); err != nil {
		t.Fatalf("attaching M1 to event 2: %v", err)
	}
	linked, err = ownerHasMediaLink(db, OwnerTypeEvent, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !linked {
		t.Fatal("expected M1 to be linked to event 2 after attaching")
	}

	// Event 5049's own link must be untouched by event 2's update.
	linked, err = ownerHasMediaLink(db, OwnerTypeEvent, 5049, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !linked {
		t.Fatal("expected event 5049's own link to survive an unrelated event's media update")
	}

	if err := db.UpdateOwnerMedia(OwnerTypeEvent, 5049, nil); err != nil {
		t.Fatalf("detaching M1 from event 5049: %v", err)
	}
	linked, err = ownerHasMediaLink(db, OwnerTypeEvent, 5049, 1)
	if err != nil {
		t.Fatal(err)
	}
	if linked {
		t.Fatal("expected event 5049's link to be gone after detaching")
	}
}

// TestUpdateOwnerMediaWorksForFamilyOwners confirms the shared diffing
// core works for OwnerTypeFamily too -- the same generic function
// backing the Relationship write handler. Family 1 (Victoria and
// Albert's real couple relationship in royal92.rmtree) has no direct
// media link to begin with; attaching and detaching M1 here proves the
// same core logic already proven for Person and Event owners isn't
// accidentally specific to either of those two.
func TestUpdateOwnerMediaWorksForFamilyOwners(t *testing.T) {
	db := setupMediaLinkTestDB(t)

	linked, err := ownerHasMediaLink(db, OwnerTypeFamily, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if linked {
		t.Fatal("expected M1 not to already be linked to family 1 in royal92.rmtree")
	}

	if err := db.UpdateOwnerMedia(OwnerTypeFamily, 1, []int64{1}); err != nil {
		t.Fatalf("attaching M1 to family 1: %v", err)
	}
	linked, err = ownerHasMediaLink(db, OwnerTypeFamily, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !linked {
		t.Fatal("expected M1 to be linked to family 1 after attaching")
	}

	if err := db.UpdateOwnerMedia(OwnerTypeFamily, 1, nil); err != nil {
		t.Fatalf("detaching M1 from family 1: %v", err)
	}
	linked, err = ownerHasMediaLink(db, OwnerTypeFamily, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if linked {
		t.Fatal("expected M1 to be unlinked from family 1 after detaching")
	}
}
