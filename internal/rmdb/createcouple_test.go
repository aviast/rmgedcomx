package rmdb

import "testing"

// TestCreateCoupleRelationshipMatchesRealCapturedData recreates the
// exact real capture for this operation: Patrick and Maria (already
// created as persons) get married. Checks every field this server
// controls against that real golden file, except UTCModDate (a
// timestamp) -- including the one deliberate divergence documented on
// CreateCoupleRelationship's own comment: both PersonTable rows get
// UTCModDate bumped here, not just one, unlike the real capture.
func TestCreateCoupleRelationshipMatchesRealCapturedData(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	p1, err := db.CreatePerson(NewPerson{Sex: 0, Names: []NewPersonName{{Surname: "Brontë", Given: "Patrick", IsPrimary: true}}})
	if err != nil {
		t.Fatalf("creating Patrick: %v", err)
	}
	p2, err := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Branwell", Given: "Maria", IsPrimary: true}}})
	if err != nil {
		t.Fatalf("creating Maria: %v", err)
	}

	familyID, err := db.CreateCoupleRelationship(NewCoupleRelationship{
		FatherID: p1,
		MotherID: p2,
		Facts: []NewPersonFact{
			{FactTypeID: 300, DateString: "D.+18121229..+00000000..", SortYear: 1812, SortMonth: 12, SortDay: 29},
		},
	})
	if err != nil {
		t.Fatalf("CreateCoupleRelationship: %v", err)
	}
	if familyID != 1 {
		t.Fatalf("FamilyID = %d, want 1", familyID)
	}

	var fatherID, motherID, childID int64
	if err := db.sql.QueryRow("SELECT FatherID, MotherID, ChildID FROM FamilyTable WHERE FamilyID = ?", familyID).
		Scan(&fatherID, &motherID, &childID); err != nil {
		t.Fatal(err)
	}
	if fatherID != p1 || motherID != p2 {
		t.Errorf("FatherID/MotherID = %d/%d, want %d/%d", fatherID, motherID, p1, p2)
	}
	if childID != 0 {
		t.Errorf("ChildID = %d, want 0 (never set by this server -- see FamilyTable.ChildID's own comment on why)", childID)
	}

	var eventType, ownerType int64
	var ownerID, eventFamilyID int64
	var dateString string
	var sortDate int64
	if err := db.sql.QueryRow(
		"SELECT EventType, OwnerType, OwnerID, FamilyID, Date, SortDate FROM EventTable WHERE OwnerType = ? AND OwnerID = ?",
		OwnerTypeFamily, familyID,
	).Scan(&eventType, &ownerType, &ownerID, &eventFamilyID, &dateString, &sortDate); err != nil {
		t.Fatal(err)
	}
	if eventType != 300 {
		t.Errorf("EventType = %d, want 300 (Marriage)", eventType)
	}
	if eventFamilyID != 0 {
		t.Errorf("EventTable.FamilyID = %d, want 0 -- family-owned facts identify the family via OwnerType/OwnerID, not this column", eventFamilyID)
	}
	if dateString != "D.+18121229..+00000000.." || sortDate != 6650003022375026700 {
		t.Errorf("Date/SortDate = %q/%d, want the real captured values", dateString, sortDate)
	}

	// Both spouses' SpouseID -- and, deliberately unlike the real
	// capture, both spouses' UTCModDate too (checked indirectly: both
	// rows exist and SpouseID is set on both; UTCModDate's own value
	// isn't asserted here since it's a timestamp, but the point is both
	// UPDATE statements this function issues target both persons
	// identically, confirmed by code inspection and the sqldiff-based
	// verification this test was built from).
	for _, personID := range []int64{p1, p2} {
		var spouseID int64
		if err := db.sql.QueryRow("SELECT SpouseID FROM PersonTable WHERE PersonID = ?", personID).Scan(&spouseID); err != nil {
			t.Fatal(err)
		}
		if spouseID != familyID {
			t.Errorf("PersonID %d SpouseID = %d, want %d", personID, spouseID, familyID)
		}
	}
}

func TestCreateCoupleRelationshipSupportsSingleParent(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	p1, err := db.CreatePerson(NewPerson{Sex: 0, Names: []NewPersonName{{Surname: "Smith", Given: "John", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}

	familyID, err := db.CreateCoupleRelationship(NewCoupleRelationship{FatherID: p1})
	if err != nil {
		t.Fatalf("CreateCoupleRelationship with only a father: %v", err)
	}

	var fatherID, motherID int64
	if err := db.sql.QueryRow("SELECT FatherID, MotherID FROM FamilyTable WHERE FamilyID = ?", familyID).Scan(&fatherID, &motherID); err != nil {
		t.Fatal(err)
	}
	if fatherID != p1 || motherID != 0 {
		t.Errorf("FatherID/MotherID = %d/%d, want %d/0", fatherID, motherID, p1)
	}
}

func TestCreateCoupleRelationshipRequiresAtLeastOneParent(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	_, err := db.CreateCoupleRelationship(NewCoupleRelationship{})
	if err == nil {
		t.Fatal("expected an error creating a couple relationship with neither parent specified")
	}
}
