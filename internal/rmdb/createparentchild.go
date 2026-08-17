package rmdb

import (
	"database/sql"
	"fmt"
)

// RelType* are the RootsMagic RelFather/RelMother values this project
// populates. RootsMagic itself defines eight (0=Birth, 1=Adopted,
// 2=Step, 3=Foster, 4=Related, 5=Guardian, 6=Sealed, 7=Unknown --
// confirmed against the RM4-11 data dictionary). Five of them
// (everything except Related, Sealed, and Unknown) have a real, direct
// counterpart in GEDCOM X's own dedicated "Parent-Child Relationship
// Fact Types" (fact-types-specification.md, Section 2.3 -- a separate
// document from the conceptual model spec's own person-scoped fact
// types, found and checked directly, not assumed): BiologicalParent,
// AdoptiveParent, StepParent, FosterParent, GuardianParent. See
// relTypeFromFacts (internal/api/createrelationship.go) for the actual
// mapping. Related/Sealed/Unknown are left unmapped -- nothing in
// GEDCOM X's own vocabulary corresponds to them cleanly (Related and
// Unknown are too vague to justify guessing which GEDCOM X fact, if
// any, was meant; Sealed is a specifically LDS-temple-ordinance concept
// with no GEDCOM X Parent-Child fact type at all), and populating them
// without a real fact type driving the choice would be exactly the kind
// of speculative guess this project has consistently avoided elsewhere.
const (
	RelTypeBirth    = 0
	RelTypeAdopted  = 1
	RelTypeStep     = 2
	RelTypeFoster   = 3
	RelTypeGuardian = 5
)

// NewParentChildRelationship is the input to
// CreateParentChildRelationship.
type NewParentChildRelationship struct {
	ParentID, ChildID int64
	// RelType: one of the RelType* constants above -- the API layer
	// derives this from the GEDCOM X ParentChild relationship's own
	// Facts, matching GEDCOM X's dedicated Parent-Child Relationship
	// Fact Types (see the RelType* constants' own comment for the full
	// account). Defaults to RelTypeBirth when no matching fact is
	// present.
	RelType int
}

// CreateParentChildRelationship links a child to a parent.
//
// RootsMagic's own schema doesn't have anywhere to attach a bare
// "parent, child" pair directly, unlike this project's own read-side
// modeling of one (see SCOPE.md's "Relationships" section): a child
// belongs to a FAMILY (ChildTable.FamilyID), and that family separately
// has a FatherID/MotherID -- the "father-child" and "mother-child"
// relationships this server's read side already exposes as two distinct
// Relationship resources are really two views onto the same underlying
// family membership, not two independent facts RootsMagic stores
// separately. Creating one has to resolve which family is actually
// meant.
//
// The resolution deliberately never infers anything about a parent this
// server wasn't actually told about. An earlier version of this
// function resolved a bare (parent, child) pair by checking whether the
// named parent already had exactly one family on file, and used it
// directly if so -- but "the parent happens to have one family
// recorded" is a fact about this database's current contents, not a
// fact about the parent's real life, and treating it as an answer means
// silently asserting a co-parent that was never actually named. If
// Mary's only recorded family happens to be with Patrick, and a bare
// ParentChild(Mary, Child) request arrives, that says nothing at all
// about whether Patrick is Child's other parent -- it could just as
// easily be Robert. This was a real, corrected mistake in this
// project's own design process, not a hypothetical -- see SCOPE.md's
// "Stage 3" section for the full account, including empirical
// verification that a parent with two real, distinct partners keeps
// each child correctly attributed to the right one.
//
// The resolution below only ever reuses or completes a family based on
// something already established about the CHILD specifically:
//
//  1. If the child already belongs to a family that already has this
//     exact parent in the matching role, this is a no-op (idempotent).
//  2. If the child already belongs to a matching-kind family (see
//     RelType below) with that role empty, it's completed with this
//     parent. If completing it would create a second family record for
//     parents already paired elsewhere (a real case: the other
//     parent's own ParentChild request, or a Couple relationship, may
//     have already established that exact pairing under a different
//     FamilyID), the child's link is moved to the pre-existing family
//     instead, and the now-redundant one is removed -- merging only
//     ever happens once both parents are independently confirmed for
//     the same child, not as a guess.
//  3. If the child has no matching family at all, a new one is created
//     for this parent alone (RootsMagic itself supports single-parent
//     families -- see CreateCoupleRelationship's own comment).
//  4. If more than one of the child's existing families could match,
//     this is genuinely ambiguous and rejected rather than guessed at.
//
// "Matching kind" (RelType) is what lets a child who already has, say,
// an incomplete biological family and a separate incomplete adoptive
// family still resolve unambiguously: a new birth-type link only
// considers birth-type candidates, an adopted-type link only considers
// adopted-type ones. A candidate family is one where the target role is
// empty and the other role is either also empty or already the same
// RelType; a filled other role with a different RelType excludes it.
// This is a real refinement prompted by RootsMagic's own schema
// supporting more than one family per child at all
// (ChildTable.RelFather/RelMother -- confirmed against the data
// dictionary), but is not yet verified against a real captured adoption
// case -- see SCOPE.md's own note on this.
//
// Sex "Unknown" (PersonTable.Sex = 2) can't be resolved to Father or
// Mother at all and is rejected -- this server has no basis to guess
// which role was meant.
func (db *DB) CreateParentChildRelationship(input NewParentChildRelationship) (familyID int64, err error) {
	var parentSex int
	err = db.sql.QueryRow("SELECT Sex FROM PersonTable WHERE PersonID = ?", input.ParentID).Scan(&parentSex)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("%w: parent %s%d", ErrNotFound, "P", input.ParentID)
	}
	if err != nil {
		return 0, fmt.Errorf("looking up parent's sex: %w", err)
	}

	var childExists int
	err = db.sql.QueryRow("SELECT 1 FROM PersonTable WHERE PersonID = ?", input.ChildID).Scan(&childExists)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("%w: child %s%d", ErrNotFound, "P", input.ChildID)
	}
	if err != nil {
		return 0, fmt.Errorf("looking up child: %w", err)
	}

	var roleColumn, relColumn string
	switch parentSex {
	case 0:
		roleColumn, relColumn = "FatherID", "RelFather"
	case 1:
		roleColumn, relColumn = "MotherID", "RelMother"
	default:
		return 0, fmt.Errorf("%w: parent %d has unknown sex -- can't determine whether this is a father-child or mother-child relationship", ErrAmbiguous, input.ParentID)
	}

	tx, err := db.sql.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Find the child's existing family links, each with enough context
	// (both roles' ids and RelTypes) to decide whether it's a match, an
	// idempotent repeat, or irrelevant.
	rows, err := tx.Query(
		"SELECT ct.FamilyID, ft.FatherID, ft.MotherID, ct.RelFather, ct.RelMother "+
			"FROM ChildTable ct JOIN FamilyTable ft ON ft.FamilyID = ct.FamilyID WHERE ct.ChildID = ?",
		input.ChildID,
	)
	if err != nil {
		return 0, fmt.Errorf("finding child's existing families: %w", err)
	}
	type existingFamily struct {
		familyID             int64
		fatherID, motherID   int64
		relFather, relMother int
	}
	var childFamilies []existingFamily
	for rows.Next() {
		var f existingFamily
		if err := rows.Scan(&f.familyID, &f.fatherID, &f.motherID, &f.relFather, &f.relMother); err != nil {
			rows.Close()
			return 0, err
		}
		childFamilies = append(childFamilies, f)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	roleID := func(f existingFamily) int64 {
		if roleColumn == "FatherID" {
			return f.fatherID
		}
		return f.motherID
	}
	otherRoleID := func(f existingFamily) int64 {
		if roleColumn == "FatherID" {
			return f.motherID
		}
		return f.fatherID
	}
	otherRelType := func(f existingFamily) int {
		if roleColumn == "FatherID" {
			return f.relMother
		}
		return f.relFather
	}

	// Case 1: already exactly this parent in this role somewhere --
	// idempotent, regardless of RelType (the parent's identity already
	// settles it; there's nothing left to disambiguate).
	for _, f := range childFamilies {
		if roleID(f) == input.ParentID {
			if err := tx.Commit(); err != nil {
				return 0, fmt.Errorf("committing (no-op, already linked): %w", err)
			}
			return f.familyID, nil
		}
	}

	// Case 2/4: candidates where the target role is empty and the other
	// role (if filled) doesn't conflict in kind.
	var candidates []existingFamily
	for _, f := range childFamilies {
		if roleID(f) != 0 {
			continue // role already filled by someone else
		}
		if otherRoleID(f) != 0 && otherRelType(f) != input.RelType {
			continue // other parent already recorded, but as a different kind of relationship
		}
		candidates = append(candidates, f)
	}

	switch len(candidates) {
	case 1:
		fid := candidates[0].familyID
		other := otherRoleID(candidates[0])

		// Would completing this family duplicate one that already has
		// this exact pairing (established independently, e.g. via the
		// other parent's own ParentChild request or a Couple
		// relationship)? If so, merge into that one instead of leaving
		// two records for the same real pairing.
		var dupFamilyID int64
		var dupErr error
		if roleColumn == "FatherID" {
			dupErr = tx.QueryRow("SELECT FamilyID FROM FamilyTable WHERE FatherID = ? AND MotherID = ? AND FamilyID != ?", input.ParentID, other, fid).Scan(&dupFamilyID)
		} else {
			dupErr = tx.QueryRow("SELECT FamilyID FROM FamilyTable WHERE MotherID = ? AND FatherID = ? AND FamilyID != ?", input.ParentID, other, fid).Scan(&dupFamilyID)
		}
		if dupErr == nil {
			// Moving the child to the pre-existing family also means
			// its ChildOrder needs recomputing against THAT family's
			// own existing children, not preserved from the temporary
			// family it's leaving (which, being newly created for this
			// child alone, always had ChildOrder=1 -- every merged
			// child would otherwise silently collide at 1).
			var maxOrder sql.NullInt64
			if err := tx.QueryRow("SELECT MAX(ChildOrder) FROM ChildTable WHERE FamilyID = ?", dupFamilyID).Scan(&maxOrder); err != nil {
				return 0, fmt.Errorf("finding next child order in the pre-existing family: %w", err)
			}
			newOrder := 1
			if maxOrder.Valid {
				newOrder = int(maxOrder.Int64) + 1
			}
			if _, err := tx.Exec(
				"UPDATE ChildTable SET FamilyID = ?, ChildOrder = ? WHERE FamilyID = ? AND ChildID = ?",
				dupFamilyID, newOrder, fid, input.ChildID,
			); err != nil {
				return 0, fmt.Errorf("moving child to the pre-existing family: %w", err)
			}
			if _, err := tx.Exec(
				"UPDATE PersonTable SET ParentID = ?, UTCModDate = "+utcModDateExpr+" WHERE PersonID = ?",
				dupFamilyID, input.ChildID,
			); err != nil {
				return 0, fmt.Errorf("updating child's ParentID to the pre-existing family: %w", err)
			}
			var remaining int
			if err := tx.QueryRow("SELECT COUNT(*) FROM ChildTable WHERE FamilyID = ?", fid).Scan(&remaining); err != nil {
				return 0, fmt.Errorf("checking for remaining children in the now-redundant family: %w", err)
			}
			if remaining == 0 {
				if _, err := tx.Exec("DELETE FROM FamilyTable WHERE FamilyID = ?", fid); err != nil {
					return 0, fmt.Errorf("removing the now-redundant family: %w", err)
				}
			}
			if err := bumpConfigTableModDate(tx); err != nil {
				return 0, err
			}
			if err := tx.Commit(); err != nil {
				return 0, fmt.Errorf("committing merge into pre-existing family: %w", err)
			}
			return dupFamilyID, nil
		}

		if _, err := tx.Exec(
			"UPDATE FamilyTable SET "+roleColumn+" = ?, UTCModDate = "+utcModDateExpr+" WHERE FamilyID = ?",
			input.ParentID, fid,
		); err != nil {
			return 0, fmt.Errorf("completing existing family: %w", err)
		}
		if _, err := tx.Exec(
			"UPDATE ChildTable SET "+relColumn+" = ? WHERE FamilyID = ? AND ChildID = ?",
			input.RelType, fid, input.ChildID,
		); err != nil {
			return 0, fmt.Errorf("recording relationship kind: %w", err)
		}
		if _, err := tx.Exec(
			"UPDATE PersonTable SET SpouseID = ?, UTCModDate = "+utcModDateExpr+" WHERE PersonID = ?",
			fid, input.ParentID,
		); err != nil {
			return 0, fmt.Errorf("updating parent's SpouseID: %w", err)
		}
		if err := bumpConfigTableModDate(tx); err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("committing completed family: %w", err)
		}
		return fid, nil

	case 0:
		// No matching family at all -- create a new one for this parent
		// alone. Never falls back to searching by the parent's own role
		// in isolation (the mistake documented above): a child with no
		// established link of this kind gets a fresh family every time,
		// regardless of how many other families the named parent
		// already has on file for other children.
		familyID, err = nextID(tx, "FamilyTable", "FamilyID")
		if err != nil {
			return 0, fmt.Errorf("assigning new FamilyID: %w", err)
		}
		fatherID, motherID := int64(0), int64(0)
		if roleColumn == "FatherID" {
			fatherID = input.ParentID
		} else {
			motherID = input.ParentID
		}
		_, err = tx.Exec(
			`INSERT INTO FamilyTable
			 (FamilyID, FatherID, MotherID, ChildID, HusbOrder, WifeOrder, IsPrivate, Proof,
			  SpouseLabel, FatherLabel, MotherLabel, SpouseLabelStr, FatherLabelStr, MotherLabelStr,
			  Note, UTCModDate)
			 VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, 0, 0, '', '', '', '', `+utcModDateExpr+`)`,
			familyID, fatherID, motherID,
		)
		if err != nil {
			return 0, fmt.Errorf("creating single-parent family: %w", err)
		}
		if _, err := tx.Exec(
			"UPDATE PersonTable SET SpouseID = ?, UTCModDate = "+utcModDateExpr+" WHERE PersonID = ?",
			familyID, input.ParentID,
		); err != nil {
			return 0, fmt.Errorf("updating parent's SpouseID: %w", err)
		}

		relFather, relMother := 0, 0
		if roleColumn == "FatherID" {
			relFather = input.RelType
		} else {
			relMother = input.RelType
		}
		var maxOrder sql.NullInt64
		if err := tx.QueryRow("SELECT MAX(ChildOrder) FROM ChildTable WHERE FamilyID = ?", familyID).Scan(&maxOrder); err != nil {
			return 0, fmt.Errorf("finding next child order: %w", err)
		}
		childOrder := 1
		if maxOrder.Valid {
			childOrder = int(maxOrder.Int64) + 1
		}
		recID, err := nextID(tx, "ChildTable", "RecID")
		if err != nil {
			return 0, fmt.Errorf("assigning new ChildTable RecID: %w", err)
		}
		_, err = tx.Exec(
			`INSERT INTO ChildTable
			 (RecID, ChildID, FamilyID, RelFather, RelMother, ChildOrder, IsPrivate, ProofFather,
			  ProofMother, Note, UTCModDate)
			 VALUES (?, ?, ?, ?, ?, ?, 0, 0, 0, '', `+utcModDateExpr+`)`,
			recID, input.ChildID, familyID, relFather, relMother, childOrder,
		)
		if err != nil {
			return 0, fmt.Errorf("linking child to family: %w", err)
		}
		if _, err := tx.Exec(
			"UPDATE PersonTable SET ParentID = ?, UTCModDate = "+utcModDateExpr+" WHERE PersonID = ?",
			familyID, input.ChildID,
		); err != nil {
			return 0, fmt.Errorf("updating child's ParentID: %w", err)
		}
		if err := bumpConfigTableModDate(tx); err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("committing new parent-child relationship: %w", err)
		}
		return familyID, nil

	default:
		return 0, fmt.Errorf(
			"%w: child already has %d families matching this relationship's role and kind -- which one this applies to can't be determined from a parent-child relationship alone",
			ErrAmbiguous, len(candidates))
	}
}
