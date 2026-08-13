package rmdb

import "testing"

func TestFoldAccentsMatchesConfirmedRealExample(t *testing.T) {
	// The one real, captured example this project has: RootsMagic's own
	// SurnameMP for "Brontë" is "Bronte". See accentFoldTable's own
	// comment -- everything beyond this one example is Unicode's NFD
	// decomposition rule, not a further guess.
	got := FoldAccents("Brontë")
	if got != "Bronte" {
		t.Errorf("FoldAccents(%q) = %q, want %q", "Brontë", got, "Bronte")
	}
}

func TestFoldAccentsLeavesPlainASCIIUnchanged(t *testing.T) {
	for _, s := range []string{"Nicholls", "McClory", "Carne", "", "123", "O'Brien"} {
		if got := FoldAccents(s); got != s {
			t.Errorf("FoldAccents(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestFoldAccentsCommonEuropeanNames(t *testing.T) {
	// Not independently confirmed against real RootsMagic output the way
	// "Brontë" is -- these exercise the general NFD-based rule beyond
	// the one confirmed data point, per accentFoldTable's own comment.
	cases := map[string]string{
		"Müller":   "Muller",
		"François": "Francois",
		"José":     "Jose",
		"Åström":   "Astrom",
		"Núñez":    "Nunez",
	}
	for input, want := range cases {
		if got := FoldAccents(input); got != want {
			t.Errorf("FoldAccents(%q) = %q, want %q", input, got, want)
		}
	}
}
