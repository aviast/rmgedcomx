package gedcomx

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// TestParseGedcom5DateSpecificExamples covers every supported pattern
// explicitly, including real values pulled directly from real requests
// this project has actually seen -- not constructed. "abt 1808"
// specifically is an independent cross-check: its expected rmDateString
// was confirmed against real captured RootsMagic data much earlier in
// this project (Hugh Brunty's death date, in an entirely different
// investigation), so getting the identical value here again, via a
// completely different code path, is stronger evidence than either
// check alone.
func TestParseGedcom5DateSpecificExamples(t *testing.T) {
	cases := []struct {
		input               string
		wantRMDateString    string
		wantY, wantM, wantD int
	}{
		{"       1870", "D.+18700000..+00000000..", 1870, 0, 0},  // real: royal92.ged I785's death
		{"24 MAY 1819", "D.+18190524..+00000000..", 1819, 5, 24}, // real: Victoria's birth
		{"22 JAN 1901", "D.+19010122..+00000000..", 1901, 1, 22}, // real: Victoria's death
		{"abt 1808", "D.+18080000.A+00000000..", 1808, 0, 0},     // cross-checked against real RootsMagic capture
		{"MAY 1870", "D.+18700500..+00000000..", 1870, 5, 0},
		{"BEF 1877", "DB+18770000..+00000000..", 1877, 0, 0},
		{"AFT 1989", "DA+19890000..+00000000..", 1989, 0, 0},
		{"BEF 16 FEB 1337", "DB+13370216..+00000000..", 1337, 2, 16},
		{"AFT 8 MAY 1326", "DA+13260508..+00000000..", 1326, 5, 8},
		{"ABT 14 AUG 1479", "D.+14790814.A+00000000..", 1479, 8, 14},
		{"CAL 1900", "D.+19000000.L+00000000..", 1900, 0, 0},
		{"EST 1900", "D.+19000000.E+00000000..", 1900, 0, 0},
		{"996", "D.+09960000..+00000000..", 996, 0, 0},     // 3-digit year
		{"ABT 968", "D.+09680000.A+00000000..", 968, 0, 0}, // 3-digit year, qualified
	}
	for _, c := range cases {
		rmDateString, y, m, d, ok := ParseGedcom5Date(c.input)
		if !ok {
			t.Errorf("ParseGedcom5Date(%q): got ok=false, want ok=true", c.input)
			continue
		}
		if rmDateString != c.wantRMDateString || y != c.wantY || m != c.wantM || d != c.wantD {
			t.Errorf("ParseGedcom5Date(%q) = %q, %d, %d, %d; want %q, %d, %d, %d",
				c.input, rmDateString, y, m, d, c.wantRMDateString, c.wantY, c.wantM, c.wantD)
		}
	}
}

func TestParseGedcom5DateRejectsUnsupportedForms(t *testing.T) {
	cases := []string{
		"1815/1816",          // double-dating
		"12 MAR 1637/1638",   // double-dating with day/month
		"10 JAN",             // day+month, no year
		"BET 1900 AND 1910",  // range
		"FROM 1900 TO 1910",  // range
		"INT 1900 (guessed)", // interpreted date
		"not a date at all",
		"",
		"   ",
	}
	for _, c := range cases {
		_, _, _, _, ok := ParseGedcom5Date(c)
		if ok {
			t.Errorf("ParseGedcom5Date(%q): got ok=true, want ok=false (unsupported form)", c)
		}
	}
}

// TestParseGedcom5DateAgainstRealRoyal92GedFile runs ParseGedcom5Date
// against every single DATE value in this project's own real
// royal92.ged reference file -- not a sample, all of them -- confirming
// both the overall coverage rate this function's own comment claims
// (99.5%, 3998 of 4018) and, more importantly, the *exact* set of
// values that don't match, so a future change that silently narrows or
// widens support gets caught here rather than discovered later against
// a real request.
func TestParseGedcom5DateAgainstRealRoyal92GedFile(t *testing.T) {
	f, err := os.Open("../../testdata/royal92.ged")
	if err != nil {
		t.Fatalf("opening royal92.ged: %v", err)
	}
	defer f.Close()

	var total, matched int
	var unmatched []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "2 DATE") {
			continue
		}
		value := strings.TrimPrefix(line, "2 DATE")
		total++
		if _, _, _, _, ok := ParseGedcom5Date(value); ok {
			matched++
		} else {
			unmatched = append(unmatched, strings.TrimSpace(value))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading royal92.ged: %v", err)
	}

	if total != 4018 {
		t.Fatalf("found %d DATE lines in royal92.ged, expected 4018 -- the file itself may have changed; if so, this test's own expectations need reviewing, not just updating", total)
	}
	if matched != 3998 {
		t.Errorf("matched %d of %d DATE lines, want exactly 3998 (99.5%%) -- a change in either direction means real-world coverage shifted; confirm it's intentional before updating this number", matched, total)
	}

	wantUnmatched := map[string]bool{
		"1815/1816": true, "1951/1952": true, "1942/1943": true, "12 MAR 1637/1638": true,
		"10 JAN": true, "1361/1362": true, "15 SEP 1396/1397": true, "1761/1762": true,
		"1675/1676": true, "1495/1496": true, "1027/1028": true, "1056/1060": true,
		"8 MAR 1137/1138": true, "1079/1080": true, "ABT    1103/1104": true, "ABT    1103/1105": true,
		"1130/1131": true, "1556/1557": true, "1380/1381": true, "20 JUL": true,
	}
	if len(unmatched) != len(wantUnmatched) {
		t.Errorf("got %d unmatched dates, want %d: %v", len(unmatched), len(wantUnmatched), unmatched)
	}
	for _, u := range unmatched {
		if !wantUnmatched[u] {
			t.Errorf("unexpected new unmatched date %q -- either a real regression, or real coverage genuinely changed and this test's expectations should be updated deliberately, not silently", u)
		}
	}
}
