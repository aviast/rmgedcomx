package rmdb

import (
	"database/sql"
	"fmt"
)

// NewPersonName is one name to create for a new Person, via CreatePerson.
type NewPersonName struct {
	Surname, Given, Prefix, Suffix, Nickname string
	NameType                                 int
	IsPrimary                                bool
}

// NewPersonFact is one fact (EventTable row) to create for a new Person,
// via CreatePerson. DateString and the sort components are expected to
// already be encoded (gedcomx.EncodeRMDate for DateString,
// gedcomx.EncodeRMDate's own year/month/day output for the sort
// components, fed to ComputeSortDate) -- CreatePerson itself does no date
// parsing, keeping the GEDCOM X <-> RootsMagic translation entirely at
// the API layer, consistent with every other write handler in this
// project.
type NewPersonFact struct {
	FactTypeID                   int64
	DateString                   string // "." if no date at all
	SortYear, SortMonth, SortDay int    // ignored when DateString == "."
	PlaceText                    string // original place text; "" if no place
}

// NewPerson is the input to CreatePerson.
type NewPerson struct {
	// Sex: 0=Male, 1=Female, 2=Unknown -- matches PersonTable.Sex and
	// gedcomx.GenderTypeURI's own encoding directly, so the API layer
	// can pass through what it already computed for the read path
	// without a second mapping table.
	Sex   int
	Names []NewPersonName
	Facts []NewPersonFact
}

// CreatePerson inserts a new Person -- PersonTable, one or more NameTable
// rows, zero or more EventTable rows (one per fact), and zero or more new
// PlaceTable rows (one per fact whose place text doesn't already match an
// existing place) -- as a single transaction, and bumps ConfigTable's own
// UTCModDate the same way every other write in this project does.
//
// Returns the newly assigned PersonID.
//
// Every ID (PersonID, NameID, EventID, and any new PlaceID) is assigned
// as one past the current maximum in its table -- confirmed against real
// captured RootsMagic writes for this exact scenario (see SCOPE.md's
// "Stage 3" section): empty.rmtree's own pre-seeded PlaceTable (205 rows
// RootsMagic itself ships with every new database) correctly got its
// first two new places assigned PlaceID 206 and 207 in the real capture,
// not IDs starting from 1.
//
// UTCModDate is set, at full precision, on every row this function
// itself writes -- and only those rows. A real captured diff showed
// RootsMagic's own behavior here is inconsistent in ways not worth
// replicating (e.g. touching unrelated pre-existing events/places when
// adding an alias) -- see SCOPE.md's "Stage 3" section for the full
// account and why this was a deliberate choice, not an oversight.
func (db *DB) CreatePerson(input NewPerson) (personID int64, err error) {
	if len(input.Names) == 0 {
		return 0, fmt.Errorf("a new person must have at least one name")
	}

	tx, err := db.sql.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Resolve or create a place for each fact that names one, before
	// anything else -- so a failure here (unlikely, but possible on a
	// genuine SQL error) leaves nothing else half-written.
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

	personID, err = nextID(tx, "PersonTable", "PersonID")
	if err != nil {
		return 0, fmt.Errorf("assigning new PersonID: %w", err)
	}

	var birthYear, deathYear int
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
			eventID, f.FactTypeID, OwnerTypePerson, personID, placeIDs[i], f.DateString, sortDate,
		)
		if err != nil {
			return 0, fmt.Errorf("creating event: %w", err)
		}
		// BirthYear/DeathYear (NameTable, primary name only -- see
		// below) are derived specifically from Birth(1)/Death(2), the
		// two built-in fact types confirmed to populate them; any other
		// fact type in the same request doesn't touch these.
		switch f.FactTypeID {
		case 1:
			birthYear = f.SortYear
		case 2:
			deathYear = f.SortYear
		}
	}

	for _, n := range input.Names {
		nameID, err := nextID(tx, "NameTable", "NameID")
		if err != nil {
			return 0, fmt.Errorf("assigning new NameID: %w", err)
		}
		isPrimary := 0
		nameBirthYear, nameDeathYear := 0, 0
		if n.IsPrimary {
			isPrimary = 1
			// Confirmed against a real captured diff: BirthYear/DeathYear
			// are set on the primary name only -- adding an alias (a
			// non-primary name) to an existing person left them at 0 on
			// that alias's own NameTable row, even though the person's
			// primary name already carried the real birth/death years.
			nameBirthYear, nameDeathYear = birthYear, deathYear
		}
		_, err = tx.Exec(
			`INSERT INTO NameTable
			 (NameID, OwnerID, Surname, Given, Prefix, Suffix, Nickname, NameType, Date, SortDate,
			  IsPrimary, IsPrivate, Proof, Sentence, Note, BirthYear, DeathYear, Display, Language,
			  UTCModDate, SurnameMP, GivenMP, NicknameMP)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, '.', ?, ?, 0, 0, '', '', ?, ?, 0, '', `+utcModDateExpr+`, ?, ?, ?)`,
			nameID, personID, n.Surname, n.Given, n.Prefix, n.Suffix, n.Nickname, n.NameType,
			NoDateSortValue, isPrimary, nameBirthYear, nameDeathYear,
			FoldAccents(n.Surname), FoldAccents(n.Given), FoldAccents(n.Nickname),
		)
		if err != nil {
			return 0, fmt.Errorf("creating name: %w", err)
		}
	}

	uid, err := GenerateUniqueID()
	if err != nil {
		return 0, fmt.Errorf("generating UniqueID: %w", err)
	}
	_, err = tx.Exec(
		`INSERT INTO PersonTable
		 (PersonID, UniqueID, Sex, ParentID, SpouseID, Color, Color1, Color2, Color3, Color4,
		  Color5, Color6, Color7, Color8, Color9, Relate1, Relate2, Flags, Living, IsPrivate,
		  Proof, Bookmark, Note, UTCModDate)
		 VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, '', `+utcModDateExpr+`)`,
		personID, uid, input.Sex,
	)
	if err != nil {
		return 0, fmt.Errorf("creating person: %w", err)
	}

	if err := bumpConfigTableModDate(tx); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing new person: %w", err)
	}
	return personID, nil
}

// nextID returns one past the current maximum value of idColumn in
// table, within the given transaction -- RootsMagic's own apparent ID
// assignment scheme (confirmed against real captured writes; see
// CreatePerson's own comment), rather than relying on SQLite's implicit
// rowid autoincrement, so this server's own behavior is deliberate and
// verifiable rather than incidental to how the column happens to be
// declared.
func nextID(tx *sql.Tx, table, idColumn string) (int64, error) {
	var maxID sql.NullInt64
	// table/idColumn come only from this file's own call sites (never
	// user input), so this is safe despite not being a bound parameter.
	err := tx.QueryRow(fmt.Sprintf("SELECT MAX(%s) FROM %s", idColumn, table)).Scan(&maxID)
	if err != nil {
		return 0, fmt.Errorf("finding next %s.%s: %w", table, idColumn, err)
	}
	if !maxID.Valid {
		return 1, nil
	}
	return maxID.Int64 + 1, nil
}

// resolveOrCreatePlace returns the PlaceID of an existing place matching
// name exactly (RMNOCASE, PlaceTable.Name's own declared collation, so
// this needs no explicit COLLATE clause), or creates a new one if none
// matches -- confirmed against real captured data that reusing a place
// name (e.g. "Howarth", referenced by several different events in the
// Brontë test data) does not create a duplicate row.
func resolveOrCreatePlace(tx *sql.Tx, name string) (int64, error) {
	var existing int64
	err := tx.QueryRow("SELECT PlaceID FROM PlaceTable WHERE Name = ?", name).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("looking up existing place: %w", err)
	}

	placeID, err := nextID(tx, "PlaceTable", "PlaceID")
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(
		`INSERT INTO PlaceTable
		 (PlaceID, PlaceType, Name, Abbrev, Normalized, Latitude, Longitude, LatLongExact,
		  MasterID, Note, Reverse, fsID, anID, UTCModDate)
		 VALUES (?, 0, ?, '', '', 0, 0, 0, 0, '', ?, 0, 0, `+utcModDateExpr+`)`,
		placeID, name, ComputePlaceReverse(name),
	)
	if err != nil {
		return 0, fmt.Errorf("creating place: %w", err)
	}
	return placeID, nil
}
