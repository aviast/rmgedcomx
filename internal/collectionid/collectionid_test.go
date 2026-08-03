package collectionid

import (
	"reflect"
	"testing"
)

func TestDerive(t *testing.T) {
	cases := []struct {
		name           string
		rootPersonName string
		dbPath         string
		wantID         string
		wantTitle      string
	}{
		{
			name:           "person and filename both available",
			rootPersonName: "Victoria Hanover",
			dbPath:         "/home/claude/royal92_final.rmtree",
			wantID:         "victoria-hanover-royal92-final",
			wantTitle:      "Victoria Hanover (royal92_final)",
		},
		{
			name:           "windows-style path on a non-windows host",
			rootPersonName: "Jane Smith",
			dbPath:         `G:\My Drive\Genealogy\Family Tree\Smith.rmtree`,
			wantID:         "jane-smith-smith",
			wantTitle:      "Jane Smith (Smith)",
		},
		{
			name:           "backup filename disambiguates two snapshots of the same tree",
			rootPersonName: "Victoria Hanover",
			dbPath:         `G:\My Drive\Royal Genealogy\00 - Backups\royal92 - 2024 06 24 09-29.rmtree`,
			wantID:         "victoria-hanover-royal92-2024-06-24-09-29",
			wantTitle:      "Victoria Hanover (royal92 - 2024 06 24 09-29)",
		},
		{
			name:           "no root person determinable, falls back to filename",
			rootPersonName: "",
			dbPath:         "/data/royal92.rmtree",
			wantID:         "royal92",
			wantTitle:      "royal92",
		},
		{
			name:           "surname-only root person",
			rootPersonName: "Smith",
			dbPath:         "/data/tree.rmtree",
			wantID:         "smith-tree",
			wantTitle:      "Smith (tree)",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, title := Derive(c.rootPersonName, c.dbPath)
			if id != c.wantID {
				t.Errorf("id = %q, want %q", id, c.wantID)
			}
			if title != c.wantTitle {
				t.Errorf("title = %q, want %q", title, c.wantTitle)
			}
		})
	}
}

func TestDeriveNoUsableInput(t *testing.T) {
	// A path with no filename at all and no root person name -- shouldn't
	// happen in practice (dbPath always comes from a real -db flag), but
	// the fallback must still produce a non-empty id rather than panic or
	// return "".
	id, _ := Derive("", "")
	if id == "" {
		t.Error("expected a non-empty fallback id")
	}
}

func TestDedupe(t *testing.T) {
	in := []string{"hanover", "windsor", "hanover", "hanover", "royal92"}
	want := []string{"hanover", "windsor", "hanover-2", "hanover-3", "royal92"}
	got := Dedupe(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dedupe(%v) = %v, want %v", in, got, want)
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Victoria Hanover", "victoria-hanover"},
		{"royal92 - 2024 06 24 09-29", "royal92-2024-06-24-09-29"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
