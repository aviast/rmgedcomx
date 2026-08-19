package rmdb

import (
	"database/sql"
	"fmt"
)

// NewCoupleRelationship is the input to CreateCoupleRelationship.
type NewCoupleRelationship struct {
	// FatherID/MotherID: 0 means "not specified" -- RootsMagic supports
	// single-parent families (confirmed against real data: several
	// families in royal92.rmtree have FatherID or MotherID at 0), so at
	// least one, but not necessarily both, is required.
	FatherID, MotherID int64
	Facts              []NewPersonFact // reused as-is; see its own comment
}

// CreateCoupleRelationship creates or reuses a FamilyTable row for a
// "couple" relationship in GEDCOM X terms, and attaches zero or more
// EventTable rows (one per fact, e.g. a Marriage) to it.
//
// Returns the family's id -- freshly created, or an existing one this
// exact pairing already matches or can complete (see below). Unlike
// CreateParentChildRelationship, checking for an existing match here is
// safe to do eagerly: a Couple relationship explicitly names both
// people, so there's no unstated "who's the other parent" that reusing
// an existing family might silently assume incorrectly.
//
//   - If a family already exists with exactly this Father/Mother
//     pairing, that one is used directly (idempotent) rather than
//     creating a duplicate.
//   - If either person already has an existing family with the other
//     role empty, it's completed with this pairing rather than a new
//     family being created -- the real case this covers: a child's
//     CreateParentChildRelationship calls may have already established
//     both parents independently (see its own comment), making a
//     subsequent Couple relationship for the same two people fully
//     redundant except for whatever Facts it carries.
//
// Either way, this function's own Facts still get attached to whichever
// family id is actually in play -- reusing or completing a family isn't
// a shortcut that skips the rest of the request, only the part that
// would otherwise create a duplicate FamilyTable row.
//
// Deliberately does not touch PersonTable.SpouseID at all -- see
// CreateParentChildRelationship's own comment for why this project no
// longer tries to compute a value for it.
//
// Matches CreatePerson's own conventions throughout: ids assigned as one
// past the current maximum (see nextID), UTCModDate set at full
// precision on exactly the rows this function itself writes and nothing
// else.
func (db *DB) CreateCoupleRelationship(input NewCoupleRelationship) (familyID int64, err error) {
	if input.FatherID == 0 && input.MotherID == 0 {
		return 0, fmt.Errorf("a couple relationship needs at least one of FatherID/MotherID")
	}

	tx, err := db.sql.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	if input.FatherID != 0 && input.MotherID != 0 {
		var existing int64
		err := tx.QueryRow("SELECT FamilyID FROM FamilyTable WHERE FatherID = ? AND MotherID = ?", input.FatherID, input.MotherID).Scan(&existing)
		if err != nil && err != sql.ErrNoRows {
			return 0, fmt.Errorf("checking for an existing pairing: %w", err)
		}
		if err == nil {
			familyID = existing
		} else {
			var completeFamilyID int64
			var col string
			var completeVal int64
			err = tx.QueryRow("SELECT FamilyID FROM FamilyTable WHERE FatherID = ? AND MotherID = 0", input.FatherID).Scan(&completeFamilyID)
			if err != nil && err != sql.ErrNoRows {
				return 0, fmt.Errorf("checking for a completable family (father's side): %w", err)
			}
			if err == nil {
				col, completeVal = "MotherID", input.MotherID
			} else {
				err = tx.QueryRow("SELECT FamilyID FROM FamilyTable WHERE MotherID = ? AND FatherID = 0", input.MotherID).Scan(&completeFamilyID)
				if err != nil && err != sql.ErrNoRows {
					return 0, fmt.Errorf("checking for a completable family (mother's side): %w", err)
				}
				if err == nil {
					col, completeVal = "FatherID", input.FatherID
				}
			}
			if completeFamilyID != 0 {
				if _, err := tx.Exec(
					"UPDATE FamilyTable SET "+col+" = ?, UTCModDate = "+utcModDateExpr+" WHERE FamilyID = ?",
					completeVal, completeFamilyID,
				); err != nil {
					return 0, fmt.Errorf("completing existing family: %w", err)
				}
				familyID = completeFamilyID
			}
		}
	}

	if familyID == 0 {
		familyID, err = nextID(tx, "FamilyTable", "FamilyID")
		if err != nil {
			return 0, fmt.Errorf("assigning new FamilyID: %w", err)
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
	}

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
			 VALUES (?, ?, ?, ?, 0, ?, 0, ?, ?, 0, 0, 0, 0, '', '', ?, `+utcModDateExpr+`)`,
			eventID, f.FactTypeID, OwnerTypeFamily, familyID, placeIDs[i], f.DateString, sortDate, f.Note,
		)
		if err != nil {
			return 0, fmt.Errorf("creating fact: %w", err)
		}
	}

	if err := bumpConfigTableModDate(tx); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing couple relationship: %w", err)
	}
	return familyID, nil
}
