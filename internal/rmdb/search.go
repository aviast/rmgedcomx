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

// SearchCriteria holds the 10 "direct" GEDCOM X RS search parameters
// (RS spec Section 6): name, givenName, surname, gender, birthDate,
// birthPlace, deathDate, deathPlace, marriageDate, marriagePlace. The
// "{relation}"-prefixed parameters (father/mother/spouse/parent,
// applied to each of 8 of these) are a deliberately separate, later
// piece of work -- see SCOPE.md's own account of why.
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
}

// textCondition returns a SQL condition and its bind argument(s) for a
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
// (RS spec Section 4.11) for the 10 direct search parameters. Facts
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
