// Package gedcomx implements the subset of the GEDCOM X Conceptual Model and the
// GEDCOM X RS extensions needed by this server, as Go structs that (de)serialize
// to the GEDCOM X JSON representation (http://gedcomx.org/json/v1) plus the RS
// extensions (http://gedcomx.org/rs/v1).
package gedcomx

// Link is the GEDCOM X RS "Link" data type (Section 2.1 of the RS spec).
type Link struct {
	Href     string `json:"href,omitempty"`
	Template string `json:"template,omitempty"`
	Type     string `json:"type,omitempty"`
	Allow    string `json:"allow,omitempty"`
	Title    string `json:"title,omitempty"`
}

// Links is a map of link relation -> Link, per Section 2.1.3 of the RS spec.
type Links map[string]Link

// ResourceReference is a URI reference to another resource.
type ResourceReference struct {
	Resource   string `json:"resource"`
	ResourceID string `json:"resourceId,omitempty"`
}

// TextValue is a literal text value, optionally localized.
type TextValue struct {
	Lang  string `json:"lang,omitempty"`
	Value string `json:"value"`
}

// Identifiers groups identifiers of a given type together, keyed by identifier
// type URI, per GEDCOM X JSON.
type Identifiers map[string][]string

// SourceReference is a reference from a genealogical resource to a source that
// supports it.
type SourceReference struct {
	Description   string `json:"description"`
	DescriptionID string `json:"descriptionId,omitempty"`
}

// Note is a freeform annotation attached to a genealogical resource.
type Note struct {
	Lang string `json:"lang,omitempty"`
	Text string `json:"text"`
}

// Date is a genealogical date, with an optional original textual form and (per
// the RS spec) a list of normalized display values.
type Date struct {
	Original   string      `json:"original,omitempty"`
	Formal     string      `json:"formal,omitempty"`
	Normalized []TextValue `json:"normalized,omitempty"`
}

// PlaceReference is a reference to a place, with an optional inline original
// text and (per the RS spec) normalized display values.
type PlaceReference struct {
	Original   string      `json:"original,omitempty"`
	Resource   string      `json:"resource,omitempty"`
	Normalized []TextValue `json:"normalized,omitempty"`
}

// Conclusion is the base type embedded (in spirit; Go has no struct
// inheritance, so these fields are duplicated where needed) by Fact, Name,
// Gender, etc. It's kept here for documentation purposes.

// Fact represents a conclusion about an event, characteristic, or
// circumstance in a person's life or a relationship.
type Fact struct {
	ID         string            `json:"id,omitempty"`
	Type       string            `json:"type"`
	Date       *Date             `json:"date,omitempty"`
	Place      *PlaceReference   `json:"place,omitempty"`
	Value      string            `json:"value,omitempty"`
	Primary    *bool             `json:"primary,omitempty"`
	Confidence string            `json:"confidence,omitempty"`
	Sources    []SourceReference `json:"sources,omitempty"`
	Notes      []Note            `json:"notes,omitempty"`
}

// NamePart is a single part of a name (given, surname, prefix, suffix, ...).
type NamePart struct {
	Type  string `json:"type,omitempty"`
	Value string `json:"value"`
}

// NameForm is one linguistic/cultural rendering of a Name.
type NameForm struct {
	FullText string     `json:"fullText,omitempty"`
	Parts    []NamePart `json:"parts,omitempty"`
}

// Name is a conclusion about a name used to identify a person.
type Name struct {
	ID        string            `json:"id,omitempty"`
	Type      string            `json:"type,omitempty"`
	Preferred *bool             `json:"preferred,omitempty"` // RS extension (Section 3.3)
	Date      *Date             `json:"date,omitempty"`
	NameForms []NameForm        `json:"nameForms"`
	Sources   []SourceReference `json:"sources,omitempty"`
	Notes     []Note            `json:"notes,omitempty"`
}

// Gender is a conclusion about the gender of a person.
type Gender struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type"`
}

// FamilyView is the RS "FamilyView" data type (Section 2.3): a display-oriented
// view of a family unit.
type FamilyView struct {
	Parent1  *ResourceReference  `json:"parent1,omitempty"`
	Parent2  *ResourceReference  `json:"parent2,omitempty"`
	Children []ResourceReference `json:"children,omitempty"`
}

// DisplayProperties is the RS "DisplayProperties" data type (Section 2.2): a
// set of convenience, display-oriented properties for a Person.
type DisplayProperties struct {
	Name              string       `json:"name,omitempty"`
	Gender            string       `json:"gender,omitempty"`
	Lifespan          string       `json:"lifespan,omitempty"`
	BirthDate         string       `json:"birthDate,omitempty"`
	BirthPlace        string       `json:"birthPlace,omitempty"`
	DeathDate         string       `json:"deathDate,omitempty"`
	DeathPlace        string       `json:"deathPlace,omitempty"`
	AscendancyNumber  string       `json:"ascendancyNumber,omitempty"`
	DescendancyNumber string       `json:"descendancyNumber,omitempty"`
	FamiliesAsParent  []FamilyView `json:"familiesAsParent,omitempty"`
	FamiliesAsChild   []FamilyView `json:"familiesAsChild,omitempty"`
}

// Person is a description of a person, per the GEDCOM X Conceptual Model,
// plus the RS extensions `living` and `display` (Section 3.4).
type Person struct {
	ID          string             `json:"id"`
	Identifiers Identifiers        `json:"identifiers,omitempty"`
	Living      *bool              `json:"living,omitempty"`
	Private     bool               `json:"private,omitempty"`
	Gender      *Gender            `json:"gender,omitempty"`
	Names       []Name             `json:"names,omitempty"`
	Facts       []Fact             `json:"facts,omitempty"`
	Sources     []SourceReference  `json:"sources,omitempty"`
	Notes       []Note             `json:"notes,omitempty"`
	Display     *DisplayProperties `json:"display,omitempty"`
	Links       Links              `json:"links,omitempty"`
}

// RelationshipType URIs, per the GEDCOM X Conceptual Model.
const (
	RelationshipTypeCouple      = "http://gedcomx.org/Couple"
	RelationshipTypeParentChild = "http://gedcomx.org/ParentChild"
)

// Relationship is a description of the relationship between two persons.
type Relationship struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Person1 ResourceReference `json:"person1"`
	Person2 ResourceReference `json:"person2"`
	Facts   []Fact            `json:"facts,omitempty"`
	Notes   []Note            `json:"notes,omitempty"`
	Links   Links             `json:"links,omitempty"`
}

// PlaceDisplayProperties is the RS "PlaceDisplayProperties" data type
// (Section 2.5).
type PlaceDisplayProperties struct {
	Name     string `json:"name,omitempty"`
	FullName string `json:"fullName,omitempty"`
	Type     string `json:"type,omitempty"`
}

// PlaceDescription is a description of a geographic place.
type PlaceDescription struct {
	ID        string                  `json:"id"`
	Names     []TextValue             `json:"names"`
	Latitude  *float64                `json:"latitude,omitempty"`
	Longitude *float64                `json:"longitude,omitempty"`
	Notes     []Note                  `json:"notes,omitempty"`
	Display   *PlaceDisplayProperties `json:"display,omitempty"`
	Links     Links                   `json:"links,omitempty"`
}

// SourceCitation is a bibliographic reference to a source in some catalog,
// library, or other information store.
type SourceCitation struct {
	Value string `json:"value"`
}

// SourceDescription is a description of/reference to a source of genealogical
// information.
type SourceDescription struct {
	ID        string           `json:"id"`
	Citations []SourceCitation `json:"citations,omitempty"`
	Titles    []TextValue      `json:"titles,omitempty"`
	Notes     []Note           `json:"notes,omitempty"`
	SortKey   string           `json:"sortKey,omitempty"` // RS extension (Section 3.5)
	Links     Links            `json:"links,omitempty"`
}

// EntryList is a generic paged list envelope used for the Persons,
// Relationships, Place Descriptions, and Source Descriptions states. GEDCOM X
// JSON represents lists of top-level resources as top-level array members
// (e.g. "persons": [...]), so each concrete list document sets exactly one of
// these together with paging metadata; see the *List types below.
type paging struct {
	Results int   `json:"results"`
	Links   Links `json:"links,omitempty"`
}

// PersonsDocument is the `Persons` application state representation.
type PersonsDocument struct {
	Results int      `json:"results"`
	Persons []Person `json:"persons"`
	Links   Links    `json:"links,omitempty"`
}

// RelationshipsDocument is the `Relationships` application state representation.
type RelationshipsDocument struct {
	Results       int            `json:"results"`
	Relationships []Relationship `json:"relationships"`
	Links         Links          `json:"links,omitempty"`
}

// PlaceDescriptionsDocument is the `Place Descriptions` application state representation.
type PlaceDescriptionsDocument struct {
	Results int                `json:"results"`
	Places  []PlaceDescription `json:"places"`
	Links   Links              `json:"links,omitempty"`
}

// SourceDescriptionsDocument is the `Source Descriptions` application state representation.
type SourceDescriptionsDocument struct {
	Results            int                 `json:"results"`
	SourceDescriptions []SourceDescription `json:"sourceDescriptions"`
	Links              Links               `json:"links,omitempty"`
}

// PersonDocument wraps a single Person as the top-level `Person` application
// state document (a GEDCOM X document with exactly one "main" person first in
// the list, per Section 4.10.3 of the RS spec).
type PersonDocument struct {
	Persons []Person `json:"persons"`
	Links   Links    `json:"links,omitempty"`
}

// RelationshipDocument wraps a single Relationship.
type RelationshipDocument struct {
	Relationships []Relationship `json:"relationships"`
	Links         Links          `json:"links,omitempty"`
}

// PlaceDescriptionDocument wraps a single PlaceDescription.
type PlaceDescriptionDocument struct {
	Places []PlaceDescription `json:"places"`
	Links  Links              `json:"links,omitempty"`
}

// SourceDescriptionDocument wraps a single SourceDescription.
type SourceDescriptionDocument struct {
	SourceDescriptions []SourceDescription `json:"sourceDescriptions"`
	Links              Links               `json:"links,omitempty"`
}

// PersonRelativesDocument is the representation used by the `Person
// Parents`, `Person Children`, and `Person Spouses` application states: a
// list of persons plus (per Sections 4.12.3, 4.13.3, 4.14.3) the
// relationships describing how each relates to the subject person.
type PersonRelativesDocument struct {
	Results       int            `json:"results"`
	Persons       []Person       `json:"persons"`
	Relationships []Relationship `json:"relationships,omitempty"`
	Links         Links          `json:"links,omitempty"`
}

// AncestryResultsDocument is the `Ancestry Results` application state
// representation: a flat list of persons, each carrying its
// display.ascendancyNumber (Ahnentafel number), per Section 4.2.
type AncestryResultsDocument struct {
	Results int      `json:"results"`
	Persons []Person `json:"persons"`
	Links   Links    `json:"links,omitempty"`
}

// DescendancyResultsDocument is the `Descendancy Results` application state
// representation: a flat list of persons, each carrying its
// display.descendancyNumber (d'Aboville number), per Section 4.6.
type DescendancyResultsDocument struct {
	Results int      `json:"results"`
	Persons []Person `json:"persons"`
	Links   Links    `json:"links,omitempty"`
}

// RootDocument is the lightweight, non-normative application entry point
// served at "/". It is NOT the spec's `Collection` state (that's out of
// scope; see SCOPE.md) -- it's just a set of links to the entry points this
// server does implement, per Section 1.3.7 ("It is RECOMMENDED that entry
// points include Collections, Collection, and Person").
type RootDocument struct {
	Title            string `json:"title"`
	Description      string `json:"description"`
	RootsMagicSchema string `json:"rootsMagicSchema,omitempty"`
	Links            Links  `json:"links"`
}

func boolPtr(b bool) *bool { return &b }

// BoolPtr returns a pointer to b. Exported for use by other packages building
// gedcomx documents.
func BoolPtr(b bool) *bool { return boolPtr(b) }
