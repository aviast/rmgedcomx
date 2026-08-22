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
		// The reverse of buildNewPerson's own write-side absorption
		// (internal/api/createperson.go): NameTable.Nickname holds a
		// single string on this one name record, but GEDCOM X's own
		// model has no matching concept -- checked directly against the
		// conceptual model spec's "Known Name Types" (Section 3.13.1),
		// a nickname there is its own, separate Name (type=Nickname),
		// not a value attached to another Name. Synthesized as a
		// second Name here so a round trip (write a nickname, read the
		// person back) doesn't silently lose it. Deliberately given no
		// id of its own: it isn't a real, separately addressable
		// NameTable row, and assigning one (e.g. reusing the parent
		// name's) would misleadingly imply it were.
		if n.Nickname != "" {
			names = append(names, gedcomx.Name{
				Type:      "http://gedcomx.org/Nickname",
				NameForms: []gedcomx.NameForm{{FullText: n.Nickname}},
			})
		}
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

	sources, media, err := s.buildSourcesAndMedia(rmdb.OwnerTypePerson, rp.PersonID)
	if err != nil {
		return gedcomx.Person{}, err
	}

	display, err := s.buildDisplayProperties(rp.PersonID, rmNames, facts, rp.Sex)
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
		Media:   media,
		Display: display,
		Links:   gedcomx.Links{},
	}
	p.Links["person"] = gedcomx.Link{Href: s.url("/persons/" + id)}
	// RS spec Section 4.10.4, "Transitions": collection | Collection
	// State | Link to the collection that contains this person.
	p.Links["collection"] = gedcomx.Link{Href: s.collectionBaseURL}
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
	// Fact is a Conclusion, not a Subject (see SCOPE.md's "Sources versus
	// media" section) -- it has no media field to put artifact references
	// in, only sources (bibliographic). Any media attached to this same
	// EventTable row IS surfaced, just not here: it's on the
	// corresponding standalone Event instead (same id, see SCOPE.md's
	// "Events" section on the Fact/Event id cross-reference), which is a
	// proper Subject and does have a media field.
	sources, _, err := s.buildSourcesAndMedia(rmdb.OwnerTypeEvent, e.EventID)
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

// buildSourcesAndMedia gathers everything that evidences or illustrates a
// given owner (a person, family, event, place, or name) and separates it
// into GEDCOM X's two distinct concepts, per the Conclusion/Subject data
// types' own definitions -- see SCOPE.md's "Sources versus media" section
// for why an earlier version of this server combined them into one array,
// and why that was wrong, not just imprecise:
//
//   - sources: bibliographic evidence (Conclusion.sources) -- real
//     Source citations, via CitationLinkTable -> CitationTable ->
//     SourceTable, pointing at /source-descriptions/S{id}.
//   - media: illustrative artifacts (Subject.media) -- multimedia
//     attached directly to the owner (via MediaLinkTable, pointing at
//     /artifacts/M{id}), plus -- this turns out to be the dominant
//     real-world case, not an edge case -- multimedia attached to the
//     owner's *citations* themselves rather than to the owner directly
//     (e.g. a scanned census page attached to the "1911 Census" citation
//     on a person's residence fact, rather than to the fact itself). See
//     SCOPE.md's "Multimedia" section.
//
// Only called for owners that are GEDCOM X Subjects (Person, Relationship,
// Event, PlaceDescription) or their sub-parts that share an owner with
// one (a Name belongs to a Person, for instance) -- Fact is a Conclusion,
// not a Subject, and has no media field to put the second return value
// in; buildFact calls this and deliberately discards it, see its own
// comment for where that media actually surfaces instead.
func (s *Server) buildSourcesAndMedia(ownerType int, ownerID int64) (sources []gedcomx.SourceReference, media []gedcomx.SourceReference, err error) {
	sourceIDs, err := s.db.SourceIDsForOwner(ownerType, ownerID)
	if err != nil {
		return nil, nil, err
	}
	for _, sid := range sourceIDs {
		id := sourceRef(sid)
		sources = append(sources, gedcomx.SourceReference{
			Description:   s.url("/source-descriptions/" + id),
			DescriptionID: id,
		})
	}

	seenMedia := map[int64]bool{}
	addMedia := func(mediaIDs []int64) {
		for _, mid := range mediaIDs {
			if seenMedia[mid] {
				continue
			}
			seenMedia[mid] = true
			id := mediaRef(mid)
			media = append(media, gedcomx.SourceReference{
				Description:   s.url("/artifacts/" + id),
				DescriptionID: id,
			})
		}
	}

	directMedia, err := s.db.MediaIDsForOwner(ownerType, ownerID)
	if err != nil {
		return nil, nil, err
	}
	addMedia(directMedia)

	citationIDs, err := s.db.CitationIDsForOwner(ownerType, ownerID)
	if err != nil {
		return nil, nil, err
	}
	for _, cid := range citationIDs {
		citationMedia, err := s.db.MediaIDsForOwner(rmdb.OwnerTypeCitation, cid)
		if err != nil {
			return nil, nil, err
		}
		addMedia(citationMedia)
	}

	return sources, media, nil
}

// buildDisplayProperties assembles the RS "DisplayProperties" extension
// (Section 2.2) -- checked directly against the spec's own properties
// table before implementing any of this, not assumed: name, gender,
// lifespan, birthDate, birthPlace, deathDate, deathPlace, marriageDate,
// marriagePlace, ascendancyNumber, descendancyNumber, familiesAsParent,
// familiesAsChild.
//
// A real, reported gap: marriageDate/marriagePlace weren't implemented
// at all -- the struct itself had no fields for them until fixed.
// birthPlace/deathPlace turned out to have the exact same problem one
// level less visible: the struct fields already existed, but this
// function never actually populated them. familiesAsParent/
// familiesAsChild were flagged, at the time, as a real, separate gap
// too large to fold into that same fix (each FamilyView needs its own
// parent1/parent2/children construction across every family a person is
// in, both as parent and as child) -- implemented here, in the
// dedicated turn that was flagged for.
//
// birthPlace/deathPlace come from this same person's own Birth/Death
// facts (already-built facts, reused directly rather than re-fetched --
// buildPerson computes these before calling this function). There's
// only ever one Birth and one Death fact per real person in practice,
// so no "which one" ambiguity the way marriage has.
//
// marriageDate/marriagePlace require a real design decision the spec
// itself doesn't make: a person can have more than one marriage, and
// DisplayProperties has room for exactly one of each. Resolved the same
// way this project has resolved other "which one, when there are
// several" questions with no other guidance -- take the first,
// consistently ordered (FamiliesAsParent's own query, ORDER BY
// FamilyID -- matching the existing convention used for a person's
// primary name and other "the first one" choices), and skip to the next
// family only if the first has no Marriage fact at all rather than
// picking a family known not to have one.
//
// familiesAsParent/familiesAsChild carry no such ambiguity -- OPTIONAL,
// "Order is preserved," lists rather than a single "which one" value,
// so every family a person is a parent or a child in is included, not
// just the first. See buildFamilyView's own comment for how each one is
// built.
func (s *Server) buildDisplayProperties(personID int64, names []rmdb.Name, facts []gedcomx.Fact, sex int) (*gedcomx.DisplayProperties, error) {
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

	for _, f := range facts {
		if f.Place == nil || f.Place.Original == "" {
			continue
		}
		switch f.Type {
		case "http://gedcomx.org/Birth":
			disp.BirthPlace = f.Place.Original
		case "http://gedcomx.org/Death":
			disp.DeathPlace = f.Place.Original
		}
	}

	families, err := s.db.FamiliesAsParent(personID)
	if err != nil {
		return nil, err
	}
	for _, fam := range families {
		events, err := s.db.GetEvents(rmdb.OwnerTypeFamily, fam.FamilyID)
		if err != nil {
			return nil, err
		}
		var marriageFact *gedcomx.Fact
		for _, e := range events {
			f, err := s.buildFact(e)
			if err != nil {
				return nil, err
			}
			if f.Type == "http://gedcomx.org/Marriage" {
				marriageFact = &f
				break
			}
		}
		if marriageFact == nil {
			continue
		}
		if marriageFact.Date != nil {
			disp.MarriageDate = marriageFact.Date.Original
		}
		if marriageFact.Place != nil {
			disp.MarriagePlace = marriageFact.Place.Original
		}
		break
	}

	// familiesAsParent/familiesAsChild: OPTIONAL (Section 2.2's own
	// properties table), unlike PersonDocument.Relationships' own MUST
	// (Section 4.10.5) -- left nil, not an empty slice, when a person
	// has none, so they're correctly omitted (json:"...,omitempty")
	// rather than serialized as "[]", matching the spec's own
	// "OPTIONAL" rather than treating this the same as that separate,
	// stronger requirement.
	for _, fam := range families {
		fv, err := s.buildFamilyView(fam)
		if err != nil {
			return nil, err
		}
		disp.FamiliesAsParent = append(disp.FamiliesAsParent, fv)
	}

	childRows, err := s.db.ChildRowsAsChild(personID)
	if err != nil {
		return nil, err
	}
	for _, cr := range childRows {
		fam, err := s.db.GetFamily(cr.FamilyID)
		if err != nil {
			return nil, err
		}
		if fam == nil {
			continue
		}
		fv, err := s.buildFamilyView(*fam)
		if err != nil {
			return nil, err
		}
		disp.FamiliesAsChild = append(disp.FamiliesAsChild, fv)
	}

	return disp, nil
}

// buildFamilyView assembles the RS "FamilyView" extension (Section 2.3)
// for a single family -- "up to two parents and a list of children who
// have that set of parents in common," per the spec's own description,
// used for both DisplayProperties.familiesAsParent and
// .familiesAsChild. parent1/parent2 (each OPTIONAL, spec doesn't define
// which is which -- checked directly, not assumed) are assigned
// Father/Mother respectively, matching buildCoupleRelationship's own
// existing Person1=Father/Person2=Mother convention for the same
// FatherID/MotherID pair, for consistency across this project rather
// than introducing a second, different convention for the same
// underlying data.
func (s *Server) buildFamilyView(fam rmdb.Family) (gedcomx.FamilyView, error) {
	fv := gedcomx.FamilyView{}
	if fam.FatherID != 0 {
		fv.Parent1 = &gedcomx.ResourceReference{Resource: s.url("/persons/" + personRef(fam.FatherID)), ResourceID: personRef(fam.FatherID)}
	}
	if fam.MotherID != 0 {
		fv.Parent2 = &gedcomx.ResourceReference{Resource: s.url("/persons/" + personRef(fam.MotherID)), ResourceID: personRef(fam.MotherID)}
	}
	children, err := s.db.ChildRowsOfFamily(fam.FamilyID)
	if err != nil {
		return gedcomx.FamilyView{}, err
	}
	for _, c := range children {
		fv.Children = append(fv.Children, gedcomx.ResourceReference{Resource: s.url("/persons/" + personRef(c.ChildID)), ResourceID: personRef(c.ChildID)})
	}
	return fv, nil
}

// --- Collection ---

// buildCollection assembles the Collection this Server exposes -- one
// RootsMagic database. With multiple databases open (multiple -db flags),
// there's one Server (and one buildCollection) per database; see
// SCOPE.md's "Multiple databases / Collections" section.
func (s *Server) buildCollection() (gedcomx.Collection, error) {
	stats, err := s.db.CollectionStats()
	if err != nil {
		return gedcomx.Collection{}, err
	}

	// Never fatal to building the Collection: same treatment as
	// RootPersonDisplayName in collectionid derivation -- if RootsMagic's
	// UniqueID can't be determined, the Collection is still built, just
	// without an identifiers entry, rather than failing the whole request.
	var identifiers gedcomx.Identifiers
	if uid, err := s.db.UniqueID(); err == nil && uid != "" {
		identifiers = gedcomx.Identifiers{gedcomx.IdentifierTypeRootsMagicUniqueID: {uid}}
	}

	return gedcomx.Collection{
		ID:          s.cfg.ID,
		Title:       s.cfg.Title,
		Identifiers: identifiers,
		Content: []gedcomx.CollectionContent{
			{ResourceType: gedcomx.ResourceTypePerson, Count: stats.Persons},
			{ResourceType: gedcomx.ResourceTypeRelationship, Count: stats.Relationships},
			{ResourceType: gedcomx.ResourceTypePlaceDescription, Count: stats.Places},
			{ResourceType: gedcomx.ResourceTypeSourceDescription, Count: stats.Sources},
			{ResourceType: gedcomx.ResourceTypeDigitalArtifact, Count: stats.Artifacts},
			{ResourceType: gedcomx.ResourceTypeEvent, Count: stats.Events},
		},
		Links: gedcomx.Links{
			"collection":          {Href: s.collectionBaseURL},
			"subcollections":      {Href: s.globalURL("/collections")},
			"persons":             {Href: s.url("/persons")},
			"relationships":       {Href: s.url("/relationships")},
			"source-descriptions": {Href: s.url("/source-descriptions")},
			"artifacts":           {Href: s.url("/artifacts")},
			"events":              {Href: s.url("/events")},
			// "place-descriptions" isn't one of the formally-defined
			// Collection transitions in RS spec Section 4.5.4 (there's no
			// plural rel for the Place Descriptions state anywhere in the
			// spec's master link-relation table, Section 5.2) but is
			// included here as a RECOMMENDED "other transition" per that
			// same section, following the naming convention of the
			// existing "source-descriptions" rel.
			"place-descriptions": {Href: s.url("/places")},
			// RS spec Section 4.5.4's own master transitions table
			// (Section 5.2) lists "person-search" as a templated link
			// to the Person Search Results state (RFC 6570 URI
			// Template, per the GEDCOM X Atom Extensions spec's own
			// "template" attribute -- checked directly, not assumed).
			// "q" is the spec's own template variable name (Section 5.3);
			// "limit"/"offset" match this server's own existing paging
			// parameter names elsewhere (pagingParams, server.go)
			// rather than the spec's generic "count"/"start" variable
			// names, for consistency with every other paged endpoint in
			// this server rather than introducing a second, different
			// naming convention for this one endpoint alone.
			"person-search": {Template: s.url("/persons/search") + "{?q,limit,offset}"},
			// Same master transitions table (Section 5.2) as
			// person-search above, this time for the Place Search
			// Results state (Section 4.17). Unlike person-search,
			// "q" here only ever accepts a single "name" parameter --
			// see rmdb.SearchPlaces's own comment for why the RS spec
			// itself gives no further guidance on place-specific
			// search parameters.
			"place-search": {Template: s.url("/places/search") + "{?q,limit,offset}"},
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

	sources, media, err := s.buildSourcesAndMedia(rmdb.OwnerTypeFamily, f.FamilyID)
	if err != nil {
		return gedcomx.Relationship{}, err
	}

	rel := gedcomx.Relationship{
		ID:      id,
		Type:    gedcomx.RelationshipTypeCouple,
		Person1: gedcomx.ResourceReference{Resource: s.url("/persons/" + personRef(f.FatherID)), ResourceID: personRef(f.FatherID)},
		Person2: gedcomx.ResourceReference{Resource: s.url("/persons/" + personRef(f.MotherID)), ResourceID: personRef(f.MotherID)},
		Facts:   facts,
		Sources: sources,
		Media:   media,
		// RS spec Section 4.21.4, "Transitions": collection | Collection
		// State | Link to the collection that contains this relationship.
		Links: gedcomx.Links{
			"relationship": {Href: s.url("/relationships/" + id)},
			"collection":   {Href: s.collectionBaseURL},
		},
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
		Links: gedcomx.Links{
			"relationship": {Href: s.url("/relationships/" + id)},
			"collection":   {Href: s.collectionBaseURL},
		},
	}
}

// --- Places ---

func (s *Server) buildPlaceDescription(p rmdb.Place) (gedcomx.PlaceDescription, error) {
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

	// PlaceDescription is a Subject too (see SCOPE.md's "Sources versus
	// media" section) -- a place can have its own bibliographic sources
	// (e.g. a gazetteer or authority citing this place's exact
	// definition) as well as its own attached media (a map, a photo of
	// the location), same as Person/Relationship/Event.
	sources, media, err := s.buildSourcesAndMedia(rmdb.OwnerTypePlace, p.PlaceID)
	if err != nil {
		return gedcomx.PlaceDescription{}, err
	}
	pd.Sources = sources
	pd.Media = media

	return pd, nil
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
	if citation == "" {
		// citations is REQUIRED (at least one) per the SourceDescription
		// data type -- fall back to something rather than emit an empty list.
		citation = strings.TrimSpace(src.Name)
	}
	if citation == "" {
		citation = fmt.Sprintf("RootsMagic source %d", src.SourceID)
	}
	sd.Citations = []gedcomx.SourceCitation{{Value: citation}}
	if src.Comments != "" {
		sd.Notes = []gedcomx.Note{{Text: src.Comments}}
	}
	return sd
}

// --- Artifacts (multimedia) ---

// buildArtifactDescription converts a RootsMagic MultimediaTable row into a
// SourceDescription with resourceType DigitalArtifact, per RS spec Section
// 4.3.3 ("A list of instances of the SourceDescription Data Type... MUST
// be provided" for the Artifacts state).
//
// Two real-world cases, both observed in actual RootsMagic files during
// development (see SCOPE.md's "Multimedia" section):
//
//   - A genuine local file: `about` and the content link point at
//     GET /artifacts/{id}/content, which streams the actual bytes (see
//     handleArtifactContent). mediaType is inferred from the filename.
//   - A web-hint / external reference (MediaPath already looks like a URL,
//     e.g. from an online-search integration): this server can't reliably
//     resolve or serve it (see rmdb.LooksLikeExternalReference), so no
//     `about`/content link is set, and a note explains why -- rather than
//     presenting a broken link as if it worked.
func (s *Server) buildArtifactDescription(item rmdb.MultimediaItem) gedcomx.SourceDescription {
	id := mediaRef(item.MediaID)
	sd := gedcomx.SourceDescription{
		ID:           id,
		ResourceType: gedcomx.ResourceTypeDigitalArtifact,
		Links:        gedcomx.Links{"description": {Href: s.url("/artifacts/" + id)}},
	}
	if item.Caption != "" {
		sd.Titles = []gedcomx.TextValue{{Value: item.Caption}}
	}

	citation := strings.TrimSpace(strings.Join(nonEmpty(item.Caption, item.RefNumber, item.MediaFile), " -- "))
	if citation == "" {
		citation = fmt.Sprintf("RootsMagic multimedia item %d", item.MediaID)
	}
	sd.Citations = []gedcomx.SourceCitation{{Value: citation}}

	if item.Description != "" {
		sd.Notes = append(sd.Notes, gedcomx.Note{Text: item.Description})
	}

	if rmdb.LooksLikeExternalReference(item.MediaPath) {
		sd.Notes = append(sd.Notes, gedcomx.Note{
			Text: "This item references an external location (" + item.MediaPath +
				") rather than a local file; this server can't resolve or serve its bytes.",
		})
		return sd
	}

	sd.MediaType = gedcomx.MediaTypeForFilename(item.MediaFile)
	contentURL := s.url("/artifacts/" + id + "/content")
	sd.About = contentURL
	sd.Links["digital-artifact"] = gedcomx.Link{Href: contentURL, Type: sd.MediaType}
	return sd
}

// --- Events ---

// buildEvent assembles a full gedcomx.Event from a RootsMagic EventTable
// row -- distinct from the person/relationship-scoped Fact built from the
// very same row by buildFact (see SCOPE.md's "Events" section for the
// spec basis of that distinction: GEDCOM X Section 2.5.2 says the two are
// "described independently"). Participant roles come from two sources:
// the event's own owner (always given the Principal role: the one person
// for a person-owned fact, or both known parents for a family-owned one
// like a marriage) and RootsMagic's WitnessTable (whatever role the user
// assigned via RoleTable, resolved through gedcomx.EventRoleType).
func (s *Server) buildEvent(e rmdb.Event) (gedcomx.Event, error) {
	ft := s.factTypes[e.EventType]
	id := factRef(e.EventID)
	ev := gedcomx.Event{
		ID:    id,
		Type:  gedcomx.EventType(ft.GedcomTag, ft.Name),
		Links: gedcomx.Links{"event": {Href: s.url("/events/" + id)}},
	}
	if d := gedcomx.ParseRMDate(e.Date); d != nil {
		ev.Date = d
	}
	if e.PlaceID != 0 {
		pref, err := s.buildPlaceReference(e.PlaceID)
		if err != nil {
			return gedcomx.Event{}, err
		}
		ev.Place = pref
	}

	var notes []gedcomx.Note
	if e.Note != "" {
		notes = append(notes, gedcomx.Note{Text: e.Note})
	}

	var roles []gedcomx.EventRole
	switch e.OwnerType {
	case rmdb.OwnerTypePerson:
		roles = append(roles, s.principalRole(e.OwnerID))
	case rmdb.OwnerTypeFamily:
		fam, err := s.db.GetFamily(e.OwnerID)
		if err != nil {
			return gedcomx.Event{}, err
		}
		if fam != nil {
			if fam.FatherID != 0 {
				roles = append(roles, s.principalRole(fam.FatherID))
			}
			if fam.MotherID != 0 {
				roles = append(roles, s.principalRole(fam.MotherID))
			}
		}
	}

	witnesses, err := s.db.GetWitnesses(e.EventID)
	if err != nil {
		return gedcomx.Event{}, err
	}
	var unlisted []string
	for _, w := range witnesses {
		roleName := s.roles[w.RoleID].RoleName
		if w.PersonID == 0 {
			// Not a person recorded in this database -- RootsMagic stores
			// their name as free text instead (WitnessTable.Given/Surname).
			// EventRole.person is REQUIRED and MUST resolve to a real
			// Person resource, which this witness structurally can't
			// satisfy -- inventing one would misrepresent what's actually
			// in the source database. Rather than silently drop the
			// information, it's preserved in a note instead. See
			// SCOPE.md's "Events" section.
			name := strings.TrimSpace(w.Given + " " + w.Surname)
			if name == "" {
				name = fmt.Sprintf("witness %d", w.WitnessID)
			}
			// Role is always shown when set -- RoleTable is the correct,
			// intentional place for a short categorical label (e.g.
			// "Bridesmaid"), including custom roles a user added
			// specifically for this (RootsMagic's Note field is a
			// multi-line free-text area, not meant for this, and
			// shouldn't be treated as if it were an alternative way to
			// set the role). If Note is ALSO present -- genuine
			// supplementary commentary, not a role label -- it's appended
			// separately rather than folded into or overriding the role,
			// mirroring how EventRole.details works for witnesses who ARE
			// a Person in this database (see below): the role type and
			// its free-text details are always two distinct pieces of
			// information, never one replacing the other.
			if roleName != "" {
				name += " (" + roleName + ")"
			}
			if note := strings.TrimSpace(w.Note); note != "" {
				name += ": " + note
			}
			unlisted = append(unlisted, name)
			continue
		}
		roles = append(roles, gedcomx.EventRole{
			Person: gedcomx.ResourceReference{
				Resource:   s.url("/persons/" + personRef(w.PersonID)),
				ResourceID: personRef(w.PersonID),
			},
			Type:    gedcomx.EventRoleType(roleName),
			Details: w.Note,
		})
	}
	if len(unlisted) > 0 {
		notes = append(notes, gedcomx.Note{
			Text: "Additional participants recorded by name only, not as persons in this database: " + strings.Join(unlisted, "; "),
		})
	}
	ev.Roles = roles
	ev.Notes = notes

	sources, media, err := s.buildSourcesAndMedia(rmdb.OwnerTypeEvent, e.EventID)
	if err != nil {
		return gedcomx.Event{}, err
	}
	ev.Sources = sources
	ev.Media = media

	return ev, nil
}

// principalRole builds the EventRole for an event's own subject: the
// Person it belongs to (for a person-owned fact), or one parent of the
// Family it belongs to (for a family-owned fact like a marriage).
func (s *Server) principalRole(personID int64) gedcomx.EventRole {
	id := personRef(personID)
	return gedcomx.EventRole{
		Person: gedcomx.ResourceReference{Resource: s.url("/persons/" + id), ResourceID: id},
		Type:   "http://gedcomx.org/Principal",
	}
}
