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
	MarriageDate      string       `json:"marriageDate,omitempty"`
	MarriagePlace     string       `json:"marriagePlace,omitempty"`
	AscendancyNumber  string       `json:"ascendancyNumber,omitempty"`
	DescendancyNumber string       `json:"descendancyNumber,omitempty"`
	FamiliesAsParent  []FamilyView `json:"familiesAsParent,omitempty"`
	FamiliesAsChild   []FamilyView `json:"familiesAsChild,omitempty"`
}

// Person is a description of a person, per the GEDCOM X Conceptual Model,
// plus the RS extensions `living` and `display` (Section 3.4).
type Person struct {
	ID          string            `json:"id"`
	Identifiers Identifiers       `json:"identifiers,omitempty"`
	Living      *bool             `json:"living,omitempty"`
	Private     bool              `json:"private,omitempty"`
	Gender      *Gender           `json:"gender,omitempty"`
	Names       []Name            `json:"names,omitempty"`
	Facts       []Fact            `json:"facts,omitempty"`
	Sources     []SourceReference `json:"sources,omitempty"`
	// Media holds illustrative artifacts (photos, scans, ...) -- distinct
	// from Sources (bibliographic evidence), per the Subject data type's
	// own definition. See SCOPE.md's "Sources versus media" section for
	// why these were combined in an earlier version of this server, and
	// why that turned out to be wrong.
	Media   []SourceReference  `json:"media,omitempty"`
	Notes   []Note             `json:"notes,omitempty"`
	Display *DisplayProperties `json:"display,omitempty"`
	Links   Links              `json:"links,omitempty"`
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
	Sources []SourceReference `json:"sources,omitempty"`
	Media   []SourceReference `json:"media,omitempty"`
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
	Sources   []SourceReference       `json:"sources,omitempty"`
	Media     []SourceReference       `json:"media,omitempty"`
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
	ID           string           `json:"id"`
	ResourceType string           `json:"resourceType,omitempty"`
	Citations    []SourceCitation `json:"citations,omitempty"`
	MediaType    string           `json:"mediaType,omitempty"`
	About        string           `json:"about,omitempty"`
	Titles       []TextValue      `json:"titles,omitempty"`
	Notes        []Note           `json:"notes,omitempty"`
	SortKey      string           `json:"sortKey,omitempty"` // RS extension (Section 3.5)
	// MediaPath is write-only, artifacts-specific, and not part of the
	// GEDCOM X spec (which has no concept of a raw filesystem path -- see
	// SCOPE.md's "Write support" section for why one's needed anyway, and
	// why "?" is the only encoding this server will ever write). Never
	// populated on read: a real absolute path, from the client's own
	// filesystem, sent when updating an artifact's location via
	// POST /artifacts/{id}. Ignored entirely for updates to actual
	// Source Descriptions (POST /source-descriptions/{id}), which share
	// this same Go type for their response/request shape but have no
	// concept of a file location.
	MediaPath string `json:"mediaPath,omitempty"`
	Links     Links  `json:"links,omitempty"`
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
	// Relationships is always present, even as an empty array -- RS
	// spec Section 4.10.5, "Embedded States": child-relationships,
	// parent-relationships, and spouse-relationships are each MUST,
	// either as a link or embedded directly. This server embeds. An
	// omitted field would be ambiguous between "this person genuinely
	// has none" and "this server doesn't support the embedded state at
	// all" -- the second was true until this was added; the field's
	// unconditional presence is what actually resolves that ambiguity
	// for a real client.
	Relationships []Relationship `json:"relationships"`
	Links         Links          `json:"links,omitempty"`
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

// IdentifierTypeRootsMagicUniqueID is a custom identifier type URI for
// RootsMagic's own per-database <UniqueID> (see rmdb.UniqueID), exposed on
// Collection.identifiers. Not part of the GEDCOM X RS spec -- Collection's
// defined fields (gedcomx-record spec Section 2.1) are id/lang/content/
// title/size/attribution, with no identifiers field -- but `identifiers`
// itself is a standard, reusable GEDCOM X JSON mechanism (used elsewhere
// in this server on Person), and a stable per-database identifier is
// useful supplementary metadata for a client sophisticated enough to want
// one, alongside (never instead of) Collection.id -- see SCOPE.md's
// "Multiple databases / Collections" section for why id itself is
// deliberately human-recognizable rather than durable. Follows the same
// custom-URI convention already used for fact types (facttypes.go):
// http://rootsmagic.local/...
const IdentifierTypeRootsMagicUniqueID = "http://rootsmagic.local/unique-id"

// Collection is the GEDCOM X Record Extensions "Collection" data type
// (http://gedcomx.org/v1/Collection, gedcomx-record spec Section 2.1): a
// collection of genealogical data. A RootsMagic database file is modeled
// as exactly one Collection -- see SCOPE.md's "Multiple databases /
// Collections" section.
type Collection struct {
	ID          string              `json:"id,omitempty"`
	Lang        string              `json:"lang,omitempty"`
	Content     []CollectionContent `json:"content,omitempty"`
	Title       string              `json:"title,omitempty"`
	Identifiers Identifiers         `json:"identifiers,omitempty"`
	Links       Links               `json:"links,omitempty"`
}

// CollectionContent is the GEDCOM X Record Extensions "CollectionContent"
// data type (http://gedcomx.org/v1/CollectionContent, gedcomx-record spec
// Section 2.2): a count of resources of a given type held by a Collection.
type CollectionContent struct {
	ResourceType string `json:"resourceType"`
	Count        int    `json:"count,omitempty"`
}

// Known resourceType URIs for CollectionContent, taken from the
// "identifier" declared for each corresponding data type in the GEDCOM X
// Conceptual Model.
const (
	ResourceTypePerson            = "http://gedcomx.org/v1/Person"
	ResourceTypeRelationship      = "http://gedcomx.org/v1/Relationship"
	ResourceTypePlaceDescription  = "http://gedcomx.org/v1/PlaceDescription"
	ResourceTypeSourceDescription = "http://gedcomx.org/v1/SourceDescription"
	ResourceTypeEvent             = "http://gedcomx.org/v1/Event"
)

// ResourceTypeDigitalArtifact is one of the "known resource types" from the
// GEDCOM X Conceptual Model, Section 2.3.1 (distinct namespace from the
// "/v1/" data-type identifiers above -- this one describes what *kind* of
// thing a SourceDescription is, not which data type it is). Used both as
// the resourceType of each multimedia-derived SourceDescription and as the
// CollectionContent.resourceType for the artifacts count, since it's more
// specific than the generic SourceDescription identifier.
const ResourceTypeDigitalArtifact = "http://gedcomx.org/DigitalArtifact"

// SubjectReference and SubjectReferencesDocument are rmgedcomx's own
// non-spec extension for reverse lookup: given an artifact, which
// Subjects (Person, Relationship, Event, or PlaceDescription) reference
// it? GEDCOM X RS defines no such operation -- SourceDescription has no
// inverse "referencedBy" field in the conceptual model (see SCOPE.md's
// "Sources versus media" section), so a client can only ever discover
// this by enumerating every Subject in a collection and checking each
// one's own media array, which doesn't scale. See
// /artifacts/{id}/persons, /events, and /subjects in handlers.go.
//
// Deliberately a lightweight reference, not the embedded full resource:
// a caller that needs the full resource fetches it separately via Href.
// ResourceType uses the same ResourceType* URIs as CollectionContent
// above, so a client already handling those doesn't need a second
// vocabulary to recognize which kind of Subject each reference is.
type SubjectReference struct {
	ResourceType string `json:"resourceType"`
	ID           string `json:"id"`
	Href         string `json:"href"`
}

type SubjectReferencesDocument struct {
	References []SubjectReference `json:"references"`
}

// CollectionsDocument is the `Collections` application state representation
// (RS spec Section 4.4): a list of collections.
type CollectionsDocument struct {
	Results     int          `json:"results"`
	Collections []Collection `json:"collections"`
	Links       Links        `json:"links,omitempty"`
}

// CollectionDocument wraps a single Collection as the top-level
// `Collection` application state document (RS spec Section 4.5).
type CollectionDocument struct {
	Collections []Collection `json:"collections"`
	Links       Links        `json:"links,omitempty"`
}

// Event is the GEDCOM X Conceptual Model "Event" data type
// (http://gedcomx.org/v1/Event, Section 2.5): a description of a
// historical event, extending Subject (id/sources/notes/etc., per the
// Conclusion/Subject inheritance chain -- Event doesn't get its own
// distinct extra Subject fields modeled here, since RootsMagic has no
// data backing extracted/evidence/media/identifiers for a shared event).
//
// This is deliberately a different concept from Fact (Section 2.5.2 of the
// spec draws the distinction explicitly: "this specification dictates that
// the two concepts are described independently"). A Fact belongs to and is
// meaningless outside the context of one Person or Relationship; an Event
// exists independently and can have multiple participants in different
// roles -- exactly the case RootsMagic's WitnessTable exists for. See
// SCOPE.md's "Events" section for how the two map onto the same
// underlying RootsMagic EventTable row.
type Event struct {
	ID      string            `json:"id,omitempty"`
	Type    string            `json:"type,omitempty"`
	Date    *Date             `json:"date,omitempty"`
	Place   *PlaceReference   `json:"place,omitempty"`
	Roles   []EventRole       `json:"roles,omitempty"`
	Sources []SourceReference `json:"sources,omitempty"`
	Media   []SourceReference `json:"media,omitempty"`
	Notes   []Note            `json:"notes,omitempty"`
	Links   Links             `json:"links,omitempty"`
}

// EventRole is the GEDCOM X Conceptual Model "EventRole" data type
// (http://gedcomx.org/v1/EventRole, Section 3.15): a role played in an
// event by a person.
type EventRole struct {
	Person  ResourceReference `json:"person"`
	Type    string            `json:"type,omitempty"`
	Details string            `json:"details,omitempty"`
}

// EventsDocument is the `Events` application state representation (RS
// spec Section 4.7): a list of events.
type EventsDocument struct {
	Results int     `json:"results"`
	Events  []Event `json:"events"`
	Links   Links   `json:"links,omitempty"`
}

// EventDocument wraps a single Event as the top-level `Event` application
// state document (RS spec Section 4.8).
type EventDocument struct {
	Events []Event `json:"events"`
	Links  Links   `json:"links,omitempty"`
}

func boolPtr(b bool) *bool { return &b }

// BoolPtr returns a pointer to b. Exported for use by other packages building
// gedcomx documents.
func BoolPtr(b bool) *bool { return boolPtr(b) }
