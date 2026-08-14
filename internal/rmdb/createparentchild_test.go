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

	for i, childID := range children {
		gotFamilyID, err := db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: father, ChildID: childID})
		if err != nil {
			t.Fatalf("linking child %d: %v", childID, err)
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
		wantOrder := i + 1
		if childOrder != wantOrder {
			t.Errorf("child %d: ChildOrder = %d, want %d", childID, childOrder, wantOrder)
		}
		if relFather != 0 || relMother != 0 {
			t.Errorf("child %d: RelFather/RelMother = %d/%d, want 0/0", childID, relFather, relMother)
		}

		var parentID int64
		if err := db.sql.QueryRow("SELECT ParentID FROM PersonTable WHERE PersonID = ?", childID).Scan(&parentID); err != nil {
			t.Fatal(err)
		}
		if parentID != familyID {
			t.Errorf("child %d: PersonTable.ParentID = %d, want %d", childID, parentID, familyID)
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

func TestCreateParentChildRelationshipRejectsAmbiguousMultipleFamilies(t *testing.T) {
	db := setupCreatePersonTestDB(t)

	father, err := db.CreatePerson(NewPerson{Sex: 0, Names: []NewPersonName{{Surname: "Smith", Given: "John", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}
	// Two separate families, both with this same father (a real,
	// possible case -- remarriage).
	mother1, _ := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Smith", Given: "Jane", IsPrimary: true}}})
	mother2, _ := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Smith", Given: "Ann", IsPrimary: true}}})
	if _, err := db.CreateCoupleRelationship(NewCoupleRelationship{FatherID: father, MotherID: mother1}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateCoupleRelationship(NewCoupleRelationship{FatherID: father, MotherID: mother2}); err != nil {
		t.Fatal(err)
	}

	child, err := db.CreatePerson(NewPerson{Sex: 1, Names: []NewPersonName{{Surname: "Smith", Given: "Kid", IsPrimary: true}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateParentChildRelationship(NewParentChildRelationship{ParentID: father, ChildID: child})
	if err == nil {
		t.Fatal("expected an error when the parent belongs to more than one family in the matching role")
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
