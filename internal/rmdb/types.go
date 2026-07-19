package rmdb

// Person is a row from PersonTable, trimmed to the columns this server uses.
type Person struct {
	PersonID int64
	Sex      int   // 0=Male, 1=Female, 2=Unknown
	Living   int   // 0=Deceased, 1=Living
	ParentID int64 // FamilyTable.FamilyID this person is a child in (0 = none), "last active" set
	SpouseID int64 // FamilyTable.FamilyID this person is a spouse in (0 = none), "last active" set
}

// Name is a row from NameTable.
type Name struct {
	NameID    int64
	OwnerID   int64
	Surname   string
	Given     string
	Prefix    string
	Suffix    string
	Nickname  string
	NameType  int
	IsPrimary int
	SortDate  int64
	BirthYear int
	DeathYear int
	Date      string
}

// Family is a row from FamilyTable.
type Family struct {
	FamilyID int64
	FatherID int64
	MotherID int64
}

// Child is a row from ChildTable.
type Child struct {
	RecID      int64
	ChildID    int64
	FamilyID   int64
	RelFather  int
	RelMother  int
	ChildOrder int
}

// Event is a row from EventTable (used both for person facts, OwnerType=0,
// and family/couple facts, OwnerType=1).
type Event struct {
	EventID   int64
	EventType int64 // FactTypeTable.FactTypeID
	OwnerType int   // 0=Person, 1=Family
	OwnerID   int64
	FamilyID  int64
	PlaceID   int64
	Date      string
	IsPrimary int
	Details   string
	Note      string
}

// FactType is a row from FactTypeTable.
type FactType struct {
	FactTypeID int64
	Name       string
	GedcomTag  string
	OwnerType  int
}

// Place is a row from PlaceTable.
type Place struct {
	PlaceID   int64
	PlaceType int
	Name      string
	Latitude  int64 // decimal degrees * 1e7
	Longitude int64 // decimal degrees * 1e7
	Note      string
}

// Source is a row from SourceTable.
type Source struct {
	SourceID   int64
	Name       string
	RefNumber  string
	ActualText string
	Comments   string
}

// CitationLink is a row from CitationLinkTable joined to CitationTable, used
// to resolve which sources support a given person/family/event/name.
type CitationLink struct {
	OwnerType int // 0=Person, 1=Family, 2=Event, 6=Task, 7=Name, 19=Association
	OwnerID   int64
	SourceID  int64
}

// OwnerType values for CitationLinkTable.OwnerType (and, where applicable,
// EventTable.OwnerType).
const (
	OwnerTypePerson = 0
	OwnerTypeFamily = 1
	OwnerTypeEvent  = 2
	OwnerTypeName   = 7
)
