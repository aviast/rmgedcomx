package gedcomx

// AtomFeed is the "application/x-gedcomx-atom+json" root object -- the
// GEDCOM X Atom Extensions specification's own JSON representation
// (Section 3) of an "atom:feed" element (RFC 4287, Section 4.1.1), used
// by the Person Search Results state (RS spec Section 4.11) and other
// search/results states.
//
// ID/Title/Updated have no `omitempty` -- checked directly against RFC
// 4287's own RELAX NG grammar for atomFeed (atomId & atomTitle &
// atomUpdated, none of them optional or repeatable) before deciding
// this, not assumed: each is REQUIRED, exactly once.
type AtomFeed struct {
	ID      string      `json:"id"`
	Title   string      `json:"title"`
	Updated int64       `json:"updated"`           // milliseconds since the Unix epoch -- GEDCOM X Atom Extensions spec, Section 3: "the JSON format provides the date as a number indicating the number of milliseconds since January 1, 1970"
	Index   int         `json:"index,omitempty"`   // gx:index (Atom Extensions Section 2.1.1) -- first entry's index in a page of results
	Results int         `json:"results,omitempty"` // gx:results (Section 2.1.2) -- total number of available results
	Links   Links       `json:"links,omitempty"`
	Entries []AtomEntry `json:"entries,omitempty"`
}

// AtomEntry is the JSON representation of a single "atom:entry" element
// (RFC 4287, Section 4.1.2) within an AtomFeed. Same ID/Title/Updated
// requiredness as AtomFeed, for the same reason (atomEntry's own RELAX
// NG grammar: atomId & atomTitle & atomUpdated, all required).
type AtomEntry struct {
	ID         string       `json:"id"`
	Title      string       `json:"title"`
	Updated    int64        `json:"updated"`
	Score      *float64     `json:"score,omitempty"`      // gx:score (Atom Extensions Section 2.2.1) -- OPTIONAL relevance ranking; omitted rather than guessed at, since this server does no relevance ranking of its own (see the search implementation's own comment)
	Confidence *int         `json:"confidence,omitempty"` // gx:confidence (Section 2.2.2) -- OPTIONAL, an integer 1-5; omitted for the same reason as Score
	Links      Links        `json:"links,omitempty"`
	Content    *AtomContent `json:"content,omitempty"`
}

// AtomContent is the JSON representation of an "atom:content" element
// restricted to GEDCOM X Atom Extensions' own content processing model
// (Section 2.3.1/3.2): "the content of an entry MAY have at most a
// single member, 'gedcomx', to supply the genealogical data associated
// with the entry." Which concrete GEDCOM X document shape that member
// holds depends on which search state produced it -- Person Search
// Results (Section 4.11.3) requires "at least one Person"; Place Search
// Results (Section 4.17.3) requires "at least one PlaceDescription,"
// with the "main" one first if there's more than one. GedcomX is typed
// `any` rather than a single concrete document type so both reuse the
// exact same document shape every other Person- or PlaceDescription-
// returning endpoint already produces (PersonDocument,
// PlaceDescriptionsDocument) instead of each search state inventing its
// own narrower type for data this server already has a real
// representation of.
type AtomContent struct {
	GedcomX any `json:"gedcomx,omitempty"`
}
