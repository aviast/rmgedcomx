package rmdb

import (
	"database/sql"
	"fmt"
)

// GetPerson fetches a single person by PersonID. Returns (nil, nil) if not found.
func (db *DB) GetPerson(id int64) (*Person, error) {
	row := db.sql.QueryRow(`SELECT PersonID, Sex, Living, ParentID, SpouseID FROM PersonTable WHERE PersonID = ?`, id)
	var p Person
	if err := row.Scan(&p.PersonID, &p.Sex, &p.Living, &p.ParentID, &p.SpouseID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying person %d: %w", id, err)
	}
	return &p, nil
}

// ListPersons returns a page of persons ordered by PersonID, optionally
// filtered by a case-insensitive substring match against the primary name
// (surname or given name), along with the total count of matching persons.
func (db *DB) ListPersons(limit, offset int, nameFilter string) ([]Person, int, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if nameFilter != "" {
		like := "%" + nameFilter + "%"
		query := `
			SELECT DISTINCT p.PersonID, p.Sex, p.Living, p.ParentID, p.SpouseID
			FROM PersonTable p
			JOIN NameTable n ON n.OwnerID = p.PersonID
			WHERE n.Surname LIKE ? OR n.Given LIKE ?
			ORDER BY p.PersonID
			LIMIT ? OFFSET ?`
		rows, err = db.sql.Query(query, like, like, limit, offset)
	} else {
		rows, err = db.sql.Query(`SELECT PersonID, Sex, Living, ParentID, SpouseID FROM PersonTable ORDER BY PersonID LIMIT ? OFFSET ?`, limit, offset)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("listing persons: %w", err)
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

	total, err := db.countPersons(nameFilter)
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (db *DB) countPersons(nameFilter string) (int, error) {
	var count int
	if nameFilter != "" {
		like := "%" + nameFilter + "%"
		query := `
			SELECT COUNT(DISTINCT p.PersonID)
			FROM PersonTable p
			JOIN NameTable n ON n.OwnerID = p.PersonID
			WHERE n.Surname LIKE ? OR n.Given LIKE ?`
		if err := db.sql.QueryRow(query, like, like).Scan(&count); err != nil {
			return 0, fmt.Errorf("counting persons: %w", err)
		}
		return count, nil
	}
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM PersonTable`).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting persons: %w", err)
	}
	return count, nil
}

// GetNames returns all NameTable rows for a person, primary name first, then
// by NameID.
func (db *DB) GetNames(personID int64) ([]Name, error) {
	rows, err := db.sql.Query(`
		SELECT NameID, OwnerID, Surname, Given, Prefix, Suffix, Nickname, NameType, IsPrimary, SortDate, BirthYear, DeathYear, Date
		FROM NameTable WHERE OwnerID = ? ORDER BY IsPrimary DESC, NameID`, personID)
	if err != nil {
		return nil, fmt.Errorf("querying names for person %d: %w", personID, err)
	}
	defer rows.Close()

	var out []Name
	for rows.Next() {
		var n Name
		if err := rows.Scan(&n.NameID, &n.OwnerID, &n.Surname, &n.Given, &n.Prefix, &n.Suffix, &n.Nickname, &n.NameType, &n.IsPrimary, &n.SortDate, &n.BirthYear, &n.DeathYear, &n.Date); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// PrimaryName returns the primary NameTable row for a person, or nil if the
// person has no names.
func (db *DB) PrimaryName(personID int64) (*Name, error) {
	names, err := db.GetNames(personID)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	return &names[0], nil
}

// GetEvents returns EventTable rows for a given owner (a person or a
// family), ordered by SortDate.
func (db *DB) GetEvents(ownerType int, ownerID int64) ([]Event, error) {
	rows, err := db.sql.Query(`
		SELECT EventID, EventType, OwnerType, OwnerID, FamilyID, PlaceID, Date, IsPrimary, Details, Note
		FROM EventTable WHERE OwnerType = ? AND OwnerID = ? ORDER BY SortDate`, ownerType, ownerID)
	if err != nil {
		return nil, fmt.Errorf("querying events for owner (%d,%d): %w", ownerType, ownerID, err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.EventID, &e.EventType, &e.OwnerType, &e.OwnerID, &e.FamilyID, &e.PlaceID, &e.Date, &e.IsPrimary, &e.Details, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AllFactTypes loads the entire FactTypeTable (small: typically well under a
// few hundred rows) into memory once, keyed by FactTypeID.
func (db *DB) AllFactTypes() (map[int64]FactType, error) {
	rows, err := db.sql.Query(`SELECT FactTypeID, Name, GedcomTag, OwnerType FROM FactTypeTable`)
	if err != nil {
		return nil, fmt.Errorf("querying fact types: %w", err)
	}
	defer rows.Close()

	out := map[int64]FactType{}
	for rows.Next() {
		var ft FactType
		if err := rows.Scan(&ft.FactTypeID, &ft.Name, &ft.GedcomTag, &ft.OwnerType); err != nil {
			return nil, err
		}
		out[ft.FactTypeID] = ft
	}
	return out, rows.Err()
}

// GetFamily fetches a single family by FamilyID. Returns (nil, nil) if not found.
func (db *DB) GetFamily(id int64) (*Family, error) {
	row := db.sql.QueryRow(`SELECT FamilyID, FatherID, MotherID FROM FamilyTable WHERE FamilyID = ?`, id)
	var f Family
	if err := row.Scan(&f.FamilyID, &f.FatherID, &f.MotherID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying family %d: %w", id, err)
	}
	return &f, nil
}

// ListFamilies returns a page of families ordered by FamilyID, along with
// the total count.
func (db *DB) ListFamilies(limit, offset int) ([]Family, int, error) {
	rows, err := db.sql.Query(`SELECT FamilyID, FatherID, MotherID FROM FamilyTable ORDER BY FamilyID LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing families: %w", err)
	}
	defer rows.Close()

	var out []Family
	for rows.Next() {
		var f Family
		if err := rows.Scan(&f.FamilyID, &f.FatherID, &f.MotherID); err != nil {
			return nil, 0, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM FamilyTable`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting families: %w", err)
	}
	return out, total, nil
}

// FamiliesAsParent returns all families in which the given person is the
// father or the mother.
func (db *DB) FamiliesAsParent(personID int64) ([]Family, error) {
	rows, err := db.sql.Query(`SELECT FamilyID, FatherID, MotherID FROM FamilyTable WHERE FatherID = ? OR MotherID = ? ORDER BY FamilyID`, personID, personID)
	if err != nil {
		return nil, fmt.Errorf("querying families as parent for person %d: %w", personID, err)
	}
	defer rows.Close()

	var out []Family
	for rows.Next() {
		var f Family
		if err := rows.Scan(&f.FamilyID, &f.FatherID, &f.MotherID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ChildRowsAsChild returns all ChildTable rows where the given person is the
// child (i.e. the families in which this person appears as a child).
func (db *DB) ChildRowsAsChild(personID int64) ([]Child, error) {
	rows, err := db.sql.Query(`
		SELECT RecID, ChildID, FamilyID, RelFather, RelMother, ChildOrder
		FROM ChildTable WHERE ChildID = ? ORDER BY FamilyID`, personID)
	if err != nil {
		return nil, fmt.Errorf("querying child rows for person %d: %w", personID, err)
	}
	defer rows.Close()

	var out []Child
	for rows.Next() {
		var c Child
		if err := rows.Scan(&c.RecID, &c.ChildID, &c.FamilyID, &c.RelFather, &c.RelMother, &c.ChildOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChildRowsOfFamily returns all ChildTable rows for a given family, in
// display order (ChildOrder, falling back to RecID).
func (db *DB) ChildRowsOfFamily(familyID int64) ([]Child, error) {
	rows, err := db.sql.Query(`
		SELECT RecID, ChildID, FamilyID, RelFather, RelMother, ChildOrder
		FROM ChildTable WHERE FamilyID = ? ORDER BY ChildOrder, RecID`, familyID)
	if err != nil {
		return nil, fmt.Errorf("querying children of family %d: %w", familyID, err)
	}
	defer rows.Close()

	var out []Child
	for rows.Next() {
		var c Child
		if err := rows.Scan(&c.RecID, &c.ChildID, &c.FamilyID, &c.RelFather, &c.RelMother, &c.ChildOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetPlace fetches a single place by PlaceID. Returns (nil, nil) if not found.
func (db *DB) GetPlace(id int64) (*Place, error) {
	row := db.sql.QueryRow(`SELECT PlaceID, PlaceType, Name, Latitude, Longitude, Note FROM PlaceTable WHERE PlaceID = ?`, id)
	var p Place
	if err := row.Scan(&p.PlaceID, &p.PlaceType, &p.Name, &p.Latitude, &p.Longitude, &p.Note); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying place %d: %w", id, err)
	}
	return &p, nil
}

// ListPlaces returns a page of places ordered by name, along with the total count.
func (db *DB) ListPlaces(limit, offset int) ([]Place, int, error) {
	rows, err := db.sql.Query(`
		SELECT PlaceID, PlaceType, Name, Latitude, Longitude, Note
		FROM PlaceTable ORDER BY Name, PlaceID LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing places: %w", err)
	}
	defer rows.Close()

	var out []Place
	for rows.Next() {
		var p Place
		if err := rows.Scan(&p.PlaceID, &p.PlaceType, &p.Name, &p.Latitude, &p.Longitude, &p.Note); err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM PlaceTable`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting places: %w", err)
	}
	return out, total, nil
}

// GetSource fetches a single source by SourceID. Returns (nil, nil) if not found.
func (db *DB) GetSource(id int64) (*Source, error) {
	row := db.sql.QueryRow(`SELECT SourceID, Name, RefNumber, ActualText, Comments FROM SourceTable WHERE SourceID = ?`, id)
	var s Source
	if err := row.Scan(&s.SourceID, &s.Name, &s.RefNumber, &s.ActualText, &s.Comments); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying source %d: %w", id, err)
	}
	return &s, nil
}

// ListSources returns a page of sources ordered by name, along with the total count.
func (db *DB) ListSources(limit, offset int) ([]Source, int, error) {
	rows, err := db.sql.Query(`
		SELECT SourceID, Name, RefNumber, ActualText, Comments
		FROM SourceTable ORDER BY Name, SourceID LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing sources: %w", err)
	}
	defer rows.Close()

	var out []Source
	for rows.Next() {
		var s Source
		if err := rows.Scan(&s.SourceID, &s.Name, &s.RefNumber, &s.ActualText, &s.Comments); err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM SourceTable`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting sources: %w", err)
	}
	return out, total, nil
}

// SourceIDsForOwner resolves which sources (SourceTable.SourceID) support a
// given owner (a person, family, event, or name), via CitationLinkTable and
// CitationTable.
func (db *DB) SourceIDsForOwner(ownerType int, ownerID int64) ([]int64, error) {
	rows, err := db.sql.Query(`
		SELECT DISTINCT c.SourceID
		FROM CitationLinkTable cl
		JOIN CitationTable c ON c.CitationID = cl.CitationID
		WHERE cl.OwnerType = ? AND cl.OwnerID = ?
		ORDER BY c.SourceID`, ownerType, ownerID)
	if err != nil {
		return nil, fmt.Errorf("querying source citations for owner (%d,%d): %w", ownerType, ownerID, err)
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CollectionStats holds counts of the resource types this server exposes,
// for the Collection resource's `content` summary.
type CollectionStats struct {
	Persons       int
	Relationships int
	Places        int
	Sources       int
}

// CollectionStats computes cheap COUNT(*)-based totals for each resource
// type this server exposes. The relationship count mirrors exactly how
// handleRelationships builds the actual list (one Couple relationship per
// family with both parents, plus one ParentChild relationship per
// parent-child pair), so it stays consistent with what GET /relationships
// actually returns.
func (db *DB) CollectionStats() (CollectionStats, error) {
	var s CollectionStats
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM PersonTable`).Scan(&s.Persons); err != nil {
		return s, fmt.Errorf("counting persons: %w", err)
	}
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM PlaceTable`).Scan(&s.Places); err != nil {
		return s, fmt.Errorf("counting places: %w", err)
	}
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM SourceTable`).Scan(&s.Sources); err != nil {
		return s, fmt.Errorf("counting sources: %w", err)
	}

	var couples, fatherChild, motherChild int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM FamilyTable WHERE FatherID != 0 AND MotherID != 0`).Scan(&couples); err != nil {
		return s, fmt.Errorf("counting couple relationships: %w", err)
	}
	if err := db.sql.QueryRow(`
		SELECT COUNT(*) FROM ChildTable c
		JOIN FamilyTable f ON f.FamilyID = c.FamilyID
		WHERE f.FatherID != 0`).Scan(&fatherChild); err != nil {
		return s, fmt.Errorf("counting father-child relationships: %w", err)
	}
	if err := db.sql.QueryRow(`
		SELECT COUNT(*) FROM ChildTable c
		JOIN FamilyTable f ON f.FamilyID = c.FamilyID
		WHERE f.MotherID != 0`).Scan(&motherChild); err != nil {
		return s, fmt.Errorf("counting mother-child relationships: %w", err)
	}
	s.Relationships = couples + fatherChild + motherChild
	return s, nil
}
