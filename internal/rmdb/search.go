package rmdb

import (
	"fmt"
	"strings"
)

// SearchTextCriterion is one text-valued search criterion -- Value to
// match, and whether the match must be exact or a substring ("~" in the
// RS spec's own search query grammar -- Section 6, "q" template
// variable). Substring, not fuzzy/phonetic, matching for "~": checked
// directly against NameTable.GivenMP/SurnameMP before assuming
// otherwise -- both are accent-folded (FoldAccents) copies of
// Given/Surname, not a phonetic encoding (RootsMagic has no Metaphone
// or Soundex column despite the "MP" name), so there's no existing
// RootsMagic infrastructure to build fuzzy matching on top of here.
type SearchTextCriterion struct {
	Value string
	Exact bool
}

// SearchDateCriterion is a SortDate range a fact's own SortDate must
// fall within -- computed by the caller (internal/api's
// buildSearchCriteria) via ComputeSortDate from a parsed search-query
// date value, since date-text parsing belongs at the API layer
// (internal/gedcomx.ParseGedcom5Date), not here; this package only ever
// operates on already-resolved values, the same layering already used
// throughout this project's own write support.
type SearchDateCriterion struct {
	MinSortDate, MaxSortDate int64
}

// RelationCriteria holds the 9 "{relation}"-prefixed GEDCOM X RS search
// parameters (RS spec Section 6) for one relation -- {relation}Name,
// GivenName, Surname, BirthDate, BirthPlace, DeathDate, DeathPlace,
// MarriageDate, MarriagePlace. {relation} substitutes "father",
// "mother", "spouse", or "parent"; SearchCriteria holds up to one of
// these per relation, matched against a specific relative found via the
// searched person's own real family data, not the relative's identity
// alone.
//
// All 9 criteria within one RelationCriteria are matched against the
// *same* specific relative, not independently against any relative of
// that kind -- checked directly against the spec's own wording
// ("the given name of the {relation}", singular) before deciding this,
// not assumed: `fatherGivenName:John fatherSurname:Smith` means one
// father named John Smith, not a father named John and (possibly a
// different) father named Smith, which is the only reading consistent
// with "the {relation}" being one specific person's own facts, not a
// disjunction across everyone who has ever held that role for this
// person.
type RelationCriteria struct {
	Name          *SearchTextCriterion
	GivenName     *SearchTextCriterion
	Surname       *SearchTextCriterion
	BirthDate     *SearchDateCriterion
	BirthPlace    *SearchTextCriterion
	DeathDate     *SearchDateCriterion
	DeathPlace    *SearchTextCriterion
	MarriageDate  *SearchDateCriterion
	MarriagePlace *SearchTextCriterion
}

// SearchCriteria holds the 10 "direct" GEDCOM X RS search parameters
// (RS spec Section 6): name, givenName, surname, gender, birthDate,
// birthPlace, deathDate, deathPlace, marriageDate, marriagePlace --
// plus the 4 possible "{relation}"-prefixed groups (father, mother,
// spouse, parent), each covering the 9 fields RelationCriteria models.
type SearchCriteria struct {
	Name          *SearchTextCriterion
	GivenName     *SearchTextCriterion
	Surname       *SearchTextCriterion
	Gender        *int // 0=male, 1=female -- PersonTable.Sex's own encoding; nil means unspecified
	BirthDate     *SearchDateCriterion
	BirthPlace    *SearchTextCriterion
	DeathDate     *SearchDateCriterion
	DeathPlace    *SearchTextCriterion
	MarriageDate  *SearchDateCriterion
	MarriagePlace *SearchTextCriterion

	Father *RelationCriteria
	Mother *RelationCriteria
	Spouse *RelationCriteria
	Parent *RelationCriteria
}

// relativeConditions returns a single, AND-joined SQL condition (and its
// bind arguments, in the exact order they appear in that condition's own
// text) matching a specific relative against every non-nil field of rc.
// relativeIDExpr/familyIDExpr are raw SQL expressions referencing the
// enclosing query's own columns (e.g. "f.FatherID", "f.FamilyID"), not
// bind parameters -- the specific relative and family a caller has
// already resolved via a real family relationship, never a
// caller-supplied value.
//
// Name/GivenName/Surname match the relative's own primary NameTable
// row; BirthDate/BirthPlace/DeathDate/DeathPlace match the relative's
// own Birth/Death facts (EventTable, OwnerType=Person); MarriageDate/
// MarriagePlace match familyIDExpr's own Marriage fact (OwnerType=
// Family) -- the specific family that established this relation in the
// first place (the family a father/mother/parent relation was found via
// a ChildTable/FamilyTable row for, or the family a spouse relation
// itself is), not just any marriage the relative has ever had. This is
// a real, considered design choice, not the only possible reading:
// "{relation}MarriageDate" could instead mean any marriage of the
// relative's, matching how the direct marriageDate parameter treats a
// person with several. Tying it to the specific family in play avoids
// that ambiguity entirely and is available for free here, since that
// family is already known by the time this function is called for
// father/mother/parent/spouse alike.
//
// If rc has no fields set at all, returns "1=1" (a no-op condition,
// with no arguments) rather than an empty string -- defensive against
// a RelationCriteria that was constructed but never actually
// populated; buildSearchCriteria (internal/api) never produces one,
// since a relation group with zero fields is never created there, but
// this function doesn't assume that invariant holds in every future
// caller.
func relativeConditions(relativeIDExpr, familyIDExpr string, rc *RelationCriteria) (string, []any) {
	var conds []string
	var args []any

	if rc.Name != nil {
		cond, a := textCondition("rn.Given || ' ' || rn.Surname", rc.Name)
		conds = append(conds, "EXISTS (SELECT 1 FROM NameTable rn WHERE rn.OwnerID = "+relativeIDExpr+" AND rn.IsPrimary = 1 AND "+cond+")")
		args = append(args, a...)
	}
	if rc.GivenName != nil {
		cond, a := textCondition("rn.Given", rc.GivenName)
		conds = append(conds, "EXISTS (SELECT 1 FROM NameTable rn WHERE rn.OwnerID = "+relativeIDExpr+" AND rn.IsPrimary = 1 AND "+cond+")")
		args = append(args, a...)
	}
	if rc.Surname != nil {
		cond, a := textCondition("rn.Surname", rc.Surname)
		conds = append(conds, "EXISTS (SELECT 1 FROM NameTable rn WHERE rn.OwnerID = "+relativeIDExpr+" AND rn.IsPrimary = 1 AND "+cond+")")
		args = append(args, a...)
	}
	if rc.BirthDate != nil {
		conds = append(conds, `EXISTS (
			SELECT 1 FROM EventTable re
			WHERE re.OwnerType = `+fmt.Sprint(OwnerTypePerson)+` AND re.OwnerID = `+relativeIDExpr+` AND re.EventType = 1
			AND re.SortDate BETWEEN ? AND ?)`)
		args = append(args, rc.BirthDate.MinSortDate, rc.BirthDate.MaxSortDate)
	}
	if rc.BirthPlace != nil {
		cond, a := textCondition("rpl.Name", rc.BirthPlace)
		conds = append(conds, `EXISTS (
			SELECT 1 FROM EventTable re JOIN PlaceTable rpl ON rpl.PlaceID = re.PlaceID
			WHERE re.OwnerType = `+fmt.Sprint(OwnerTypePerson)+` AND re.OwnerID = `+relativeIDExpr+` AND re.EventType = 1
			AND `+cond+`)`)
		args = append(args, a...)
	}
	if rc.DeathDate != nil {
		conds = append(conds, `EXISTS (
			SELECT 1 FROM EventTable re
			WHERE re.OwnerType = `+fmt.Sprint(OwnerTypePerson)+` AND re.OwnerID = `+relativeIDExpr+` AND re.EventType = 2
			AND re.SortDate BETWEEN ? AND ?)`)
		args = append(args, rc.DeathDate.MinSortDate, rc.DeathDate.MaxSortDate)
	}
	if rc.DeathPlace != nil {
		cond, a := textCondition("rpl.Name", rc.DeathPlace)
		conds = append(conds, `EXISTS (
			SELECT 1 FROM EventTable re JOIN PlaceTable rpl ON rpl.PlaceID = re.PlaceID
			WHERE re.OwnerType = `+fmt.Sprint(OwnerTypePerson)+` AND re.OwnerID = `+relativeIDExpr+` AND re.EventType = 2
			AND `+cond+`)`)
		args = append(args, a...)
	}
	if rc.MarriageDate != nil {
		conds = append(conds, `EXISTS (
			SELECT 1 FROM EventTable fe
			WHERE fe.OwnerType = `+fmt.Sprint(OwnerTypeFamily)+` AND fe.OwnerID = `+familyIDExpr+` AND fe.EventType = 300
			AND fe.SortDate BETWEEN ? AND ?)`)
		args = append(args, rc.MarriageDate.MinSortDate, rc.MarriageDate.MaxSortDate)
	}
	if rc.MarriagePlace != nil {
		cond, a := textCondition("fpl.Name", rc.MarriagePlace)
		conds = append(conds, `EXISTS (
			SELECT 1 FROM EventTable fe JOIN PlaceTable fpl ON fpl.PlaceID = fe.PlaceID
			WHERE fe.OwnerType = `+fmt.Sprint(OwnerTypeFamily)+` AND fe.OwnerID = `+familyIDExpr+` AND fe.EventType = 300
			AND `+cond+`)`)
		args = append(args, a...)
	}

	if len(conds) == 0 {
		return "1=1", nil
	}
	return "(" + strings.Join(conds, " AND ") + ")", args
}

// relationCondition builds the EXISTS condition for one of the four
// "{relation}" search parameter groups against the person being
// searched (p.PersonID, the enclosing SearchPersons query's own
// column).
//
//   - father/mother: resolved via ChildTable/FamilyTable, the same
//     "which family is this person a child in" relationship
//     buildDisplayProperties' own familiesAsChild already uses --
//     matches if *any* of the person's families-as-child has a
//     father/mother (respectively) satisfying rc, the same
//     "any of several, not all" precedent already established for the
//     direct marriageDate parameter's own multiple-marriages handling.
//   - parent: father OR mother, but -- per RelationCriteria's own
//     comment -- all of rc's own fields must be satisfied by the *same*
//     parent, not one field by the father and another by the mother.
//   - spouse: resolved via FamilyTable directly (families where this
//     person is one of the two parents), matching the other parent in
//     that same family; also "any of several" if the person has more
//     than one.
func relationCondition(role string, rc *RelationCriteria) (string, []any) {
	switch role {
	case "father", "mother":
		col := "FatherID"
		if role == "mother" {
			col = "MotherID"
		}
		core, args := relativeConditions("f."+col, "f.FamilyID", rc)
		return `EXISTS (
			SELECT 1 FROM ChildTable ct JOIN FamilyTable f ON f.FamilyID = ct.FamilyID
			WHERE ct.ChildID = p.PersonID AND f.` + col + ` != 0 AND ` + core + `)`, args
	case "parent":
		fatherCore, fatherArgs := relativeConditions("f.FatherID", "f.FamilyID", rc)
		motherCore, motherArgs := relativeConditions("f.MotherID", "f.FamilyID", rc)
		sql := `EXISTS (
			SELECT 1 FROM ChildTable ct JOIN FamilyTable f ON f.FamilyID = ct.FamilyID
			WHERE ct.ChildID = p.PersonID
			AND ((f.FatherID != 0 AND ` + fatherCore + `) OR (f.MotherID != 0 AND ` + motherCore + `)))`
		return sql, append(fatherArgs, motherArgs...)
	case "spouse":
		// The relative is whichever of FatherID/MotherID *isn't* this
		// person -- two branches, since which column holds "the other
		// one" depends on which role this person themselves occupies
		// in that family.
		motherCore, motherArgs := relativeConditions("f.MotherID", "f.FamilyID", rc) // used when p is the father
		fatherCore, fatherArgs := relativeConditions("f.FatherID", "f.FamilyID", rc) // used when p is the mother
		sql := `EXISTS (
			SELECT 1 FROM FamilyTable f
			WHERE (f.FatherID = p.PersonID AND f.MotherID != 0 AND ` + motherCore + `)
			OR (f.MotherID = p.PersonID AND f.FatherID != 0 AND ` + fatherCore + `))`
		return sql, append(motherArgs, fatherArgs...)
	default:
		// Unreachable: buildSearchCriteria (internal/api) only ever
		// constructs SearchCriteria.Father/Mother/Spouse/Parent, and
		// SearchPersons (below) only ever calls relationCondition with
		// one of exactly those four role names.
		return "1=1", nil
	}
}

// text criterion against the given column expression -- LOWER(...) on
// both sides throughout, so matching is case-insensitive regardless of
// which column's own default collation applies (PersonTable.Sex isn't
// even text; PlaceTable.Name and NameTable.Given/Surname aren't
// guaranteed to share a collation), rather than relying on an implicit,
// possibly-inconsistent one.
func textCondition(column string, c *SearchTextCriterion) (string, []any) {
	if c.Exact {
		return "LOWER(" + column + ") = LOWER(?)", []any{c.Value}
	}
	return "LOWER(" + column + ") LIKE '%' || LOWER(?) || '%'", []any{c.Value}
}

// SearchPersons implements the Person Search Results state's own query
// (RS spec Section 4.11) for all 10 direct search parameters and all 4
// possible "{relation}"-prefixed groups (see RelationCriteria's own
// comment for how those are resolved and matched). Facts
// (birth/death/marriage) are matched via EXISTS subqueries rather than
// JOINs, deliberately: a person can have more than one marriage (or, in
// principle, more than one recorded Birth/Death fact), and a JOIN would
// multiply PersonID rows that a naive SELECT DISTINCT could mask
// incorrectly for the COUNT query specifically -- EXISTS avoids that
// row-multiplication entirely rather than working around it after the
// fact.
func (db *DB) SearchPersons(criteria SearchCriteria, limit, offset int) ([]Person, int, error) {
	var conditions []string
	var args []any

	if criteria.Name != nil {
		cond, a := textCondition("n.Given || ' ' || n.Surname", criteria.Name)
		conditions = append(conditions, cond)
		args = append(args, a...)
	}
	if criteria.GivenName != nil {
		cond, a := textCondition("n.Given", criteria.GivenName)
		conditions = append(conditions, cond)
		args = append(args, a...)
	}
	if criteria.Surname != nil {
		cond, a := textCondition("n.Surname", criteria.Surname)
		conditions = append(conditions, cond)
		args = append(args, a...)
	}
	if criteria.Gender != nil {
		conditions = append(conditions, "p.Sex = ?")
		args = append(args, *criteria.Gender)
	}
	if criteria.BirthDate != nil {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM EventTable e
			WHERE e.OwnerType = `+fmt.Sprint(OwnerTypePerson)+` AND e.OwnerID = p.PersonID AND e.EventType = 1
			AND e.SortDate BETWEEN ? AND ?)`)
		args = append(args, criteria.BirthDate.MinSortDate, criteria.BirthDate.MaxSortDate)
	}
	if criteria.BirthPlace != nil {
		cond, a := textCondition("pl.Name", criteria.BirthPlace)
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM EventTable e JOIN PlaceTable pl ON pl.PlaceID = e.PlaceID
			WHERE e.OwnerType = `+fmt.Sprint(OwnerTypePerson)+` AND e.OwnerID = p.PersonID AND e.EventType = 1
			AND `+cond+`)`)
		args = append(args, a...)
	}
	if criteria.DeathDate != nil {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM EventTable e
			WHERE e.OwnerType = `+fmt.Sprint(OwnerTypePerson)+` AND e.OwnerID = p.PersonID AND e.EventType = 2
			AND e.SortDate BETWEEN ? AND ?)`)
		args = append(args, criteria.DeathDate.MinSortDate, criteria.DeathDate.MaxSortDate)
	}
	if criteria.DeathPlace != nil {
		cond, a := textCondition("pl.Name", criteria.DeathPlace)
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM EventTable e JOIN PlaceTable pl ON pl.PlaceID = e.PlaceID
			WHERE e.OwnerType = `+fmt.Sprint(OwnerTypePerson)+` AND e.OwnerID = p.PersonID AND e.EventType = 2
			AND `+cond+`)`)
		args = append(args, a...)
	}
	if criteria.MarriageDate != nil {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM FamilyTable f JOIN EventTable e ON e.OwnerType = `+fmt.Sprint(OwnerTypeFamily)+` AND e.OwnerID = f.FamilyID AND e.EventType = 300
			WHERE (f.FatherID = p.PersonID OR f.MotherID = p.PersonID)
			AND e.SortDate BETWEEN ? AND ?)`)
		args = append(args, criteria.MarriageDate.MinSortDate, criteria.MarriageDate.MaxSortDate)
	}
	if criteria.MarriagePlace != nil {
		cond, a := textCondition("pl.Name", criteria.MarriagePlace)
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM FamilyTable f
			JOIN EventTable e ON e.OwnerType = `+fmt.Sprint(OwnerTypeFamily)+` AND e.OwnerID = f.FamilyID AND e.EventType = 300
			JOIN PlaceTable pl ON pl.PlaceID = e.PlaceID
			WHERE (f.FatherID = p.PersonID OR f.MotherID = p.PersonID)
			AND `+cond+`)`)
		args = append(args, a...)
	}

	// A fixed slice, not a map: Go's map iteration order is randomized,
	// and while each (cond, args) pair below is self-contained and
	// appended atomically -- so randomized order wouldn't actually
	// misalign any bind argument -- non-deterministic SQL text is still
	// worth avoiding on its own, for predictable logging and debugging.
	relationGroups := []struct {
		role string
		rc   *RelationCriteria
	}{
		{"father", criteria.Father},
		{"mother", criteria.Mother},
		{"spouse", criteria.Spouse},
		{"parent", criteria.Parent},
	}
	for _, g := range relationGroups {
		if g.rc == nil {
			continue
		}
		cond, a := relationCondition(g.role, g.rc)
		conditions = append(conditions, cond)
		args = append(args, a...)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	// LEFT JOIN, not JOIN: a person with no primary NameTable row at all
	// (an edge case this project has already had to handle for write
	// support -- see SCOPE.md's account of a real royal92.ged individual
	// with no usable name) should still be searchable by non-name
	// criteria, not silently excluded from every search.
	fromClause := `FROM PersonTable p LEFT JOIN NameTable n ON n.OwnerID = p.PersonID AND n.IsPrimary = 1 ` + where

	countQuery := "SELECT COUNT(DISTINCT p.PersonID) " + fromClause
	var total int
	if err := db.sql.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting search results: %w", err)
	}

	query := "SELECT DISTINCT p.PersonID, p.Sex, p.Living, p.ParentID, p.SpouseID " + fromClause + " ORDER BY p.PersonID LIMIT ? OFFSET ?"
	pagedArgs := append(append([]any{}, args...), limit, offset)
	rows, err := db.sql.Query(query, pagedArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("searching persons: %w", err)
	}
	defer rows.Close()

	var out []Person
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.PersonID, &p.Sex, &p.Living, &p.ParentID, &p.SpouseID); err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
