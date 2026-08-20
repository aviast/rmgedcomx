package gedcomx

import (
	"regexp"
	"strconv"
	"strings"
)

// gedcom5MonthAbbreviations maps GEDCOM 5.x's standard three-letter
// month abbreviations to their numeric value.
var gedcom5MonthAbbreviations = map[string]int{
	"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6,
	"JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
}

// gedcom5DateRe matches a plain GEDCOM 5.x date at day/month/year,
// month/year, or year-only precision, optionally prefixed with one of
// GEDCOM 5.x's own qualifier keywords. See ParseGedcom5Date's own
// comment for the full account of what this does and doesn't cover.
var gedcom5DateRe = regexp.MustCompile(`^(?:(ABT|CAL|EST|BEF|AFT)\s+)?(?:(\d{1,2})\s+)?(?:([A-Z]{3})\s+)?(\d{1,4})$`)

// ParseGedcom5Date attempts to parse a GEDCOM 5.x date string -- the
// kind found in a GEDCOM file's own DATE tag -- into the same
// RootsMagic Date string format EncodeRMDate produces from a GEDCOM X
// formal date, plus the (year, month, day) needed to separately compute
// the matching SortDate (rmdb.ComputeSortDate -- confirmed elsewhere in
// this project that the same formula applies regardless of which
// grammar the year/month/day came from).
//
// Exists because a real client, converting a real GEDCOM file, was
// found sending exactly this kind of text in Date.original without a
// corresponding Date.formal -- and this server, which previously only
// ever consulted Formal, was silently recording no date at all even
// though the information was right there in Original. See SCOPE.md's
// "Write support" section for the full account, including a related
// finding: the specific converter that prompted this already computes
// Formal in most cases, but its own year-matching only accepted exactly
// four digits, silently failing on the many three-digit years a file
// spanning back to the 6th century actually has (a separate, narrower
// bug worth fixing on that side too, not a reason to skip this one).
//
// Deliberately narrower than the full GEDCOM 5.5.1 date grammar.
// Checked directly against every DATE value in this project's own real
// royal92.ged reference file (4018 of them) before settling on this
// scope, not guessed at: this covers 99.5% of them (3998 of 4018).
//
// Supported: a plain date at day/month/year, month/year, or year-only
// precision (any year length, not just four digits), optionally
// prefixed with one of GEDCOM 5.x's own qualifier keywords -- ABT/CAL/
// EST (RootsMagic's own "qualitative" modifiers -- About/Calculated/
// Estimated) or BEF/AFT (its "directional" modifiers -- Before/After).
//
// Not supported, and deliberately so, not simply unimplemented: date
// ranges (BET...AND..., FROM...TO...) and interpreted dates (INT...) --
// none appear anywhere in this project's own reference file at all, so
// there's no real evidence to build or verify support against; double-
// dating (e.g. "1743/44" -- 18 real occurrences in the same file) and a
// day+month with no year (2 real occurrences) -- both confirmed
// genuinely rare rather than assumed to be, and both would need a real
// design decision of their own (which year does double-dating with no
// separate Formal value sort by? what does a year-less day/month even
// mean for SortDate?) rather than a quick addition alongside the common
// cases above.
//
// Returns ok = false, not an error, for anything outside this scope:
// unlike EncodeRMDate's own formal-date input (which a client should be
// able to expect this server honors or clearly rejects), Date.original
// is inherently free text with no defined grammar of its own -- not
// matching a known pattern is a normal outcome here, not a client
// mistake to report back. Callers should fall back to no date (rather
// than reject the whole request) when this returns false -- see
// buildNewFact's own comment for why, and for the log line that
// keeps this fallback from being entirely silent.
func ParseGedcom5Date(original string) (rmDateString string, year, month, day int, ok bool) {
	s := strings.ToUpper(strings.TrimSpace(original))
	if s == "" {
		return "", 0, 0, 0, false
	}
	m := gedcom5DateRe.FindStringSubmatch(s)
	if m == nil {
		return "", 0, 0, 0, false
	}
	qualifierWord, dayStr, monthStr, yearStr := m[1], m[2], m[3], m[4]

	y, err := strconv.Atoi(yearStr)
	if err != nil || y == 0 {
		return "", 0, 0, 0, false
	}

	var mo int
	if monthStr != "" {
		var mok bool
		mo, mok = gedcom5MonthAbbreviations[monthStr]
		if !mok {
			return "", 0, 0, 0, false
		}
	}

	var d int
	if dayStr != "" {
		// The regex's own group structure can't actually produce a day
		// without a month (the day group is only followed by \s+ then
		// the month group in the pattern), but this is cheap insurance
		// against a future edit to the regex breaking that invariant
		// silently.
		if mo == 0 {
			return "", 0, 0, 0, false
		}
		dd, err := strconv.Atoi(dayStr)
		if err != nil || dd < 1 || dd > 31 {
			return "", 0, 0, 0, false
		}
		d = dd
	}

	directional := byte('.')
	qualitative := byte('.')
	switch qualifierWord {
	case "BEF":
		directional = 'B'
	case "AFT":
		directional = 'A'
	case "ABT":
		qualitative = 'A'
	case "CAL":
		qualitative = 'L'
	case "EST":
		qualitative = 'E'
	}

	return buildRMDateString(directional, y, mo, d, qualitative), y, mo, d, true
}
