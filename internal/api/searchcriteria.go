package api

import (
	"fmt"

	"github.com/aviast/rmgedcomx/internal/gedcomx"
	"github.com/aviast/rmgedcomx/internal/rmdb"
)

// searchDateFieldRange converts a search term's parsed date into the
// SortDate range it should match. Reuses gedcomx.ParseGedcom5Date --
// the same GEDCOM 5.x date grammar this project already parses
// Date.original with for write support -- since a search client typing
// "30 June 1900" is producing exactly that kind of text, not a formal
// GEDCOM X date.
//
// Exact match: the range covers only the precision actually given --
// day+month+year narrows to that exact SortDate; month+year or year
// alone widens to the smallest range consistent with what was
// specified (the whole month, or the whole year), never narrower than
// what the client actually said. Non-exact ("~"): always widens to the
// whole year, even if a more precise date was given -- a deliberate,
// simple interpretation of "less precise", consistent with this
// project's own preference (see the "~" comment on
// rmdb.SearchTextCriterion) for a plain, defensible rule over an
// invented fuzzy-matching scheme this data has no real support for.
func searchDateFieldRange(term searchTerm) (rmdb.SearchDateCriterion, error) {
	_, year, month, day, ok := gedcomx.ParseGedcom5Date(term.Value)
	if !ok {
		return rmdb.SearchDateCriterion{}, fmt.Errorf("%s: %q isn't a date this server can parse (expected a GEDCOM-style date, e.g. \"30 Jun 1900\", \"Jun 1900\", or \"1900\")", term.Field, term.Value)
	}
	if !term.Exact {
		month, day = 0, 0
	}
	minMonth, maxMonth := month, month
	minDay, maxDay := day, day
	if month == 0 {
		minMonth, maxMonth = 1, 12
	}
	if day == 0 {
		minDay, maxDay = 1, 31
	}
	return rmdb.SearchDateCriterion{
		MinSortDate: rmdb.ComputeSortDate(year, minMonth, minDay),
		MaxSortDate: rmdb.ComputeSortDate(year, maxMonth, maxDay),
	}, nil
}

// buildSearchCriteria parses a GEDCOM X RS search query string (the "q"
// template variable, RS spec Section 6) into rmdb.SearchCriteria,
// covering the 10 "direct" search parameters that section defines:
// name, givenName, surname, gender, birthDate, birthPlace, deathDate,
// deathPlace, marriageDate, marriagePlace.
//
// The "{relation}"-prefixed parameters (father/mother/spouse/parent,
// applied to 8 of the above) are deliberately not yet supported -- an
// unrecognized field name is rejected outright, naming it specifically
// as a relation-parameter if it matches that shape, rather than
// silently ignored, the same "don't silently drop what a client asked
// for" principle this project has applied to unrecognized JSON fields
// throughout its own write support.
func buildSearchCriteria(q string) (rmdb.SearchCriteria, error) {
	terms, err := parseSearchQuery(q)
	if err != nil {
		return rmdb.SearchCriteria{}, err
	}

	var criteria rmdb.SearchCriteria
	relationPrefixes := []string{"father", "mother", "spouse", "parent"}
	for _, term := range terms {
		switch term.Field {
		case "name":
			criteria.Name = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
		case "givenName":
			criteria.GivenName = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
		case "surname":
			criteria.Surname = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
		case "gender":
			var sex int
			switch term.Value {
			case "male":
				sex = 0
			case "female":
				sex = 1
			default:
				return rmdb.SearchCriteria{}, fmt.Errorf(`gender: %q -- valid values are "male" and "female" (RS spec Section 6)`, term.Value)
			}
			criteria.Gender = &sex
		case "birthDate":
			r, err := searchDateFieldRange(term)
			if err != nil {
				return rmdb.SearchCriteria{}, err
			}
			criteria.BirthDate = &r
		case "birthPlace":
			criteria.BirthPlace = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
		case "deathDate":
			r, err := searchDateFieldRange(term)
			if err != nil {
				return rmdb.SearchCriteria{}, err
			}
			criteria.DeathDate = &r
		case "deathPlace":
			criteria.DeathPlace = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
		case "marriageDate":
			r, err := searchDateFieldRange(term)
			if err != nil {
				return rmdb.SearchCriteria{}, err
			}
			criteria.MarriageDate = &r
		case "marriagePlace":
			criteria.MarriagePlace = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
		default:
			for _, prefix := range relationPrefixes {
				if len(term.Field) > len(prefix) && term.Field[:len(prefix)] == prefix {
					return rmdb.SearchCriteria{}, fmt.Errorf("%s: relation-based search parameters (father/mother/spouse/parent) aren't supported yet, only the 10 direct parameters", term.Field)
				}
			}
			return rmdb.SearchCriteria{}, fmt.Errorf("%s: unrecognized search field", term.Field)
		}
	}
	return criteria, nil
}
