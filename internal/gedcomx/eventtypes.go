package gedcomx

import (
	"net/url"
	"strings"
)

// gedcomTagToEventType maps GEDCOM 5.5.1 tags to the GEDCOM X Conceptual
// Model's "known event types" (Section 2.5.1) -- a deliberately smaller,
// separate table from gedcomTagToFactType, not a subset reused from it:
// most tags that overlap between the two (birth, death, marriage, burial,
// christening, census, divorce) do resolve to the identical URI either
// way, but "ADOP" doesn't -- as a *fact* it's
// "http://gedcomx.org/AdoptiveParent" (a fact about being an adoptive
// parent), but as an *event* it's "http://gedcomx.org/Adoption" (the
// adoption event itself). Reusing one table for both would have silently
// mislabeled that case, so they're kept independent.
var gedcomTagToEventType = map[string]string{
	"ADOP": "http://gedcomx.org/Adoption",
	"BIRT": "http://gedcomx.org/Birth",
	"BURI": "http://gedcomx.org/Burial",
	"CENS": "http://gedcomx.org/Census",
	"CHR":  "http://gedcomx.org/Christening",
	"DEAT": "http://gedcomx.org/Death",
	"DIV":  "http://gedcomx.org/Divorce",
	"MARR": "http://gedcomx.org/Marriage",
}

// EventType resolves a RootsMagic fact type (by its GEDCOM tag and, as a
// fallback, its RootsMagic display name) to a GEDCOM X *event*-type URI --
// used for Event.type, not Fact.type (see FactType for that, and the
// gedcomTagToEventType doc comment for why these are deliberately separate
// mappings despite drawing from the same RootsMagic FactTypeTable row).
func EventType(gedcomTag, rmFactTypeName string) string {
	tag := strings.ToUpper(strings.TrimSpace(gedcomTag))
	if tag != "" && tag != "EVEN" {
		if uri, ok := gedcomTagToEventType[tag]; ok {
			return uri
		}
	}
	return CustomEventType(rmFactTypeName)
}

// CustomEventType builds a stable, custom event-type URI for a RootsMagic
// fact type that has no GEDCOM X "known event type" equivalent (Section
// 2.5.1 lists only eight -- most RootsMagic fact types, built-in or
// user-defined, fall outside it). Deliberately a distinct URI namespace
// from CustomFactType ("event-type" vs "fact-type"), even though both can
// be driven by the same underlying RootsMagic fact type name, since the
// Fact and the Event built from the same EventTable row are, per spec,
// "described independently" (Section 2.5.2) and are different resources.
func CustomEventType(rmFactTypeName string) string {
	name := strings.TrimSpace(rmFactTypeName)
	if name == "" {
		name = "Unknown"
	}
	return "http://rootsmagic.local/event-type/" + url.PathEscape(name)
}

// roleNameToEventRoleType maps common English role names (as free text in
// RootsMagic's user-editable RoleTable.RoleName, from its "Edit Role Type"
// window) to the GEDCOM X Conceptual Model's "known role types" (Section
// 3.15.1). Deliberately conservative: only unambiguous, common terms are
// mapped here (RoleTable.RoleName is arbitrary user text, e.g. "Best Man"
// or "Bridesmaid", that this server has no reliable way to classify);
// anything else falls back to a custom URI rather than guessing. See
// EventRoleType.
var roleNameToEventRoleType = map[string]string{
	"principal":   "http://gedcomx.org/Principal",
	"participant": "http://gedcomx.org/Participant",
	"witness":     "http://gedcomx.org/Witness",
	"official":    "http://gedcomx.org/Official",
	"officiant":   "http://gedcomx.org/Official",
	"minister":    "http://gedcomx.org/Official",
	"clergy":      "http://gedcomx.org/Official",
	"celebrant":   "http://gedcomx.org/Official",
}

// EventRoleType resolves a RootsMagic RoleTable.RoleName to a GEDCOM X
// EventRole type URI.
func EventRoleType(roleName string) string {
	if uri, ok := roleNameToEventRoleType[strings.ToLower(strings.TrimSpace(roleName))]; ok {
		return uri
	}
	return CustomEventRoleType(roleName)
}

// CustomEventRoleType builds a stable, custom event-role-type URI for a
// RootsMagic role name that doesn't match one of GEDCOM X's four known
// role types.
func CustomEventRoleType(roleName string) string {
	name := strings.TrimSpace(roleName)
	if name == "" {
		name = "Unknown"
	}
	return "http://rootsmagic.local/event-role/" + url.PathEscape(name)
}
