package api

import (
	"net/http"

	"github.com/aviast/rmgedcomx/internal/gedcomx"
	"github.com/aviast/rmgedcomx/internal/rmdb"
)

// handleArtifactSubjects implements GET /artifacts/{id}/subjects --
// rmgedcomx's own non-spec reverse-lookup extension (see
// gedcomx.SubjectReference's own doc comment for why this exists at all).
// Returns every Person, Relationship, Event, and PlaceDescription that
// references this artifact, directly or via a citation.
func (s *Server) handleArtifactSubjects(w http.ResponseWriter, r *http.Request) {
	s.handleArtifactReverseLookup(w, r, nil)
}

// handleArtifactPersons implements GET /artifacts/{id}/persons -- the
// same reverse lookup as handleArtifactSubjects, filtered to Person only.
func (s *Server) handleArtifactPersons(w http.ResponseWriter, r *http.Request) {
	ot := rmdb.OwnerTypePerson
	s.handleArtifactReverseLookup(w, r, &ot)
}

// handleArtifactEvents implements GET /artifacts/{id}/events -- the same
// reverse lookup as handleArtifactSubjects, filtered to Event only.
func (s *Server) handleArtifactEvents(w http.ResponseWriter, r *http.Request) {
	ot := rmdb.OwnerTypeEvent
	s.handleArtifactReverseLookup(w, r, &ot)
}

// handleArtifactRelationships implements GET /artifacts/{id}/relationships
// -- the same reverse lookup as handleArtifactSubjects, filtered to
// Relationship only.
func (s *Server) handleArtifactRelationships(w http.ResponseWriter, r *http.Request) {
	ot := rmdb.OwnerTypeFamily
	s.handleArtifactReverseLookup(w, r, &ot)
}

func (s *Server) handleArtifactReverseLookup(w http.ResponseWriter, r *http.Request, filterOwnerType *int) {
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

	owners, err := s.db.OwnersOfMedia(mid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	refs := make([]gedcomx.SubjectReference, 0, len(owners))
	for _, o := range owners {
		if filterOwnerType != nil && o.OwnerType != *filterOwnerType {
			continue
		}
		if ref, ok := s.buildSubjectReference(o); ok {
			refs = append(refs, ref)
		}
	}

	writeJSON(w, http.StatusOK, gedcomx.SubjectReferencesDocument{References: refs})
}

// buildSubjectReference maps a resolved rmdb.SubjectRef to the
// corresponding API resource reference. The four cases here are exactly
// the four OwnerType* values rmdb.OwnersOfMedia ever returns (see its own
// comment) -- the default case is unreachable in practice, not a
// silently-ignored gap, but returned as ok=false rather than a panic in
// case that invariant is ever violated by a future change to
// OwnersOfMedia.
func (s *Server) buildSubjectReference(o rmdb.SubjectRef) (gedcomx.SubjectReference, bool) {
	switch o.OwnerType {
	case rmdb.OwnerTypePerson:
		id := personRef(o.OwnerID)
		return gedcomx.SubjectReference{ResourceType: gedcomx.ResourceTypePerson, ID: id, Href: s.url("/persons/" + id)}, true
	case rmdb.OwnerTypeFamily:
		id := coupleRef(o.OwnerID)
		return gedcomx.SubjectReference{ResourceType: gedcomx.ResourceTypeRelationship, ID: id, Href: s.url("/relationships/" + id)}, true
	case rmdb.OwnerTypeEvent:
		// factRef, not a differently-named "eventRef": Fact and Event
		// deliberately share the same id scheme, see SCOPE.md's "Events"
		// section.
		id := factRef(o.OwnerID)
		return gedcomx.SubjectReference{ResourceType: gedcomx.ResourceTypeEvent, ID: id, Href: s.url("/events/" + id)}, true
	case rmdb.OwnerTypePlace:
		id := placeRef(o.OwnerID)
		return gedcomx.SubjectReference{ResourceType: gedcomx.ResourceTypePlaceDescription, ID: id, Href: s.url("/places/" + id)}, true
	default:
		return gedcomx.SubjectReference{}, false
	}
}
