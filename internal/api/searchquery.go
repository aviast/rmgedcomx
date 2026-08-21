package api

import (
	"fmt"
	"strings"
)

// searchTerm is one parsed name:value pair from a GEDCOM X RS search
// query string (RS spec Section 6, "q" template variable).
type searchTerm struct {
	Field string
	Value string
	Exact bool // false if the value carried a trailing "~"
}

// parseSearchQuery parses a GEDCOM X RS search query string -- checked
// directly against the spec's own grammar description before writing
// this, not assumed: "composed of name-value pairs. A name and value is
// separated by a colon ':' and each name-value pair is separated by a
// white space. ... If white space is needed in the value then the value
// must be wrapped in double quotes. By default, values are exact. For
// non-exact matches append a tilde '~' at the end of the value."
//
// Example: `givenName:John surname:Smith gender:male birthDate:"30 June 1900"`
// Non-exact example: `givenName:Bob~`
//
// The spec gives no grammar for an escaped double-quote *within* a
// quoted value, and no real client is likely to need one for a name or
// place, so none is supported here -- a value containing '"' will
// simply end the quoted section at that character, matching the
// simplest reading of the spec's own text rather than inventing an
// escaping convention the spec doesn't define.
func parseSearchQuery(q string) ([]searchTerm, error) {
	var terms []searchTerm
	i := 0
	n := len(q)
	for i < n {
		for i < n && q[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}
		colon := strings.IndexByte(q[i:], ':')
		if colon < 0 {
			return nil, fmt.Errorf("expected a %q separating a field name from its value, near %q", ":", q[i:])
		}
		field := q[i : i+colon]
		if field == "" {
			return nil, fmt.Errorf("empty field name near %q", q[i:])
		}
		i += colon + 1

		var value string
		exact := true
		if i < n && q[i] == '"' {
			i++
			start := i
			for i < n && q[i] != '"' {
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("unterminated quoted value for field %q", field)
			}
			value = q[start:i]
			i++ // skip closing quote
			if i < n && q[i] == '~' {
				exact = false
				i++
			}
		} else {
			start := i
			for i < n && q[i] != ' ' {
				i++
			}
			value = q[start:i]
			if strings.HasSuffix(value, "~") {
				exact = false
				value = value[:len(value)-1]
			}
		}

		if value == "" {
			return nil, fmt.Errorf("empty value for field %q", field)
		}
		terms = append(terms, searchTerm{Field: field, Value: value, Exact: exact})

		if i < n && q[i] != ' ' {
			return nil, fmt.Errorf("expected white space after the value for field %q, near %q", field, q[i:])
		}
	}
	if len(terms) == 0 {
		return nil, fmt.Errorf("search query is empty")
	}
	return terms, nil
}
