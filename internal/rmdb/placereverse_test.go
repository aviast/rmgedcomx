package rmdb

import "testing"

// These cases are real values from royal92.rmtree's PlaceTable, not
// invented examples: Name and the Reverse RootsMagic itself had already
// computed for it (at GEDCOM import time), pulled directly from the file
// to confirm this function reproduces RootsMagic's own behavior exactly.
func TestComputePlaceReverse(t *testing.T) {
	cases := []struct{ name, want string }{
		{"Belgrade", "Belgrade"},
		{"Belgrade, Serbia", "Serbia, Belgrade"},
		{"Kensington, Palace, London, England", "England, London, Palace, Kensington"},
		{"Osborne House, Isle of Wight, England", "England, Isle of Wight, Osborne House"},
		{"Royal Mausoleum, Frogmore, Berkshire, England", "England, Berkshire, Frogmore, Royal Mausoleum"},
		{"Schloss Rosenau, Near Coburg, Germany", "Germany, Near Coburg, Schloss Rosenau"},
		{"Friedrichshof, Near, Kronberg, Taunus", "Taunus, Kronberg, Near, Friedrichshof"},
	}
	for _, c := range cases {
		if got := ComputePlaceReverse(c.name); got != c.want {
			t.Errorf("ComputePlaceReverse(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
