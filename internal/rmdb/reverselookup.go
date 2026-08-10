package rmdb

// SubjectRef identifies one Subject-type resource (Person, Relationship,
// Event, or PlaceDescription -- the four GEDCOM X data types that extend
// Subject, see SCOPE.md's "Sources versus media" section) that references
// a given artifact or source, found via OwnersOfMedia below.
//
// OwnerType uses the same OwnerType* constants as everywhere else in this
// package (OwnerTypePerson, OwnerTypeFamily, OwnerTypeEvent,
// OwnerTypePlace) -- deliberately never OwnerTypeSource, OwnerTypeCitation,
// or OwnerTypeName: those aren't Subject types this API exposes a
// resource for, so OwnersOfMedia resolves or drops them before a
// SubjectRef is ever created (see its own comment for exactly how).
type SubjectRef struct {
	OwnerType int
	OwnerID   int64
}

// OwnersOfMedia finds every Subject (Person, Relationship, Event, or
// PlaceDescription) that references a given artifact -- the reverse of
// buildSourcesAndMedia's own forward traversal (an owner -> its
// sources/media), and structurally the same two-hop walk, just backwards:
//
//  1. Direct links: MediaLinkTable rows naming this media id directly.
//  2. Via-citation links: MediaLinkTable rows where the "owner" is
//     actually a Citation (OwnerType = OwnerTypeCitation) -- meaning this
//     media is attached to that citation, not directly to a Subject.
//     Each such citation is then looked up in CitationLinkTable to find
//     which Subject(s) actually cite it. This mirrors the dominant
//     real-world pattern already documented for the forward direction
//     (most media in a real file is attached at the citation level, not
//     directly to the person/event it documents) -- see SCOPE.md's
//     "Multimedia" section.
//
// Two kinds of owner get special handling before a result is returned,
// rather than being passed through as-is:
//
//   - OwnerTypeName: a name record isn't a Subject with its own resource
//     in this API -- it's a sub-part of a Person (see gedcomx.Name). If
//     media is attached to a name directly, it's resolved up to that
//     name's owning Person (NameTable.OwnerID, confirmed against real
//     data to be the owning PersonID despite the generic column name --
//     NameTable has no separate OwnerType column of its own, since a name
//     is always Person-owned). An orphaned name reference (the NameID no
//     longer exists) is skipped rather than erroring the whole request.
//   - OwnerTypeSource: media attached directly to a bibliographic source
//     record itself, not to anything this API exposes a "media" field
//     for (SourceDescription doesn't extend Subject -- see SCOPE.md).
//     Dropped, not surfaced as a broken or mistyped reference; a source
//     citing itself as its own subject wouldn't mean anything.
//
// Deduplicated: the same Subject can end up referenced more than once
// (e.g. a family cites the same source via two different citations), and
// only appears once in the result either way.
func (db *DB) OwnersOfMedia(mediaID int64) ([]SubjectRef, error) {
	rows, err := db.sql.Query("SELECT OwnerType, OwnerID FROM MediaLinkTable WHERE MediaID = ?", mediaID)
	if err != nil {
		return nil, err
	}
	var direct []SubjectRef
	var citationIDs []int64
	for rows.Next() {
		var ownerType int
		var ownerID int64
		if err := rows.Scan(&ownerType, &ownerID); err != nil {
			rows.Close()
			return nil, err
		}
		if ownerType == OwnerTypeCitation {
			citationIDs = append(citationIDs, ownerID)
		} else {
			direct = append(direct, SubjectRef{OwnerType: ownerType, OwnerID: ownerID})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for _, cid := range citationIDs {
		citeRows, err := db.sql.Query("SELECT OwnerType, OwnerID FROM CitationLinkTable WHERE CitationID = ?", cid)
		if err != nil {
			return nil, err
		}
		for citeRows.Next() {
			var ownerType int
			var ownerID int64
			if err := citeRows.Scan(&ownerType, &ownerID); err != nil {
				citeRows.Close()
				return nil, err
			}
			direct = append(direct, SubjectRef{OwnerType: ownerType, OwnerID: ownerID})
		}
		if err := citeRows.Err(); err != nil {
			citeRows.Close()
			return nil, err
		}
		citeRows.Close()
	}

	seen := map[SubjectRef]bool{}
	var resolved []SubjectRef
	add := func(ref SubjectRef) {
		if !seen[ref] {
			seen[ref] = true
			resolved = append(resolved, ref)
		}
	}
	for _, o := range direct {
		switch o.OwnerType {
		case OwnerTypePerson, OwnerTypeFamily, OwnerTypeEvent, OwnerTypePlace:
			add(o)
		case OwnerTypeName:
			var personID int64
			err := db.sql.QueryRow("SELECT OwnerID FROM NameTable WHERE NameID = ?", o.OwnerID).Scan(&personID)
			if err != nil {
				continue // orphaned name reference -- skip, don't fail the whole request over it
			}
			add(SubjectRef{OwnerType: OwnerTypePerson, OwnerID: personID})
		default:
			// OwnerTypeSource, or anything else not exposed as a
			// Subject in this API -- deliberately dropped.
		}
	}
	return resolved, nil
}
