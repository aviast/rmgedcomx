package rmdb

import "strings"

// ComputePlaceReverse computes PlaceTable.Reverse from a place name: the
// data dictionary documents this as "the reverse order of the
// comma-delimited fields in PlaceTable.Name," automatically recomputed by
// RootsMagic's own UI whenever the name changes.
//
// Verified directly against real, already-computed Reverse values in
// royal92.rmtree (not just implemented from the data dictionary's prose
// description) -- e.g. "Kensington, Palace, London, England" has a stored
// Reverse of "England, London, Palace, Kensington", which this function
// reproduces exactly, including for names with internal spaces within a
// single component (e.g. "Schloss Rosenau, Near Coburg, Germany").
func ComputePlaceReverse(name string) string {
	parts := strings.Split(name, ",")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
