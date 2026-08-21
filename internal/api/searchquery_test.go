package api

import "testing"

func TestParseSearchQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []searchTerm
	}{
		{
			name:  "spec's own example",
			input: `givenName:John surname:Smith gender:male birthDate:"30 June 1900"`,
			want: []searchTerm{
				{Field: "givenName", Value: "John", Exact: true},
				{Field: "surname", Value: "Smith", Exact: true},
				{Field: "gender", Value: "male", Exact: true},
				{Field: "birthDate", Value: "30 June 1900", Exact: true},
			},
		},
		{
			name:  "spec's own non-exact example",
			input: `givenName:Bob~`,
			want:  []searchTerm{{Field: "givenName", Value: "Bob", Exact: false}},
		},
		{
			name:  "quoted value followed by tilde",
			input: `birthPlace:"New York"~`,
			want:  []searchTerm{{Field: "birthPlace", Value: "New York", Exact: false}},
		},
		{
			name:  "single quoted value, no trailing tilde",
			input: `name:"John Smith"`,
			want:  []searchTerm{{Field: "name", Value: "John Smith", Exact: true}},
		},
		{
			name:  "mixed exact and non-exact terms",
			input: `surname:Smith~ givenName:Bob`,
			want: []searchTerm{
				{Field: "surname", Value: "Smith", Exact: false},
				{Field: "givenName", Value: "Bob", Exact: true},
			},
		},
		{
			name:  "leading and trailing whitespace tolerated",
			input: `  givenName:Bob  `,
			want:  []searchTerm{{Field: "givenName", Value: "Bob", Exact: true}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSearchQuery(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d terms, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("term %d: got %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}

	errorTests := []struct {
		name  string
		input string
	}{
		{"empty query", ``},
		{"whitespace only", `   `},
		{"missing colon", `givenNameBob`},
		{"empty field name", `:Bob`},
		{"empty value", `givenName:`},
		{"unterminated quote", `name:"John`},
	}
	for _, tc := range errorTests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSearchQuery(tc.input)
			if err == nil {
				t.Errorf("expected an error for input %q, got none", tc.input)
			}
		})
	}
}
