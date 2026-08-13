package gedcomx

import (
	"net/url"
	"strings"
)

// gedcomTagToFactType maps GEDCOM 5.5.1 tags (as stored in RootsMagic's
// FactTypeTable.GedcomTag) to their corresponding GEDCOM X Conceptual Model
// fact-type URIs. This covers the built-in RootsMagic fact types. Anything
// not in this table (including all "EVEN"-tagged user-defined fact types)
// falls back to a custom URI; see CustomFactType.
var gedcomTagToFactType = map[string]string{
	"BIRT": "http://gedcomx.org/Birth",
	"CHR":  "http://gedcomx.org/Christening",
	"DEAT": "http://gedcomx.org/Death",
	"BURI": "http://gedcomx.org/Burial",
	"CREM": "http://gedcomx.org/Cremation",
	"ADOP": "http://gedcomx.org/AdoptiveParent",
	"BAPM": "http://gedcomx.org/Baptism",
	"BARM": "http://gedcomx.org/BarMitzvah",
	"BASM": "http://gedcomx.org/BatMitzvah",
	"BLES": "http://gedcomx.org/Blessing",
	"CHRA": "http://gedcomx.org/AdultChristening",
	"CONF": "http://gedcomx.org/Confirmation",
	"FCOM": "http://gedcomx.org/FirstCommunion",
	"ORDN": "http://gedcomx.org/Ordination",
	"NATU": "http://gedcomx.org/Naturalization",
	"EMIG": "http://gedcomx.org/Emigration",
	"IMMI": "http://gedcomx.org/Immigration",
	"CENS": "http://gedcomx.org/Census",
	"PROB": "http://gedcomx.org/Probate",
	"WILL": "http://gedcomx.org/Will",
	"GRAD": "http://gedcomx.org/Graduation",
	"RETI": "http://gedcomx.org/Retirement",
	"DIVF": "http://gedcomx.org/DivorceFiling",
	"OCCU": "http://gedcomx.org/Occupation",
	"RESI": "http://gedcomx.org/Residence",
	"EDUC": "http://gedcomx.org/Education",
	"NATI": "http://gedcomx.org/Nationality",
	"RELI": "http://gedcomx.org/Religion",
	"SSN":  "http://gedcomx.org/NationalId",
	"TITL": "http://gedcomx.org/TitleOfNobility",
	"CAST": "http://gedcomx.org/Caste",
	"DSCR": "http://gedcomx.org/PhysicalDescription",
	"PROP": "http://gedcomx.org/Property",
	// Family/couple facts.
	"MARR": "http://gedcomx.org/Marriage",
	"MARB": "http://gedcomx.org/MarriageBanns",
	"MARC": "http://gedcomx.org/MarriageContract",
	"MARL": "http://gedcomx.org/MarriageLicense",
	"MARS": "http://gedcomx.org/MarriageSettlement",
	"DIV":  "http://gedcomx.org/Divorce",
	"ANUL": "http://gedcomx.org/Annulment",
	"ENGA": "http://gedcomx.org/Engagement",
	"SEPR": "http://gedcomx.org/Separation",
}

// GenderTypeURI maps RootsMagic PersonTable.Sex (0=Male,1=Female,2=Unknown)
// to a GEDCOM X gender type URI.
func GenderTypeURI(sex int) string {
	switch sex {
	case 0:
		return "http://gedcomx.org/Male"
	case 1:
		return "http://gedcomx.org/Female"
	default:
		return "http://gedcomx.org/Unknown"
	}
}

// GenderCode is GenderTypeURI's inverse, for creating a new Person: maps
// a GEDCOM X gender type URI to RootsMagic's own Sex encoding. ok is
// false for anything other than the three URIs GenderTypeURI itself ever
// produces -- a client sending something else gets a clear rejection at
// the API layer rather than an arbitrary default.
func GenderCode(uri string) (sex int, ok bool) {
	switch uri {
	case "http://gedcomx.org/Male":
		return 0, true
	case "http://gedcomx.org/Female":
		return 1, true
	case "http://gedcomx.org/Unknown":
		return 2, true
	default:
		return 0, false
	}
}

// NameTypeURI maps RootsMagic NameTable.NameType to a GEDCOM X name type URI.
// RootsMagic: 0=Null(Primary) 1=AKA 2=Birth 3=Immigrant 4=Maiden 5=Married
// 6=Nickname 7=Other Spelling.
func NameTypeURI(nameType int) string {
	switch nameType {
	case 1:
		return "http://gedcomx.org/AlsoKnownAs"
	case 2:
		return "http://gedcomx.org/BirthName"
	case 4:
		return "http://gedcomx.org/MaidenName"
	case 5:
		return "http://gedcomx.org/MarriedName"
	case 6:
		return "http://gedcomx.org/Nickname"
	default:
		return ""
	}
}

// NameTypeCode is NameTypeURI's inverse, for creating a new Person's
// name: maps a GEDCOM X name type URI to RootsMagic's own NameType
// encoding. An empty uri (the field simply omitted) maps to 0 (Primary/
// Null), matching every real captured create in this project's own
// reference data, none of which set a name type explicitly. ok is false
// for anything not in NameTypeURI's own set -- a client sending
// something else gets a clear rejection rather than an arbitrary
// default.
func NameTypeCode(uri string) (nameType int, ok bool) {
	switch uri {
	case "":
		return 0, true
	case "http://gedcomx.org/AlsoKnownAs":
		return 1, true
	case "http://gedcomx.org/BirthName":
		return 2, true
	case "http://gedcomx.org/MaidenName":
		return 4, true
	case "http://gedcomx.org/MarriedName":
		return 5, true
	case "http://gedcomx.org/Nickname":
		return 6, true
	default:
		return 0, false
	}
}

// FactType resolves a RootsMagic fact type (by its GEDCOM tag and, as a
// fallback, its RootsMagic display name) to a GEDCOM X fact-type URI.
func FactType(gedcomTag, rmFactTypeName string) string {
	tag := strings.ToUpper(strings.TrimSpace(gedcomTag))
	if tag != "" && tag != "EVEN" {
		if uri, ok := gedcomTagToFactType[tag]; ok {
			return uri
		}
	}
	return CustomFactType(rmFactTypeName)
}

// factTypeToGedcomTag is gedcomTagToFactType inverted -- built once, used
// by GedcomTagForFactType below (this project's write side; the read
// side above never needs this direction).
var factTypeToGedcomTag = func() map[string]string {
	m := make(map[string]string, len(gedcomTagToFactType))
	for tag, uri := range gedcomTagToFactType {
		m[uri] = tag
	}
	return m
}()

// GedcomTagForFactType is FactType's inverse for the built-in fact types:
// given a GEDCOM X fact-type URI, returns the GEDCOM tag a caller can
// look up against RootsMagic's own FactTypeTable.GedcomTag to find the
// matching FactTypeID (see internal/api's create-person handler).
//
// Deliberately does not attempt the reverse of CustomFactType (a client
// sending a "http://rootsmagic.local/fact-type/..." URI back) -- that
// would mean either matching an existing custom fact type by name (fine)
// or silently creating a new FactTypeTable row on the fly (a materially
// bigger, riskier feature this project hasn't built or verified yet).
// ok is false for both custom URIs and anything unrecognized; callers
// should treat both the same way, a clear rejection rather than a guess.
func GedcomTagForFactType(uri string) (tag string, ok bool) {
	tag, ok = factTypeToGedcomTag[uri]
	return tag, ok
}

// CustomFactType builds a stable, custom fact-type URI for a RootsMagic fact
// type that has no GEDCOM X Conceptual Model equivalent (this includes all
// user-defined fact types, which is most of what a RootsMagic file adds
// beyond the built-ins).
func CustomFactType(rmFactTypeName string) string {
	name := strings.TrimSpace(rmFactTypeName)
	if name == "" {
		name = "Unknown"
	}
	return "http://rootsmagic.local/fact-type/" + url.PathEscape(name)
}
