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
			rootPersonName: "Jean Valerie Gould",
			dbPath:         "/home/claude/gould_final.rmtree",
			wantID:         "jean-valerie-gould-gould-final",
			wantTitle:      "Jean Valerie Gould (gould_final)",
		},
		{
			name:           "windows-style path on a non-windows host",
			rootPersonName: "Charlotte Mae Henry",
			dbPath:         `G:\My Drive\Genealogy\2 - PAF\Family.rmtree`,
			wantID:         "charlotte-mae-henry-family",
			wantTitle:      "Charlotte Mae Henry (Family)",
		},
		{
			name:           "backup filename disambiguates two snapshots of the same tree",
			rootPersonName: "Jean Valerie Gould",
			dbPath:         `G:\My Drive\Gould Genealogy\00 - FindMyPast\Gould - 2024 06 24 09-29.rmtree`,
			wantID:         "jean-valerie-gould-gould-2024-06-24-09-29",
			wantTitle:      "Jean Valerie Gould (Gould - 2024 06 24 09-29)",
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
			rootPersonName: "Hurman",
			dbPath:         "/data/tree.rmtree",
			wantID:         "hurman-tree",
			wantTitle:      "Hurman (tree)",
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
	in := []string{"gould", "henry", "gould", "gould", "royal92"}
	want := []string{"gould", "henry", "gould-2", "gould-3", "royal92"}
	got := Dedupe(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dedupe(%v) = %v, want %v", in, got, want)
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Jean Valerie Gould", "jean-valerie-gould"},
		{"Gould - 2024 06 24 09-29", "gould-2024-06-24-09-29"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
