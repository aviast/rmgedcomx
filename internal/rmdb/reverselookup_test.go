package rmdb

import (
	"os"
	"testing"
)

func setupReverseLookupTestDB(t *testing.T) *DB {
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

func containsSubjectRef(refs []SubjectRef, want SubjectRef) bool {
	for _, r := range refs {
		if r == want {
			return true
		}
	}
	return false
}

func TestOwnersOfMediaDirectLink(t *testing.T) {
	db := setupReverseLookupTestDB(t)

	// M1 is already directly linked to Event 5049 in real royal92.rmtree
	// data -- no setup needed for this one.
	refs, err := db.OwnersOfMedia(1)
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubjectRef(refs, SubjectRef{OwnerType: OwnerTypeEvent, OwnerID: 5049}) {
		t.Errorf("expected Event 5049 in results, got %+v", refs)
	}
}

func TestOwnersOfMediaViaCitationWithNameResolution(t *testing.T) {
	db := setupReverseLookupTestDB(t)

	// A citation (id 500, chosen not to collide with real data) cited
	// only by Name 1 (owned by Person 1, confirmed elsewhere against
	// real data). M1 attached to that citation, not directly to anyone.
	if _, err := db.sql.Exec(
		"INSERT INTO CitationLinkTable (CitationID, OwnerType, OwnerID, SortOrder, Quality, IsPrivate, Flags, UTCModDate) VALUES (500, ?, 1, 0, '', 0, 0, 0)",
		OwnerTypeName,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(
		"INSERT INTO MediaLinkTable (MediaID, OwnerType, OwnerID, IsPrimary, Include1, Include2, Include3, Include4, SortOrder, RectLeft, RectTop, RectRight, RectBottom, Comments, UTCModDate) VALUES (1, ?, 500, 0,0,0,0,0,0,0,0,0,0,'',0)",
		OwnerTypeCitation,
	); err != nil {
		t.Fatal(err)
	}

	refs, err := db.OwnersOfMedia(1)
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubjectRef(refs, SubjectRef{OwnerType: OwnerTypePerson, OwnerID: 1}) {
		t.Errorf("expected Person 1 (resolved via Name 1's citation) in results, got %+v", refs)
	}
	// The citation-owning row itself (OwnerType=Citation) must never
	// leak into the result -- only the resolved Person.
	if containsSubjectRef(refs, SubjectRef{OwnerType: OwnerTypeCitation, OwnerID: 500}) {
		t.Errorf("Citation itself should never appear as a resolved subject, got %+v", refs)
	}
}

func TestOwnersOfMediaDirectFamilyLink(t *testing.T) {
	db := setupReverseLookupTestDB(t)

	if _, err := db.sql.Exec(
		"INSERT INTO MediaLinkTable (MediaID, OwnerType, OwnerID, IsPrimary, Include1, Include2, Include3, Include4, SortOrder, RectLeft, RectTop, RectRight, RectBottom, Comments, UTCModDate) VALUES (1, ?, 70, 0,0,0,0,0,0,0,0,0,0,'',0)",
		OwnerTypeFamily,
	); err != nil {
		t.Fatal(err)
	}

	refs, err := db.OwnersOfMedia(1)
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubjectRef(refs, SubjectRef{OwnerType: OwnerTypeFamily, OwnerID: 70}) {
		t.Errorf("expected Family 70 in results, got %+v", refs)
	}
}

func TestOwnersOfMediaDropsSourceOwner(t *testing.T) {
	db := setupReverseLookupTestDB(t)

	// Media attached directly to a Source record (not a Subject type
	// this API exposes) must be silently dropped, not surfaced.
	if _, err := db.sql.Exec(
		"INSERT INTO MediaLinkTable (MediaID, OwnerType, OwnerID, IsPrimary, Include1, Include2, Include3, Include4, SortOrder, RectLeft, RectTop, RectRight, RectBottom, Comments, UTCModDate) VALUES (1, ?, 1, 0,0,0,0,0,0,0,0,0,0,'',0)",
		OwnerTypeSource,
	); err != nil {
		t.Fatal(err)
	}

	refs, err := db.OwnersOfMedia(1)
	if err != nil {
		t.Fatal(err)
	}
	// The existing real Event 5049 link should still be there; the
	// Source link should not have added anything else.
	if len(refs) != 1 || refs[0].OwnerType != OwnerTypeEvent || refs[0].OwnerID != 5049 {
		t.Errorf("expected only the pre-existing Event 5049 link, got %+v", refs)
	}
}

func TestOwnersOfMediaDeduplicates(t *testing.T) {
	db := setupReverseLookupTestDB(t)

	// Two separate citations, both cited by the same Person, both
	// carrying M1 -- the Person should appear exactly once in the
	// result, not twice.
	for _, citationID := range []int64{600, 601} {
		if _, err := db.sql.Exec(
			"INSERT INTO CitationLinkTable (CitationID, OwnerType, OwnerID, SortOrder, Quality, IsPrivate, Flags, UTCModDate) VALUES (?, ?, 2, 0, '', 0, 0, 0)",
			citationID, OwnerTypePerson,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := db.sql.Exec(
			"INSERT INTO MediaLinkTable (MediaID, OwnerType, OwnerID, IsPrimary, Include1, Include2, Include3, Include4, SortOrder, RectLeft, RectTop, RectRight, RectBottom, Comments, UTCModDate) VALUES (1, ?, ?, 0,0,0,0,0,0,0,0,0,0,'',0)",
			OwnerTypeCitation, citationID,
		); err != nil {
			t.Fatal(err)
		}
	}

	refs, err := db.OwnersOfMedia(1)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range refs {
		if r.OwnerType == OwnerTypePerson && r.OwnerID == 2 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected Person 2 to appear exactly once despite two separate citation paths, appeared %d times in %+v", count, refs)
	}
}

func TestOwnersOfMediaNoReferences(t *testing.T) {
	db := setupReverseLookupTestDB(t)

	// M2 has no MultimediaTable row at all in royal92.rmtree (only M1
	// exists), so it also has no MediaLinkTable rows referencing it --
	// a clean "nothing found" case, not an error.
	refs, err := db.OwnersOfMedia(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Errorf("expected no references for a nonexistent media id, got %+v", refs)
	}
}
