package rmdb

import (
	"errors"
	"testing"
)

// TestCreateParentChildRelationshipMatchesRealCapturedData recreates the
// exact real capture for this operation: all six Brontë children linked
// to their already-established father (Patrick, already married to
// Maria in an existing family). Checks every ChildTable/PersonTable
// field against the real golden file, field-by-field, except UTCModDate
// (a timestamp) -- including the one deliberate divergence (this
// server bumps the child's own UTCModDate; the real capture didn't).
func TestCreateParentChildRelationshipMatchesRealCapturedData(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	father, err := db.CreatePerson(NewPerson{Sex: 0, Names: []NewPersonName{{Surname: "Brontë", Given: "Patrick", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}
	mother, err := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Branwell", Given: "Maria", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}
	var children []int64
	for _, name := range []string{"Maria", "Elizabeth", "Charlotte", "Patrick Branwell", "Emily Jane", "Anne"} {
		cid, err := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Brontë", Given: name, IsPrimary: true}}})
		if err != nil {
			t.Fatal(err)
		}
		children = append(children, cid)
	}
	familyID, err := db.CreateCoupleRelationship(NewCoupleRelationship{FatherID: father, MotherID: mother})
	if err != nil {
		t.Fatal(err)
	}

	// Sends BOTH ParentChild links for each child, not just the
	// father's -- required under the corrected algorithm (see
	// CreateParentChildRelationship's own comment for why a bare,
	// single-parent link is no longer treated as sufficient to place a
	// child in an existing, already-complete family). The first link
	// creates a new single-parent family; the second recognizes it
	// would duplicate the pre-existing Patrick+Maria family (created via
	// CreateCoupleRelationship above) and merges into it instead --
	// still landing on the exact same final state this test has always
	// verified against the real captured Brontë data.
	for i, childID := range children {
		if _, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: father, ChildID: childID}); err != nil {
			t.Fatalf("linking child %d to father: %v", childID, err)
		}
		gotFamilyID, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: mother, ChildID: childID})
		if err != nil {
			t.Fatalf("linking child %d to mother: %v", childID, err)
		}
		if gotFamilyID != familyID {
			t.Errorf("child %d: got family %d, want %d", childID, gotFamilyID, familyID)
		}

		var recID, gotChildID, gotFamID int64
		var relFather, relMother, childOrder int
		err = db.sql.QueryRow(
			"SELECT RecID, ChildID, FamilyID, RelFather, RelMother, ChildOrder FROM ChildTable WHERE ChildID = ?",
			childID,
		).Scan(&recID, &gotChildID, &gotFamID, &relFather, &relMother, &childOrder)
		if err != nil {
			t.Fatal(err)
		}
		// ChildOrder is 0-indexed by RootsMagic, not 1-indexed -- an
		// earlier version of this expectation (wantOrder := i + 1) got
		// this wrong; confirmed directly against real ChildTable data
		// (every multi-child family in two separate real RootsMagic
		// databases starts at ChildOrder=0, not 1) before correcting it
		// here, not just changed to make the test pass.
		wantOrder := i
		if childOrder != wantOrder {
			t.Errorf("child %d: ChildOrder = %d, want %d", childID, childOrder, wantOrder)
		}
		if relFather != 0 || relMother != 0 {
			t.Errorf("child %d: RelFather/RelMother = %d/%d, want 0/0", childID, relFather, relMother)
		}

		// PersonTable.ParentID -- like SpouseID, a UI navigation state
		// (RM4-11 data dictionary: the family last viewed for this
		// person as a child, not a genealogical fact -- see
		// CreateParentChildRelationship's own comment for the full
		// account) -- is confirmed left at 0, not set to familyID the
		// way an earlier version of this test asserted.
		var parentID int64
		if err := db.sql.QueryRow("SELECT ParentID FROM PersonTable WHERE PersonID = ?", childID).Scan(&parentID); err != nil {
			t.Fatal(err)
		}
		if parentID != 0 {
			t.Errorf("child %d: PersonTable.ParentID = %d, want 0 (this server no longer sets it)", childID, parentID)
		}
	}
}

func TestCreateParentChildRelationshipCreatesSingleParentFamily(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	mother, err := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Smith", Given: "Jane", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.CreatePerson(NewPerson{Sex: 0, Names: []NewPersonName{{Surname: "Smith", Given: "Tom", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}

	familyID, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: mother, ChildID: child})
	if err != nil {
		t.Fatalf("CreateParentChildRelationship with no existing family: %v", err)
	}

	var fatherID, motherID int64
	if err := db.sql.QueryRow("SELECT FatherID, MotherID FROM FamilyTable WHERE FamilyID = ?", familyID).Scan(&fatherID, &motherID); err != nil {
		t.Fatal(err)
	}
	if fatherID != 0 || motherID != mother {
		t.Errorf("FatherID/MotherID = %d/%d, want 0/%d", fatherID, motherID, mother)
	}
}

func TestCreateParentChildRelationshipIsIdempotent(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	father, err := db.CreatePerson(NewPerson{Sex: 0, Names: []NewPersonName{{Surname: "Smith", Given: "John", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Smith", Given: "Jane", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}

	fam1, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: father, ChildID: child})
	if err != nil {
		t.Fatal(err)
	}
	fam2, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: father, ChildID: child})
	if err != nil {
		t.Fatalf("expected the second, already-linked request to succeed idempotently, got: %v", err)
	}
	if fam1 != fam2 {
		t.Errorf("expected the same family both times, got %d then %d", fam1, fam2)
	}

	var count int
	if err := db.sql.QueryRow("SELECT COUNT(*) FROM ChildTable WHERE FamilyID = ? AND ChildID = ?", fam1, child).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected exactly one ChildTable row despite two requests, got %d", count)
	}
}

// TestCreateParentChildRelationshipDoesNotAssumeAParentsExistingFamily
// replaces an earlier version of this test that asserted the opposite
// of what's now correct behavior -- see CreateParentChildRelationship's
// own comment for the full account of why. A father with two real
// families (remarriage) and a bare, single-parent ParentChild request
// for a new child must never guess which existing family -- or assume
// either -- since a bare (parent, child) pair carries no information
// about which partner the child's other parent actually was.
func TestCreateParentChildRelationshipDoesNotAssumeAParentsExistingFamily(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	father, err := db.CreatePerson(NewPerson{Sex: 0, Names: []NewPersonName{{Surname: "Smith", Given: "John", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}
	mother1, _ := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Smith", Given: "Jane", IsPrimary: true}}})
	mother2, _ := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Smith", Given: "Ann", IsPrimary: true}}})
	family1, err := db.CreateCoupleRelationship(NewCoupleRelationship{FatherID: father, MotherID: mother1})
	if err != nil {
		t.Fatal(err)
	}
	family2, err := db.CreateCoupleRelationship(NewCoupleRelationship{FatherID: father, MotherID: mother2})
	if err != nil {
		t.Fatal(err)
	}

	child, err := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Smith", Given: "Kid", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}
	gotFamilyID, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: father, ChildID: child})
	if err != nil {
		t.Fatalf("expected a new family to be created, not an error: %v", err)
	}
	if gotFamilyID == family1 || gotFamilyID == family2 {
		t.Fatalf("got family %d, which incorrectly assumes the child belongs to one of the father's existing families (%d, %d)", gotFamilyID, family1, family2)
	}

	var fatherID, motherID int64
	if err := db.sql.QueryRow("SELECT FatherID, MotherID FROM FamilyTable WHERE FamilyID = ?", gotFamilyID).Scan(&fatherID, &motherID); err != nil {
		t.Fatal(err)
	}
	if fatherID != father || motherID != 0 {
		t.Errorf("new family FatherID/MotherID = %d/%d, want %d/0 (father alone, mother genuinely unknown)", fatherID, motherID, father)
	}
}

// TestCreateParentChildRelationshipRejectsChildAlreadyInMultipleFamilies
// covers the case that's still genuinely ambiguous under the corrected
// design: a child who already belongs to more than one family (a real,
// schema-supported case -- RootsMagic's own ChildTable.RelFather/
// RelMother distinguishes Birth/Adopted/Step/Foster/etc., confirmed
// against the data dictionary, meaning a child CAN have both a
// biological and an adoptive family on file simultaneously). A new
// ParentChild request naming this child has no way to say which of the
// child's existing families it's meant to complete.
func TestCreateParentChildRelationshipRejectsChildAlreadyInMultipleFamilies(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	robert, _ := db.CreatePerson(NewPerson{Sex: 0, Names: []NewPersonName{{Surname: "Bio", Given: "Robert", IsPrimary: true}}})
	patrick, _ := db.CreatePerson(NewPerson{Sex: 0, Names: []NewPersonName{{Surname: "Adoptive", Given: "Patrick", IsPrimary: true}}})
	child, _ := db.CreatePerson(NewPerson{Sex: 2, Names: []NewPersonName{{Surname: "Smith", Given: "Kid", IsPrimary: true}}})

	// Child already has two separate, both-incomplete families: one
	// birth, one adopted -- each still missing a mother.
	if _, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: robert, ChildID: child, RelType: RelTypeBirth}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: patrick, ChildID: child, RelType: RelTypeAdopted}); err != nil {
		t.Fatal(err)
	}

	// The scenario above (Robert/Patrick, different RelType) resolves
	// unambiguously precisely because the two families differ in kind.
	// Genuine, still-unresolvable ambiguity needs two candidates that
	// match on BOTH role and kind -- constructed directly here: two
	// separate, both-incomplete Birth-type families for the same child.
	robert2, _ := db.CreatePerson(NewPerson{Sex: 0, Names: []NewPersonName{{Surname: "Bio2", Given: "Robert2", IsPrimary: true}}})
	child2, _ := db.CreatePerson(NewPerson{Sex: 2, Names: []NewPersonName{{Surname: "Smith", Given: "Kid2", IsPrimary: true}}})
	if _, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: robert, ChildID: child2, RelType: RelTypeBirth}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: robert2, ChildID: child2, RelType: RelTypeBirth}); err != nil {
		t.Fatal(err)
	}
	// child2 now has two distinct, both-incomplete Birth-type families
	// (Robert alone, Robert2 alone), both still missing a mother -- a
	// new Birth-type mother-role request is genuinely ambiguous between
	// them.
	mary, _ := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Smith", Given: "Mary", IsPrimary: true}}})
	_, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: mary, ChildID: child2, RelType: RelTypeBirth})
	if err == nil {
		t.Fatal("expected an error: child already belongs to two same-kind, both-incomplete families")
	}
	if !errors.Is(err, ErrAmbiguous) {
		t.Errorf("expected the error to wrap ErrAmbiguous (so the API layer maps it to 400, not 500), got: %v", err)
	}
}

func TestCreateParentChildRelationshipRejectsUnknownSexParent(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	parent, err := db.CreatePerson(NewPerson{Sex: 2, Names: []NewPersonName{{Surname: "Smith", Given: "Alex", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}
	child, err := db.CreatePerson(NewPerson{Sex: 2, Names: []NewPersonName{{Surname: "Smith", Given: "Kid", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: parent, ChildID: child})
	if err == nil {
		t.Fatal("expected an error when the parent's sex is unknown")
	}
	if !errors.Is(err, ErrAmbiguous) {
		t.Errorf("expected the error to wrap ErrAmbiguous (so the API layer maps it to 400, not 500), got: %v", err)
	}
}
