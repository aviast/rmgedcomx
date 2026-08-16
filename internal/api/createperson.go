package api

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/aviast/rmgedcomx/internal/gedcomx"
	"github.com/aviast/rmgedcomx/internal/rmdb"
)

// handleCreatePersons implements the Persons state's POST operation (RS
// spec Section 4.9.2: "Create a person or set of persons", OPTIONAL) --
// only registered when this collection's database is writable.
//
// Built from a real, systematically captured reference (a whole family
// entered into an empty RootsMagic database one step at a time,
// reviewed at every step) -- see SCOPE.md's "Stage 3" section for the
// full account, including the two RootsMagic quirks found and
// deliberately not replicated, and the exact scope of what's supported
// below versus what returns a clear rejection instead of a guess.
//
// Per RS spec Section 4.9.2: a request creating exactly one person gets
// `201` with a `Location` header pointing to it; a request creating more
// than one gets `204`.
func (s *Server) handleCreatePersons(w http.ResponseWriter, r *http.Request) {
	var body gedcomx.PersonsDocument
	if err := decodeStrictJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(body.Persons) == 0 {
		writeError(w, http.StatusBadRequest, "request body must include at least one person (RS spec Section 4.9.3)")
		return
	}

	newPersons := make([]rmdb.NewPerson, 0, len(body.Persons))
	for i, p := range body.Persons {
		np, err := s.buildNewPerson(p)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("persons[%d]: %v", i, err))
			return
		}
		newPersons = append(newPersons, np)
	}

	if s.ensureBackupForWrite(w) != nil {
		return
	}

	createdIDs := make([]int64, 0, len(newPersons))
	for i, np := range newPersons {
		personID, err := s.db.CreatePerson(np)
		if err != nil {
			// Some persons in this request may already have been
			// created (each CreatePerson call is its own transaction --
			// see CreatePerson's own comment for why: partial creation
			// across an all-or-nothing multi-person request is a real
			// risk worth naming, not silently glossed over).
			if len(createdIDs) > 0 {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf(
					"persons[%d] failed after %d earlier person(s) in this request were already created (%v) -- this request is not all-or-nothing across multiple persons; check the collection for partial results: %v",
					i, len(createdIDs), personRefs(createdIDs), err))
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		createdIDs = append(createdIDs, personID)
	}

	if len(createdIDs) == 1 {
		w.Header().Set("Location", s.url("/persons/"+personRef(createdIDs[0])))
		w.WriteHeader(http.StatusCreated)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// buildNewPerson translates a client-supplied gedcomx.Person into
// rmdb.NewPerson, resolving GEDCOM X type URIs to RootsMagic codes and
// rejecting anything this server doesn't have confirmed support for
// creating -- see this function's own per-field comments for exactly
// what's rejected and why.
func (s *Server) buildNewPerson(p gedcomx.Person) (rmdb.NewPerson, error) {
	sex := 2 // Unknown -- RootsMagic's own default when gender isn't specified.
	if p.Gender != nil {
		code, ok := gedcomx.GenderCode(p.Gender.Type)
		if !ok {
			return rmdb.NewPerson{}, fmt.Errorf("unrecognized gender type %q", p.Gender.Type)
		}
		sex = code
	}

	// Person.names is OPTIONAL in the GEDCOM X conceptual model (Section
	// 2.1) -- checked directly against the spec text, not assumed, after
	// a real report showed this server rejecting a person who
	// genuinely has none in the source data at all (a real royal92.ged
	// individual entered with only a title, sex, and death year -- no
	// name line with any usable content). RootsMagic's own real
	// behavior for exactly this case (confirmed directly against
	// royal92.rmtree, not assumed) is not "no NameTable row at all" --
	// it's one NameTable row with Given="" and Surname="", same as this
	// server's own fallback below for a NameForm with nothing useful in
	// it. Matched here rather than diverging from it: creating zero
	// NameTable rows for a person is unconfirmed behavior this server
	// has no evidence for, and RootsMagic's own UI/data model likely
	// expects every person to have at least one.
	names := make([]rmdb.NewPersonName, 0, len(p.Names))
	for i, n := range p.Names {
		nn, err := s.buildNewPersonName(n)
		if err != nil {
			return rmdb.NewPerson{}, fmt.Errorf("names[%d]: %w", i, err)
		}
		names = append(names, nn)
	}
	if len(names) == 0 {
		names = append(names, rmdb.NewPersonName{IsPrimary: true})
	} else {
		// Found while testing the exact request that motivated the
		// comment above: a client that never sets preferred explicitly
		// (as GEDCOM X's own spec permits -- "names are assumed to be
		// given in order of preference, with the most preferred name in
		// the first position in the list") got every name created with
		// IsPrimary=0, including the person's only name. Every real
		// royal92.rmtree row checked throughout this project has
		// IsPrimary=1 on the first/only name, with no exceptions --
		// this defaults the first name to primary unless something
		// later in the list was already explicitly marked preferred,
		// matching both the spec's own ordering convention and
		// RootsMagic's own consistently observed behavior, rather than
		// silently leaving every name non-primary just because the
		// client relied on ordering instead of the explicit flag.
		hasPrimary := false
		for _, nn := range names {
			if nn.IsPrimary {
				hasPrimary = true
				break
			}
		}
		if !hasPrimary {
			names[0].IsPrimary = true
		}
	}

	facts := make([]rmdb.NewPersonFact, 0, len(p.Facts))
	for i, f := range p.Facts {
		nf, err := s.buildNewPersonFact(f)
		if err != nil {
			return rmdb.NewPerson{}, fmt.Errorf("facts[%d]: %w", i, err)
		}
		facts = append(facts, nf)
	}

	return rmdb.NewPerson{Sex: sex, Names: names, Facts: facts}, nil
}

// buildNewPersonName extracts Surname/Given/Prefix/Suffix from a Name's
// structured NamePart list when present; when the list is empty (or
// absent), falls back to storing the whole nameForms[0].fullText in
// Given, leaving Surname empty -- confirmed to be RootsMagic's own real
// behavior, not a guess this server is making up: RootsMagic itself
// parses the GEDCOM 5.x "Given /Surname/" convention, and when the
// slash-delimited surname portion is empty (e.g. a real line from this
// project's own royal92.ged test file, "1 NAME Albert Augustus
// Charles//"), it stores the whole preceding text in NameTable.Given and
// leaves NameTable.Surname empty -- checked directly against the actual
// royal92.rmtree database (NameID 2: Given="Albert Augustus Charles",
// Surname=""), not just reasoned about, and confirmed consistently
// across several more of the same pattern in the same file (Victoria
// Adelaide Mary, Alice Maud Mary, ...).
//
// Both fullText and parts are independently OPTIONAL in the GEDCOM X
// conceptual model's own NameForm data type (Section 3.19) -- checked
// directly against the spec text, not assumed -- so a fullText-only
// NameForm (no parts at all) is a fully spec-compliant request this
// server previously rejected outright. That was a real, if reasonable at
// the time, overreach: splitting an arbitrary fullText into surname/given
// is still genuinely ambiguous in general (this server isn't attempting
// that), but RootsMagic's own confirmed "whole name in Given, empty
// Surname" convention for exactly this situation gives a deterministic,
// evidence-backed answer that doesn't require guessing at a split at
// all.
//
// A NameForm with neither parts nor fullText -- a person with no usable
// name at all -- is *also* accepted now, not rejected: another real
// royal92.ged individual (a title, sex, and death year, nothing else)
// produces exactly this shape, and RootsMagic's own confirmed handling
// (checked directly against the real royal92.rmtree row this individual
// produces: Given="", Surname="", still exactly one NameTable row, not
// zero) is the same fallback as the fullText-only case above -- Given
// ends up "" simply because form.FullText already is. Rejecting this
// was the same kind of overreach as the fullText-only case: correctly
// avoiding a guess, but for a shape the spec (and RootsMagic itself)
// already has a real, deterministic answer for.
func (s *Server) buildNewPersonName(n gedcomx.Name) (rmdb.NewPersonName, error) {
	if len(n.NameForms) == 0 {
		return rmdb.NewPersonName{}, fmt.Errorf("a name must have at least one nameForm (GEDCOM X conceptual model Section 3.13: nameForms is REQUIRED)")
	}
	form := n.NameForms[0]
	nn := rmdb.NewPersonName{IsPrimary: n.Preferred != nil && *n.Preferred}

	if len(form.Parts) == 0 {
		// No structured parts. form.FullText might still be a real
		// value ("Albert Augustus Charles") or might itself be empty --
		// both are handled by this one assignment, since RootsMagic's
		// own confirmed behavior is the same in both cases: whatever
		// text is available (including none) goes in Given, Surname
		// stays empty. See this function's own doc comment for why a
		// completely empty NameForm is no longer rejected either.
		nn.Given = form.FullText
	} else {
		for _, part := range form.Parts {
			switch part.Type {
			case "http://gedcomx.org/Prefix":
				nn.Prefix = part.Value
			case "http://gedcomx.org/Given":
				nn.Given = part.Value
			case "http://gedcomx.org/Surname":
				nn.Surname = part.Value
			case "http://gedcomx.org/Suffix":
				nn.Suffix = part.Value
			default:
				return rmdb.NewPersonName{}, fmt.Errorf("unrecognized name part type %q", part.Type)
			}
		}
	}

	nameType, ok := gedcomx.NameTypeCode(n.Type)
	if !ok {
		return rmdb.NewPersonName{}, fmt.Errorf("unrecognized name type %q", n.Type)
	}
	nn.NameType = nameType
	// Nickname is its own NamePart type in the GEDCOM X conceptual model
	// but has no confirmed real capture in this project's own reference
	// data -- RootsMagic's NicknameMP handling is only inferred from
	// SurnameMP/GivenMP's own confirmed transformation, never directly
	// captured. Left unset here deliberately rather than wired to a
	// NamePart type with no confirmed mapping.
	return nn, nil
}

// buildNewPersonFact resolves a Fact's Type URI to a RootsMagic
// FactTypeID (built-in fact types only -- see
// gedcomx.GedcomTagForFactType's own comment for why a custom fact-type
// URI is rejected rather than guessed at), and its Date to RootsMagic's
// encoded Date string and sort components via gedcomx.EncodeRMDate.
func (s *Server) buildNewPersonFact(f gedcomx.Fact) (rmdb.NewPersonFact, error) {
	tag, ok := gedcomx.GedcomTagForFactType(f.Type)
	if !ok {
		return rmdb.NewPersonFact{}, fmt.Errorf("unrecognized or unsupported fact type %q -- only built-in GEDCOM X fact types with a confirmed RootsMagic mapping can be created", f.Type)
	}
	factTypeID, ownerType, ok := s.factTypeIDForTag(tag)
	if !ok {
		return rmdb.NewPersonFact{}, fmt.Errorf("fact type %q (GEDCOM tag %s) has no matching entry in this database's own FactTypeTable", f.Type, tag)
	}
	if ownerType != rmdb.OwnerTypePerson {
		return rmdb.NewPersonFact{}, fmt.Errorf("fact type %q is not a Person-level fact in this database", f.Type)
	}

	nf := rmdb.NewPersonFact{FactTypeID: factTypeID, DateString: "."}
	if f.Date != nil {
		switch {
		case f.Date.Formal != "":
			dateString, y, m, d, err := gedcomx.EncodeRMDate(f.Date.Formal)
			if err != nil {
				return rmdb.NewPersonFact{}, fmt.Errorf("date: %w", err)
			}
			nf.DateString = dateString
			nf.SortYear, nf.SortMonth, nf.SortDay = y, m, d
		case f.Date.Original != "":
			// No Formal, but Original might still be usable: a real
			// client, converting a real GEDCOM file, was found sending
			// exactly this shape (a GEDCOM 5.x date string in Original,
			// no Formal at all) -- see ParseGedcom5Date's own comment
			// for the full account of what it does and doesn't cover.
			// A date that doesn't match is not a client mistake worth
			// rejecting the whole fact over -- it's logged (so there's
			// a visible trail of real dates this server still can't
			// interpret) and the fact is still created, just without a
			// machine-readable date, matching this server's own
			// existing "log, don't reject" precedent for other
			// recognized-but-unsupported request content (see
			// logIgnoredFields).
			if dateString, y, m, d, ok := gedcomx.ParseGedcom5Date(f.Date.Original); ok {
				nf.DateString = dateString
				nf.SortYear, nf.SortMonth, nf.SortDay = y, m, d
			} else {
				slog.Info("couldn't interpret date.original as a GEDCOM 5.x date -- fact created without a date", "original", f.Date.Original)
			}
		}
	}
	if f.Place != nil {
		if f.Place.Original == "" {
			return rmdb.NewPersonFact{}, fmt.Errorf("place must have original text -- this server doesn't resolve a place from resource alone when creating a fact")
		}
		nf.PlaceText = f.Place.Original
	}
	return nf, nil
}

// factTypeIDForTag finds the FactTypeID and OwnerType for a given
// GEDCOM tag among this collection's own cached FactTypeTable rows.
// Linear scan over a small (tens of rows), request-independent cache --
// simpler than maintaining a second reverse index alongside s.factTypes
// for what's a low-frequency operation (person/fact creation, not a hot
// read path).
func (s *Server) factTypeIDForTag(tag string) (factTypeID int64, ownerType int, ok bool) {
	for id, ft := range s.factTypes {
		if ft.GedcomTag == tag {
			return id, ft.OwnerType, true
		}
	}
	return 0, 0, false
}

func personRefs(ids []int64) []string {
	refs := make([]string, len(ids))
	for i, id := range ids {
		refs[i] = personRef(id)
	}
	return refs
}
