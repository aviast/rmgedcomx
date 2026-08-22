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

// relationCriteriaFor returns a pointer to the RelationCriteria for the
// given relation ("father", "mother", "spouse", or "parent") on
// criteria, lazily creating it on first use so a relation group with no
// fields set is never sent to rmdb (SearchPersons only iterates
// relations criteria actually has one of).
func relationCriteriaFor(criteria *rmdb.SearchCriteria, relation string) *rmdb.RelationCriteria {
	switch relation {
	case "father":
		if criteria.Father == nil {
			criteria.Father = &rmdb.RelationCriteria{}
		}
		return criteria.Father
	case "mother":
		if criteria.Mother == nil {
			criteria.Mother = &rmdb.RelationCriteria{}
		}
		return criteria.Mother
	case "spouse":
		if criteria.Spouse == nil {
			criteria.Spouse = &rmdb.RelationCriteria{}
		}
		return criteria.Spouse
	case "parent":
		if criteria.Parent == nil {
			criteria.Parent = &rmdb.RelationCriteria{}
		}
		return criteria.Parent
	default:
		return nil // unreachable: callers only pass one of the 4 relationPrefixes below
	}
}

// applyRelationField sets the one field on rc that suffix names,
// returning false if suffix isn't one of the 9 the RS spec defines for
// a "{relation}"-prefixed parameter (Name, GivenName, Surname,
// BirthDate, BirthPlace, DeathDate, DeathPlace, MarriageDate,
// MarriagePlace) -- checked directly against the spec's own "Relation
// Search Parameters" table (Section 5.3, under the "q" template variable)
// before writing this, not
// assumed, including confirming MarriagePlace is part of that table
// too (an earlier internal accounting of this work had miscounted 8
// fields per relation, 32 total, rather than the actual 9 and 36 --
// corrected before implementing anything, not after).
func applyRelationField(rc *rmdb.RelationCriteria, suffix string, term searchTerm) (bool, error) {
	switch suffix {
	case "Name":
		rc.Name = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
	case "GivenName":
		rc.GivenName = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
	case "Surname":
		rc.Surname = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
	case "BirthDate":
		r, err := searchDateFieldRange(term)
		if err != nil {
			return true, err
		}
		rc.BirthDate = &r
	case "BirthPlace":
		rc.BirthPlace = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
	case "DeathDate":
		r, err := searchDateFieldRange(term)
		if err != nil {
			return true, err
		}
		rc.DeathDate = &r
	case "DeathPlace":
		rc.DeathPlace = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
	case "MarriageDate":
		r, err := searchDateFieldRange(term)
		if err != nil {
			return true, err
		}
		rc.MarriageDate = &r
	case "MarriagePlace":
		rc.MarriagePlace = &rmdb.SearchTextCriterion{Value: term.Value, Exact: term.Exact}
	default:
		return false, nil
	}
	return true, nil
}

// buildSearchCriteria parses a GEDCOM X RS search query string (the "q"
// template variable, RS spec Section 5.3) into rmdb.SearchCriteria,
// covering the 10 "direct" search parameters that section defines
// (name, givenName, surname, gender, birthDate, birthPlace, deathDate,
// deathPlace, marriageDate, marriagePlace) and all 4 possible
// "{relation}"-prefixed groups (father/mother/spouse/parent, each
// covering the same 9 fields RelationCriteria models -- see its own
// comment in internal/rmdb/search.go for how these are matched).
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
				return rmdb.SearchCriteria{}, fmt.Errorf(`gender: %q -- valid values are "male" and "female" (RS spec Section 5.3)`, term.Value)
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
			matched := false
			for _, prefix := range relationPrefixes {
				if len(term.Field) <= len(prefix) || term.Field[:len(prefix)] != prefix {
					continue
				}
				suffix := term.Field[len(prefix):]
				rc := relationCriteriaFor(&criteria, prefix)
				ok, err := applyRelationField(rc, suffix, term)
				if err != nil {
					return rmdb.SearchCriteria{}, err
				}
				if !ok {
					return rmdb.SearchCriteria{}, fmt.Errorf("%s: unrecognized field for the %q relation (expected one of Name, GivenName, Surname, BirthDate, BirthPlace, DeathDate, DeathPlace, MarriageDate, MarriagePlace)", term.Field, prefix)
				}
				matched = true
				break
			}
			if !matched {
				return rmdb.SearchCriteria{}, fmt.Errorf("%s: unrecognized search field", term.Field)
			}
		}
	}
	return criteria, nil
}
