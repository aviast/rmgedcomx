package gedcomx

import "testing"

// realDateStrings are every distinct RootsMagic Date string this project
// has actually captured from a real RootsMagic write, across both the
// original write-support work and the Brontë Person/Relationship create
// captures. Used to round-trip test EncodeRMDate: parse with ParseRMDate,
// re-encode with EncodeRMDate, and confirm the result matches the
// original exactly -- the strongest test available, since the expected
// output is the real string itself, not something hand-written.
var realDateStrings = []string{
	"D.+17440400..+00000000..",
	"D.+17460000..+00000000..",
	"D.+17550000..+00000000..",
	"D.+17680000..+00000000..",
	"D.+17760000..+00000000..",
	"D.+17770317..+00000000..",
	"D.+18080000.A+00000000..", // the "About" (ABT) case
	"D.+18080405..+00000000..",
	"D.+18091219..+00000000..",
	"D.+18121229..+00000000..", // the marriage date
	"D.+18140423..+00000000..",
	"D.+18150208..+00000000..",
	"D.+18160421..+00000000..",
	"D.+18170626..+00000000..",
	"D.+18180730..+00000000..",
	"D.+18200117..+00000000..",
	"D.+18250506..+00000000..",
	"D.+18250615..+00000000..",
	"D.+18421029..+00000000..",
	"D.+18480924..+00000000..",
	"D.+18481219..+00000000..",
	"D.+18490528..+00000000..",
	"D.+18540629..+00000000..",
	"D.+18550331..+00000000..",
	"D.+18610607..+00000000..",
}

func TestEncodeRMDateRoundTripsRealData(t *testing.T) {
	for _, raw := range realDateStrings {
		parsed := ParseRMDate(raw)
		if parsed == nil {
			t.Fatalf("ParseRMDate(%q) returned nil -- test data itself is bad", raw)
		}
		if parsed.Formal == "" {
			t.Fatalf("ParseRMDate(%q) produced no Formal value -- can't round-trip this one, remove it from the test data or investigate why", raw)
		}
		encoded, _, _, _, err := EncodeRMDate(parsed.Formal)
		if err != nil {
			t.Errorf("EncodeRMDate(%q) [from raw %q] failed: %v", parsed.Formal, raw, err)
			continue
		}
		if encoded != raw {
			t.Errorf("round trip mismatch: raw=%q -> formal=%q -> encoded=%q", raw, parsed.Formal, encoded)
		}
	}
}

func TestEncodeRMDateNoDateSentinel(t *testing.T) {
	encoded, y, m, d, err := EncodeRMDate("")
	if err != nil {
		t.Fatalf("EncodeRMDate(\"\") returned error: %v", err)
	}
	if encoded != "." {
		t.Errorf("EncodeRMDate(\"\") = %q, want %q (RootsMagic's own no-date sentinel)", encoded, ".")
	}
	if y != 0 || m != 0 || d != 0 {
		t.Errorf("EncodeRMDate(\"\") returned (%d,%d,%d), want (0,0,0)", y, m, d)
	}
}

func TestEncodeRMDateRejectsUnsupportedForms(t *testing.T) {
	cases := []string{
		"/+1910",            // before
		"+1950/",            // after
		"+1930/+1940",       // range
		"not a date at all", // malformed
		"+1900-13",          // invalid month
		"+1900-01-32",       // invalid day
	}
	for _, formal := range cases {
		_, _, _, _, err := EncodeRMDate(formal)
		if err == nil {
			t.Errorf("EncodeRMDate(%q) succeeded, expected a clear error for this unsupported/malformed form", formal)
		}
	}
}
