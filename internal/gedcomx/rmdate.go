package gedcomx

import (
	"fmt"
	"regexp"
	"strconv"
)

// ParseRMDate decodes RootsMagic's internal encoded date text (as stored in
// EventTable.Date, NameTable.Date, etc.) into a gedcomx Date.
//
// RootsMagic does not store the date as the user typed it; it stores an
// encoded form like "D.+19470101..+00000000.." or, with modifiers,
// "DB+19100000..+00000000.." (before 1910) or "DR+19300000..+19400000.."
// (between 1930 and 1940). The layout is two fixed-width date groups:
//
//	D <directional:1> ( <sign:1> <YYYY:4><MM:2><DD:2> . <qualitative:1> ){2}
//
// Group 1 is the primary date; group 2 is used for range/period dates
// (Between..And, From..To) and is all-zero otherwise. Within each group,
// MM/DD of 00 mean "not specified" (a year-only or year+month date).
//
// The two qualifier bytes were confirmed against a purpose-built RootsMagic
// test database exercising each modifier (see SCOPE.md for the full
// mapping table and how to extend it):
//
//	directional (byte right after "D", selects how the two dates combine):
//	  '.' plain (no modifier)   'B' Before   'A' After
//	  'R' Between ... And ...   'S' From ... To ...
//	qualitative (byte right after each date's 8 digits, qualifies that
//	specific date component):
//	  '.' none   'A' About   'L' Calculated   'E' Estimated
//	  'C' Circa  'S' Say
//
// RootsMagic's help documentation lists further modifiers this decoder
// doesn't have confirmed byte codes for yet (the directional modifiers By,
// To, Until, Since, and the range modifiers dash and Or). Dates using
// those still get their year/month/day decoded correctly; they just don't
// get a modifier word, since guessing wrong would misrepresent the
// record.
var rmDateRe = regexp.MustCompile(`^D(.)([+-])(\d{4})(\d{2})(\d{2})\.(.)([+-])(\d{4})(\d{2})(\d{2})\.(.)$`)

var monthNames = [...]string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// qualitativeWords maps the confirmed per-date-half qualifier bytes to
// their RootsMagic label.
var qualitativeWords = map[byte]string{
	'A': "About",
	'L': "Calculated",
	'E': "Estimated",
	'C': "Circa",
	'S': "Say",
}

func ParseRMDate(raw string) *Date {
	if raw == "" || raw == "." {
		// "." is RootsMagic's sentinel for "no date entered" (seen
		// throughout NameTable.Date for names with no Alternate Name date).
		return nil
	}
	m := rmDateRe.FindStringSubmatch(raw)
	if m == nil {
		// Unrecognized format (e.g. a Quaker date). Preserve the raw value
		// rather than lose it or guess.
		return &Date{Original: raw}
	}

	directional := m[1]
	d1 := decodeDateNumbers(m[2], m[3], m[4], m[5])
	qual1 := m[6]
	d2 := decodeDateNumbers(m[7], m[8], m[9], m[10])
	qual2 := m[11]

	if !d1.ok {
		return &Date{Original: raw}
	}

	text1 := d1.text()
	if word, ok := qualitativeWords[qual1[0]]; ok {
		text1 = word + " " + text1
	}
	var text2 string
	if d2.ok {
		text2 = d2.text()
		if word, ok := qualitativeWords[qual2[0]]; ok {
			text2 = word + " " + text2
		}
	}

	d := &Date{}
	switch directional {
	case ".":
		d.Original = text1
		if qual1 == "." {
			d.Formal = d1.formalYMD()
		} else if qual1 == "A" && d1.formalYMD() != "" {
			// "About" maps cleanly onto GEDCOM X's approximate-date prefix.
			d.Formal = "A" + d1.formalYMD()
		}
	case "B": // Before
		d.Original = "Before " + text1
		if qual1 == "." && d1.formalYMD() != "" {
			d.Formal = "/" + d1.formalYMD()
		}
	case "A": // After
		d.Original = "After " + text1
		if qual1 == "." && d1.formalYMD() != "" {
			d.Formal = d1.formalYMD() + "/"
		}
	case "R": // Between ... And ...
		if d2.ok {
			d.Original = "Between " + text1 + " and " + text2
			if qual1 == "." && qual2 == "." && d1.formalYMD() != "" && d2.formalYMD() != "" {
				d.Formal = d1.formalYMD() + "/" + d2.formalYMD()
			}
		} else {
			d.Original = text1
		}
	case "S": // From ... To ...
		if d2.ok {
			d.Original = "From " + text1 + " to " + text2
			if qual1 == "." && qual2 == "." && d1.formalYMD() != "" && d2.formalYMD() != "" {
				d.Formal = d1.formalYMD() + "/" + d2.formalYMD()
			}
		} else {
			d.Original = text1
		}
	default:
		// Unconfirmed directional modifier byte: still show the decoded
		// date(s), just without a modifier word or formal value.
		if d2.ok {
			d.Original = text1 + " - " + text2
		} else {
			d.Original = text1
		}
	}
	return d
}

// decodedDateNumbers is one decoded (sign, yyyy, mm, dd) group.
type decodedDateNumbers struct {
	year, month, day int
	bc               bool
	ok               bool // false when year=month=day=0 ("not specified")
}

func decodeDateNumbers(sign, yyyy, mm, dd string) decodedDateNumbers {
	year, _ := strconv.Atoi(yyyy)
	month, _ := strconv.Atoi(mm)
	day, _ := strconv.Atoi(dd)
	if year == 0 && month == 0 && day == 0 {
		return decodedDateNumbers{}
	}
	return decodedDateNumbers{year: year, month: month, day: day, bc: sign == "-", ok: true}
}

// text renders a human-readable form, e.g. "2 Jan 1900", "Jan 1900", "1900",
// or the same with a " BC" suffix.
func (d decodedDateNumbers) text() string {
	yearLabel := fmt.Sprintf("%04d", d.year)
	if d.bc {
		yearLabel += " BC"
	}
	switch {
	case d.day >= 1 && d.day <= 31 && d.month >= 1 && d.month <= 12:
		return fmt.Sprintf("%d %s %s", d.day, monthNames[d.month], yearLabel)
	case d.month >= 1 && d.month <= 12:
		return fmt.Sprintf("%s %s", monthNames[d.month], yearLabel)
	default:
		return yearLabel
	}
}

// formalYMD renders a GEDCOM X formal date value (the "+YYYY-MM-DD" family
// from the GEDCOM X Date Format specification), or "" if this date can't be
// cleanly represented that way (BC dates -- the simple formal profile used
// here doesn't have a clean BC representation, so we omit it rather than
// emit something misleading).
func (d decodedDateNumbers) formalYMD() string {
	if d.bc {
		return ""
	}
	year := fmt.Sprintf("+%04d", d.year)
	switch {
	case d.day >= 1 && d.day <= 31 && d.month >= 1 && d.month <= 12:
		return fmt.Sprintf("%s-%02d-%02d", year, d.month, d.day)
	case d.month >= 1 && d.month <= 12:
		return fmt.Sprintf("%s-%02d", year, d.month)
	case d.year > 0:
		return year
	default:
		return ""
	}
}
