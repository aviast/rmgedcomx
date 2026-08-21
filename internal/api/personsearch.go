package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aviast/rmgedcomx/internal/gedcomx"
)

// gedcomXAtomMediaType is the one representation this server produces
// for the Person Search Results state -- checked directly against the
// RS spec before choosing it (Section 4.11.1): "application/x-gedcomx-atom+json"
// is the MUST-support media type; full "application/atom+xml" (RFC
// 4287) is only RECOMMENDED, so -- matching this project's own existing
// choice not to build a second, XML representation for the rest of the
// API (see gedcomXMediaType's own comment) -- only the JSON one is
// implemented here either.
const gedcomXAtomMediaType = "application/x-gedcomx-atom+json"

// acceptsGedcomXAtomJSON mirrors acceptsGedcomXJSON's own logic
// (case-insensitive, parameters ignored, "*/*"/"application/*"/
// "application/json" all accepted) for this endpoint's own, different
// media type -- kept separate from the global withContentNegotiation
// middleware (server.go) rather than folded into it, since every other
// endpoint in this server produces gedcomXMediaType specifically, and
// this is the one exception.
func acceptsGedcomXAtomJSON(accept string) bool {
	accept = strings.TrimSpace(accept)
	if accept == "" {
		return true
	}
	for _, part := range strings.Split(accept, ",") {
		mediaType := part
		if i := strings.IndexByte(mediaType, ';'); i >= 0 {
			mediaType = mediaType[:i]
		}
		switch strings.ToLower(strings.TrimSpace(mediaType)) {
		case "*/*", "application/*", "application/json", gedcomXAtomMediaType:
			return true
		}
	}
	return false
}

// handlePersonSearch implements the Person Search Results state (RS
// spec Section 4.11) -- GET, the one operation that state defines. Not
// registered under the global withContentNegotiation middleware
// (server.go, multi.go): that middleware is specific to
// gedcomXMediaType, this endpoint's own required media type is
// different (gedcomXAtomMediaType, above), so this handler does its own
// Accept-header check and Content-Type instead.
//
// The 10 "direct" search parameters (RS spec Section 6) are supported:
// name, givenName, surname, gender, birthDate, birthPlace, deathDate,
// deathPlace, marriageDate, marriagePlace. The "{relation}"-prefixed
// parameters (father/mother/spouse/parent) are a deliberately separate,
// later piece of work -- rejected outright with a clear error naming
// them specifically, not silently ignored (see buildSearchCriteria's
// own comment).
func (s *Server) handlePersonSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Vary", "Accept")
	if !acceptsGedcomXAtomJSON(r.Header.Get("Accept")) {
		writeError(w, http.StatusNotAcceptable,
			"this endpoint only produces "+gedcomXAtomMediaType+"; none of the media types in your Accept header can be satisfied")
		return
	}
	w.Header().Set("Content-Type", gedcomXAtomMediaType)

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, `missing required "q" search query parameter (RS spec Section 6)`)
		return
	}
	criteria, err := buildSearchCriteria(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	limit, offset := s.pagingParams(r)
	rows, total, err := s.db.SearchPersons(criteria, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	entries := make([]gedcomx.AtomEntry, 0, len(rows))
	for _, rp := range rows {
		p, err := s.buildPerson(rp)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		updated, err := s.db.GetPersonUTCModDate(rp.PersonID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// PersonDocument.Relationships is deliberately never omitempty
		// (see its own comment, internal/gedcomx/model.go) specifically
		// so an empty result isn't mistaken for "not computed" -- an
		// entry's own content here reuses that same document type, so
		// it has to earn that guarantee the same way handlePerson does,
		// by actually computing it, not producing a misleadingly empty
		// array for the sake of avoiding the extra queries.
		rels := []gedcomx.Relationship{}
		parentRels, err := s.personParentRelationships(rp.PersonID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rels = append(rels, parentRels...)
		childRels, err := s.personChildRelationships(rp.PersonID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rels = append(rels, childRels...)
		spouseRels, err := s.personSpouseRelationships(rp.PersonID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rels = append(rels, spouseRels...)

		title := p.ID
		if p.Display != nil && p.Display.Name != "" {
			title = p.Display.Name
		}
		entries = append(entries, gedcomx.AtomEntry{
			ID:      p.ID,
			Title:   title,
			Updated: updated,
			Links:   gedcomx.Links{"person": {Href: s.url("/persons/" + p.ID)}},
			Content: &gedcomx.AtomContent{GedcomX: &gedcomx.PersonDocument{
				Persons:       []gedcomx.Person{p},
				Relationships: rels,
			}},
		})
	}

	status := http.StatusOK
	if len(entries) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.AtomFeed{
		ID:      s.url("/persons/search") + "?q=" + url.QueryEscape(q),
		Title:   fmt.Sprintf("Person Search Results for %q", q),
		Updated: time.Now().UnixMilli(),
		Index:   offset,
		Results: total,
		Links: gedcomx.Links{
			"collection": {Href: s.collectionBaseURL},
		},
		Entries: entries,
	})
}
