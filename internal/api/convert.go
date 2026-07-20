package api

import (
	"fmt"
	"strings"

	"github.com/aviast/rmgedcomx/internal/gedcomx"
	"github.com/aviast/rmgedcomx/internal/rmdb"
)

// buildPerson assembles a full gedcomx.Person (identity, names, gender,
// facts, sources, display properties, and links) from a RootsMagic person
// row.
func (s *Server) buildPerson(rp rmdb.Person) (gedcomx.Person, error) {
	id := personRef(rp.PersonID)

	rmNames, err := s.db.GetNames(rp.PersonID)
	if err != nil {
		return gedcomx.Person{}, err
	}
	names := make([]gedcomx.Name, 0, len(rmNames))
	for _, n := range rmNames {
		names = append(names, s.buildName(n))
	}

	events, err := s.db.GetEvents(rmdb.OwnerTypePerson, rp.PersonID)
	if err != nil {
		return gedcomx.Person{}, err
	}
	facts := make([]gedcomx.Fact, 0, len(events))
	for _, e := range events {
		f, err := s.buildFact(e)
		if err != nil {
			return gedcomx.Person{}, err
		}
		facts = append(facts, f)
	}

	sources, err := s.buildSourceReferences(rmdb.OwnerTypePerson, rp.PersonID)
	if err != nil {
		return gedcomx.Person{}, err
	}

	p := gedcomx.Person{
		ID:      id,
		Living:  gedcomx.BoolPtr(rp.Living == 1),
		Gender:  &gedcomx.Gender{Type: gedcomx.GenderTypeURI(rp.Sex)},
		Names:   names,
		Facts:   facts,
		Sources: sources,
		Display: s.buildDisplayProperties(rmNames, rp.Sex),
		Links:   gedcomx.Links{},
	}
	p.Links["person"] = gedcomx.Link{Href: s.url("/persons/" + id)}
	p.Links["parents"] = gedcomx.Link{Href: s.url("/persons/" + id + "/parents")}
	p.Links["children"] = gedcomx.Link{Href: s.url("/persons/" + id + "/children")}
	p.Links["spouses"] = gedcomx.Link{Href: s.url("/persons/" + id + "/spouses")}
	p.Links["ancestry"] = gedcomx.Link{Href: s.url("/persons/" + id + "/ancestry")}
	p.Links["descendancy"] = gedcomx.Link{Href: s.url("/persons/" + id + "/descendancy")}
	return p, nil
}

func (s *Server) buildName(n rmdb.Name) gedcomx.Name {
	var parts []gedcomx.NamePart
	addPart := func(typ, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		parts = append(parts, gedcomx.NamePart{Type: typ, Value: value})
	}
	addPart("http://gedcomx.org/Prefix", n.Prefix)
	addPart("http://gedcomx.org/Given", n.Given)
	addPart("http://gedcomx.org/Surname", n.Surname)
	addPart("http://gedcomx.org/Suffix", n.Suffix)

	fullText := strings.TrimSpace(strings.Join(nonEmpty(n.Prefix, n.Given, n.Surname, n.Suffix), " "))

	name := gedcomx.Name{
		ID:        nameRef(n.NameID),
		Preferred: gedcomx.BoolPtr(n.IsPrimary == 1),
		NameForms: []gedcomx.NameForm{{FullText: fullText, Parts: parts}},
	}
	if uri := gedcomx.NameTypeURI(n.NameType); uri != "" {
		name.Type = uri
	}
	if d := gedcomx.ParseRMDate(n.Date); d != nil {
		name.Date = d
	}
	return name
}

func nonEmpty(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

func (s *Server) buildFact(e rmdb.Event) (gedcomx.Fact, error) {
	ft := s.factTypes[e.EventType]
	f := gedcomx.Fact{
		ID:   factRef(e.EventID),
		Type: gedcomx.FactType(ft.GedcomTag, ft.Name),
	}
	if d := gedcomx.ParseRMDate(e.Date); d != nil {
		f.Date = d
	}
	if e.PlaceID != 0 {
		pref, err := s.buildPlaceReference(e.PlaceID)
		if err != nil {
			return gedcomx.Fact{}, err
		}
		f.Place = pref
	}
	if e.Details != "" {
		f.Value = e.Details
	}
	if e.IsPrimary == 1 {
		f.Primary = gedcomx.BoolPtr(true)
	}
	if e.Note != "" {
		f.Notes = []gedcomx.Note{{Text: e.Note}}
	}
	sources, err := s.buildSourceReferences(rmdb.OwnerTypeEvent, e.EventID)
	if err != nil {
		return gedcomx.Fact{}, err
	}
	f.Sources = sources
	return f, nil
}

func (s *Server) buildPlaceReference(placeID int64) (*gedcomx.PlaceReference, error) {
	place, err := s.db.GetPlace(placeID)
	if err != nil {
		return nil, err
	}
	if place == nil {
		return nil, nil
	}
	return &gedcomx.PlaceReference{
		Original: place.Name,
		Resource: s.url("/places/" + placeRef(place.PlaceID)),
	}, nil
}

func (s *Server) buildSourceReferences(ownerType int, ownerID int64) ([]gedcomx.SourceReference, error) {
	sourceIDs, err := s.db.SourceIDsForOwner(ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	refs := make([]gedcomx.SourceReference, 0, len(sourceIDs))
	for _, sid := range sourceIDs {
		id := sourceRef(sid)
		refs = append(refs, gedcomx.SourceReference{
			Description:   s.url("/source-descriptions/" + id),
			DescriptionID: id,
		})
	}
	return refs, nil
}

func (s *Server) buildDisplayProperties(names []rmdb.Name, sex int) *gedcomx.DisplayProperties {
	disp := &gedcomx.DisplayProperties{}
	switch sex {
	case 0:
		disp.Gender = "Male"
	case 1:
		disp.Gender = "Female"
	default:
		disp.Gender = "Unknown"
	}
	if len(names) > 0 {
		n := names[0]
		disp.Name = strings.TrimSpace(strings.Join(nonEmpty(n.Prefix, n.Given, n.Surname, n.Suffix), " "))
		if n.BirthYear != 0 || n.DeathYear != 0 {
			b, d := "", ""
			if n.BirthYear != 0 {
				b = fmt.Sprintf("%d", n.BirthYear)
			}
			if n.DeathYear != 0 {
				d = fmt.Sprintf("%d", n.DeathYear)
			}
			disp.Lifespan = b + " - " + d
			disp.BirthDate = b
			disp.DeathDate = d
		}
	}
	return disp
}

// --- Collection ---

// buildCollection assembles the single Collection this server exposes: the
// RootsMagic database it was started with. See SCOPE.md's "Collection"
// section for why one RootsMagic file maps to exactly one Collection.
func (s *Server) buildCollection() (gedcomx.Collection, error) {
	stats, err := s.db.CollectionStats()
	if err != nil {
		return gedcomx.Collection{}, err
	}
	return gedcomx.Collection{
		ID:    collectionID,
		Title: s.cfg.Title,
		Content: []gedcomx.CollectionContent{
			{ResourceType: gedcomx.ResourceTypePerson, Count: stats.Persons},
			{ResourceType: gedcomx.ResourceTypeRelationship, Count: stats.Relationships},
			{ResourceType: gedcomx.ResourceTypePlaceDescription, Count: stats.Places},
			{ResourceType: gedcomx.ResourceTypeSourceDescription, Count: stats.Sources},
		},
		Links: gedcomx.Links{
			"collection":          {Href: s.url("/collections/" + collectionID)},
			"subcollections":      {Href: s.url("/collections")},
			"persons":             {Href: s.url("/persons")},
			"relationships":       {Href: s.url("/relationships")},
			"source-descriptions": {Href: s.url("/source-descriptions")},
			// "place-descriptions" isn't one of the formally-defined
			// Collection transitions in RS spec Section 4.5.4 (there's no
			// plural rel for the Place Descriptions state anywhere in the
			// spec's master link-relation table, Section 5.2) but is
			// included here as a RECOMMENDED "other transition" per that
			// same section, following the naming convention of the
			// existing "source-descriptions" rel.
			"place-descriptions": {Href: s.url("/places")},
		},
	}, nil
}

// --- Relationships ---

func (s *Server) buildCoupleRelationship(f rmdb.Family) (gedcomx.Relationship, error) {
	id := coupleRef(f.FamilyID)
	events, err := s.db.GetEvents(rmdb.OwnerTypeFamily, f.FamilyID)
	if err != nil {
		return gedcomx.Relationship{}, err
	}
	facts := make([]gedcomx.Fact, 0, len(events))
	for _, e := range events {
		fact, err := s.buildFact(e)
		if err != nil {
			return gedcomx.Relationship{}, err
		}
		facts = append(facts, fact)
	}

	rel := gedcomx.Relationship{
		ID:      id,
		Type:    gedcomx.RelationshipTypeCouple,
		Person1: gedcomx.ResourceReference{Resource: s.url("/persons/" + personRef(f.FatherID)), ResourceID: personRef(f.FatherID)},
		Person2: gedcomx.ResourceReference{Resource: s.url("/persons/" + personRef(f.MotherID)), ResourceID: personRef(f.MotherID)},
		Facts:   facts,
		Links:   gedcomx.Links{"relationship": {Href: s.url("/relationships/" + id)}},
	}
	return rel, nil
}

func (s *Server) buildParentChildRelationship(familyID, parentID, childID int64, isFather bool) gedcomx.Relationship {
	id := parentChildRef(familyID, childID, isFather)
	return gedcomx.Relationship{
		ID:      id,
		Type:    gedcomx.RelationshipTypeParentChild,
		Person1: gedcomx.ResourceReference{Resource: s.url("/persons/" + personRef(parentID)), ResourceID: personRef(parentID)},
		Person2: gedcomx.ResourceReference{Resource: s.url("/persons/" + personRef(childID)), ResourceID: personRef(childID)},
		Links:   gedcomx.Links{"relationship": {Href: s.url("/relationships/" + id)}},
	}
}

// --- Places ---

func (s *Server) buildPlaceDescription(p rmdb.Place) gedcomx.PlaceDescription {
	id := placeRef(p.PlaceID)
	pd := gedcomx.PlaceDescription{
		ID:    id,
		Names: []gedcomx.TextValue{{Value: p.Name}},
		Links: gedcomx.Links{"description": {Href: s.url("/places/" + id)}},
	}
	if p.Latitude != 0 || p.Longitude != 0 {
		lat := float64(p.Latitude) / 1e7
		lon := float64(p.Longitude) / 1e7
		pd.Latitude = &lat
		pd.Longitude = &lon
	}
	if p.Note != "" {
		pd.Notes = []gedcomx.Note{{Text: p.Note}}
	}
	placeType := "Place"
	switch p.PlaceType {
	case 1:
		placeType = "LDS Temple"
	case 2:
		placeType = "Place Detail"
	}
	pd.Display = &gedcomx.PlaceDisplayProperties{Name: p.Name, FullName: p.Name, Type: placeType}
	return pd
}

// --- Source descriptions ---

func (s *Server) buildSourceDescription(src rmdb.Source) gedcomx.SourceDescription {
	id := sourceRef(src.SourceID)
	sd := gedcomx.SourceDescription{
		ID:    id,
		Links: gedcomx.Links{"description": {Href: s.url("/source-descriptions/" + id)}},
	}
	if src.Name != "" {
		sd.Titles = []gedcomx.TextValue{{Value: src.Name}}
	}
	citation := strings.TrimSpace(strings.Join(nonEmpty(src.ActualText, src.RefNumber), " -- "))
	if citation != "" {
		sd.Citations = []gedcomx.SourceCitation{{Value: citation}}
	}
	if src.Comments != "" {
		sd.Notes = []gedcomx.Note{{Text: src.Comments}}
	}
	return sd
}
