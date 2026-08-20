package rmdb

import (
	"os"
	"testing"
)

func setupCreatePersonTestDB(t *testing.T) *DB {
	t.Helper()
	dbPath := t.TempDir() + "/test.rmtree"
	data, err := os.ReadFile("../../testdata/empty.rmtree")
	if err != nil {
		t.Fatalf("reading testdata/empty.rmtree: %v", err)
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

// TestCreatePersonMatchesRealCapturedData recreates the exact first
// capture of this whole feature (a real RootsMagic session, starting
// from an empty database, creating Patrick Brontë with a birth and
// death fact) and checks every single field this server itself controls
// against that real captured golden file
// (testdata/../ conversation history -- see SCOPE.md's "Stage 3"
// section) -- everything except UTCModDate (expected to differ, a
// timestamp) and UniqueID (expected to differ, randomly generated per
// person; checked separately below for being well-formed instead).
func TestCreatePersonMatchesRealCapturedData(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	personID, err := db.CreatePerson(NewPerson{
		Sex: 0,
		Names: []NewPersonName{
			{Surname: "Brontë", Given: "Patrick", IsPrimary: true},
		},
		Facts: []NewPersonFact{
			{FactTypeID: 1, DateString: "D.+17770317..+00000000..", SortYear: 1777, SortMonth: 3, SortDay: 17, PlaceText: "County Down, Ireland"},
			{FactTypeID: 2, DateString: "D.+18610607..+00000000..", SortYear: 1861, SortMonth: 6, SortDay: 7, PlaceText: "Haworth, Yorks."},
		},
	})
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	if personID != 1 {
		t.Fatalf("PersonID = %d, want 1 (matching the real capture, first person in an empty database)", personID)
	}

	// PersonTable
	var sex int
	var uniqueID string
	err = db.sql.QueryRow("SELECT Sex, UniqueID FROM PersonTable WHERE PersonID = ?", personID).Scan(&sex, &uniqueID)
	if err != nil {
		t.Fatal(err)
	}
	if sex != 0 {
		t.Errorf("PersonTable.Sex = %d, want 0", sex)
	}
	if len(uniqueID) != 36 {
		t.Errorf("UniqueID = %q, want 36 characters", uniqueID)
	}

	// EventTable -- both events, in full, against the real captured values.
	rows, err := db.sql.Query("SELECT EventID, EventType, OwnerType, OwnerID, PlaceID, Date, SortDate FROM EventTable ORDER BY EventID")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type ev struct {
		eventID, eventType, ownerType, ownerID, placeID int64
		date                                            string
		sortDate                                        int64
	}
	var events []ev
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.eventID, &e.eventType, &e.ownerType, &e.ownerID, &e.placeID, &e.date, &e.sortDate); err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	want := []ev{
		{1, 1, 0, 1, 206, "D.+17770317..+00000000..", 6629976517586714636},
		{2, 2, 0, 1, 207, "D.+18610607..+00000000..", 6677364369232232460},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d", len(events), len(want))
	}
	for i, w := range want {
		if events[i] != w {
			t.Errorf("event %d: got %+v, want %+v", i, events[i], w)
		}
	}

	// NameTable -- confirm the exact real values, including the computed
	// SurnameMP/GivenMP and the primary name's BirthYear/DeathYear.
	var surname, given, surnameMP, givenMP string
	var isPrimary, birthYear, deathYear int
	var nameSortDate int64
	err = db.sql.QueryRow(
		"SELECT Surname, Given, IsPrimary, BirthYear, DeathYear, SortDate, SurnameMP, GivenMP FROM NameTable WHERE OwnerID = ?",
		personID,
	).Scan(&surname, &given, &isPrimary, &birthYear, &deathYear, &nameSortDate, &surnameMP, &givenMP)
	if err != nil {
		t.Fatal(err)
	}
	if surname != "Brontë" || given != "Patrick" {
		t.Errorf("Name = %q/%q, want %q/%q", surname, given, "Brontë", "Patrick")
	}
	if isPrimary != 1 {
		t.Errorf("IsPrimary = %d, want 1", isPrimary)
	}
	if birthYear != 1777 || deathYear != 1861 {
		t.Errorf("BirthYear/DeathYear = %d/%d, want 1777/1861", birthYear, deathYear)
	}
	if nameSortDate != NoDateSortValue {
		t.Errorf("Name SortDate = %d, want the no-date sentinel %d", nameSortDate, NoDateSortValue)
	}
	if surnameMP != "Bronte" || givenMP != "Patrick" {
		t.Errorf("SurnameMP/GivenMP = %q/%q, want %q/%q", surnameMP, givenMP, "Bronte", "Patrick")
	}

	// PlaceTable -- both places, matching the real captured Reverse values.
	var place1Name, place1Reverse, place2Name, place2Reverse string
	if err := db.sql.QueryRow("SELECT Name, Reverse FROM PlaceTable WHERE PlaceID = 206").Scan(&place1Name, &place1Reverse); err != nil {
		t.Fatal(err)
	}
	if place1Name != "County Down, Ireland" || place1Reverse != "Ireland, County Down" {
		t.Errorf("place 206 = %q/%q, want %q/%q", place1Name, place1Reverse, "County Down, Ireland", "Ireland, County Down")
	}
	if err := db.sql.QueryRow("SELECT Name, Reverse FROM PlaceTable WHERE PlaceID = 207").Scan(&place2Name, &place2Reverse); err != nil {
		t.Fatal(err)
	}
	if place2Name != "Haworth, Yorks." || place2Reverse != "Yorks., Haworth" {
		t.Errorf("place 207 = %q/%q, want %q/%q", place2Name, place2Reverse, "Haworth, Yorks.", "Yorks., Haworth")
	}
}

// TestCreatePersonDeduplicatesPlaces confirms two people created in
// separate CreatePerson calls, both referencing the same place text,
// share one PlaceTable row rather than creating a duplicate -- matching
// the real captured data (e.g. "Howarth" reused across many events in
// the Brontë test database).
func TestCreatePersonDeduplicatesPlaces(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	if _, err := db.CreatePerson(NewPerson{
		Sex:   1,
		Names: []NewPersonName{{Surname: "Brontë", Given: "Charlotte", IsPrimary: true}},
		Facts: []NewPersonFact{
			{FactTypeID: 2, DateString: "D.+18550331..+00000000..", SortYear: 1855, SortMonth: 3, SortDay: 31, PlaceText: "Howarth"},
		},
	}); err != nil {
		t.Fatalf("creating first person: %v", err)
	}
	if _, err := db.CreatePerson(NewPerson{
		Sex:   0,
		Names: []NewPersonName{{Surname: "Brontë", Given: "Patrick Branwell", IsPrimary: true}},
		Facts: []NewPersonFact{
			{FactTypeID: 2, DateString: "D.+18480924..+00000000..", SortYear: 1848, SortMonth: 9, SortDay: 24, PlaceText: "Howarth"},
		},
	}); err != nil {
		t.Fatalf("creating second person: %v", err)
	}

	var count int
	if err := db.sql.QueryRow("SELECT COUNT(*) FROM PlaceTable WHERE Name = 'Howarth'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected exactly one PlaceTable row for 'Howarth' shared across both people, got %d", count)
	}
}

// TestCreatePersonNonPrimaryNameHasSameYears replaces an earlier version
// of this test (TestCreatePersonNonPrimaryNameHasNoYears) that asserted
// the opposite of what's actually correct -- see CreatePerson's own
// comment for the full account. Checked directly against real
// multi-name people in two separate real RootsMagic databases before
// concluding the earlier "confirmed against a real captured diff" claim
// had been wrong: every non-primary name row carried the same
// BirthYear/DeathYear as its person's primary name.
func TestCreatePersonNonPrimaryNameHasSameYears(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	personID, err := db.CreatePerson(NewPerson{
		Sex: 0,
		Names: []NewPersonName{
			{Surname: "Brontë", Given: "Patrick", IsPrimary: true},
			{Surname: "Brunty", IsPrimary: false},
		},
		Facts: []NewPersonFact{
			{FactTypeID: 1, DateString: "D.+17770317..+00000000..", SortYear: 1777, SortMonth: 3, SortDay: 17},
			{FactTypeID: 2, DateString: "D.+18610607..+00000000..", SortYear: 1861, SortMonth: 6, SortDay: 7},
		},
	})
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}

	rows, err := db.sql.Query("SELECT Surname, IsPrimary, BirthYear, DeathYear FROM NameTable WHERE OwnerID = ? ORDER BY NameID", personID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []struct {
		surname              string
		isPrimary            int
		birthYear, deathYear int
	}
	for rows.Next() {
		var r struct {
			surname              string
			isPrimary            int
			birthYear, deathYear int
		}
		if err := rows.Scan(&r.surname, &r.isPrimary, &r.birthYear, &r.deathYear); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 names, got %d", len(got))
	}
	if got[0].isPrimary != 1 || got[0].birthYear != 1777 || got[0].deathYear != 1861 {
		t.Errorf("primary name: got %+v, want IsPrimary=1, BirthYear=1777, DeathYear=1861", got[0])
	}
	if got[1].isPrimary != 0 || got[1].birthYear != 1777 || got[1].deathYear != 1861 {
		t.Errorf("non-primary name: got %+v, want IsPrimary=0, BirthYear=1777, DeathYear=1861 (duplicated from the primary name, not left at 0)", got[1])
	}
}

func TestCreatePersonRequiresAtLeastOneName(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	_, err := db.CreatePerson(NewPerson{Sex: 2})
	if err == nil {
		t.Fatal("expected an error creating a person with no names, got nil")
	}
}

// TestCreatePersonNameWithNickname confirms NewPersonName.Nickname is
// persisted to NameTable.Nickname, and NicknameMP is computed via
// FoldAccents the same way SurnameMP/GivenMP already are -- inferred by
// analogy with those two (RM4-11 data dictionary's own description of
// NicknameMP is unfortunately just a copy-paste artifact of SurnameMP's,
// not independently useful), not yet verified against a real captured
// example (no NICK value exists anywhere in this project's own
// royal92.ged reference file). The API layer (buildNewPerson,
// internal/api/createperson.go) is what actually decides when to
// populate this field, from a real request; this only confirms
// CreatePerson correctly writes whatever it's given, including folding.
func TestCreatePersonNameWithNickname(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	personID, err := db.CreatePerson(NewPerson{
		Sex:   1,
		Names: []NewPersonName{{Surname: "Brontë", Given: "Anne", Nickname: "Ańné", IsPrimary: true}},
	})
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}

	var nickname, nicknameMP string
	if err := db.sql.QueryRow("SELECT Nickname, NicknameMP FROM NameTable WHERE OwnerID = ?", personID).Scan(&nickname, &nicknameMP); err != nil {
		t.Fatal(err)
	}
	if nickname != "Ańné" {
		t.Errorf("NameTable.Nickname = %q, want %q (verbatim, not folded)", nickname, "Ańné")
	}
	if nicknameMP != "Anne" {
		t.Errorf("NameTable.NicknameMP = %q, want %q (accent-folded, matching SurnameMP/GivenMP's own confirmed behavior)", nicknameMP, "Anne")
	}
}

// TestCreatePersonFactWithDetails confirms NewPersonFact.Details is
// persisted to EventTable.Details -- the storage-layer half of a real
// reported gap: GEDCOM X's Fact.value (a value-only fact like Occupation
// or Religion, with no date or place of its own) was never reaching
// EventTable.Details at all, even though the read side
// (internal/api/convert.go's buildFact) already reversed this exact
// mapping the other way. The API layer (buildNewPersonFact,
// internal/api/createperson.go) is what actually decides when to
// populate this field, from a real request; this only confirms
// CreatePerson correctly writes whatever it's given.
func TestCreatePersonFactWithDetails(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	const details = "Bank President, Ambassador"
	personID, err := db.CreatePerson(NewPerson{
		Sex:   0,
		Names: []NewPersonName{{Surname: "Kennedy", Given: "Joseph", IsPrimary: true}},
		Facts: []NewPersonFact{
			{FactTypeID: 26, DateString: ".", Details: details}, // Occupation
		},
	})
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}

	var got string
	if err := db.sql.QueryRow("SELECT Details FROM EventTable WHERE OwnerID = ?", personID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != details {
		t.Errorf("EventTable.Details = %q, want %q", got, details)
	}
}

// TestCreatePersonFactWithNote confirms NewPersonFact.Note is actually
// persisted to EventTable.Note -- the storage-layer half of the
// Date.original-unparseable-preserves-the-text feature (the API layer,
// internal/api/createperson.go's buildNewPersonFact, is what actually
// decides when to populate this field and with what text; this only
// confirms CreatePerson correctly writes whatever it's given).
func TestCreatePersonFactWithNote(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	const noteText = "rmgedcomx was unable to parse this text as a date: BET 1900 AND 1910"
	personID, err := db.CreatePerson(NewPerson{
		Sex:   2,
		Names: []NewPersonName{{Surname: "Smith", Given: "X", IsPrimary: true}},
		Facts: []NewPersonFact{
			{FactTypeID: 1, DateString: ".", Note: noteText},
		},
	})
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}

	var note string
	if err := db.sql.QueryRow("SELECT Note FROM EventTable WHERE OwnerID = ?", personID).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != noteText {
		t.Errorf("EventTable.Note = %q, want %q", note, noteText)
	}
}

func TestCreatePersonNoFactsOrPlaces(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	personID, err := db.CreatePerson(NewPerson{
		Sex:   2,
		Names: []NewPersonName{{Surname: "Nicholls", Given: "Arthur Bell", IsPrimary: true}},
	})
	if err != nil {
		t.Fatalf("CreatePerson with no facts: %v", err)
	}
	var count int
	if err := db.sql.QueryRow("SELECT COUNT(*) FROM EventTable WHERE OwnerID = ?", personID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected no events created, got %d", count)
	}
}
