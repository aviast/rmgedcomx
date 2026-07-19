package api

import (
	"net/http"
	"strconv"

	"github.com/example/rmgedcomx/internal/gedcomx"
)

// --- Root / entry point ---

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	doc := gedcomx.RootDocument{
		Title:            "rmgedcomx",
		Description:      "Read-only GEDCOM X RS API over a RootsMagic database. See SCOPE.md in the repository for what's implemented.",
		RootsMagicSchema: s.db.SchemaHint(),
		Links: gedcomx.Links{
			"persons":             {Href: s.url("/persons")},
			"person-search":       {Template: s.url("/persons{?name,limit,offset}")},
			"relationships":       {Href: s.url("/relationships")},
			"place-descriptions":  {Href: s.url("/places")},
			"source-descriptions": {Href: s.url("/source-descriptions")},
		},
	}
	s.writeJSON(w, http.StatusOK, doc)
}

// --- Persons ---

func (s *Server) handlePersons(w http.ResponseWriter, r *http.Request) {
	limit, offset := s.pagingParams(r)
	nameFilter := r.URL.Query().Get("name")

	rows, total, err := s.db.ListPersons(limit, offset, nameFilter)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	persons := make([]gedcomx.Person, 0, len(rows))
	for _, rp := range rows {
		p, err := s.buildPerson(rp)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		persons = append(persons, p)
	}

	status := http.StatusOK
	if len(persons) == 0 {
		status = http.StatusNoContent
	}
	s.writeJSON(w, status, gedcomx.PersonsDocument{
		Results: len(persons),
		Persons: persons,
		Links:   pagingLinks(s, "/persons", limit, offset, total),
	})
}

func (s *Server) handlePerson(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := parsePersonID(id)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rp, err := s.db.GetPerson(pid)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rp == nil {
		s.notFound(w, "person", id)
		return
	}
	p, err := s.buildPerson(*rp)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, gedcomx.PersonDocument{Persons: []gedcomx.Person{p}, Links: p.Links})
}

// --- Person Parents / Children / Spouses ---

func (s *Server) handlePersonParents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := parsePersonID(id)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	childRows, err := s.db.ChildRowsAsChild(pid)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var persons []gedcomx.Person
	var rels []gedcomx.Relationship
	seen := map[int64]bool{}
	for _, cr := range childRows {
		fam, err := s.db.GetFamily(cr.FamilyID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
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
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if rp == nil {
				continue
			}
			p, err := s.buildPerson(*rp)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
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
	s.writeJSON(w, status, gedcomx.PersonRelativesDocument{
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
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	families, err := s.db.FamiliesAsParent(pid)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var persons []gedcomx.Person
	var rels []gedcomx.Relationship
	isFather := true
	for _, fam := range families {
		isFather = fam.FatherID == pid
		children, err := s.db.ChildRowsOfFamily(fam.FamilyID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, c := range children {
			rp, err := s.db.GetPerson(c.ChildID)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if rp == nil {
				continue
			}
			p, err := s.buildPerson(*rp)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
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
	s.writeJSON(w, status, gedcomx.PersonRelativesDocument{
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
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	families, err := s.db.FamiliesAsParent(pid)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
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
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if rp == nil {
			continue
		}
		p, err := s.buildPerson(*rp)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		persons = append(persons, p)
		if fam.FatherID != 0 && fam.MotherID != 0 {
			rel, err := s.buildCoupleRelationship(fam)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			rels = append(rels, rel)
		}
	}

	status := http.StatusOK
	if len(persons) == 0 {
		status = http.StatusNoContent
	}
	s.writeJSON(w, status, gedcomx.PersonRelativesDocument{
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
		s.writeError(w, http.StatusBadRequest, err.Error())
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
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if root == nil {
		s.notFound(w, "person", id)
		return
	}

	var persons []gedcomx.Person
	frontier := []ancestryNode{{personID: pid, number: 1}}
	for gen := 1; gen <= generations && len(frontier) > 0; gen++ {
		var next []ancestryNode
		for _, node := range frontier {
			rp, err := s.db.GetPerson(node.personID)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if rp == nil {
				continue
			}
			p, err := s.buildPerson(*rp)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
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
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if len(childRows) == 0 {
				continue
			}
			fam, err := s.db.GetFamily(childRows[0].FamilyID)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
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

	s.writeJSON(w, http.StatusOK, gedcomx.AncestryResultsDocument{
		Results: len(persons),
		Persons: persons,
		Links:   gedcomx.Links{"person": {Href: s.url("/persons/" + id)}},
	})
}

func (s *Server) handleDescendancy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := parsePersonID(id)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
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
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if root == nil {
		s.notFound(w, "person", id)
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
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, gedcomx.DescendancyResultsDocument{
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
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var rels []gedcomx.Relationship
	for _, fam := range families {
		if fam.FatherID != 0 && fam.MotherID != 0 {
			rel, err := s.buildCoupleRelationship(fam)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			rels = append(rels, rel)
		}
		children, err := s.db.ChildRowsOfFamily(fam.FamilyID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
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
	s.writeJSON(w, status, gedcomx.RelationshipsDocument{
		Results:       len(rels),
		Relationships: rels,
		Links:         pagingLinks(s, "/relationships", limit, offset, total),
	})
}

func (s *Server) handleRelationship(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	parsed, err := parseRelationshipID(id)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fam, err := s.db.GetFamily(parsed.FamilyID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fam == nil {
		s.notFound(w, "relationship", id)
		return
	}

	if parsed.Kind == "couple" {
		if fam.FatherID == 0 || fam.MotherID == 0 {
			s.notFound(w, "relationship", id)
			return
		}
		rel, err := s.buildCoupleRelationship(*fam)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, gedcomx.RelationshipDocument{Relationships: []gedcomx.Relationship{rel}, Links: rel.Links})
		return
	}

	parentID := fam.MotherID
	if parsed.IsFather {
		parentID = fam.FatherID
	}
	if parentID == 0 {
		s.notFound(w, "relationship", id)
		return
	}
	rel := s.buildParentChildRelationship(parsed.FamilyID, parentID, parsed.ChildID, parsed.IsFather)
	s.writeJSON(w, http.StatusOK, gedcomx.RelationshipDocument{Relationships: []gedcomx.Relationship{rel}, Links: rel.Links})
}

// --- Places ---

func (s *Server) handlePlaces(w http.ResponseWriter, r *http.Request) {
	limit, offset := s.pagingParams(r)
	rows, total, err := s.db.ListPlaces(limit, offset)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	places := make([]gedcomx.PlaceDescription, 0, len(rows))
	for _, p := range rows {
		places = append(places, s.buildPlaceDescription(p))
	}
	status := http.StatusOK
	if len(places) == 0 {
		status = http.StatusNoContent
	}
	s.writeJSON(w, status, gedcomx.PlaceDescriptionsDocument{
		Results: len(places),
		Places:  places,
		Links:   pagingLinks(s, "/places", limit, offset, total),
	})
}

func (s *Server) handlePlace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	plid, err := parsePlaceID(id)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	place, err := s.db.GetPlace(plid)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if place == nil {
		s.notFound(w, "place", id)
		return
	}
	pd := s.buildPlaceDescription(*place)
	s.writeJSON(w, http.StatusOK, gedcomx.PlaceDescriptionDocument{Places: []gedcomx.PlaceDescription{pd}, Links: pd.Links})
}

// --- Source descriptions ---

func (s *Server) handleSourceDescriptions(w http.ResponseWriter, r *http.Request) {
	limit, offset := s.pagingParams(r)
	rows, total, err := s.db.ListSources(limit, offset)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
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
	s.writeJSON(w, status, gedcomx.SourceDescriptionsDocument{
		Results:            len(descs),
		SourceDescriptions: descs,
		Links:              pagingLinks(s, "/source-descriptions", limit, offset, total),
	})
}

func (s *Server) handleSourceDescription(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sid, err := parseSourceID(id)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	src, err := s.db.GetSource(sid)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if src == nil {
		s.notFound(w, "source description", id)
		return
	}
	sd := s.buildSourceDescription(*src)
	s.writeJSON(w, http.StatusOK, gedcomx.SourceDescriptionDocument{SourceDescriptions: []gedcomx.SourceDescription{sd}, Links: sd.Links})
}
