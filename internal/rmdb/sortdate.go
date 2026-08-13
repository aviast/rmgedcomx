package rmdb

// NoDateSortValue is RootsMagic's own sentinel for "no date to sort by"
// -- confirmed directly against real captured data (every NameTable row
// for a name with no date entered, e.g. every primary name in the
// Brontë test database, uses exactly this value) -- max int64, sorting
// such rows last.
const NoDateSortValue int64 = 9223372036854775807

// sortDateKy, sortDateKm, sortDateKd, and sortDateC are the coefficients
// in RootsMagic's own SortDate encoding, documented at
// https://sqlitetoolsforrootsmagic.com/dates-sortdate-algorithm/ and
// confirmed here against 18 real (year, month, day, SortDate) values
// pulled directly from captured RootsMagic writes (see
// TestComputeSortDateMatchesRealData) -- full dates, month-only dates,
// year-only dates, and a date carrying the "About" qualifier (confirmed
// separately that qualifiers don't change this value at all; they only
// affect the separate Date string -- see rmdate.go).
const (
	sortDateKy = 562949953421312 // 2^49
	sortDateKm = 35184372088832  // 2^45
	sortDateKd = 549755813888    // 2^39
	sortDateC  = 17178820620
)

// ComputeSortDate encodes a (year, month, day) into RootsMagic's own
// SortDate integer encoding, used by EventTable.SortDate and
// NameTable.SortDate to order records chronologically regardless of the
// precision (or complete absence) of the date actually entered.
//
// month and day are 0 when not specified (a year-only or year+month
// date) -- matching the same convention ParseRMDate's own decoding uses
// for the reverse direction, and RootsMagic's own convention in the
// Date string itself (see rmdate.go).
//
// Does not handle BC dates (RootsMagic's own encoding for these, if any,
// isn't confirmed -- see rmdate.go's own note that the simple formal
// date profile used throughout this project has no clean BC
// representation either) -- callers should not call this for a BC year.
func ComputeSortDate(year, month, day int) int64 {
	return sortDateKy*int64(year+10000) + sortDateKm*int64(month) + sortDateKd*int64(day) + sortDateC
}
