package rmdb

import "fmt"

// NewCoupleRelationship is the input to CreateCoupleRelationship.
type NewCoupleRelationship struct {
	// FatherID/MotherID: 0 means "not specified" -- RootsMagic supports
	// single-parent families (confirmed against real data: several
	// families in royal92.rmtree have FatherID or MotherID at 0), so at
	// least one, but not necessarily both, is required.
	FatherID, MotherID int64
	Facts              []NewPersonFact // reused as-is; see its own comment
}

// CreateCoupleRelationship creates a new FamilyTable row -- a "couple"
// relationship in GEDCOM X terms -- plus zero or more EventTable rows
// (one per fact, e.g. a Marriage), and updates SpouseID on whichever of
// FatherID/MotherID were actually specified.
//
// Returns the newly assigned FamilyID.
//
// Matches CreatePerson's own conventions throughout: ids assigned as one
// past the current maximum (see nextID), UTCModDate set at full
// precision on exactly the rows this function itself writes and nothing
// else. A real captured diff for this exact operation showed RootsMagic
// itself bumping UTCModDate on only one of the two PersonTable rows
// (confirmed directly against raw, unredacted values, not just the
// golden file) -- deliberately not replicated, per the same reasoning
// documented on CreatePerson and in SCOPE.md's "Stage 3" section: both
// rows get bumped here, consistently, since there's no principled reason
// to prefer one spouse's timestamp over the other's.
func (db *DB) CreateCoupleRelationship(input NewCoupleRelationship) (familyID int64, err error) {
	if input.FatherID == 0 && input.MotherID == 0 {
		return 0, fmt.Errorf("a couple relationship needs at least one of FatherID/MotherID")
	}

	tx, err := db.sql.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	placeIDs := make([]int64, len(input.Facts))
	for i, f := range input.Facts {
		if f.PlaceText == "" {
			continue
		}
		pid, err := resolveOrCreatePlace(tx, f.PlaceText)
		if err != nil {
			return 0, fmt.Errorf("resolving place %q: %w", f.PlaceText, err)
		}
		placeIDs[i] = pid
	}

	familyID, err = nextID(tx, "FamilyTable", "FamilyID")
	if err != nil {
		return 0, fmt.Errorf("assigning new FamilyID: %w", err)
	}

	for i, f := range input.Facts {
		eventID, err := nextID(tx, "EventTable", "EventID")
		if err != nil {
			return 0, fmt.Errorf("assigning new EventID: %w", err)
		}
		sortDate := NoDateSortValue
		if f.DateString != "." {
			sortDate = ComputeSortDate(f.SortYear, f.SortMonth, f.SortDay)
		}
		_, err = tx.Exec(
			`INSERT INTO EventTable
			 (EventID, EventType, OwnerType, OwnerID, FamilyID, PlaceID, SiteID, Date, SortDate,
			  IsPrimary, IsPrivate, Proof, Status, Sentence, Details, Note, UTCModDate)
			 VALUES (?, ?, ?, ?, 0, ?, 0, ?, ?, 0, 0, 0, 0, '', '', '', `+utcModDateExpr+`)`,
			eventID, f.FactTypeID, OwnerTypeFamily, familyID, placeIDs[i], f.DateString, sortDate,
		)
		if err != nil {
			return 0, fmt.Errorf("creating fact: %w", err)
		}
	}

	_, err = tx.Exec(
		`INSERT INTO FamilyTable
		 (FamilyID, FatherID, MotherID, ChildID, HusbOrder, WifeOrder, IsPrivate, Proof,
		  SpouseLabel, FatherLabel, MotherLabel, SpouseLabelStr, FatherLabelStr, MotherLabelStr,
		  Note, UTCModDate)
		 VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, 0, 0, '', '', '', '', `+utcModDateExpr+`)`,
		familyID, input.FatherID, input.MotherID,
	)
	if err != nil {
		return 0, fmt.Errorf("creating family: %w", err)
	}

	for _, personID := range []int64{input.FatherID, input.MotherID} {
		if personID == 0 {
			continue
		}
		if _, err := tx.Exec(
			"UPDATE PersonTable SET SpouseID = ?, UTCModDate = "+utcModDateExpr+" WHERE PersonID = ?",
			familyID, personID,
		); err != nil {
			return 0, fmt.Errorf("updating spouse's SpouseID: %w", err)
		}
	}

	if err := bumpConfigTableModDate(tx); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing new couple relationship: %w", err)
	}
	return familyID, nil
}
