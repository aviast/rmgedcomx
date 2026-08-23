# Scope and design notes

This document describes what rmgedcomx implements, what it deliberately
leaves out, and why -- as the server behaves today. For the history of how
it got here (bugs found and fixed, design mistakes corrected, the staged
rollout of write support), see [HISTORY.md](./HISTORY.md).

rmgedcomx is a [GEDCOM X RS](https://github.com/FamilySearch/gedcomx-rs)
API server backed directly by a RootsMagic 8+ `.rmtree` SQLite file. It
reads the file's own tables at request time -- there's no separate index
or intermediate database -- and, when `-write` is enabled, can create and
update a limited set of records directly in that same file.

## Why "core resources" and not the whole spec

GEDCOM X RS is written for multi-user, hosted, collaborative genealogy
services (its worked examples are effectively FamilySearch's own API). A
big chunk of it exists to support things a single-user desktop database
doesn't need:

- **OAuth2** (`Section 9`) -- there's no multi-tenant auth story for a file
  on your own disk. Bolting on OAuth would add real complexity for no real
  security benefit on `localhost`/LAN use, and would get in the way of
  hitting the API from a script. To expose this server on the open
  internet, put it behind a reverse proxy (e.g. Caddy/nginx) with its own
  auth.
- **`Records`** -- this models a hosted archive of historical records (e.g.
  "the 1940 U.S. Census" as a queryable collection in its own right).
  RootsMagic doesn't have an equivalent concept.
- **`Person Matches` / record hinting** -- matching a person against
  external record collections (FamilySearch, Ancestry, etc.) is a hosted
  service concern, not something a local RootsMagic file has any part in.
- **Full write coverage** -- write support exists but is scoped narrowly;
  see "Write support" below for exactly what's implemented and what isn't.

### What is included

The resources that map directly onto what's actually in a RootsMagic file,
and are useful for read (and, where noted, write) access from another tool
(a family tree viewer, a static site generator, a Digital Asset Management
tool, a chatbot, etc.):

`Collection`, `Collections`, `Person`, `Persons`, `Person Parents`, `Person
Children`, `Person Spouses`, `Ancestry Results`, `Descendancy Results`,
`Relationship`, `Relationships`, `Place Description`, `Place Descriptions`,
`Source Description`, `Source Descriptions`, `Artifacts` (backed by
`MultimediaTable` -- scanned certificates, photos, and similar), `Events` /
`Event` (backed by `EventTable` + `WitnessTable` + `RoleTable` -- shared
events with multiple participants, like a marriage with witnesses),
`Person Search Results`, `Place Search Results` (Atom/JSON-based query
search -- see "Search" below for what's actually supported).

Each `Person` embeds its conclusions (names, gender, facts) directly in
the same response, per the spec's fallback rule in Section 4.10.5 ("If no
link to `conclusions` is provided, the list of conclusions MUST be
included in the original request"). This avoids needing separate
`/persons/{id}/conclusions` endpoints.

## Multiple databases / Collections

The `Collection` state (RS spec Section 4.5, data type defined in the
[GEDCOM X Record Extensions](https://github.com/FamilySearch/gedcomx-record/blob/master/specifications/record-specification.md#collection))
is the discovery root of this API: it's the one state formally specified
to carry `persons`, `relationships`, `source-descriptions`, and
`subcollections` links (Section 4.5.4's transitions table), which is
exactly the set of top-level resources this server exposes. The spec's
`Collection` data type is just `id` / `title` / `content` (counts by
resource type) / `links`, which maps onto a single RootsMagic file
directly.

**One RootsMagic file == one `Collection`.** `-db` is repeatable -- pass it
multiple times to serve several databases in one process, each as its own,
fully independent `Collection`:

```sh
./rmgedcomx -db Tree1.rmtree -db Tree2.rmtree -db royal92.rmtree
```

Every resource URL is scoped under its collection:
`/collections/{id}/persons/P1`, `/collections/{id}/relationships/F3`, and
so on. This is what makes multiple open collections unambiguous: two
different databases' `P1` are different URLs, not the same one.

Discovery states span every collection:

- `GET /` -- the root, for a client that only knows the base URL. Serves
  the `Collections` list (see below), not a single `Collection` -- with
  potentially more than one collection open, there's no longer a single
  one for the root to unambiguously be.
- `GET /collections` -- the formal `Collections` (list) state: every
  collection currently open, in the order their `-db` flags were given.
- `GET /collections/{id}` -- the formal `Collection` (single) state for
  one of them.

Architecturally, this is implemented without threading a collection id
through every handler and builder function. Each `*api.Server`
(`internal/api/server.go`) stays scoped to one database, unaware any other
might exist. `internal/api/multi.go` (`NewMultiCollectionHandler`) is the
only thing that knows there can be more than one -- it owns the
cross-collection `Collections`/`Collection` handlers, and mounts each
`Server`'s own resource routes (`Server.resourceHandler()`) under
`/collections/{id}/` via `http.StripPrefix`. `Server.url()` builds every
resource link against a precomputed, collection-scoped base URL
(`BaseURL + "/collections/" + ID`), so `convert.go` and `handlers.go` need
no special-casing to produce correctly-scoped links -- they only ever
build links relative to "this collection's base," which simply includes
the `/collections/{id}` prefix. (`Server.globalURL()` is the one
exception, used only for the `subcollections` link, which intentionally
points outside the collection's own scope, at the global `/collections`
list.)

`content` counts (`CollectionStats` in `internal/rmdb/queries.go`) are
computed with plain `COUNT(*)` SQL, not by materializing the full resource
lists, so listing collections stays cheap even with several large trees
open. The relationship count mirrors the exact logic `handleRelationships`
uses to build the real list (one `Couple` relationship per family with
both parents present, plus one `ParentChild` relationship per parent-child
pair), so the number a client sees here always matches what `GET
/collections/{id}/relationships` actually returns.

One link is a deliberate departure from the spec: `place-descriptions`.
The formal transitions table for `Collection` (Section 4.5.4) doesn't
define a plural rel for the Place Descriptions list state, and neither
does the master link-relation table (Section 5.2) -- there's a singular
`description` rel for one place description, but nothing for the list.
Rather than leave `/places` undiscoverable from the Collection,
`place-descriptions` is added anyway, following the `source-descriptions`
naming convention, under Section 4.5.4's explicit allowance: "other
transitions... is RECOMMENDED where applicable." A strict client that only
walks formally-specified rels won't find `/places` this way.

### Collection ids and titles: human-recognizable, not persistent

`internal/collectionid` derives each collection's id and title from two
things: the RootsMagic **Home Person** (`RootPerson` in `ConfigTable`,
read via `rmdb.RootPersonDisplayName()` -- a plain integer `PersonID`
inside `ConfigTable`'s Database Configuration record, which is itself
plain, readable XML, not an opaque format; see "SQLite driver" below) and
the database's filename. Both are combined deliberately: the Home
Person's name is what a human actually recognizes a family tree by, but
the filename is what disambiguates between multiple exports/backups of
the *same* tree over time, where the Home Person is identical between
them and only the filename differs. RootsMagic's own auto-backup naming
embeds a timestamp in the filename for exactly this reason (`royal92 -
2024 06 24 09-29.rmtree`), and rather than parsing out "the timestamp"
specifically -- fragile, and only correct for RootsMagic's own naming
convention -- `Derive` just uses the whole filename stem, which captures
that case for free and degrades gracefully for any other naming. A
trivial numeric-suffix pass (`Dedupe`) runs across the whole batch of
collections at startup as a last resort, only visible in the rare case
two collections would otherwise land on the exact same id.

**This server makes no promise that a given database is represented by
the same Collection id across restarts, and doesn't try to.** A few
concrete reasons it can't be: the Home Person is a user-editable setting
in RootsMagic, not a fixed property of the file; files get renamed,
moved, or restored from backup; and which `-db` flags are passed, and in
what order, is under the user's control each time the server starts. A
technically stable identifier *does* exist -- RootsMagic assigns a
`UniqueID` per database at creation (`ConfigTable`'s XML again) that never
changes -- but it's a meaningless opaque string to a human, and the whole
point of the id is to be something a person glances at and recognizes.

So the design leans the other way entirely: derive the friendliest id
reasonably possible, and make it easy to see what's actually running. At
startup, the server prints a table mapping every collection id to its
title and source file (see `cmd/server/main.go`, `printCollectionTable`);
a person reads that table and connects a client to the right id for that
session. **No client should persist a collection id across sessions** --
discover fresh via `GET /collections` every time a client starts.

## Multimedia

`GET /artifacts` and `GET /artifacts/{id}` implement the RS spec's
`Artifacts` state (Section 4.3), backed by RootsMagic's `MultimediaTable`.
Per Section 4.3.3, the data returned is a list of the same
`SourceDescription` data type used by `/source-descriptions`
(`resourceType` is set to the more specific
`http://gedcomx.org/DigitalArtifact` to distinguish artifacts from
bibliographic sources -- see the doc comment on
`ResourceTypeDigitalArtifact` in `internal/gedcomx/model.go`), so
`internal/api/handlers.go` reuses the same
`SourceDescriptionsDocument`/`SourceDescriptionDocument` JSON envelope
types for both endpoints; that's spec-correct, since the JSON member name
(`sourceDescriptions`) is a property of the *data type*, not of which
state returned it.

There's no formally-specified state for "download the actual bytes" -- the
spec's mechanism for that is `SourceDescription.about`, "a URI for the
resource being described." This server points `about` (and a non-spec
`digital-artifact` link, for convenience) at `GET
/artifacts/{id}/content`, which streams the raw file with a `Content-Type`
inferred from the filename and supports HTTP range requests (via
`http.ServeContent`) -- useful for a client previewing large images or
seeking within video.

### How photos/certificates attach to people: citations, not just facts

Media isn't only found via `MediaLinkTable` rows attached directly to a
person or a fact -- in a real, well-populated RootsMagic database, the
majority of attached files live on *citations* instead (`MediaLinkTable`
`OwnerType = 4`, `OwnerID = CitationTable.CitationID`): e.g. a scanned
1911 census image lives on the "1911 Census" citation attached to a
residence fact, not on the fact itself.

So `buildSourcesAndMedia` (`internal/api/convert.go`, which populates the
`sources` and `media` arrays on every `Person`, `Relationship`, `Event`,
and `PlaceDescription`) does three lookups and merges them, deduplicated:
bibliographic sources cited directly, media attached directly to the
person/family/event/name/place, and media attached to *that owner's
citations*. All of this surfaces together, as `SourceReference`s or media
references pointing at either `/source-descriptions/S{id}` or
`/artifacts/M{id}` depending on which kind of thing they are -- a client
doesn't need to know which case it is; the URI shape distinguishes them.
This is currently done for `Person`, `Relationship`, `Event`,
`PlaceDescription`, and `Fact`; it isn't yet done for `Name`
(`OwnerType = 7`) specifically.

### Resolving a file to an actual path on disk

RootsMagic's `MultimediaTable.MediaPath` isn't a plain path -- it uses a
leading-symbol convention (`?` = the "Media Folder" configured in
RootsMagic's Folder Settings window, `~` = home directory, `*` = the
folder containing the database file), and in practice you'll also see
absolute paths with no symbol at all (`C:\Users\...`, `G:\My Drive\...` --
often a cloud-sync-mapped drive letter). `internal/rmdb/mediapath.go`
(`ResolveMediaPath`) implements this, normalizing backslashes and handling
each case; `internal/rmdb/mediapath_test.go` covers all of them against
concrete examples.

Two real limits worth knowing about:

- **The `?` (Media Folder) symbol can't be resolved automatically from the
  database file alone.** The Media Folder setting genuinely isn't part of
  the `.rmtree` file's data model at all -- it lives in a separate,
  per-Windows-user application settings file,
  `%APPDATA%\RootsMagic\Version 9\RootsMagicUser.xml`, under
  `<Folders><Media>`, entirely outside any database file. That makes
  sense once you think about it: a folder path is inherently specific to
  the machine it's configured on, so it can't sensibly travel with a
  database file that gets copied, shared, or opened on a different
  computer -- and it's also why `-media-folder` is a single flag shared
  across every `-db` given, rather than something configured per
  collection. If any `MediaPath` in your file uses `?`, pass the folder
  explicitly with `-media-folder` (for read-only use); without it, those
  items resolve with a clear error (`GET .../content` returns `500`
  naming the problem) rather than silently pointing at the wrong place.
  In write mode, the Media Folder is discovered automatically from
  `RootsMagicUser.xml` instead -- see "Write support" below.
- **A Windows absolute path (a drive letter) can't be resolved on a
  non-Windows host, full stop** -- `G:\My Drive\...` means nothing on
  Linux or macOS regardless of how cleverly it's parsed. This server
  passes such paths through as-is (best effort: if you're running the
  server on Windows itself, or the drive is genuinely mounted at that
  letter, it'll work) and returns a clear `404` naming the exact resolved
  path it tried, rather than a confusing generic error, when the file
  isn't actually there.

**A few confirmed facts about how RootsMagic itself produces `MediaPath`
values, worth knowing when preparing a database for use with this
server:**

- RootsMagic's own file-chooser dialog has no "store as relative path"
  option -- selecting a file always records its full, absolute path.
  Getting a `*`-relative path at all means manually editing the
  `MediaPath` field afterward in RootsMagic's own UI.
- A bare, symbol-less relative path is rejected by RootsMagic itself
  ("Media file not found"). This server's resolver still has a fallback
  for it (treating a symbol-less `MediaPath` as relative to the
  database's own directory) purely as defensive robustness.
- `*` requires a path separator immediately after it (`*royal92` is
  rejected; `*\royal92` is accepted). This server's own resolver isn't
  affected either way -- it trims a leading separator uniformly, so it
  treats both forms identically.
- RootsMagic's UI displays the expanded absolute path even after
  accepting and storing the symbolic form -- the symbol is a
  storage-and-portability convenience, transparent to the person using
  the software.

`~` is presumed to behave the same way as `*` (same symbol-expansion
mechanism), but that's inference, not independently confirmed the way `*`
was.

### Items that are links, not files

Not every `MultimediaTable` row is a local file. Databases built partly
from online-search integrations can have rows where `MediaPath` is
already a URL-shaped value from an external provider (e.g. `MediaPath =
http:\search.findmypast.com{0}\transcript?id=...`, `MediaFile` a number
presumably meant to be substituted into the `{0}` placeholder). That
substitution rule isn't documented anywhere this server could verify, so
rather than guess and risk presenting a broken link as if it worked,
`rmdb.LooksLikeExternalReference` detects the pattern (a URI-scheme-like
prefix) and, for those items, `buildArtifactDescription` skips `about`
and the content link entirely and adds a note explaining why. `GET
/artifacts/{id}/content` for one of these returns a clear `404` rather
than trying to open `http:\...` as a local file path. The item's other
metadata (caption, description, citation) is still returned normally --
only the "fetch the bytes" part is unavailable.

### MIME type inference

RootsMagic's own `MediaType` column is a coarse 4-value enum
(Image/File/Sound/Video), not reliable for a proper `Content-Type`, and
its `URL` column is documented as "Not implemented" and empty in every
real file used during development. Instead, `gedcomx.MediaTypeForFilename`
infers a MIME type from the file extension, checking a small built-in
table first (covering every extension actually observed:
jpg/jpeg/png/gif/bmp/tif/pdf/doc/docx/htm/html and a few others) before
falling back to Go's `mime.TypeByExtension`, so behavior doesn't depend
on the deployment environment having a populated `/etc/mime.types`.

### Sources versus media

`Person`, `Relationship`, `Event`, and `PlaceDescription` -- everything
that extends the conceptual model's `Subject` data type -- expose two
separate arrays, `sources` and `media`, not one combined list. `Fact` (a
`Conclusion`, not a `Subject`) only ever gets `sources`.

This follows the spec's own text, which draws the line explicitly:
`Subject.media` is defined as references to multimedia "intended to
provide additional context or illustration for the subject and *not*
considered evidence supporting the identity of the subject or its
supporting conclusions" -- a direct contrast with `sources`. Two
independent implementations (`gedcomx-js`'s `Subject.js` and
`gedcomx-rs`'s `person.rs`/`relationship.rs`/`event.rs`/
`placedescription.rs`) both have a distinct `media` field alongside
`sources`, with doc comments quoting the same spec language.

`buildSourcesAndMedia` (`internal/api/convert.go`) returns both arrays
from the same underlying query -- bibliographic citations go in
`sources`; artifacts (attached directly via `MediaLinkTable`, or via the
owner's citations -- see above) go in `media`. `Fact` calls this and
deliberately discards the `media` return value: a `Fact` has nowhere to
put it, but the same `EventTable` row's corresponding standalone `Event`
(same id, see "Events" below) does, and that's where it surfaces instead.
`PlaceDescription` has its own query for this too (`rmdb.OwnerTypePlace`).

## Events

`GET /events` and `GET /events/{id}` implement the RS spec's
`Events`/`Event` states (Sections 4.7, 4.8), backed by RootsMagic's
`EventTable` + `WitnessTable` + `RoleTable`. This is a genuinely
different GEDCOM X concept from the `Fact`s already embedded on every
`Person` and `Relationship` -- worth being precise about, since both are
built from the exact same underlying RootsMagic rows.

### Event versus Fact

The conceptual model spec draws this distinction explicitly (Section
2.5.2, "Events Versus Facts"): a `Fact` belongs to, and is meaningless
outside the context of, one `Person` or `Relationship` -- "facts do not
exist outside the scope of the subject to which they apply." An `Event`
exists independently and can have multiple participants in different
roles, "described independently" of any one person. The spec's own
illustrating example is close to this project's own motivating one: "a
birth record that provides information about biological parents, adoptive
parents, additional witnesses, etc. might justify a description of the
event in addition to descriptions of any facts provided by the record."
RootsMagic's `WitnessTable` -- additional participants beyond an event's
own owner, each with a role -- is precisely that "additional witnesses"
case, and a marriage is the clearest instance of it: the event has (at
least) two principals and, often, witnesses who aren't the couple
themselves.

`buildFact` and `buildEvent` both start from the same `rmdb.Event` (an
`EventTable` row) and produce two different resources, at two different
URLs, not one resource wearing two hats. They share an id on purpose: an
`Event`'s id is `E{EventID}`, the identical scheme `factRef` uses for the
corresponding `Fact`'s id nested inside a `Person` or `Relationship` (see
`parseEventID`'s doc comment) -- so if a client sees `"id": "E5049"` in a
`Relationship`'s `facts`, it already knows `GET /events/E5049` will
resolve to the fuller, multi-participant picture of that same occurrence,
with no separate lookup needed to make the connection.

### Every EventTable row becomes an Event, not just ones with witnesses

Consistent with how this server exposes every row of `PersonTable`,
`PlaceTable`, `SourceTable`, and `MultimediaTable` rather than filtering
to a subset it judges "interesting," `/events` covers the entire
`EventTable` -- most of which will have exactly one participant (the
`Principal`) and no witnesses, which is still a perfectly valid, if
unremarkable, `Event`. The alternative (only exposing events that have
`WitnessTable` rows) would mean `/events` silently omitted most events,
and would make the presence or absence of an `Event` resource depend on
incidental data richness rather than on the underlying occurrence itself.

### Roles: the owner as Principal, witnesses from WitnessTable

Every `Event`'s `roles` starts with its own owner, always given the
`Principal` role (Section 3.15.1's "known role type" for exactly this:
"the principal of a birth event is the person that was born"): one role
for a person-owned fact's `OwnerID`, or one role per known parent
(`FatherID`/`MotherID`) for a family-owned fact like a marriage.
Additional participants come from `WitnessTable` rows for that `EventID`,
with `EventRole.type` resolved from `RoleTable.RoleName` (free text the
user assigns via RootsMagic's "Edit Role Type" window) through
`gedcomx.EventRoleType` -- a conservative, small table of common English
terms ("witness" -> `Witness`, "officiant"/"minister"/"clergy" ->
`Official`, etc.) mapping to Section 3.15.1's four known role types, with
a `http://rootsmagic.local/event-role/...` custom-URI fallback for
anything else, following the same convention as fact types
(`CustomFactType`) and event types (`CustomEventType`, below).

`Event.type` is resolved by a *separate* function from `Fact.type`,
`EventType`, not by reusing `FactType` -- the two tables mostly agree
where the concepts overlap (birth, death, marriage, divorce, burial,
christening, census all resolve to the identical URI either way) but not
entirely: RootsMagic's "ADOP" fact type is `http://gedcomx.org/AdoptiveParent`
as a *fact* (a fact about being an adoptive parent) but
`http://gedcomx.org/Adoption` as an *event* (the adoption event itself) --
per the spec's own "known event types" (Section 2.5.1) versus "known fact
types" tables. `CustomEventType`'s fallback URI namespace (`event-type`
vs `fact-type`) is kept distinct too.

### Witnesses who aren't in the database

`WitnessTable.PersonID` can be `0`, meaning the witness isn't a person
recorded in this database at all -- RootsMagic stores their name as free
text instead (`WitnessTable.Given`/`Surname`). `royal92.rmtree`'s own
marriage event for Victoria and Albert (`E5049`) has both kinds side by
side -- twelve witnesses who are real `Person`s already in the database
(family members like Queen Adelaide, `P219`), and Victoria's twelve
bridesmaids, who aren't.

`EventRole.person` is REQUIRED by the spec and MUST resolve to a real
`Person` resource. A `PersonID=0` witness structurally cannot satisfy
that -- there is no `Person` resource to reference, and inventing one
(synthesizing a fake `Person` from just a name, or fabricating a
resolvable-looking URI that doesn't actually resolve) would misrepresent
what's actually in the source database. So these witnesses are simply
left out of `roles`, but not dropped from the response altogether: they're
collected into an `Event`-level note instead, e.g. (real data):

> Additional participants recorded by name only, not as persons in this
> database: Mary Howard (Bridesmaid); Caroline Gordon-Lennox (Bridesmaid);
> ... [ten more]

The role label in that note (e.g. "Bridesmaid") comes from `RoleTable`,
not `WitnessTable.Note`. The two are distinct pieces of information: the
role name (resolved through `gedcomx.EventRoleType`) is always shown when
set, and `Note` -- a multi-line free-text field for genuine supplementary
commentary, on the rare chance it's populated *alongside* a role -- is
appended separately (`"Name (Role): note text"`) rather than overriding
or blending with it. This mirrors how `EventRole.details` already works
for witnesses who *are* real `Person`s: the role type and its free-text
details are always two distinct pieces of information, never one
replacing the other.

## Embedded relationship states on the `Person` state

Per RS spec Section 4.10.5, "Embedded States": `child-relationships`,
`parent-relationships`, and `spouse-relationships` are each `MUST` for the
`Person` state -- "If no link to `child-relationships` is provided, the
list of child relationships MUST be included" in the same response (and
correspondingly for the other two).

`PersonDocument` (`internal/gedcomx/model.go`) has a `Relationships`
field, deliberately without `omitempty` -- an absent field would be
indistinguishable from a person who genuinely has none, which is the
whole ambiguity the spec's own `MUST` is there to resolve. `handlePerson`
(`internal/api/handlers.go`) populates it with every `ParentChild`
relationship where this person is a child, every `ParentChild`
relationship where this person is a parent, and every `Couple`
relationship this person is part of -- via
`personParentRelationships`/`personChildRelationships`/
`personSpouseRelationships`, the same helpers `GET
.../persons/{id}/parents`, `.../children`, and `.../spouses` use for
their own, separately-modeled relationship lists
(`PersonRelativesDocument`).

`royal92.rmtree`'s Victoria (`P1`) embeds exactly 12 relationships this
way -- her two parents, all nine of her real children with Albert, and
her own `Couple` relationship to Albert (complete with its `Marriage`
fact and sources). A person with genuinely zero relationships gets
`"relationships":[]`, never `null` or an absent field.

## `collection` link on the `Person` and `Relationship` states

Per the RS spec's own "Transitions" tables for both states: Section
4.10.4 for `Person` ("Link to the collection that contains this
person"), Section 4.21.4 for `Relationship` ("Link to the collection that
contains this relationship"). Both link directly to `s.collectionBaseURL`
(`internal/api/server.go`) -- the collection's own URL, computed once at
server startup -- with no path appended, since the collection state's own
URL is exactly that value.

## `DisplayProperties`

The RS spec's `DisplayProperties` extension (Section 2.2) is a set of
convenience, display-oriented properties on a `Person`: `name`, `gender`,
`lifespan`, `birthDate`, `birthPlace`, `deathDate`, `deathPlace`,
`marriageDate`, `marriagePlace`, `ascendancyNumber`, `descendancyNumber`,
`familiesAsParent`, `familiesAsChild`. All of these are populated except
`ascendancyNumber`/`descendancyNumber` (which only have meaning relative to
a specific ancestry/descendancy traversal, not a standalone person).

`birthPlace`/`deathPlace` come from this same person's own Birth/Death
facts. There's only ever one Birth and one Death fact per real person in
practice, so no "which one" ambiguity the way marriage has.

`marriageDate`/`marriagePlace` needed a real design decision the spec
itself doesn't make: a person can have more than one marriage, and
`DisplayProperties` has room for exactly one of each. Resolved by taking
the first family, consistently ordered (`FamiliesAsParent`'s own query,
`ORDER BY FamilyID` -- the same convention used for a person's primary
name and other "the first one" choices), and skipping to the next family
only if the first has no Marriage fact at all, rather than treating a
family known not to have one as this person's answer.

`familiesAsParent`/`familiesAsChild` carry no such ambiguity -- both are
`OPTIONAL`, "Order is preserved" *lists* (Section 2.2's own properties
table), so every family a person is a parent or a child in is included,
not just one. `buildFamilyView` (`internal/api/convert.go`) builds a
single `FamilyView` from an `rmdb.Family`: `parent1`/`parent2` are each
individually `OPTIONAL` ("up to two parents"), and `children` is "a list
of references to the children ... who have that set of parents in
common." The spec is silent on which of `parent1`/`parent2` is which, so
they're assigned Father/Mother respectively, matching
`buildCoupleRelationship`'s own `Person1`=Father/`Person2`=Mother
convention for the exact same `FatherID`/`MotherID` pair. A
`familiesAsChild` entry for the person's own family as a child correctly
includes that same person among `children` (alongside any siblings) -- a
direct consequence of `buildFamilyView` fetching *every* child of the
family, which this person is trivially one of.

`familiesAsParent`/`familiesAsChild` (a list of `FamilyView`, each with
`parent1`/`parent2`/`children` references) are a separate, larger feature
from the two "which single value" properties above, but reuse the same
underlying family-resolution queries `buildDisplayProperties` already
needs for `marriageDate`.

## Search

The RS spec defines two Atom-based search states: `Person Search Results`
(Section 4.11) and `Place Search Results` (Section 4.17), both reachable
via `GET /persons/search?q=...` and `GET /places/search?q=...`
respectively, and both discoverable from the `Collection` state via
`person-search`/`place-search` templated links (`gedcomx.Link`'s
`Template` field, RFC 6570 URI Template syntax) -- `q`/`limit`/`offset`
are the template variables (`limit`/`offset` rather than the spec's
generic `count`/`start`, for consistency with every other paged endpoint
in this server).

### Response format

`application/x-gedcomx-atom+json` (the GEDCOM X Atom Extensions
specification's own JSON representation) is the only representation
produced -- it's the `MUST`-support media type for both states (Sections
4.11.1, 4.17.1); full `application/atom+xml` (RFC 4287) is only
`RECOMMENDED`, and, matching this project's choice not to build XML
support for the rest of the API, isn't implemented. The envelope
(`internal/gedcomx/atom.go`: `AtomFeed`/`AtomEntry`/`AtomContent`) is
thin and flat; each entry's `content.gedcomx` is exactly the same
document type every other endpoint already produces for that resource
(`PersonDocument` for Person Search Results, `PlaceDescriptionsDocument`
for Place Search Results, typed as `any` on `AtomContent.GedcomX` so both
can reuse the one envelope) -- not a second, narrower type invented for
identical data. `id`/`title`/`updated` are never `omitempty` on either
`AtomFeed` or `AtomEntry`, matching RFC 4287's own RELAX NG grammar,
which requires all three exactly once.

Each Person Search Results entry's own content also includes the
person's embedded `relationships`, computed the same way `GET
.../persons/{id}` computes them (reusing
`personParentRelationships`/`personChildRelationships`/
`personSpouseRelationships`) -- not left as an empty array, since
`PersonDocument.Relationships` being non-`omitempty` specifically exists
to distinguish "computed, genuinely none" from "not computed at all."

`atom:updated` on both feed and entry is milliseconds since the Unix
epoch, computed from the corresponding table's own `UTCModDate` column
(`PersonTable`/`PlaceTable`), which is itself stored as days since
1899-12-30 (the OLE Automation epoch) -- see
`GetPersonUTCModDate`/`GetPlaceUTCModDate` (`internal/rmdb/queries.go`)
for the conversion.

Each Person Search Results entry links to its person via `person`; each
Place Search Results entry links to its place description via
`description` -- the one transition each state's own Transitions table
(Sections 4.11.4, 4.17.4) actually defines.

`GET .../persons/search` and `GET .../places/search` are exempted from
the global `withContentNegotiation` middleware (`server.go`), which
enforces `gedcomXMediaType` everywhere else -- each does its own
Accept-header check and sets its own `Content-Type` instead
(`internal/api/personsearch.go`, `placesearch.go`).

### Person Search Results parameters

Ten "direct" parameters (RS spec Section 5.3, the "q" template variable's
own documentation): `name`, `givenName`, `surname`, `gender`,
`birthDate`, `birthPlace`, `deathDate`, `deathPlace`, `marriageDate`,
`marriagePlace`. Plus 36 "`{relation}`"-prefixed parameters: `{relation}`
substitutes `father`/`mother`/`spouse`/`parent`, each with the same 9
fields (`Name`, `GivenName`, `Surname`, `BirthDate`, `BirthPlace`,
`DeathDate`, `DeathPlace`, `MarriageDate`, `MarriagePlace`).

The query string itself is a small grammar (`internal/api/searchquery.go`,
`parseSearchQuery`), per the spec's own description: name-value pairs
separated by whitespace, `name:value`, a value containing whitespace
wrapped in double quotes, and a trailing `~` on a value for a non-exact
match (default is exact). E.g. `givenName:John surname:Smith
gender:male birthDate:"30 June 1900"`, or `givenName:Bob~` for a
non-exact match.

**Non-exact (`~`) matching is a plain SQL substring match** --
`LIKE '%value%'` versus `=` for exact, both sides wrapped in `LOWER(...)`
for case-insensitivity throughout (`internal/rmdb/search.go`,
`textCondition`). `NameTable.GivenMP`/`SurnameMP` are accent-folded
(`FoldAccents`) copies of `Given`/`Surname`, not a phonetic (Metaphone/
Soundex) encoding despite the column name, so there's no fuzzy-matching
infrastructure in RootsMagic itself to build on.

**Date matching** reuses `gedcomx.ParseGedcom5Date` (the same GEDCOM 5.x
date grammar this server's write side parses `Date.original` with) to
parse the search value into a `SortDate` range (`ComputeSortDate`): exact
match narrows to only the precision actually given (a bare year widens
to the whole year, a month+year to the whole month); non-exact always
widens to the whole year regardless of how precise the given date was.
An unparseable date is rejected with `400`, not silently matched against
nothing.

**Fact-based criteria (birth/death/marriage) use `EXISTS` subqueries, not
`JOIN`s** -- a person can have more than one marriage (and, in principle,
more than one recorded Birth/Death fact), and a `JOIN` would multiply
`PersonID` rows in a way that could distort the separate `COUNT` query
needed for `gx:results` (the spec's own paging semantics). Marriage
criteria join through `FamilyTable` (`FatherID = ? OR MotherID = ?`) to
each of a person's own families.

**All fields within one `{relation}` group are matched against the same
specific relative**, not independently against any relative who has ever
held that role -- per the spec's own wording ("the given name of **the**
father," singular, definite article). `fatherGivenName:John
fatherSurname:Smith` means one father named John Smith, not a
disjunction across different relatives.

`father`/`mother` are resolved via `ChildTable`/`FamilyTable` (the same
"which family is this person a child in" relationship
`familiesAsChild` uses), matching if *any* of the person's
families-as-child qualifies. `parent` is father OR mother, but each side
of that OR still has to satisfy every field of the group by itself.
`spouse` is resolved via `FamilyTable` directly (families where the
searched person is one of the two parents), matching whichever of
`FatherID`/`MotherID` isn't them.

`{relation}MarriageDate`/`{relation}MarriagePlace` are tied to the
specific family that established the relation -- for
`father`/`mother`/`parent`, the marriage of the family the searched
person was found to be a child of; for `spouse`, the searched person's
own marriage to that specific spouse -- rather than any marriage the
relative has ever had.

### Place Search Results parameters

A single parameter, `name`, matching `PlaceTable.Name` (exact or
non-exact substring, same as the Person Search Results parameters). The
RS spec's own "q" template variable documentation (Section 5.3) defines
search parameters exclusively for persons -- there is no
"Place Search Parameters" table anywhere in the specification, even
though the `Place Search Results` state itself, its media type, its
operations, and its data elements are all fully specified. `name` is
supported as the one reasonable choice available without inventing spec
text that doesn't exist: `PlaceDescription` has essentially one
searchable text attribute at all (`Names`; `Latitude`/`Longitude` aren't
meaningfully "searched" the same way), and the field name matches Person
Search Results' own `name` parameter for the same underlying concept.
Any other field is rejected with `400`, naming `name` as the only one
supported.

## Write support

Off by default. `-write` enables it; without it, this server is
functionally read-only -- every write attempt gets a `405 Method Not
Allowed` with a correct `Allow` header (see "HTTP semantics" below), the
same as any resource this server doesn't implement writes for at all.
Write route registration is gated by `Server.resourceHandler()` checking
`!s.db.ReadOnly()` directly, not a separately-tracked setting that could
drift out of sync with the database connection's own actual state: when
false, the write routes simply aren't registered at all, so there is no
code path by which a write can reach the database in read-only mode.

Write support covers a specific, bounded set of operations -- not general
CRUD across every resource this server reads. What's covered: updating a
`Place` or `Source Description`'s core fields; updating an `Artifact`'s
stored file location; attaching/detaching media (`MediaLinkTable` rows)
on a `Person`, `Event`, or `Relationship`; and creating new `Person` and
`Relationship` (`Couple`/`ParentChild`) records. Not covered: updating an
existing `Person`'s or `Relationship`'s own conclusions (names, facts,
gender) once created, deleting anything, and creating `Event`,
`PlaceDescription`, or `SourceDescription` records directly (a
`PlaceDescription` is only ever created as a side effect of a `Fact`'s
`place.original` text during person/relationship creation).

### Why this is risky enough to be careful about

A `.rmtree` file is frequently years of someone's actual research.
Getting this wrong isn't like getting a read endpoint wrong -- a read bug
returns bad data; a write bug can destroy real data, permanently, in a
file most people don't rigorously version-control. Three mechanisms
address that risk directly:

- **RootsMagic must not be running at the same time.** Two writers on one
  SQLite file -- RootsMagic's own desktop app and this server -- is a real
  corruption risk. `-write` refuses to start at all if `RootsMagic.exe`
  appears to be running (checked via `tasklist`, a built-in Windows
  command). This is enforced as a hard precondition: the server exits with
  a clear error rather than proceeding. See
  `cmd/server/rootsmagic_running_check.go` for the exact mechanism and its
  real limits: it's meaningful only on Windows, where RootsMagic actually
  runs (a no-op everywhere else), and on its own it only checks at
  startup -- see "Write availability is re-checked periodically" below
  for how the startup-only gap is addressed.
- **A backup happens automatically before this server's first write.**
  `DB.EnsureBackup()` (`internal/rmdb/backup.go`) copies the source file
  to a timestamped sibling (`royal92-backup-20260806-091724.rmtree`) the
  first time it's called on a given connection -- once per server
  session, not once per write, via `sync.Once`, so every write handler
  can call it unconditionally without worrying about redundant copies. It
  defaults to the same directory as the source file. This isn't a
  substitute for RootsMagic's own backup feature -- it's a narrower,
  automatic safety net specifically for changes made by this server, so a
  mistake here (a bug, a bad request, this server writing something
  RootsMagic doesn't expect) can always be undone by restoring one file.
- **Write availability is re-checked periodically, not just at
  startup.** Every write handler goes through `requireWriteAllowed`
  (`internal/api/server.go`), consulting a `WriteGuard`
  (`cmd/server/writeguard.go`) on top of the `db.ReadOnly()` gate. The
  concrete implementation re-checks whether RootsMagic is running,
  rate-limited to once per 10 seconds and only triggered by an actual
  write attempt, not a background timer. **Once tripped, it latches
  permanently** -- every write for the rest of this server process's life
  gets `405`, even after RootsMagic later closes again, requiring a
  restart to resume. The tripped response reuses the exact shape a
  genuinely read-only server already returns for the identical request --
  `405`, `Allow: GET, HEAD`, the same RFC 7807 error body -- so "this
  server started read-only" and "this server was writable but RootsMagic
  showed up" look identical from a client's point of view.

Every write is wrapped in an explicit SQL transaction
(`internal/rmdb/writes.go`), even for a single-statement update, so a
partial failure partway through a multi-table write (e.g. a `Person`
create touching both `PersonTable` and `NameTable`) can't leave the
database in an inconsistent state.

**This server does not, and will not, write to `ConfigTable.DataRec`** --
a ~15KB, undocumented XML blob RootsMagic itself rewrites on nearly every
edit (see "Collection ids and titles" above for the two things this
server *does* read from elsewhere in that same blob: `UniqueID` and
`RootPerson`). `DataRec` holds RootsMagic's own UI window/panel layout
state, not genealogical data -- there's no UI panel for a headless server
to have state for in the first place, RootsMagic's own value for it
isn't reliable evidence of anything (the same conclusion this project
has reached elsewhere for `IsPrivate` and `fsID`/`anID`/`LatLongExact`),
and touching it at all would mean parsing an entire undocumented XML
document and re-serializing it byte-for-byte correctly, real risk of
corrupting settings this project doesn't understand, for a value there's
no reason to get right in the first place.

**Every write handler decodes its request body via `decodeStrictJSON`
(`internal/api/server.go`), not plain JSON decoding** -- it sets
`DisallowUnknownFields`, so a field name a target type doesn't recognize
is a `400`, not silently dropped. This matters specifically because a
client following the ordinary GET-then-modify-then-POST pattern, with one
mistyped field name, could otherwise produce a request that looks like a
legitimate no-op and returns a misleading `204` -- the intended write
never took effect, with no signal anything went wrong.

**Update semantics: a field that's absent, or present-but-empty, is left
unchanged -- there is currently no way to explicitly clear a field back to
empty via this API,** with one exception. Cleanly distinguishing "the
client omitted this key" from "the client explicitly wants to blank it"
would require either JSON presence detection against the raw request body
or restructuring the existing output types to use pointers throughout.
`Place`'s `latitude`/`longitude` are the one exception and get this for
free: `PlaceDescription.Latitude`/`Longitude` are already `*float64` (nil
when a place has no coordinates), so Go's JSON decoding already
distinguishes "key absent" from "key present" for those two fields.

### `Place` and `Source Description`

`POST /collections/{id}/places/{id}` and `POST
/collections/{id}/source-descriptions/{id}`, per RS spec Sections 4.16.2
and 4.23.2 ("Update a place description" / "Update a source description",
both `OPTIONAL`) and Section 8 ("Updating Application States": a data
element supplying its own `id` is an update candidate for that id). A
successful update returns `204 No Content`; an invalid request returns
`400` with an [RFC 7807](https://www.rfc-editor.org/rfc/rfc7807) body.

**`Place`**: `names[0].value` -> `PlaceTable.Name`;
`latitude`/`longitude` together -> `PlaceTable.Latitude`/`Longitude`
(converted to RootsMagic's own decimal-degrees-times-1e7 integer
encoding, via `math.Round` rather than truncation -- most decimal
fractions have no exact binary representation in float64, so a bare
`int64(...)` conversion would silently round some real coordinates down
by up to 1 in the last digit). Providing exactly one of
`latitude`/`longitude` without the other is rejected with `400` rather
than silently storing a nonsensical half-coordinate. `notes[0].text` ->
`PlaceTable.Note`.

A coordinates change also sets `PlaceTable.LatLongExact = 1`; a
`Name`-only change sets it to `0` and also resets `fsID`/`anID` to `0`
(RootsMagic's own FamilySearch/Ancestry match-lookup fields) -- a stale
match against the *old* name would be misleading once the name has
changed, so clearing them is the honest choice. A `Note`-only or
coordinates-only update leaves `fsID`/`anID` untouched, since that same
justification doesn't apply when the name hasn't changed.

**`Source Description`**: `titles[0].value` -> `SourceTable.Name`;
`notes[0].text` -> `SourceTable.Comments`. **`citations` is rejected with
`400`** if present at all, rather than silently accepted and ignored, or
guessed at: this API's own `citations` output is `ActualText` and
`RefNumber` concatenated into one string, and there's no way to safely
split an arbitrary string back into those two original fields. `IsPrivate`
is set to `0` unconditionally on every update (the data dictionary
documents this field as "not implemented," and `0` is the only value ever
observed).

### `Artifact` location

**`POST /collections/{id}/artifacts/{id}`** updates a multimedia item's
stored location. The request body's `mediaPath` (a write-only, non-spec
field on `SourceDescription`) is a real, absolute filesystem path, exactly
as it exists on disk; the client never constructs RootsMagic's own path
syntax itself. This server encodes it into RootsMagic's `?`-relative
notation (`internal/rmdb/encodemediapath.go`, `encodeMediaPath` -- the
reverse of `ResolveMediaPath`, above), the same way `UpdatePlace` computes
`Reverse` rather than expecting a client to.

`?` is the only one of RootsMagic's three path symbols (`*`/`~`/`?`) this
server ever *writes*: an absolute path is only meaningful on whichever
machine typed it, and `*`/`~` both depend on machine-specific context a
remote client has no way to see or reconstruct. `?` is different in
kind -- it isn't relative to a filesystem location at all, it's relative to
a named, centrally configured setting this server can resolve on its own.

But resolving `?` for *writing* requires actually knowing the Media
Folder's value with real confidence, unlike reading (where a wrong guess
just means a failed lookup, a contained and visible failure) -- a wrong
assumption on write means silently writing a `?`-relative path that
doesn't match what RootsMagic itself believes the Media Folder is,
corrupting the link from RootsMagic's own point of view without
surfacing until someone opens the file in RootsMagic later. So in write
mode, the Media Folder isn't a flag:

- **`-write` and `-media-folder` are mutually exclusive** -- passing both
  is refused at startup with a clear error.
- **`-write` reads the Media Folder itself**, straight from
  `RootsMagicUser.xml` (`cmd/server/mediafolder_discovery.go`,
  `discoverMediaFolder`), and refuses to start if it can't. Two locations
  are supported: **Windows**, `%APPDATA%\RootsMagic\Version
  N\RootsMagicUser.xml`; **macOS**, `~/RootsMagic/Version
  N\RootsMagicUser.xml` (based on community reports, including a direct
  quote from RootsMagic's own support staff, not independently confirmed
  against a real Mac installation the way the Windows path was). Anywhere
  else, `-write` refuses to start with a clear error.
- **Multiple RootsMagic versions**: `RootsMagicUser.xml` lives under a
  per-version folder, so more than one may exist on the same machine. The
  highest version number found is used, since RootsMagic's schema
  migrations are one-directional. If found configurations' Media Folder
  values disagree, that's logged in detail but isn't fatal.
- **`-bypass-os-check`** is a hidden flag (not registered via the `flag`
  package, so it never appears in `-h`/`--help`) that forces
  `discoverMediaFolder` to use the macOS-style discovery path regardless
  of the actual platform -- meaningful, not a no-op, since
  `os.UserHomeDir()` returns a real, usable directory on any platform, so
  this exercises the genuine macOS discovery convention against whatever
  the current platform's actual home directory is. This is a
  development/testing aid, not a supported way to run write mode in
  production on an unsupported platform.

A real path that isn't actually under the Media Folder is rejected
(`ErrPathNotUnderMediaFolder`, surfaced as `400`) rather than written
anyway as an absolute path. The prefix check requires a genuine
path-separator boundary, not just a string prefix match (`C:\tmp2\...`
does not match a Media Folder of `C:\tmp`).

### `MediaLinkTable` -- attaching and detaching media

**`POST /collections/{id}/persons/{id}`, `.../events/{id}`, and
`.../relationships/{id}`** each accept an updated `media` array,
diffed against `MediaLinkTable` rather than replaced wholesale --
entries newly present get a new row, entries newly absent get their row
removed, entries in both are left completely untouched. The shared
diffing logic is `rmdb.UpdateOwnerMedia`, parameterized by owner
type/id.

`MediaLinkTable` also has `IsPrimary` ("Primary Photo" -- determines the
image shown in reports/Pedigree view/People Side View) and `Include1`
("Include in Scrapbook"); both always get `0` on a newly-created link,
since this server has no basis to assert a newly-created link should be
someone's primary photo or scrapbook item -- a real editorial choice, not
something to claim on a user's behalf without evidence. There's no
GEDCOM X data type conceptually similar to RootsMagic's Scrapbook at all,
so `Include1` specifically is RootsMagic-only functionality this API
doesn't expose. `Include2-4` and the four `Rect*` columns are documented
as "Not implemented" and always left at `0`.

`MediaLinkTable` has no uniqueness constraint on `(MediaID, OwnerType,
OwnerID)` -- nothing stops the same artifact being linked to the same
owner more than once. Removing a media id removes *every* matching row,
not just one, so a removal can't leave an orphaned duplicate behind.

These three endpoints are scoped to `media` only, not general editing of
the owning resource -- `names`/`gender`/`facts`/`sources`/`type`/`date`/
`place`/`roles`/`notes` (whichever apply to that resource type) aren't
writable through them. A request that includes any of them isn't
rejected -- a client following the ordinary GET-then-modify-then-POST
pattern will naturally send back what it received -- but it's logged
(`logIgnoredFields`, naming exactly which fields were present), so
there's a visible trail of demand if broader editing is added later.
(`Relationship`'s own `type`/`person1`/`person2` are the one exception to
this logging: they're always present on every real `Relationship` this
server returns, so logging their presence on every request would be
noise, not signal.)

Attaching or detaching media doesn't bump the owning resource's own
`UTCModDate`.

**One restriction specific to `Relationship`, with no equivalent for
`Person`/`Event`: only the "couple" relationship kind is writable, never
a parent-child relationship.** A relationship id like `F1-FC2` (a
specific parent-child pair) is rejected with `400`, not silently
redirected to the family it belongs to -- RootsMagic's own schema has no
identity for "this specific parent-child pair" to attach anything to at
all; `MediaLinkTable`'s `OwnerType=Family` is scoped to the family as a
whole, the same identity the "couple" relationship already represents. A
family with only one recorded partner (`FatherID` or `MotherID` is `0`)
has no valid "couple" relationship for this endpoint to target, and
correctly `404`s -- the same existence check `GET /relationships/{id}`
already applies for reading.

### Reverse lookup: what references a given artifact

Three non-spec extension endpoints answer "which people, relationships,
events, and places reference this specific artifact": `GET
/artifacts/{id}/subjects` (every `Person`, `Relationship`, `Event`, and
`PlaceDescription` referencing it), and `.../persons`/`.../events`/
`.../relationships` (the same lookup, filtered to one type). This is the
reverse of `buildSourcesAndMedia`'s own forward traversal (a given owner
-> its sources/media) -- nothing else in this API lets a client start
from an artifact and find its owners without enumerating every resource
in a collection and checking each one's own `media` array by hand.

Response shape is a lightweight reference list
(`gedcomx.SubjectReference`/`SubjectReferencesDocument`), not embedded
full resources -- a caller that needs full details fetches them
separately via each reference's `href`. `resourceType` reuses the
existing `ResourceType*` URI constants already defined for
`CollectionContent`.

The underlying traversal (`rmdb.OwnersOfMedia`) checks direct
`MediaLinkTable` rows naming the artifact, plus a second hop through
`CitationLinkTable` for any citation the artifact is attached to (since a
real file's media is more often attached to a *citation* than directly to
what it documents -- see "Multimedia" above). A name-owned link
(`OwnerTypeName`) is resolved up to its owning `Person` via
`NameTable.OwnerID`, since a name isn't a `Subject` with its own resource
in this API; an orphaned name reference is skipped, not treated as a
request failure. A source-owned link (`OwnerTypeSource`) is dropped
outright -- not a `Subject` type this API exposes a `media` field for at
all.

### Creating `Person` records

**`POST /persons`**, per RS spec Section 4.9.2: `201` + `Location` header
if exactly one person was created, `204` if a request created several at
once. A multi-person request is explicitly *not* all-or-nothing across
those persons -- each person is its own transaction, so a failure
partway through a multi-person request can leave earlier persons in that
same request already committed; the error response names which persons
already exist.

`internal/rmdb.CreatePerson` does the actual multi-table transaction
(`PlaceTable` resolution/creation, `EventTable` per fact, one
`NameTable` row per name, `PersonTable`, `ConfigTable`'s `UTCModDate`
bump); `internal/api`'s `handleCreatePersons` and its
`buildNewPerson`/`buildNewPersonName`/`buildNewFact` helpers translate an
incoming GEDCOM X request into that call.

**Scoped narrower than the full GEDCOM X conceptual model, by design**:
only built-in GEDCOM X fact types with a confirmed RootsMagic
`FactTypeTable` mapping can be created; a custom fact-type URI is
rejected rather than matched fuzzily or used to silently create a new
`FactTypeTable` row. A `Fact.place` must carry `original` text --
resolving a place from a bare `resource` reference isn't supported.
Every rejection returns a clear `400` naming what was wrong, not a best
guess.

**Name handling**: a `NameForm` may carry either `parts` (structured
`Given`/`Surname`/...) or just `fullText`, or genuinely neither (an
empty name) -- all three are valid per the conceptual model spec (Section
3.19: `fullText` and `parts` are independently `OPTIONAL`). When `parts`
is absent, the whole `fullText` (which may itself be empty) is stored in
`NameTable.Given`, with `NameTable.Surname` left empty -- this matches
RootsMagic's own actual behavior for the equivalent GEDCOM 5.x case (a
name with an empty slash-delimited surname portion). `Person.names`
itself is `OPTIONAL` (Section 2.1) -- a `Person` with no `names` field, or
an empty `names` array, is accepted, falling back to exactly one empty,
primary name, matching RootsMagic's own "always at least one `NameTable`
row" behavior. If nothing in a multi-name list is explicitly marked
`preferred`, the first name defaults to primary (`IsPrimary=1`) -- an
explicit `preferred: true` later in the list still overrides this
default.

**`BirthYear`/`DeathYear` are duplicated across every one of a person's
`NameTable` rows, not just the primary one** -- confirmed against real
multi-name people in real RootsMagic databases. **`ChildOrder` is
0-indexed.** **`PersonTable.SpouseID` and `PersonTable.ParentID` are
never set by this server at all** -- per the RM4-11 data dictionary,
each holds the `FamilyID` of whichever family was last *viewed* for this
person in RootsMagic's own UI (as a spouse, or as a child, respectively)
-- a UI navigation state, not a genealogical fact, with no principled
correct value for a record created through this API, which was never
viewed in that UI. The authoritative source of the real relationship is
always `ChildTable`/`FamilyTable`, never either of these two
`PersonTable` columns.

**Nicknames**: GEDCOM 5.x nests a nickname *within* a `NAME` record, but
GEDCOM X models a nickname as its own, separate `Name`
(`type=http://gedcomx.org/Nickname`) -- and RootsMagic's schema sits
between the two: `NameTable.Nickname`/`NicknameMP` are a single pair of
columns on *one* name record, not a slot for a separately addressable
nickname entity. A `Name` with `type=Nickname` in a create request is
absorbed into the primary name's `Nickname`/`NicknameMP` columns rather
than creating a second `NameTable` row -- its text comes from
`nameForms[0].fullText`, falling back to every part's value concatenated
with spaces. A second `Name(type=Nickname)` in the same request is
dropped (logged at `Info` level), since RootsMagic's schema only has room
for one. The read side synthesizes a nickname back as its own, separate
`Name(type=Nickname)` in the response whenever `NameTable.Nickname` is
non-empty -- deliberately given no `id` of its own, since it isn't a
real, separately addressable `NameTable` row.

**`Fact.value`** (a free-text value for value-only facts like
`Occupation`/`Education`/`Religion`) maps to `EventTable.Details`,
matching how the read side already reverses this mapping.

**Date handling** tries, in order: (1) `Date.formal`, if present and
valid -- `EncodeRMDate` (`internal/gedcomx/rmdate.go`) supports only a
plain date and an "About"-qualified date for *encoding* (the only two
GEDCOM X `Formal` shapes `ParseRMDate` ever produces on the read side in
the first place; `Before`/`After`/`Between`/`From-To` are not supported
for encoding, since GEDCOM X's formal grammar represents "Between X and
Y" and "From X to Y" with the identical string shape, which cannot be
mapped back to one specific RootsMagic directional code without
guessing); (2) if `Formal` is absent or invalid, `Date.original` --
`ParseGedcom5Date` (`internal/gedcomx/gedcom5date.go`) covers GEDCOM
5.5.1's `DATE_GREG` grammar (day/month/year, month/year, or year-only
precision) and the `ABT`/`CAL`/`EST`/`BEF`/`AFT` qualifiers -- not yet
`BET...AND...`/`FROM...TO...` (the same two-date ambiguity as
`EncodeRMDate`'s own limit), `INT`/bare parenthetical phrases (explicitly
unstructured by the GEDCOM 5.5.1 spec itself), the `B.C.` suffix, or
double-dating (`"1743/44"`); (3) if neither produces a usable date, the
fact is created without one, and -- if `Original` was present but
unparseable -- the original text is preserved verbatim in
`EventTable.Note`, prefixed `"rmgedcomx was unable to parse this text as
a date: "` so it's clearly this server's own annotation, not something
RootsMagic itself wrote.

Three encoding schemes RootsMagic computes are independently
implemented and verified against real captured values:
`PersonTable.UniqueID` (a standard v4 UUID, 32 hex characters with
hyphens stripped, followed by a 4-character Fletcher-16-style checksum --
`internal/rmdb/generateuniqueid.go`); `EventTable.SortDate` /
`NameTable.SortDate` (a bit-packed 64-bit integer, `SortDate =
2^49*(Y+10000) + 2^45*M + 2^39*D + 17178820620`, with
`9223372036854775807` as RootsMagic's own sentinel for "no date to sort
by" -- `internal/rmdb/sortdate.go`); and `EventTable.Date` /
`NameTable.Date` (the encoded date string, via `EncodeRMDate`, the
inverse of the existing `ParseRMDate` read-side decoder).

**`SurnameMP`/`GivenMP`/`NicknameMP`** (accent-folded copies of
`Surname`/`Given`/`Nickname`, used by RootsMagic for accent-insensitive
sorting/searching) are computed via `FoldAccents`
(`internal/rmdb/accentfold_table.go`, a 488-entry table covering the
Latin-1 Supplement, Latin Extended-A/B, and Latin Extended Additional
Unicode blocks, generated from real NFD decomposition data) --
deliberately excludes ligatures/special letters (`ø`, `æ`, `œ`, `ß`),
since NFD decomposition doesn't touch these at all (they aren't a base
letter plus a combining accent mark).

**Two confirmed RootsMagic quirks this server deliberately doesn't
replicate**: `FamilyTable.ChildID` is pure UI state ("PersonID of Child
last active as the root person in Pedigree view"), left at its default
(`0`) always, since this server has no Pedigree view to have a root
person for. `UTCModDate` during marriage/child creation is set on
exactly the row(s) each create/update handler directly writes, always
full-precision, never cascaded to unrelated records -- RootsMagic's own
behavior here is inconsistent (only one partner's row bumped on a
marriage, truncated to midnight; neither parent's nor the child's own row
bumped on a child link at all), and not treated as a behavior worth
matching.

### Creating `Relationship` records (`Couple` and `ParentChild`)

**`POST /relationships`**, per RS spec Section 4.20.2: `201` +
`Location` header for exactly one relationship created, `204` for
several. `internal/api/createrelationship.go`'s
`handleCreateRelationships` resolves which of the two supported types a
request is (`Couple` or `ParentChild`; anything else is rejected) and
which person plays which role. The `Location` returned for a single
creation identifies the specific relationship actually created --
`coupleRef` (`F{id}`) for `Couple`, `parentChildRef` (`F{id}-FC{child}` /
`F{id}-MC{child}`) for `ParentChild`, determined by fetching the
just-created family fresh (`FatherID == parentID`) rather than
re-deriving the parent's role a second, separate way.

**`Couple`**: `CreateCoupleRelationship`
(`internal/rmdb/createcouple.go`) creates a `FamilyTable` row plus zero
or more family-owned facts (e.g. a `Marriage`). `resolveCoupleRoles`
determines Father/Mother from each person's own recorded `Sex`, not from
`person1`/`person2`'s order (the RS spec doesn't define which is which);
a pair that isn't exactly one Male and one Female is rejected. RootsMagic
itself supports single-parent families, so only one of
`FatherID`/`MotherID` is required, not both. If a family already exists
with exactly this Father/Mother pairing, it's reused (idempotent) rather
than duplicated; if either person already has an existing family with
the other role empty, it's completed rather than a new one created --
covering the case where a `Couple` relationship arrives after both
parents were already independently established via separate
`ParentChild` requests. Both spouses' `UTCModDate` are bumped (not just
one), and `PersonTable.SpouseID` is left untouched entirely, consistent
with the reasoning above for `SpouseID`/`ParentID`.

**`ParentChild`**: `CreateParentChildRelationship`
(`internal/rmdb/createparentchild.go`) resolves a bare (parent, child)
pair to a specific family using only information already established
about the *child*, never based on what the named parent alone happens to
already have on file (a parent having exactly one existing family
doesn't mean this child belongs to it -- that's a fact about the
database's current contents, not about the parent's real life):

1. If the child already belongs to a matching-kind family (see `RelType`
   below) that already has this exact parent in the matching role, this
   is a no-op (idempotent).
2. If the child already belongs to a matching-kind family with that role
   empty, it's completed with this parent. If completing it would create
   a second family record for parents already paired elsewhere, the
   child's link is *moved* to the pre-existing family instead, and the
   now-redundant one is removed (with `ChildOrder` recomputed against the
   target family's own existing children).
3. If the child has no matching family at all, a new one is created for
   this parent alone -- regardless of how many *other* families the named
   parent already has on file for other children.
4. If more than one of the child's existing families could match, this
   is genuinely ambiguous (a child can belong to more than one family at
   once, e.g. biological and adoptive) and rejected with `400`
   (`rmdb.ErrAmbiguous`) rather than guessed at.

Sex "Unknown" on the parent is rejected outright, the same reasoning
`resolveCoupleRoles` applies to a couple where either person's sex isn't
Male/Female. A nonexistent `childId` is rejected with `400` rather than
creating a dangling `ChildTable` reference to a person that doesn't
exist.

**`RelType`** (`ChildTable.RelFather`/`RelMother`) distinguishes
biological, adoptive, step, foster, and guardian relationships -- an
eight-value code (0=Birth, 1=Adopted, 2=Step, 3=Foster, 4=Related,
5=Guardian, 6=Sealed, 7=Unknown), meaning a person can genuinely belong
to more than one family as a child at once. Five values have a direct
GEDCOM X counterpart (the conceptual model's separate "Parent-Child
Relationship Fact Types" section, not the person-scoped `Adoption` fact
type): `BiologicalParent` (0), `AdoptiveParent` (1), `StepParent` (2),
`FosterParent` (3), `GuardianParent` (5). `Related` (4), `Sealed` (6, an
LDS-temple-ordinance concept specific to RootsMagic), and `Unknown` (7)
have no GEDCOM X counterpart and are left unmapped -- and the mapping is
asymmetric the other way too: the same GEDCOM X specification section
defines five further parent-child fact types (`ChildOrder`,
`EnteringHeir`, `ExitingHeir`, `SociologicalParent`, `SurrogateParent`)
that don't correspond to any RootsMagic `RelFather`/`RelMother` value at
all.
`relTypeFromFacts` (`internal/api/createrelationship.go`) determines a
request's `RelType` from its own `Facts`, first match wins, defaulting
to `RelTypeBirth` when none are present. This is load-bearing for case 4
above: a candidate family only matches if the target role is empty *and*
the other role (if already filled) is either also empty or the same
`RelType` -- which is what lets a child who already has an incomplete
biological family and a separate incomplete adoptive family still
resolve correctly.

## RootsMagic version handling

RootsMagic 8 or later is required. `PersonTable`, `NameTable`,
`FamilyTable`, `ChildTable`, `EventTable`, `FactTypeTable`, `PlaceTable`,
`SourceTable`, `CitationTable`, `CitationLinkTable`, and `RoleTable` are
unchanged between RootsMagic 8 and RootsMagic 10/11 for every column this
server reads, so rather than branching logic on a detected version
number, `internal/rmdb` does two things:

1. **Discovers columns dynamically** with `PRAGMA table_info(...)` at
   startup, and only selects columns it knows how to use. If a future
   RootsMagic version adds columns, nothing breaks. If a column this
   server wants is missing, it's treated as absent/zero-value rather than
   causing an error.
2. **Reports a best-effort version string** in the startup log line
   (based on which optional tables exist, e.g. `DNATable`,
   `FamilySearchTable`, `AncestryTable` are later additions) -- purely
   informational, doesn't gate functionality.

If a required table or column is missing, `Open` fails at startup with a
clear error naming what's missing. `requiredTablesAndColumns`
(`internal/rmdb/db.go`) requires `UTCModDate` specifically on the five
tables write support touches (`PlaceTable`, `SourceTable`,
`MultimediaTable`, `MediaLinkTable`, `ConfigTable`) -- these tables have
no modification-timestamp column at all in RootsMagic 7, under any name,
which is the specific reason RootsMagic 7 isn't supported (a version this
server otherwise has no particular reason to exclude, given the dynamic
column discovery above).

## Fact type mapping

RootsMagic's `FactTypeTable` has built-in fact types (IDs below 1000) and
can have user-defined ones (1000+). Built-in types generally carry a real
GEDCOM tag (`BIRT`, `DEAT`, `MARR`, ...); user-defined types usually have
`GedcomTag = "EVEN"`. `internal/gedcomx/facttypes.go` maps the common
GEDCOM tags to their GEDCOM X Conceptual Model fact-type URIs
(`http://gedcomx.org/Birth`, etc.). Anything that doesn't match a known
tag is emitted as a custom fact type URI built from the RootsMagic fact
type name, so no fact is silently dropped, e.g.:
`http://rootsmagic.local/fact-type/Occupation`.

## Date qualifier encoding

RootsMagic encodes a date as a fixed-width string,
`sign+YYYYMMDD+qualifier`, doubled (one group for the value itself, one
for a range's second half, unused for a single date) plus two
single-byte qualifier codes -- one directional, one qualitative:

| Byte | Position | Meaning | Confirmed? |
|---|---|---|---|
| `.` | directional | plain, no modifier | yes |
| `B` | directional | Before | yes |
| `A` | directional | After | yes |
| `R` | directional | Between ... And ... | yes |
| `S` | directional | From ... To ... | yes |
| `.` | qualitative | none | yes |
| `A` | qualitative | About | yes |
| `L` | qualitative | Calculated | yes |
| `E` | qualitative | Estimated | yes |

The two `A` bytes are in different positions and mean different things
(`After` as the directional byte, `About` as the qualitative byte) --
`decodeRMDate` never confuses them, since they're captured from different
regex groups. RootsMagic's own documentation
(https://help.rootsmagic.com, "Date formats") lists further modifiers
this decoder doesn't have confirmed byte codes for: the single-date
directional modifiers By, To, Until, Since; the range modifiers dash
("–") and Or; and the qualitative modifiers Circa and Say. Dates using
those still get their year/month/day decoded correctly (the digit
positions are reliable regardless of qualifier); they just don't get an
English modifier word, on the principle that guessing wrong would
misrepresent the record.

GEDCOM X formal dates (`Date.formal`) are populated for the confirmed
cases where the GEDCOM X Date Format profile has a clean representation
(plain, About via the `A` approximate prefix, Before/After/Between/
From-To via the `/` range syntax) and left empty otherwise (BC dates,
Calculated, Estimated, and any unconfirmed modifier) -- `Date.original`
always has the best available human-readable text regardless.

On the write side, `internal/gedcomx/gedcom5date.go`'s
`ParseGedcom5Date` covers GEDCOM 5.5.1's own `DATE_VALUE` grammar for
`Date.original` -- see "Write support" above for what's covered and
what isn't.

## RMNOCASE collation

RootsMagic declares several indexed text columns (`PlaceTable.Name`,
`SourceTable.Name`, etc.) `COLLATE RMNOCASE`, a custom collation
RootsMagic registers at the application level to emulate Windows'
Unicode case-insensitive string comparison. Without that collation
registered, SQLite fails any query that touches those columns (including
implicitly, via `ORDER BY` or an index) with `no such collation
sequence: RMNOCASE`.

This server registers an approximation: Go's Unicode-aware
`strings.ToLower` comparison (this handles non-ASCII case folding, e.g.
"É" vs "é", not just ASCII). What it doesn't reproduce is Windows'
accent/diacritic-insensitivity -- on Windows, RootsMagic likely treats
"café" and "cafe" as equal for sorting/searching purposes; here they sort
as distinct. That only affects sort order and place/source name matching,
never which rows exist or their content.
[unifuzz](https://github.com/mooredan/unifuzz) reimplements RMNOCASE more
precisely (via Wine's collation logic, as a loadable SQLite extension) if
exact Windows-parity sorting matters for a given use case; the same idea
(accent-stripping before comparison) could be ported into
`registerCollation()` in `internal/rmdb/db.go` if needed.

## SQLite driver

This server uses [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite),
a CGo-free, pure-Go SQLite implementation, so building doesn't require a
C compiler and cross-compilation works normally.

`Open()` uses `file:%s?mode=%s` where `%s` is `ro` or `rw`. `modernc.org/sqlite`
transpiles the actual SQLite C source (via `ccgo`) rather than
reimplementing SQLite's URI-filename parsing, so `mode=ro`/`mode=rw` gets
SQLite's own well-established handling (see
[sqlite.org/uri.html](https://sqlite.org/uri.html)): `mode=ro` gives
genuine, OS/engine-level read-only access (a write fails with
`SQLITE_READONLY`, and a missing path fails to open rather than silently
creating an empty file); `mode=rw` (used only when `-write` is passed)
opens with a 5-second `busy_timeout`, so a brief, incidental lock causes
a short wait-and-retry rather than an immediate failure.

Which mode gets used is decided in exactly one place: the unexported
`open` function in `internal/rmdb/db.go` takes a `readOnly bool`.

Custom collations (RMNOCASE) are registered once, globally, via the
package-level `sqlite.RegisterCollationUtf8`, rather than per-connection.

## HTTP semantics

### Status codes: 405/404 for what this server doesn't do

This server registers `GET` (and, where write support covers it,
`POST`) handlers only for the resources it actually implements, and
nothing else -- no custom stub handlers for unsupported methods or
unimplemented resource families. Go 1.22's `net/http.ServeMux` handles
the rest automatically: a path registered only for `GET` returns `405`
with a correct `Allow: GET, HEAD` header for any other method, `HEAD`
requests are automatically answered from the `GET` handler with the body
discarded, and a path that was never registered at all returns a plain
`404`.

One consequence worth being explicit about: this server's HTTP responses
don't distinguish "a real GEDCOM X RS feature this server hasn't built"
from "not a thing at all" -- both are a plain 404/405. That distinction
lives in documentation (this file) rather than runtime response bodies,
which is the more correct place for it: a spec-aware client should
consult a server's stated capabilities, not probe error responses to
reverse-engineer them. There's also no `/oauth2/token` stub route:
RS spec Section 9 makes authentication a `MAY`, this server has no
protected states to gate behind it, and nothing in this server's own
`links` ever advertises that URL to a client.

`GET /` is registered as `"GET /{$}"` (Go 1.22's exact-match syntax),
not a bare `"/"` -- `net/http`'s router otherwise treats a bare `"/"`
pattern as a catch-all for every unmatched path, which would mean a
typo'd path gets silently served the Collections list instead of a 404.

### Content negotiation

This server produces exactly one representation for its core resources,
`application/x-gedcomx-v1+json` -- there's no XML support. `Accept` is
checked against that one representation (a plain yes/no check -- there's
nothing to rank with q-values when there's only one option); a request
whose `Accept` header can't be satisfied gets `406 Not Acceptable`.
`Vary: Accept` is set on every response, since `Content-Type` genuinely
does depend on that header.

Two endpoints are exempt: `GET .../artifacts/{id}/content` (not a GEDCOM X
RS state at all -- see "Multimedia" above -- its whole purpose is to
return the artifact's own real `Content-Type`) and `GET
.../persons/search` / `.../places/search` (a different required media
type, `application/x-gedcomx-atom+json` -- see "Search" above), each
handling their own Accept-header check and `Content-Type` independently.

### Error bodies: RFC 7807

GEDCOM X RS doesn't define an error body schema of its own -- error
responses are outside the spec's scope. Every error response is [RFC
7807](https://www.rfc-editor.org/rfc/rfc7807) Problem Details
(`internal/api/server.go`'s `problemDetails`,
`application/problem+json`): `title` (from `http.StatusText`), `status`,
and `detail` (a specific, human-readable explanation). `type` is
deliberately omitted, which RFC 7807 defines as meaning `about:blank` --
this server doesn't have a taxonomy of distinct problem-type URIs worth
inventing and maintaining, just a status code and a message per
occurrence.

### Paging: `first`/`last` too, not just `prev`/`next`

RS spec Section 7 defines four paging rels: `first`, `next`, `prev`,
`last`. All four are included whenever a resource has more than one
page, on every page -- `first`/`last` mark the fixed ends of the whole
list (not a position relative to where the client currently is), so
they're included even on the first/last page itself, where they're
arguably most useful for a client to confirm what it's looking at.
`last`'s offset is computed from `total`, which the caller already has
(`((total-1)/limit)*limit`).

This is still a simpler mechanism than the spec's full paging-as-links
model overall (`?limit=`/`?offset=` query parameters, not opaque
server-chosen page tokens).

## Logging

Structured logging via `log/slog`. Both packages that log anything
(`internal/api`, `cmd/server`) call `slog`'s package-level functions
(`slog.Info`, `slog.Warn`, ...) directly against the default logger --
there's exactly one process, one log stream, and one place
(`cmd/server/main.go`'s `setupLogging`, run first thing in `main`) that
decides its level and format.

**Two flags** (`cmd/server/main.go`): `-log-level`
(`trace`/`debug`/`info`/`warn`/`error`, default `info`) and `-log-format`
(`text`/`json`, default `text`). Logs go to stderr; the startup
collection table (`printCollectionTable`) is a separate,
`fmt.Fprint*`-to-stdout human-readable report, not a log line -- it's
meant to be read once at a terminal, and an aligned table (via
`tabwriter`) has no sensible representation as structured log attributes
without losing the alignment.

`internal/api/server.go`'s request-logging middleware (`withLogging`)
emits two things per request:

- An `Info` line for every request: method, path, status, duration.
- A separate `Debug` line, *only* when the response status is `>= 400`,
  carrying both the request body that produced it and the actual
  response body. For this API, the response body is always one of two
  things, and telling them apart is itself often the diagnosis: this
  server's own detailed RFC 7807 reason, or -- if the request never
  reached this server's own handler code at all (e.g. `POST /persons`
  when `-write` wasn't passed and that route was never registered) --
  Go's own bare `"Method Not Allowed"` text.

At `-log-level=trace`, that same `Debug` detail line is emitted for
every request, including successful responses.

These are two separate log lines at two separate levels, not one line
with the bodies as always-present attributes, since slog's level
filtering is per-call, not per-attribute -- a body attribute on the
`Info` line would always render regardless of the configured level.
Both captures (`statusRecorder` for the response, a re-wrapped `r.Body`
for the request) are gated on whether `Debug` is actually enabled,
checked once up front, since reading and re-wrapping `r.Body` has a
real, if small, cost that shouldn't be paid on a server run at the
default `-log-level=info`.
