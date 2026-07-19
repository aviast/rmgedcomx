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
