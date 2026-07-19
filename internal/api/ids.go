package api

import (
	"fmt"
	"strconv"
	"strings"
)

// Resource IDs exposed by this API are prefixed by resource kind so that
// they're unambiguous and self-describing on their own (e.g. in a "sources"
// list mixed with other resource references). They map 1:1 onto RootsMagic
// primary keys.

func personRef(id int64) string       { return fmt.Sprintf("P%d", id) }
func nameRef(id int64) string         { return fmt.Sprintf("N%d", id) }
func factRef(id int64) string         { return fmt.Sprintf("E%d", id) }
func placeRef(id int64) string        { return fmt.Sprintf("PL%d", id) }
func sourceRef(id int64) string       { return fmt.Sprintf("S%d", id) }
func coupleRef(familyID int64) string { return fmt.Sprintf("F%d", familyID) }
func parentChildRef(familyID, childID int64, isFather bool) string {
	if isFather {
		return fmt.Sprintf("F%d-FC%d", familyID, childID)
	}
	return fmt.Sprintf("F%d-MC%d", familyID, childID)
}

func parsePersonID(s string) (int64, error) { return parsePrefixedID(s, "P") }
func parsePlaceID(s string) (int64, error)  { return parsePrefixedID(s, "PL") }
func parseSourceID(s string) (int64, error) { return parsePrefixedID(s, "S") }

func parsePrefixedID(s, prefix string) (int64, error) {
	if !strings.HasPrefix(s, prefix) {
		return 0, fmt.Errorf("invalid id %q: expected prefix %q", s, prefix)
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(s, prefix), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id %q: %w", s, err)
	}
	return n, nil
}

// parsedRelationshipID is the decoded form of a relationship resource ID.
type parsedRelationshipID struct {
	FamilyID int64
	Kind     string // "couple" or "parent-child"
	ChildID  int64  // set when Kind == "parent-child"
	IsFather bool   // set when Kind == "parent-child"
}

// parseRelationshipID parses a relationship ID of the form "F{familyID}",
// "F{familyID}-FC{childID}" (father-child), or "F{familyID}-MC{childID}"
// (mother-child).
func parseRelationshipID(s string) (*parsedRelationshipID, error) {
	if !strings.HasPrefix(s, "F") {
		return nil, fmt.Errorf("invalid relationship id %q", s)
	}
	rest := strings.TrimPrefix(s, "F")

	if idx := strings.Index(rest, "-FC"); idx >= 0 {
		familyID, err := strconv.ParseInt(rest[:idx], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid relationship id %q: %w", s, err)
		}
		childID, err := strconv.ParseInt(rest[idx+3:], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid relationship id %q: %w", s, err)
		}
		return &parsedRelationshipID{FamilyID: familyID, Kind: "parent-child", ChildID: childID, IsFather: true}, nil
	}
	if idx := strings.Index(rest, "-MC"); idx >= 0 {
		familyID, err := strconv.ParseInt(rest[:idx], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid relationship id %q: %w", s, err)
		}
		childID, err := strconv.ParseInt(rest[idx+3:], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid relationship id %q: %w", s, err)
		}
		return &parsedRelationshipID{FamilyID: familyID, Kind: "parent-child", ChildID: childID, IsFather: false}, nil
	}

	familyID, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid relationship id %q: %w", s, err)
	}
	return &parsedRelationshipID{FamilyID: familyID, Kind: "couple"}, nil
}
