package api

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/aviast/rmgedcomx/internal/gedcomx"
	"github.com/aviast/rmgedcomx/internal/rmdb"
)

// handlePlaceSearch implements the Place Search Results state (RS spec
// Section 4.17) -- GET, the one operation that state defines. Media
// type and content-negotiation handling are identical to
// handlePersonSearch's own (gedcomXAtomMediaType, acceptsGedcomXAtomJSON)
// -- checked directly, Section 4.17.1 states the identical requirement
// as Person Search Results' own Section 4.11.1 -- so this handler is
// exempted from the global withContentNegotiation middleware the same
// way (server.go).
//
// Only a single "name" search parameter is supported -- see
// rmdb.SearchPlaces's own comment for why: the RS spec's "q" template
// variable documentation (Section 5.3) defines search parameters
// exclusively for persons, with no equivalent table for places at all,
// so "name" (matching PlaceDescription's own, essentially only
// searchable attribute, and Person Search Results' own "name"
// parameter name for the same underlying concept) is the one
// reasonable choice available without inventing spec text that doesn't
// exist.
func (s *Server) handlePlaceSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Vary", "Accept")
	if !acceptsGedcomXAtomJSON(r.Header.Get("Accept")) {
		writeError(w, http.StatusNotAcceptable,
			"this endpoint only produces "+gedcomXAtomMediaType+"; none of the media types in your Accept header can be satisfied")
		return
	}
	w.Header().Set("Content-Type", gedcomXAtomMediaType)

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, `missing required "q" search query parameter (RS spec Section 5.3)`)
		return
	}
	terms, err := parseSearchQuery(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var name *rmdb.SearchTextCriterion
	for _, term := range terms {
		if term.Field != "name" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s: unrecognized search field (Place Search Results only supports \"name\")", term.Field))
			return
		}
		name = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
	}

	limit, offset := s.pagingParams(r)
	rows, total, err := s.db.SearchPlaces(name, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	entries := make([]gedcomx.AtomEntry, 0, len(rows))
	for _, rp := range rows {
		pd, err := s.buildPlaceDescription(rp)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		updated, err := s.db.GetPlaceUTCModDate(rp.PlaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		title := pd.ID
		if len(pd.Names) > 0 {
			title = pd.Names[0].Value
		}
		entries = append(entries, gedcomx.AtomEntry{
			ID:      pd.ID,
			Title:   title,
			Updated: updated,
			// "description", not "person" -- RS spec Section 4.17.4,
			// "Transitions": the one rel that section itself defines,
			// "Transition from the search results to the descriptions
			// of the places in the results."
			Links: gedcomx.Links{"description": {Href: s.url("/places/" + pd.ID)}},
			Content: &gedcomx.AtomContent{GedcomX: &gedcomx.PlaceDescriptionsDocument{
				Results: 1,
				Places:  []gedcomx.PlaceDescription{pd},
			}},
		})
	}

	status := http.StatusOK
	if len(entries) == 0 {
		status = http.StatusNoContent
	}
	writeJSON(w, status, gedcomx.AtomFeed{
		ID:      s.url("/places/search") + "?q=" + url.QueryEscape(q),
		Title:   fmt.Sprintf("Place Search Results for %q", q),
		Updated: time.Now().UnixMilli(),
		Index:   offset,
		Results: total,
		Links: gedcomx.Links{
			"collection": {Href: s.collectionBaseURL},
		},
		Entries: entries,
	})
}
