package rmdb

import "testing"

// realSortDates are (year, month, day, SortDate) tuples pulled directly
// from real captured RootsMagic writes for the Brontë test database --
// not constructed. Covers full dates, month-only, and year-only
// precision. See ComputeSortDate's own comment for the "About" qualifier
// case, confirmed separately not to change this value.
var realSortDates = []struct {
	year, month, day int
	want             int64
}{
	{1777, 3, 17, 6629976517586714636},
	{1861, 6, 7, 6677364369232232460},
	{1814, 4, 23, 6650844148770275340},
	{1825, 5, 6, 6657062436781162508},
	{1815, 2, 8, 6651328483642310668},
	{1825, 6, 15, 6657102568955576332},
	{1816, 4, 21, 6651968949165490188},
	{1855, 3, 31, 6673894310534971404},
	{1817, 6, 26, 6652605016642158604},
	{1848, 9, 24, 6670160918802857996},
	{1818, 7, 30, 6653205349990924300},
	{1848, 12, 19, 6670263723140055052},
	{1820, 1, 17, 6654112996839653388},
	{1849, 5, 28, 6670585330291179532},
	{1812, 12, 29, 6650003022375026700}, // the marriage fact date
	{1755, 0, 0, 6617476719646343180},   // year only
	{1746, 0, 0, 6612410170065551372},   // year only
	{1776, 0, 0, 6629298668668190732},   // year only
	{1744, 4, 0, 6611425007647064076},   // year + month, no day
	{1808, 0, 0, 6647313067177672716},   // year only, "ABT" in the source -- confirms the qualifier doesn't change SortDate
}

func TestComputeSortDateMatchesRealData(t *testing.T) {
	for _, c := range realSortDates {
		got := ComputeSortDate(c.year, c.month, c.day)
		if got != c.want {
			t.Errorf("ComputeSortDate(%d, %d, %d) = %d, want %d", c.year, c.month, c.day, got, c.want)
		}
	}
}
