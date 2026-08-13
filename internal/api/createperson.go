package api

import (
	"fmt"
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

	if len(p.Names) == 0 {
		return rmdb.NewPerson{}, fmt.Errorf("a new person must have at least one name")
	}
	names := make([]rmdb.NewPersonName, 0, len(p.Names))
	for i, n := range p.Names {
		nn, err := s.buildNewPersonName(n)
		if err != nil {
			return rmdb.NewPerson{}, fmt.Errorf("names[%d]: %w", i, err)
		}
		names = append(names, nn)
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

// buildNewPersonName extracts Surname/Given/Prefix/Suffix/Nickname from
// a Name's structured NamePart list -- deliberately requires Parts to be
// present and does not attempt to split a bare FullText into parts
// itself: splitting a free-text name into surname/given is inherently
// ambiguous (which word is the surname?), and this project's standing
// principle is to reject an ambiguous write rather than guess at one
// (see e.g. EncodeRMDate's own comment for the same reasoning applied to
// dates).
func (s *Server) buildNewPersonName(n gedcomx.Name) (rmdb.NewPersonName, error) {
	if len(n.NameForms) == 0 || len(n.NameForms[0].Parts) == 0 {
		return rmdb.NewPersonName{}, fmt.Errorf("a name must have nameForms[0].parts (structured Given/Surname/...) -- this server doesn't split a bare fullText into parts itself, since that's inherently ambiguous")
	}
	nn := rmdb.NewPersonName{IsPrimary: n.Preferred != nil && *n.Preferred}
	for _, part := range n.NameForms[0].Parts {
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
	if f.Date != nil && f.Date.Formal != "" {
		dateString, y, m, d, err := gedcomx.EncodeRMDate(f.Date.Formal)
		if err != nil {
			return rmdb.NewPersonFact{}, fmt.Errorf("date: %w", err)
		}
		nf.DateString = dateString
		nf.SortYear, nf.SortMonth, nf.SortDay = y, m, d
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
