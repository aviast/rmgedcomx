package api

import (
	"encoding/json"
	"errors"
	"fmt"
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
	writeJSON(w, http.StatusOK, gedcomx.PersonDocument{Persons: []gedcomx.Person{p}, Links: p.Links})
}

// --- Person Parents / Children / Spouses ---

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
	var rels []gedcomx.Relationship
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
		if fam.FatherID != 0 {
			rels = append(rels, s.buildParentChildRelationship(fam.FamilyID, fam.FatherID, pid, true))
		}
		if fam.MotherID != 0 {
			rels = append(rels, s.buildParentChildRelationship(fam.FamilyID, fam.MotherID, pid, false))
		}
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
	var rels []gedcomx.Relationship
	isFather := true
	for _, fam := range families {
		isFather = fam.FatherID == pid
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
			rels = append(rels, s.buildParentChildRelationship(fam.FamilyID, pid, c.ChildID, isFather))
		}
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
	var rels []gedcomx.Relationship
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
		if fam.FatherID != 0 && fam.MotherID != 0 {
			rel, err := s.buildCoupleRelationship(fam)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			rels = append(rels, rel)
		}
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
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
		lat := int64(*place.Latitude * 1e7)
		lon := int64(*place.Longitude * 1e7)
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
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
