package api

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aviast/rmgedcomx/internal/gedcomx"
	"github.com/aviast/rmgedcomx/internal/rmdb"
)

// --- Persons ---

func (s *Server) handlePersons(w http.ResponseWriter, r *http.Request) {
	limit, offset := s.pagingParams(r)
	nameFilter := r.URL.Query().Get("name")

	rows, total, err := s.db.ListPersons(limit, offset, nameFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	persons := make([]gedcomx.Person, 0, len(rows))
	for _, rp := range rows {
		p, err := s.buildPerson(rp)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		persons = append(persons, p)
	}

	status := http.StatusOK
	if len(persons) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.PersonsDocument{
		Results: len(persons),
		Persons: persons,
		Links:   pagingLinks(s, "/persons", limit, offset, total),
	})
}

func (s *Server) handlePerson(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := parsePersonID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rp, err := s.db.GetPerson(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rp == nil {
		notFound(w, "person", id)
		return
	}
	p, err := s.buildPerson(*rp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// RS spec Section 4.10.5, "Embedded States": child-relationships,
	// parent-relationships, and spouse-relationships are each MUST --
	// either a link, or the data embedded directly in this same
	// response. This server embeds, reusing the identical computation
	// GET .../parents, .../children, and .../spouses already needed for
	// their own Relationships fields (see personParentRelationships's
	// own comment).
	rels := []gedcomx.Relationship{}
	parentRels, err := s.personParentRelationships(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rels = append(rels, parentRels...)
	childRels, err := s.personChildRelationships(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rels = append(rels, childRels...)
	spouseRels, err := s.personSpouseRelationships(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rels = append(rels, spouseRels...)

	writeJSON(w, http.StatusOK, gedcomx.PersonDocument{Persons: []gedcomx.Person{p}, Relationships: rels, Links: p.Links})
}

// handleUpdatePerson implements the Person state's POST operation (RS
// spec Section 4.4.2: "Update a person", OPTIONAL) -- only registered
// when this collection's database is writable (see resourceHandler).
//
// Deliberately scoped to ONLY media links for now, not general person
// editing -- see SCOPE.md's "Write support" section. names/gender/facts/
// sources aren't writable yet; a request that includes any of them isn't
// rejected (a client following the ordinary GET-then-modify-then-POST
// pattern will naturally send them back unchanged, and breaking that
// pattern over fields this endpoint doesn't touch would be its own kind
// of unhelpful), but is logged, so there's a visible trail of real
// demand for expanding this later rather than a silent gap.
func (s *Server) handleUpdatePerson(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := parsePersonID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rp, err := s.db.GetPerson(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rp == nil {
		notFound(w, "person", id)
		return
	}

	var body gedcomx.PersonDocument
	if err := decodeStrictJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(body.Persons) == 0 {
		writeError(w, http.StatusBadRequest, "request body must include at least one person (RS spec Section 4.4.3)")
		return
	}
	person := body.Persons[0]
	if person.ID != "" && person.ID != id {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("body id %q doesn't match URL id %q", person.ID, id))
		return
	}

	var ignoredFields []string
	if len(person.Names) > 0 {
		ignoredFields = append(ignoredFields, "names")
	}
	if person.Gender != nil {
		ignoredFields = append(ignoredFields, "gender")
	}
	if len(person.Facts) > 0 {
		ignoredFields = append(ignoredFields, "facts")
	}
	if len(person.Sources) > 0 {
		ignoredFields = append(ignoredFields, "sources")
	}
	if len(ignoredFields) > 0 {
		logIgnoredFields("person", "/persons", id, ignoredFields)
	}

	mediaIDs := make([]int64, 0, len(person.Media))
	for _, ref := range person.Media {
		mid, err := mediaIDFromReference(ref)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid media reference: %v", err))
			return
		}
		mediaIDs = append(mediaIDs, mid)
	}

	if s.ensureBackupForWrite(w) != nil {
		return
	}
	if err := s.db.UpdateOwnerMedia(rmdb.OwnerTypePerson, pid, mediaIDs); err != nil {
		if errors.Is(err, rmdb.ErrArtifactNotFound) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mediaIDFromReference extracts an artifact id from a SourceReference
// sent in a write request -- prefers descriptionId (e.g. "M1") since it's
// simplest and most direct; falls back to parsing the id off the end of
// description (e.g. ".../artifacts/M1") for a client that only sent that.
// logIgnoredFields records, at Info level, that a write request included
// one or more recognized-but-unsupported fields -- see each call site's
// own surrounding comment for why these are silently accepted rather
// than rejected (the ordinary GET-then-modify-then-POST client pattern),
// but still worth a visible trail: this is the signal that would tell a
// future maintainer there's real demand for expanding what's writable.
// Shared by the Person/Relationship/Event write handlers so the message
// shape stays consistent across all three rather than drifting.
func logIgnoredFields(resourceType, path, id string, fields []string) {
	if len(fields) == 0 {
		return
	}
	slog.Info("ignoring unsupported field(s) on write -- see SCOPE.md's \"Write support\" section",
		"resource", resourceType, "path", path, "id", id, "fields", fields)
}

func mediaIDFromReference(ref gedcomx.SourceReference) (int64, error) {
	if ref.DescriptionID != "" {
		return parseMediaID(ref.DescriptionID)
	}
	if ref.Description != "" {
		parts := strings.Split(strings.TrimRight(ref.Description, "/"), "/")
		return parseMediaID(parts[len(parts)-1])
	}
	return 0, fmt.Errorf("media reference has neither descriptionId nor description")
}

// --- Person Parents / Children / Spouses ---

// personParentRelationships returns the ParentChild relationships where
// pid is the CHILD -- the same computation handlePersonParents already
// needed for its own Relationships field, extracted so handlePerson can
// reuse it (RS spec Section 4.10.5: the Person state's
// "parent-relationships" embedded state is MUST -- either a link, or
// the data embedded directly in the Person state's own response; this
// server does the latter, in PersonDocument.Relationships).
func (s *Server) personParentRelationships(pid int64) ([]gedcomx.Relationship, error) {
	childRows, err := s.db.ChildRowsAsChild(pid)
	if err != nil {
		return nil, err
	}
	var rels []gedcomx.Relationship
	for _, cr := range childRows {
		fam, err := s.db.GetFamily(cr.FamilyID)
		if err != nil {
			return nil, err
		}
		if fam == nil {
			continue
		}
		if fam.FatherID != 0 {
			rels = append(rels, s.buildParentChildRelationship(fam.FamilyID, fam.FatherID, pid, true))
		}
		if fam.MotherID != 0 {
			rels = append(rels, s.buildParentChildRelationship(fam.FamilyID, fam.MotherID, pid, false))
		}
	}
	return rels, nil
}

func (s *Server) handlePersonParents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := parsePersonID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	childRows, err := s.db.ChildRowsAsChild(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var persons []gedcomx.Person
	seen := map[int64]bool{}
	for _, cr := range childRows {
		fam, err := s.db.GetFamily(cr.FamilyID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if fam == nil {
			continue
		}
		for _, parentID := range []int64{fam.FatherID, fam.MotherID} {
			if parentID == 0 || seen[parentID] {
				continue
			}
			seen[parentID] = true
			rp, err := s.db.GetPerson(parentID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if rp == nil {
				continue
			}
			p, err := s.buildPerson(*rp)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			persons = append(persons, p)
		}
	}
	rels, err := s.personParentRelationships(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := http.StatusOK
	if len(persons) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.PersonRelativesDocument{
		Results:       len(persons),
		Persons:       persons,
		Relationships: rels,
		Links:         gedcomx.Links{"person": {Href: s.url("/persons/" + id)}},
	})
}

// personChildRelationships returns the ParentChild relationships where
// pid is a PARENT -- the same computation handlePersonChildren already
// needed for its own Relationships field, extracted so handlePerson can
// reuse it. See personParentRelationships's own comment for the full
// account of why this exists.
func (s *Server) personChildRelationships(pid int64) ([]gedcomx.Relationship, error) {
	families, err := s.db.FamiliesAsParent(pid)
	if err != nil {
		return nil, err
	}
	var rels []gedcomx.Relationship
	for _, fam := range families {
		isFather := fam.FatherID == pid
		children, err := s.db.ChildRowsOfFamily(fam.FamilyID)
		if err != nil {
			return nil, err
		}
		for _, c := range children {
			rels = append(rels, s.buildParentChildRelationship(fam.FamilyID, pid, c.ChildID, isFather))
		}
	}
	return rels, nil
}

func (s *Server) handlePersonChildren(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := parsePersonID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	families, err := s.db.FamiliesAsParent(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var persons []gedcomx.Person
	for _, fam := range families {
		children, err := s.db.ChildRowsOfFamily(fam.FamilyID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, c := range children {
			rp, err := s.db.GetPerson(c.ChildID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if rp == nil {
				continue
			}
			p, err := s.buildPerson(*rp)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			persons = append(persons, p)
		}
	}
	rels, err := s.personChildRelationships(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := http.StatusOK
	if len(persons) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.PersonRelativesDocument{
		Results:       len(persons),
		Persons:       persons,
		Relationships: rels,
		Links:         gedcomx.Links{"person": {Href: s.url("/persons/" + id)}},
	})
}

// personSpouseRelationships returns the Couple relationships where pid
// is one of the two parents -- the same computation handlePersonSpouses
// already needed for its own Relationships field, extracted so
// handlePerson can reuse it. See personParentRelationships's own
// comment for the full account of why this exists. Matches
// handlePersonSpouses's own existing behavior exactly, including
// skipping a family whose spouse PersonID doesn't actually resolve to a
// real person (a dangling reference, unlikely but not impossible) --
// consistency with what GET .../spouses itself would return matters
// more here than the extra GetPerson call it costs.
func (s *Server) personSpouseRelationships(pid int64) ([]gedcomx.Relationship, error) {
	families, err := s.db.FamiliesAsParent(pid)
	if err != nil {
		return nil, err
	}
	var rels []gedcomx.Relationship
	for _, fam := range families {
		if fam.FatherID == 0 || fam.MotherID == 0 {
			continue
		}
		spouseID := fam.MotherID
		if fam.FatherID != pid {
			spouseID = fam.FatherID
		}
		rp, err := s.db.GetPerson(spouseID)
		if err != nil {
			return nil, err
		}
		if rp == nil {
			continue
		}
		rel, err := s.buildCoupleRelationship(fam)
		if err != nil {
			return nil, err
		}
		rels = append(rels, rel)
	}
	return rels, nil
}

func (s *Server) handlePersonSpouses(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := parsePersonID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	families, err := s.db.FamiliesAsParent(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var persons []gedcomx.Person
	for _, fam := range families {
		spouseID := fam.MotherID
		if fam.FatherID != pid {
			spouseID = fam.FatherID
		}
		if spouseID == 0 {
			continue
		}
		rp, err := s.db.GetPerson(spouseID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if rp == nil {
			continue
		}
		p, err := s.buildPerson(*rp)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		persons = append(persons, p)
	}
	rels, err := s.personSpouseRelationships(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := http.StatusOK
	if len(persons) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.PersonRelativesDocument{
		Results:       len(persons),
		Persons:       persons,
		Relationships: rels,
		Links:         gedcomx.Links{"person": {Href: s.url("/persons/" + id)}},
	})
}

// --- Ancestry / Descendancy ---

type ancestryNode struct {
	personID int64
	number   int64 // Ahnentafel number
}

func (s *Server) handleAncestry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := parsePersonID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	generations := s.cfg.DefaultGenerations
	if v := r.URL.Query().Get("generations"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			generations = n
		}
	}

	root, err := s.db.GetPerson(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if root == nil {
		notFound(w, "person", id)
		return
	}

	var persons []gedcomx.Person
	frontier := []ancestryNode{{personID: pid, number: 1}}
	for gen := 1; gen <= generations && len(frontier) > 0; gen++ {
		var next []ancestryNode
		for _, node := range frontier {
			rp, err := s.db.GetPerson(node.personID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if rp == nil {
				continue
			}
			p, err := s.buildPerson(*rp)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if p.Display == nil {
				p.Display = &gedcomx.DisplayProperties{}
			}
			p.Display.AscendancyNumber = strconv.FormatInt(node.number, 10)
			persons = append(persons, p)

			if gen == generations {
				continue
			}
			childRows, err := s.db.ChildRowsAsChild(node.personID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if len(childRows) == 0 {
				continue
			}
			fam, err := s.db.GetFamily(childRows[0].FamilyID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if fam == nil {
				continue
			}
			if fam.FatherID != 0 {
				next = append(next, ancestryNode{personID: fam.FatherID, number: node.number * 2})
			}
			if fam.MotherID != 0 {
				next = append(next, ancestryNode{personID: fam.MotherID, number: node.number*2 + 1})
			}
		}
		frontier = next
	}

	writeJSON(w, http.StatusOK, gedcomx.AncestryResultsDocument{
		Results: len(persons),
		Persons: persons,
		Links:   gedcomx.Links{"person": {Href: s.url("/persons/" + id)}},
	})
}

func (s *Server) handleDescendancy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := parsePersonID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	generations := s.cfg.DefaultGenerations
	if v := r.URL.Query().Get("generations"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			generations = n
		}
	}

	root, err := s.db.GetPerson(pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if root == nil {
		notFound(w, "person", id)
		return
	}

	var persons []gedcomx.Person
	var walk func(personID int64, number string, depth int) error
	walk = func(personID int64, number string, depth int) error {
		rp, err := s.db.GetPerson(personID)
		if err != nil {
			return err
		}
		if rp == nil {
			return nil
		}
		p, err := s.buildPerson(*rp)
		if err != nil {
			return err
		}
		if p.Display == nil {
			p.Display = &gedcomx.DisplayProperties{}
		}
		p.Display.DescendancyNumber = number
		persons = append(persons, p)

		if depth >= generations {
			return nil
		}
		families, err := s.db.FamiliesAsParent(personID)
		if err != nil {
			return err
		}
		childIndex := 0
		for _, fam := range families {
			children, err := s.db.ChildRowsOfFamily(fam.FamilyID)
			if err != nil {
				return err
			}
			for _, c := range children {
				childIndex++
				childNumber := number + "." + strconv.Itoa(childIndex)
				if err := walk(c.ChildID, childNumber, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(pid, "1", 1); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, gedcomx.DescendancyResultsDocument{
		Results: len(persons),
		Persons: persons,
		Links:   gedcomx.Links{"person": {Href: s.url("/persons/" + id)}},
	})
}

// --- Relationships ---

func (s *Server) handleRelationships(w http.ResponseWriter, r *http.Request) {
	limit, offset := s.pagingParams(r)
	families, total, err := s.db.ListFamilies(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var rels []gedcomx.Relationship
	for _, fam := range families {
		if fam.FatherID != 0 && fam.MotherID != 0 {
			rel, err := s.buildCoupleRelationship(fam)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			rels = append(rels, rel)
		}
		children, err := s.db.ChildRowsOfFamily(fam.FamilyID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, c := range children {
			if fam.FatherID != 0 {
				rels = append(rels, s.buildParentChildRelationship(fam.FamilyID, fam.FatherID, c.ChildID, true))
			}
			if fam.MotherID != 0 {
				rels = append(rels, s.buildParentChildRelationship(fam.FamilyID, fam.MotherID, c.ChildID, false))
			}
		}
	}

	status := http.StatusOK
	if len(rels) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.RelationshipsDocument{
		Results:       len(rels),
		Relationships: rels,
		Links:         pagingLinks(s, "/relationships", limit, offset, total),
	})
}

func (s *Server) handleRelationship(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	parsed, err := parseRelationshipID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fam, err := s.db.GetFamily(parsed.FamilyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fam == nil {
		notFound(w, "relationship", id)
		return
	}

	if parsed.Kind == "couple" {
		if fam.FatherID == 0 || fam.MotherID == 0 {
			notFound(w, "relationship", id)
			return
		}
		rel, err := s.buildCoupleRelationship(*fam)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, gedcomx.RelationshipDocument{Relationships: []gedcomx.Relationship{rel}, Links: rel.Links})
		return
	}

	parentID := fam.MotherID
	if parsed.IsFather {
		parentID = fam.FatherID
	}
	if parentID == 0 {
		notFound(w, "relationship", id)
		return
	}
	rel := s.buildParentChildRelationship(parsed.FamilyID, parentID, parsed.ChildID, parsed.IsFather)
	writeJSON(w, http.StatusOK, gedcomx.RelationshipDocument{Relationships: []gedcomx.Relationship{rel}, Links: rel.Links})
}

// handleUpdateRelationship implements the Relationship state's POST
// operation (RS spec Section 4.7.2: "Update a relationship", OPTIONAL)
// -- only registered when this collection's database is writable.
//
// Scoped to ONLY media, same reasoning as handleUpdatePerson/
// handleUpdateEvent (see their own doc comments) -- names/facts/sources
// aren't writable, requests including them are logged rather than
// rejected. One more restriction specific to Relationship: only the
// "couple" kind is writable, never a parent-child relationship.
// RootsMagic has no place to attach media to a specific parent-child
// EDGE at all -- MediaLinkTable's OwnerType=Family, OwnerID=FamilyID
// belongs to the family as a whole, the same identity the "couple"
// relationship represents; there's no separate identity for "this one
// parent-child pair" to attach anything to. A parent-child relationship
// id is rejected with a clear 400, not silently redirected to the
// family it belongs to, which would attach media to a different
// relationship than the one actually named in the URL.
//
// Type/Person1/Person2 are deliberately NOT part of the ignored-fields
// log below, unlike every other write handler's equivalent check --
// checked directly against buildCoupleRelationship: all three are
// always populated on every real Relationship this server returns
// (Person1/Person2 are required by the RS spec itself), so a client
// following the ordinary GET-then-modify-then-POST pattern will always
// send back non-empty values for them regardless of intent. Logging
// their presence would be noise on every single request, not a signal
// of anything -- unlike Facts/Sources, which are genuinely optional and
// only present when there's real data behind them.
func (s *Server) handleUpdateRelationship(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	parsed, err := parseRelationshipID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if parsed.Kind != "couple" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"media can only be linked to the couple relationship, not a specific parent-child relationship (%q) -- RootsMagic has no place to attach media to a parent-child pair on its own", id))
		return
	}
	fam, err := s.db.GetFamily(parsed.FamilyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fam == nil || fam.FatherID == 0 || fam.MotherID == 0 {
		notFound(w, "relationship", id)
		return
	}

	var body gedcomx.RelationshipDocument
	if err := decodeStrictJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(body.Relationships) == 0 {
		writeError(w, http.StatusBadRequest, "request body must include at least one relationship (RS spec Section 4.7.3)")
		return
	}
	rel := body.Relationships[0]
	if rel.ID != "" && rel.ID != id {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("body id %q doesn't match URL id %q", rel.ID, id))
		return
	}

	var ignoredFields []string
	if len(rel.Facts) > 0 {
		ignoredFields = append(ignoredFields, "facts")
	}
	if len(rel.Sources) > 0 {
		ignoredFields = append(ignoredFields, "sources")
	}
	if len(ignoredFields) > 0 {
		logIgnoredFields("relationship", "/relationships", id, ignoredFields)
	}

	mediaIDs := make([]int64, 0, len(rel.Media))
	for _, ref := range rel.Media {
		mid, err := mediaIDFromReference(ref)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid media reference: %v", err))
			return
		}
		mediaIDs = append(mediaIDs, mid)
	}

	if s.ensureBackupForWrite(w) != nil {
		return
	}
	if err := s.db.UpdateOwnerMedia(rmdb.OwnerTypeFamily, parsed.FamilyID, mediaIDs); err != nil {
		if errors.Is(err, rmdb.ErrArtifactNotFound) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Places ---

func (s *Server) handlePlaces(w http.ResponseWriter, r *http.Request) {
	limit, offset := s.pagingParams(r)
	rows, total, err := s.db.ListPlaces(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	places := make([]gedcomx.PlaceDescription, 0, len(rows))
	for _, p := range rows {
		pd, err := s.buildPlaceDescription(p)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		places = append(places, pd)
	}
	status := http.StatusOK
	if len(places) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.PlaceDescriptionsDocument{
		Results: len(places),
		Places:  places,
		Links:   pagingLinks(s, "/places", limit, offset, total),
	})
}

func (s *Server) handlePlace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	plid, err := parsePlaceID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	place, err := s.db.GetPlace(plid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if place == nil {
		notFound(w, "place", id)
		return
	}
	pd, err := s.buildPlaceDescription(*place)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gedcomx.PlaceDescriptionDocument{Places: []gedcomx.PlaceDescription{pd}, Links: pd.Links})
}

// handleUpdatePlace implements the Place Description state's POST
// operation (RS spec Section 4.16.2: "Update a place description",
// OPTIONAL) -- only registered at all when this collection's database is
// writable (see resourceHandler). Updates PlaceTable's Name,
// Latitude/Longitude, and Note; see rmdb.PlaceUpdate and SCOPE.md's
// "Write support" section for the current, deliberately limited, set of
// writable fields and update semantics.
func (s *Server) handleUpdatePlace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	plid, err := parsePlaceID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var body gedcomx.PlaceDescriptionDocument
	if err := decodeStrictJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(body.Places) == 0 {
		writeError(w, http.StatusBadRequest, "request body must include at least one place description (RS spec Section 4.16.3)")
		return
	}
	place := body.Places[0]
	// RS spec Section 8: a data element WITH an id is an update candidate
	// for that id -- cross-check it matches the URL rather than silently
	// trusting or ignoring a mismatch.
	if place.ID != "" && place.ID != id {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("body id %q doesn't match URL id %q", place.ID, id))
		return
	}
	if (place.Latitude == nil) != (place.Longitude == nil) {
		writeError(w, http.StatusBadRequest, "latitude and longitude must be provided together, or not at all")
		return
	}

	var update rmdb.PlaceUpdate
	if len(place.Names) > 0 && strings.TrimSpace(place.Names[0].Value) != "" {
		name := place.Names[0].Value
		update.Name = &name
	}
	if place.Latitude != nil && place.Longitude != nil {
		// math.Round, not a bare int64(...) conversion: float64 can't
		// represent most decimal fractions exactly (44.817778 * 1e7
		// evaluates to 448177779.9999999404 in float64 arithmetic, not
		// exactly 448177780.0), and int64(...) truncates toward zero
		// rather than rounding -- so a bare conversion here silently
		// rounds coordinates down by up to 1 in the last digit (roughly
		// a centimeter), confirmed directly against this exact value.
		// math.Round first gives the mathematically intended integer.
		lat := int64(math.Round(*place.Latitude * 1e7))
		lon := int64(math.Round(*place.Longitude * 1e7))
		update.Latitude = &lat
		update.Longitude = &lon
	}
	if len(place.Notes) > 0 && strings.TrimSpace(place.Notes[0].Text) != "" {
		note := place.Notes[0].Text
		update.Note = &note
	}

	if s.ensureBackupForWrite(w) != nil {
		return
	}
	if err := s.db.UpdatePlace(plid, update); err != nil {
		if errors.Is(err, rmdb.ErrNotFound) {
			notFound(w, "place", id)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Source descriptions ---

func (s *Server) handleSourceDescriptions(w http.ResponseWriter, r *http.Request) {
	limit, offset := s.pagingParams(r)
	rows, total, err := s.db.ListSources(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	descs := make([]gedcomx.SourceDescription, 0, len(rows))
	for _, src := range rows {
		descs = append(descs, s.buildSourceDescription(src))
	}
	status := http.StatusOK
	if len(descs) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.SourceDescriptionsDocument{
		Results:            len(descs),
		SourceDescriptions: descs,
		Links:              pagingLinks(s, "/source-descriptions", limit, offset, total),
	})
}

func (s *Server) handleSourceDescription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sid, err := parseSourceID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	src, err := s.db.GetSource(sid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if src == nil {
		notFound(w, "source description", id)
		return
	}
	sd := s.buildSourceDescription(*src)
	writeJSON(w, http.StatusOK, gedcomx.SourceDescriptionDocument{SourceDescriptions: []gedcomx.SourceDescription{sd}, Links: sd.Links})
}

// handleUpdateSourceDescription implements the Source Description state's
// POST operation (RS spec Section 4.23.2: "Update a source description",
// OPTIONAL) -- only registered at all when this collection's database is
// writable (see resourceHandler). Updates SourceTable's Name and
// Comments; see rmdb.SourceUpdate and SCOPE.md's "Write support" section
// for why `citations` is deliberately not (yet) writable.
func (s *Server) handleUpdateSourceDescription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sid, err := parseSourceID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var body gedcomx.SourceDescriptionDocument
	if err := decodeStrictJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(body.SourceDescriptions) == 0 {
		writeError(w, http.StatusBadRequest, "request body must include at least one source description (RS spec Section 4.23.3)")
		return
	}
	src := body.SourceDescriptions[0]
	if src.ID != "" && src.ID != id {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("body id %q doesn't match URL id %q", src.ID, id))
		return
	}
	if len(src.Citations) > 0 {
		writeError(w, http.StatusBadRequest,
			"updating citations isn't supported yet -- RootsMagic's ActualText and RefNumber fields are combined into this API's single citations value on the way out, and can't be unambiguously split back apart on the way in; see SCOPE.md's \"Write support\" section. Omit citations from the request body to update the other fields.")
		return
	}

	var update rmdb.SourceUpdate
	if len(src.Titles) > 0 && strings.TrimSpace(src.Titles[0].Value) != "" {
		name := src.Titles[0].Value
		update.Name = &name
	}
	if len(src.Notes) > 0 && strings.TrimSpace(src.Notes[0].Text) != "" {
		comments := src.Notes[0].Text
		update.Comments = &comments
	}

	if s.ensureBackupForWrite(w) != nil {
		return
	}
	if err := s.db.UpdateSource(sid, update); err != nil {
		if errors.Is(err, rmdb.ErrNotFound) {
			notFound(w, "source description", id)
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Artifacts (multimedia) ---

// handleArtifacts serves the `Artifacts` state (RS spec Section 4.3): a
// list of digital artifacts, described as SourceDescriptions, backed by
// RootsMagic's MultimediaTable.
func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	limit, offset := s.pagingParams(r)
	rows, total, err := s.db.ListMultimedia(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	descs := make([]gedcomx.SourceDescription, 0, len(rows))
	for _, item := range rows {
		descs = append(descs, s.buildArtifactDescription(item))
	}
	status := http.StatusOK
	if len(descs) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.SourceDescriptionsDocument{
		Results:            len(descs),
		SourceDescriptions: descs,
		Links:              pagingLinks(s, "/artifacts", limit, offset, total),
	})
}

func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mid, err := parseMediaID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.db.GetMultimediaItem(mid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item == nil {
		notFound(w, "artifact", id)
		return
	}
	sd := s.buildArtifactDescription(*item)
	writeJSON(w, http.StatusOK, gedcomx.SourceDescriptionDocument{SourceDescriptions: []gedcomx.SourceDescription{sd}, Links: sd.Links})
}

// handleUpdateArtifact updates a multimedia item's stored location --
// there's no dedicated RS spec transition for this either, same as
// handleArtifactContent above; only registered at all when this
// collection's database is writable (see resourceHandler). The request
// body's mediaPath is a real, absolute filesystem path (see
// gedcomx.SourceDescription.MediaPath's own doc comment); this server
// encodes it into RootsMagic's "?"-relative notation itself
// (rmdb.UpdateArtifactPath / encodeMediaPath) -- the client never
// constructs RootsMagic's own path syntax. Requires this server to have a
// configured Media Folder (write mode's own startup precondition -- see
// SCOPE.md's "Write support" section); if somehow missing here anyway,
// that's a server misconfiguration (500), not a bad request.
func (s *Server) handleUpdateArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mid, err := parseMediaID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var body gedcomx.SourceDescriptionDocument
	if err := decodeStrictJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(body.SourceDescriptions) == 0 {
		writeError(w, http.StatusBadRequest, "request body must include at least one artifact description")
		return
	}
	artifact := body.SourceDescriptions[0]
	if artifact.ID != "" && artifact.ID != id {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("body id %q doesn't match URL id %q", artifact.ID, id))
		return
	}
	mediaPath := strings.TrimSpace(artifact.MediaPath)
	if mediaPath == "" {
		writeError(w, http.StatusBadRequest, "mediaPath is required to update an artifact's location")
		return
	}
	if s.cfg.Media.MediaFolder == "" {
		writeError(w, http.StatusInternalServerError, "no Media Folder is configured for this server -- artifact locations can't be written without one")
		return
	}

	if s.ensureBackupForWrite(w) != nil {
		return
	}
	if err := s.db.UpdateArtifactPath(mid, s.cfg.Media.MediaFolder, mediaPath); err != nil {
		if errors.Is(err, rmdb.ErrNotFound) {
			notFound(w, "artifact", id)
			return
		}
		if errors.Is(err, rmdb.ErrPathNotUnderMediaFolder) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleArtifactContent streams the actual bytes of a multimedia item --
// there's no dedicated state for this in the RS spec (SourceDescription's
// `about` field is the spec's mechanism for pointing at a resource's
// actual location; this endpoint is what `about` points to). Not served
// for items that turn out to be external/web-hint references rather than
// local files -- see rmdb.LooksLikeExternalReference and SCOPE.md.
func (s *Server) handleArtifactContent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mid, err := parseMediaID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.db.GetMultimediaItem(mid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item == nil {
		notFound(w, "artifact", id)
		return
	}
	if rmdb.LooksLikeExternalReference(item.MediaPath) {
		writeError(w, http.StatusNotFound,
			"this artifact references an external location ("+item.MediaPath+"), not a local file; no content is available from this server")
		return
	}

	path, err := rmdb.ResolveMediaPath(item.MediaPath, item.MediaFile, s.cfg.Media)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolving media path: "+err.Error())
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "the artifact's file could not be opened at "+path+": "+err.Error())
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Override the server-wide "application/x-gedcomx-v1+json" default
	// (set by the withGedcomXContentType middleware) -- this response is
	// the raw file, not a GEDCOM X document.
	w.Header().Del("Content-Type")
	if mt := gedcomx.MediaTypeForFilename(item.MediaFile); mt != "" {
		w.Header().Set("Content-Type", mt)
	}
	http.ServeContent(w, r, item.MediaFile, info.ModTime(), f)
}

// --- Events ---

// handleEvents serves the `Events` state (RS spec Section 4.7): a list of
// events, backed by every row in RootsMagic's EventTable (not just ones
// with witnesses -- see SCOPE.md's "Events" section for why this server
// exposes the full set rather than filtering to "interesting" ones).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit, offset := s.pagingParams(r)
	rows, total, err := s.db.ListEvents(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events := make([]gedcomx.Event, 0, len(rows))
	for _, e := range rows {
		ev, err := s.buildEvent(e)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		events = append(events, ev)
	}
	status := http.StatusOK
	if len(events) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.EventsDocument{
		Results: len(events),
		Events:  events,
		Links:   pagingLinks(s, "/events", limit, offset, total),
	})
}

// handleEvent serves the `Event` state (RS spec Section 4.8) for a single
// event. Its id is the same "E{EventID}" used for the corresponding Fact
// on whichever Person or Relationship this event also belongs to -- see
// parseEventID's doc comment.
func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	eid, err := parseEventID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := s.db.GetEvent(eid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if e == nil {
		notFound(w, "event", id)
		return
	}
	ev, err := s.buildEvent(*e)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, gedcomx.EventDocument{Events: []gedcomx.Event{ev}, Links: ev.Links})
}

// handleUpdateEvent implements the Event state's POST operation (RS spec
// Section 4.9.2: "Update an event", OPTIONAL) -- only registered when
// this collection's database is writable (see resourceHandler).
//
// Scoped to ONLY media links, same as handleUpdatePerson and for the same
// reason -- see its own doc comment, and SCOPE.md's "Write support"
// section, for the full reasoning (shared verbatim here: unsupported
// fields are logged, not rejected, so the natural GET-then-modify-then-
// POST client pattern keeps working for the one thing this endpoint
// supports, while leaving a visible trail of real demand for anything
// beyond that).
func (s *Server) handleUpdateEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	eid, err := parseEventID(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := s.db.GetEvent(eid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if e == nil {
		notFound(w, "event", id)
		return
	}

	var body gedcomx.EventDocument
	if err := decodeStrictJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(body.Events) == 0 {
		writeError(w, http.StatusBadRequest, "request body must include at least one event (RS spec Section 4.9.3)")
		return
	}
	event := body.Events[0]
	if event.ID != "" && event.ID != id {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("body id %q doesn't match URL id %q", event.ID, id))
		return
	}

	var ignoredFields []string
	if event.Type != "" {
		ignoredFields = append(ignoredFields, "type")
	}
	if event.Date != nil {
		ignoredFields = append(ignoredFields, "date")
	}
	if event.Place != nil {
		ignoredFields = append(ignoredFields, "place")
	}
	if len(event.Roles) > 0 {
		ignoredFields = append(ignoredFields, "roles")
	}
	if len(event.Sources) > 0 {
		ignoredFields = append(ignoredFields, "sources")
	}
	if len(event.Notes) > 0 {
		ignoredFields = append(ignoredFields, "notes")
	}
	if len(ignoredFields) > 0 {
		logIgnoredFields("event", "/events", id, ignoredFields)
	}

	mediaIDs := make([]int64, 0, len(event.Media))
	for _, ref := range event.Media {
		mid, err := mediaIDFromReference(ref)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid media reference: %v", err))
			return
		}
		mediaIDs = append(mediaIDs, mid)
	}

	if s.ensureBackupForWrite(w) != nil {
		return
	}
	if err := s.db.UpdateOwnerMedia(rmdb.OwnerTypeEvent, eid, mediaIDs); err != nil {
		if errors.Is(err, rmdb.ErrArtifactNotFound) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
