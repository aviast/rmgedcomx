package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aviast/rmgedcomx/internal/gedcomx"
	"github.com/aviast/rmgedcomx/internal/rmdb"
)

// handleCreateRelationships implements the Relationships state's POST
// operation (RS spec Section 4.20.2: "Create a relationship or set of
// relationships", OPTIONAL) -- only registered when this collection's
// database is writable.
//
// Supports both relationship types this server's read side already
// models: `http://gedcomx.org/Couple` (creates a new RootsMagic
// "family") and `http://gedcomx.org/ParentChild` (links an existing
// person as a child of an existing parent's family, creating a
// single-parent family first if the parent doesn't have one yet in the
// matching role) -- see rmdb.CreateCoupleRelationship's and
// rmdb.CreateParentChildRelationship's own comments for the full
// mechanics and the deliberate scope limits on each, and SCOPE.md's
// "Stage 3" section for the account of why ParentChild specifically
// can't simply take a bare (parent, child) pair at face value.
//
// Per RS spec Section 4.20.2, mirroring Persons' own POST (Section
// 4.9.2): `201` + `Location` when exactly one relationship was created,
// `204` when several were.
func (s *Server) handleCreateRelationships(w http.ResponseWriter, r *http.Request) {
	var body gedcomx.RelationshipsDocument
	if err := decodeStrictJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(body.Relationships) == 0 {
		writeError(w, http.StatusBadRequest, "request body must include at least one relationship (RS spec Section 4.20.3)")
		return
	}

	type resolved struct {
		isCouple           bool
		fatherID, motherID int64 // isCouple
		parentID, childID  int64 // !isCouple
		relType            int   // !isCouple; rmdb.RelTypeBirth or rmdb.RelTypeAdopted
	}
	toCreate := make([]resolved, 0, len(body.Relationships))
	for i, rel := range body.Relationships {
		switch rel.Type {
		case gedcomx.RelationshipTypeCouple:
			p1, err := personIDFromReference(rel.Person1)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("relationships[%d].person1: %v", i, err))
				return
			}
			p2, err := personIDFromReference(rel.Person2)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("relationships[%d].person2: %v", i, err))
				return
			}
			fatherID, motherID, err := s.resolveCoupleRoles(p1, p2)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("relationships[%d]: %v", i, err))
				return
			}
			toCreate = append(toCreate, resolved{isCouple: true, fatherID: fatherID, motherID: motherID})
		case gedcomx.RelationshipTypeParentChild:
			parentID, err := personIDFromReference(rel.Person1)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("relationships[%d].person1 (parent): %v", i, err))
				return
			}
			childID, err := personIDFromReference(rel.Person2)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("relationships[%d].person2 (child): %v", i, err))
				return
			}
			toCreate = append(toCreate, resolved{isCouple: false, parentID: parentID, childID: childID, relType: relTypeFromFacts(rel.Facts)})
		default:
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"relationships[%d]: unsupported relationship type %q -- only Couple and ParentChild can be created", i, rel.Type))
			return
		}
	}

	if s.ensureBackupForWrite(w) != nil {
		return
	}

	createdFamilyIDs := make([]int64, 0, len(toCreate))
	for i, rc := range toCreate {
		var familyID int64
		var err error
		if rc.isCouple {
			familyID, err = s.db.CreateCoupleRelationship(rmdb.NewCoupleRelationship{FatherID: rc.fatherID, MotherID: rc.motherID})
		} else {
			familyID, err = s.db.CreateParentChildRelationship(rmdb.NewParentChildRelationship{ParentID: rc.parentID, ChildID: rc.childID, RelType: rc.relType})
		}
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, rmdb.ErrNotFound) || errors.Is(err, rmdb.ErrAmbiguous) {
				status = http.StatusBadRequest
			}
			if len(createdFamilyIDs) > 0 {
				writeError(w, status, fmt.Sprintf(
					"relationships[%d] failed after %d earlier relationship(s) in this request were already created (families %v) -- this request is not all-or-nothing across multiple relationships: %v",
					i, len(createdFamilyIDs), createdFamilyIDs, err))
				return
			}
			writeError(w, status, err.Error())
			return
		}
		createdFamilyIDs = append(createdFamilyIDs, familyID)
	}

	if len(createdFamilyIDs) == 1 {
		w.Header().Set("Location", s.url("/relationships/"+coupleRef(createdFamilyIDs[0])))
		w.WriteHeader(http.StatusCreated)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// relTypeFromFacts determines a ParentChild relationship's RelType (one
// of rmdb.RelType*) from its own Facts, matching GEDCOM X's dedicated
// "Parent-Child Relationship Fact Types"
// (fact-types-specification.md, Section 2.3 -- a separate document from
// the conceptual model spec's own person-scoped fact types; checked
// directly, not assumed, after an earlier draft of this mistakenly
// reached for the person-scoped http://gedcomx.org/Adoption instead).
// Five of that section's ten fact types have a real, direct RootsMagic
// counterpart -- see rmdb's own RelType* constants for the full
// rationale on why the other five aren't mapped. Defaults to
// rmdb.RelTypeBirth (matching RootsMagic's own default for an
// unspecified GEDCOM 5.x PEDIGREE_LINKAGE_TYPE) when none of these are
// present, including when BiologicalParent itself isn't explicitly
// given -- BiologicalParent exists so a client CAN say so explicitly,
// not so every client MUST.
func relTypeFromFacts(facts []gedcomx.Fact) int {
	for _, f := range facts {
		switch f.Type {
		case "http://gedcomx.org/AdoptiveParent":
			return rmdb.RelTypeAdopted
		case "http://gedcomx.org/StepParent":
			return rmdb.RelTypeStep
		case "http://gedcomx.org/FosterParent":
			return rmdb.RelTypeFoster
		case "http://gedcomx.org/GuardianParent":
			return rmdb.RelTypeGuardian
		case "http://gedcomx.org/BiologicalParent":
			return rmdb.RelTypeBirth
		}
	}
	return rmdb.RelTypeBirth
}

// resolveCoupleRoles assigns two person ids to Father/Mother roles by
// their own recorded Sex, rather than trusting person1/person2's order
// (the RS spec's Couple relationship type doesn't itself define which
// of person1/person2 is which role -- see the conceptual model). Sex
// "Unknown" on either person makes the role assignment ambiguous and is
// rejected rather than guessed, the same reasoning
// CreateParentChildRelationship's own comment applies to a parent's
// unknown sex.
func (s *Server) resolveCoupleRoles(p1, p2 int64) (fatherID, motherID int64, err error) {
	person1, err := s.db.GetPerson(p1)
	if err != nil {
		return 0, 0, err
	}
	if person1 == nil {
		return 0, 0, fmt.Errorf("%w: person1 P%d", rmdb.ErrNotFound, p1)
	}
	person2, err := s.db.GetPerson(p2)
	if err != nil {
		return 0, 0, err
	}
	if person2 == nil {
		return 0, 0, fmt.Errorf("%w: person2 P%d", rmdb.ErrNotFound, p2)
	}
	switch {
	case person1.Sex == 0 && person2.Sex == 1:
		return p1, p2, nil
	case person1.Sex == 1 && person2.Sex == 0:
		return p2, p1, nil
	default:
		return 0, 0, fmt.Errorf("%w: can't determine father/mother roles for persons with Sex %d and %d -- exactly one must be Male and one Female", rmdb.ErrAmbiguous, person1.Sex, person2.Sex)
	}
}

// personIDFromReference extracts a Person id from a ResourceReference
// sent in a write request -- prefers resourceId (e.g. "P1") since it's
// simplest and most direct; falls back to parsing the id off the end of
// resource (e.g. ".../persons/P1") for a client that only sent that.
// Mirrors mediaIDFromReference's own fallback pattern.
func personIDFromReference(ref gedcomx.ResourceReference) (int64, error) {
	if ref.ResourceID != "" {
		return parsePersonID(ref.ResourceID)
	}
	if ref.Resource != "" {
		parts := strings.Split(strings.TrimRight(ref.Resource, "/"), "/")
		return parsePersonID(parts[len(parts)-1])
	}
	return 0, fmt.Errorf("reference has neither resourceId nor resource")
}
