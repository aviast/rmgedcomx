package rmdb

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNotFound is returned by update operations when the target row doesn't
// exist -- callers map it to a 404.
var ErrNotFound = errors.New("not found")

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
func (db *DB) UpdatePlace(id int64, u PlaceUpdate) error {
	var sets []string
	var args []any
	if u.Name != nil {
		sets = append(sets, "Name = ?")
		args = append(args, *u.Name)
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
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing update to source %d: %w", id, err)
	}
	return nil
}
