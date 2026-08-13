package rmdb

import (
	"database/sql"
	"fmt"
)

// NewParentChildRelationship is the input to
// CreateParentChildRelationship.
type NewParentChildRelationship struct {
	ParentID, ChildID int64
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
// meant, which the scope below is deliberately narrow about -- see each
// numbered case's own comment for why.
//
//  1. If ParentID already has an existing family in the matching role
//     (Father, if ParentID's own Sex is male; Mother, if female) --
//     confirmed to be exactly one such family -- the child is added
//     there. This is the confirmed, real workflow (see SCOPE.md's
//     "Stage 3" section): create the couple relationship first, giving
//     both parents an established family, then link each child with a
//     single ParentChild request naming either parent.
//  2. If ParentID has no existing family in that role at all, a new
//     single-parent family is created (RootsMagic itself supports this
//     -- see CreateCoupleRelationship's own comment), and the child is
//     added there.
//  3. If ParentID already has more than one family in that role (a
//     real, if less common, case -- remarriage), which family the
//     child belongs to is genuinely ambiguous from a bare (parent,
//     child) pair alone -- GEDCOM X's own ParentChild relationship type
//     has no third field to name a specific family. Rejected with a
//     clear error rather than guessing.
//
// If the child is already linked to the resolved family, this is a
// no-op (idempotent) rather than a duplicate ChildTable row or an
// error -- a second ParentChild request naming the other parent, once
// both parents already share a family, doesn't need to do anything
// further.
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

	var roleColumn string
	switch parentSex {
	case 0:
		roleColumn = "FatherID"
	case 1:
		roleColumn = "MotherID"
	default:
		return 0, fmt.Errorf("parent %d has unknown sex -- can't determine whether this is a father-child or mother-child relationship", input.ParentID)
	}

	tx, err := db.sql.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT FamilyID FROM FamilyTable WHERE "+roleColumn+" = ?", input.ParentID)
	if err != nil {
		return 0, fmt.Errorf("finding parent's existing families: %w", err)
	}
	var candidates []int64
	for rows.Next() {
		var fid int64
		if err := rows.Scan(&fid); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, fid)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	switch len(candidates) {
	case 0:
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
	case 1:
		familyID = candidates[0]
	default:
		return 0, fmt.Errorf(
			"parent %d already belongs to %d different families as %s -- which one this child belongs to can't be determined from a parent-child relationship alone; create or identify the specific couple relationship first",
			input.ParentID, len(candidates), roleColumn)
	}

	var alreadyLinked int
	err = tx.QueryRow(
		"SELECT COUNT(*) FROM ChildTable WHERE FamilyID = ? AND ChildID = ?",
		familyID, input.ChildID,
	).Scan(&alreadyLinked)
	if err != nil {
		return 0, fmt.Errorf("checking existing child link: %w", err)
	}
	if alreadyLinked > 0 {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("committing (no-op, already linked): %w", err)
		}
		return familyID, nil
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
		 VALUES (?, ?, ?, 0, 0, ?, 0, 0, 0, '', `+utcModDateExpr+`)`,
		recID, input.ChildID, familyID, childOrder,
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
}
