package gedcomx

import "testing"

func TestParseRMDateQualifiers(t *testing.T) {
	cases := []struct{ raw, expect string }{
		{"D.+19000000.A+00000000..", "About 1900"},
		{"DR+19300000..+19400000..", "Between 1930 and 1940"},
		{"D.+19500000.L+00000000..", "Calculated 1950"},
		{"D.+20000000.E+00000000..", "Estimated 2000"},
		{"DB+19100000..+00000000..", "Before 1910"},
		{"DA+19500000..+00000000..", "After 1950"},
		{"DR+19600000..+19700000..", "Between 1960 and 1970"},
		{"DS+19100000..+19500000..", "From 1910 to 1950"},
		{"D.+19350000.C+00000000..", "Circa 1935"},
		{"D.+19050000.S+00000000..", "Say 1905"},
	}
	for _, c := range cases {
		d := ParseRMDate(c.raw)
		if d == nil {
			t.Errorf("raw=%q got nil, want %q", c.raw, c.expect)
			continue
		}
		if d.Original != c.expect {
			t.Errorf("raw=%q got %q, want %q", c.raw, d.Original, c.expect)
		}
		t.Logf("raw=%-28s -> %-28s formal=%q", c.raw, d.Original, d.Formal)
	}
}
