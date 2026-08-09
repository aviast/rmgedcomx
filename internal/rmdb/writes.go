package rmdb

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound is returned by update operations when the target row doesn't
// exist -- callers map it to a 404.
var ErrNotFound = errors.New("not found")

// utcModDateExpr computes RootsMagic's own UTCModDate encoding directly in
// SQL, rather than computing a timestamp in Go and converting it -- this
// guarantees exact agreement with RootsMagic's own convention without
// reimplementing Julian date math ourselves (the formula itself was
// confirmed against real RootsMagic data earlier in this project).
const utcModDateExpr = "(julianday('now') - 2415018.5)"

// bumpConfigTableModDate updates ConfigTable's own UTCModDate, confirmed
// by a real captured RootsMagic diff (see SCOPE.md's "Write support"
// section) to be touched on every edit, not just the specific row being
// changed -- RootsMagic maintains this as a database-wide "last modified"
// timestamp, separate from any one row's own UTCModDate.
//
// `WHERE RecID = 1`, not `WHERE RecType = 1` (how this project otherwise
// identifies this row -- see rootperson.go, uniqueid.go), deliberately
// matches RootsMagic's own generated SQL exactly, which uses RecID
// directly; since RecID is ConfigTable's actual declared primary key,
// this is also the more precise target of the two (guaranteed at most
// one row, unlike RecType, which isn't declared unique).
func bumpConfigTableModDate(tx *sql.Tx) error {
	if _, err := tx.Exec("UPDATE ConfigTable SET UTCModDate = " + utcModDateExpr + " WHERE RecID = 1"); err != nil {
		return fmt.Errorf("updating ConfigTable's modification date: %w", err)
	}
	return nil
}

// PlaceUpdate describes a partial update to a place: a nil field means
// "leave this column unchanged," not "clear it." See SCOPE.md's "Write
// support" section for why Stage 1 doesn't yet support explicitly clearing
// a field back to empty, and for why Latitude/Longitude are updated
// together or not at all.
type PlaceUpdate struct {
	Name      *string
	Latitude  *int64 // decimal degrees * 1e7, matching PlaceTable's own encoding
	Longitude *int64
	Note      *string
}

// UpdatePlace applies a partial update to a place. Returns ErrNotFound if
// no place with this id exists.
//
// When Name changes, four more columns change alongside it, all
// confirmed against a real captured RootsMagic diff rather than assumed
// (see SCOPE.md's "Write support" section for the full account):
//
//   - Reverse is recomputed (ComputePlaceReverse), matching RootsMagic's
//     own behavior of keeping this derived field in sync with Name.
//   - fsID and anID -- RootsMagic's own matched FamilySearch/Ancestry
//     place identifiers -- are reset to 0, RootsMagic's own "never looked
//     up" sentinel (confirmed exhaustively: all 922 places in
//     royal92.rmtree, none of which have ever been through RootsMagic's
//     place-standardization workflow, have fsID=0, anID=0, with zero
//     exceptions). This server doesn't call out to FamilySearch's or
//     Ancestry's own place-matching services -- that's a fundamentally
//     different kind of feature (external network dependency, third-party
//     API keys) than "write a field to SQLite," and out of scope
//     deliberately, not by oversight. Leaving a stale match against the
//     *old* name in place would be actively misleading once the name has
//     changed, which is worse than clearing it -- an absent match is
//     honest about what this server actually did (nothing, regarding
//     external verification); a stale one looks like it's still valid
//     when it isn't.
//   - LatLongExact is reset to 0 for the same reason: it isn't documented
//     anywhere (checked the data dictionary directly; it's simply
//     absent), but a real captured RootsMagic diff shows it set to 1
//     alongside a successful fsID/anID match, and every one of
//     royal92.rmtree's 922 places -- none externally verified -- has
//     LatLongExact=0, with zero exceptions. The likely (unconfirmed)
//     meaning is "these coordinates are corroborated by an external
//     authority," which this server has no basis to claim without doing
//     the lookup that would justify it.
//
// Deliberately does NOT touch fsID/anID/LatLongExact when only Note (or
// only coordinates) change, even though a second real captured diff shows
// RootsMagic itself re-running its FamilySearch/Ancestry lookup on *any*
// field edit, not just Name -- reasonable on RootsMagic's side, since a
// fresh match only adds value there. But the reasoning above for clearing
// these fields is specifically that a stale match against the *old name*
// is misleading once the name changes; that reasoning doesn't hold when
// the name hasn't changed at all, so there's no basis here for touching
// them on a Note-only or coordinates-only edit. Diverging from
// RootsMagic's broader real behavior here is a deliberate, narrower
// choice, not an oversight -- see SCOPE.md's "Write support" section.
func (db *DB) UpdatePlace(id int64, u PlaceUpdate) error {
	var sets []string
	var args []any
	if u.Name != nil {
		sets = append(sets, "Name = ?", "Reverse = ?", "fsID = 0", "anID = 0", "LatLongExact = 0")
		args = append(args, *u.Name, ComputePlaceReverse(*u.Name))
	}
	if u.Latitude != nil {
		sets = append(sets, "Latitude = ?")
		args = append(args, *u.Latitude)
	}
	if u.Longitude != nil {
		sets = append(sets, "Longitude = ?")
		args = append(args, *u.Longitude)
	}
	if u.Note != nil {
		sets = append(sets, "Note = ?")
		args = append(args, *u.Note)
	}
	if len(sets) == 0 {
		// Nothing to change -- still confirm the place actually exists,
		// so this isn't silently treated as a no-op success against a
		// nonexistent id.
		p, err := db.GetPlace(id)
		if err != nil {
			return err
		}
		if p == nil {
			return ErrNotFound
		}
		return nil
	}
	sets = append(sets, "UTCModDate = "+utcModDateExpr)
	args = append(args, id)

	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	query := "UPDATE PlaceTable SET " + strings.Join(sets, ", ") + " WHERE PlaceID = ?"
	res, err := tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("updating place %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking update result for place %d: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := bumpConfigTableModDate(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing update to place %d: %w", id, err)
	}
	return nil
}

// SourceUpdate describes a partial update to a source: a nil field means
// "leave this column unchanged." Deliberately doesn't cover
// SourceTable.ActualText/RefNumber (the fields this API's own `citations`
// output is built from) -- see SCOPE.md's "Write support" section for why
// those aren't safely reversible from the single combined string this API
// produces on the way out, and are left for a future revision rather than
// guessed at now.
type SourceUpdate struct {
	Name     *string
	Comments *string
}

// UpdateSource applies a partial update to a source. Returns ErrNotFound
// if no source with this id exists.
//
// Every successful update also sets IsPrivate = 0, unconditionally --
// unlike Place's fsID/anID/LatLongExact (only touched when Name
// specifically changes, since they're tied to that one field), this
// isn't tied to which field changed. The data dictionary documents
// IsPrivate as "not implemented," noting only 0 has ever been observed in
// real files; both sources in royal92.rmtree confirm that. A real
// captured RootsMagic diff for this project showed IsPrivate flipping to
// 1 during a name-only edit, which contradicts that documentation and
// doesn't have an obvious causal explanation -- possibly an artifact of
// that specific edit session rather than deterministic behavior tied to
// the edit itself (see SCOPE.md's "Write support" section). Given a
// field documented as unimplemented, with a real observation that
// contradicts the only documentation available, writing the
// well-evidenced, only-ever-observed value (0) is the more defensible
// choice than reproducing an unexplained one-off.
func (db *DB) UpdateSource(id int64, u SourceUpdate) error {
	var sets []string
	var args []any
	if u.Name != nil {
		sets = append(sets, "Name = ?")
		args = append(args, *u.Name)
	}
	if u.Comments != nil {
		sets = append(sets, "Comments = ?")
		args = append(args, *u.Comments)
	}
	if len(sets) == 0 {
		s, err := db.GetSource(id)
		if err != nil {
			return err
		}
		if s == nil {
			return ErrNotFound
		}
		return nil
	}
	sets = append(sets, "IsPrivate = 0")
	sets = append(sets, "UTCModDate = "+utcModDateExpr)
	args = append(args, id)

	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	query := "UPDATE SourceTable SET " + strings.Join(sets, ", ") + " WHERE SourceID = ?"
	res, err := tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("updating source %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking update result for source %d: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := bumpConfigTableModDate(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing update to source %d: %w", id, err)
	}
	return nil
}

// UpdateArtifactPath updates a multimedia item's stored location.
// realPath must be a real, absolute filesystem path under mediaFolder --
// this server only ever writes RootsMagic's "?" (Media Folder-relative)
// encoding, never "*", "~", or an absolute path (see encodeMediaPath's own
// doc comment, and SCOPE.md's "Write support" section, for why). Returns
// ErrPathNotUnderMediaFolder if realPath isn't under mediaFolder, or
// ErrNotFound if no artifact with this id exists.
func (db *DB) UpdateArtifactPath(id int64, mediaFolder, realPath string) error {
	mediaPath, mediaFile, err := encodeMediaPath(mediaFolder, realPath)
	if err != nil {
		return err
	}

	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		"UPDATE MultimediaTable SET MediaPath = ?, MediaFile = ?, UTCModDate = "+utcModDateExpr+" WHERE MediaID = ?",
		mediaPath, mediaFile, id)
	if err != nil {
		return fmt.Errorf("updating artifact %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking update result for artifact %d: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := bumpConfigTableModDate(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing update to artifact %d: %w", id, err)
	}
	return nil
}

// ErrArtifactNotFound is returned by UpdateOwnerMedia when the desired
// media list references an artifact that doesn't exist -- distinct from
// ErrNotFound (which means the *owner* -- the person, event, etc. --
// doesn't exist) so callers can map the two to the right response: a
// missing owner is a 404 against the URL the client asked for, a missing
// artifact is a 400 against something inside the request body.
var ErrArtifactNotFound = errors.New("artifact not found")

// UpdateOwnerMedia diffs a desired set of media (artifact) ids against
// the MediaLinkTable rows that currently exist for a given owner (a
// person, event, family, etc. -- see the OwnerType* constants), creating
// the ones newly present and removing the ones newly absent. Entries
// present in both are left completely untouched, including columns this
// function doesn't otherwise touch at all (IsPrimary, Include1,
// Comments, ...) -- whatever a human set for an existing link through
// RootsMagic's own UI survives a media-list update unrelated to it.
//
// If the same media id happens to be linked more than once for this
// owner (MediaLinkTable has no uniqueness constraint on
// (MediaID, OwnerType, OwnerID) -- nothing stops this happening, e.g. via
// RootsMagic's own UI), removing that id deletes every matching row, not
// just one, so a removal can't leave an orphaned duplicate behind.
//
// New links are always created with IsPrimary=0 and Include1=0 --
// deliberately, not just as an unconsidered default. IsPrimary
// ("Determines image displayed in reports, the Pedigree view, and the
// People Side View pane") and Include1 ("Include in Scrapbook") are both
// real, documented, user-facing choices in RootsMagic's own UI that this
// server has no basis to assert on someone's behalf -- the same
// reasoning as fsID/anID in UpdatePlace. GEDCOM X has no data type
// conceptually similar to RootsMagic's Scrapbook at all, so this is
// documented as RootsMagic-only functionality this API doesn't expose --
// see SCOPE.md's "Write support" section. A newly-linked artifact simply
// won't appear in the Scrapbook, or be treated as anyone's primary photo,
// until a person sets that manually in RootsMagic itself.
//
// Every other column not mentioned above (Include2-4, Rect*) is
// documented as "Not implemented" in RootsMagic's own data dictionary and
// is left at 0 on new rows for that reason.
//
// Returns ErrArtifactNotFound if any desired media id doesn't exist in
// MultimediaTable -- this server won't create a link to an artifact that
// isn't there.
func (db *DB) UpdateOwnerMedia(ownerType int, ownerID int64, desiredMediaIDs []int64) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	desired := make(map[int64]bool, len(desiredMediaIDs))
	for _, mid := range desiredMediaIDs {
		if desired[mid] {
			continue // duplicate in the request itself; harmless, just don't double-process it
		}
		desired[mid] = true

		var exists int
		err := tx.QueryRow("SELECT 1 FROM MultimediaTable WHERE MediaID = ?", mid).Scan(&exists)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: M%d", ErrArtifactNotFound, mid)
		}
		if err != nil {
			return fmt.Errorf("checking artifact M%d exists: %w", mid, err)
		}
	}

	rows, err := tx.Query("SELECT MediaID FROM MediaLinkTable WHERE OwnerType = ? AND OwnerID = ?", ownerType, ownerID)
	if err != nil {
		return fmt.Errorf("reading existing media links: %w", err)
	}
	existing := map[int64]bool{}
	for rows.Next() {
		var mid int64
		if err := rows.Scan(&mid); err != nil {
			rows.Close()
			return fmt.Errorf("reading existing media links: %w", err)
		}
		existing[mid] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("reading existing media links: %w", err)
	}
	rows.Close()

	for mid := range desired {
		if existing[mid] {
			continue
		}
		_, err := tx.Exec(
			`INSERT INTO MediaLinkTable
			 (MediaID, OwnerType, OwnerID, IsPrimary, Include1, Include2, Include3, Include4,
			  SortOrder, RectLeft, RectTop, RectRight, RectBottom, Comments, UTCModDate)
			 VALUES (?, ?, ?, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, '', `+utcModDateExpr+`)`,
			mid, ownerType, ownerID)
		if err != nil {
			return fmt.Errorf("linking artifact M%d: %w", mid, err)
		}
	}
	for mid := range existing {
		if desired[mid] {
			continue
		}
		if _, err := tx.Exec("DELETE FROM MediaLinkTable WHERE OwnerType = ? AND OwnerID = ? AND MediaID = ?", ownerType, ownerID, mid); err != nil {
			return fmt.Errorf("unlinking artifact M%d: %w", mid, err)
		}
	}

	// Deliberately does NOT bump the owner's own UTCModDate (e.g.
	// PersonTable.UTCModDate) -- unlike Name/Note/coordinate edits, there's
	// no real captured RootsMagic diff yet confirming whether attaching or
	// detaching media touches the owner row's own timestamp the way
	// editing one of its own fields does. Asserting that without evidence
	// would be exactly the kind of unverified claim this project has
	// consistently avoided elsewhere. ConfigTable's own UTCModDate is
	// still bumped, since that's confirmed to happen on every write
	// regardless of what changed.
	if err := bumpConfigTableModDate(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing media link update for %s %d: %w", ownerTypeName(ownerType), ownerID, err)
	}
	return nil
}

func ownerTypeName(ownerType int) string {
	switch ownerType {
	case OwnerTypePerson:
		return "person"
	case OwnerTypeFamily:
		return "family"
	case OwnerTypeEvent:
		return "event"
	case OwnerTypePlace:
		return "place"
	default:
		return fmt.Sprintf("owner type %d", ownerType)
	}
}
