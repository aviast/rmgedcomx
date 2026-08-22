# Scope and design notes

## Why "core resources, read-only" and not the whole spec

GEDCOM X RS is written for multi-user, hosted, collaborative genealogy services (its
worked examples are effectively FamilySearch's own API). A big chunk of it exists to
support things a single-user desktop database doesn't need:

- **OAuth2** (`Section 9`) — there's no multi-tenant auth story for a file on your own
  disk. Bolting on OAuth would add real complexity for no real security benefit on
  `localhost`/LAN use, and would get in the way if you want to hit the API from a
  script. If you do want to expose this server on the open internet, put it behind a
  reverse proxy (e.g. Caddy/nginx) with your own auth, or ask for OAuth to be added.
- **`Records`** — this models a hosted archive of historical records (e.g. "the
  1940 U.S. Census" as a queryable collection in its own right). RootsMagic
  doesn't have an equivalent concept.
- **Write operations** — off by default, and the vast majority of resources are
  still read-only even when write support is enabled. See the "Write support"
  section below for what's actually implemented, staged incrementally, and why.

`Collections` / `Collection`, `Artifacts`, `Events` / `Event`, and `Person Search
Results` / `Place Search Results` **are** implemented -- see the "Multiple
databases / Collections", "Multimedia", "Events", and "Person Search Results" /
"Place Search Results" sections below for why and how.

### What is included

The resources that map directly onto what's actually in a RootsMagic file, and that
are useful for read access from another tool (a family tree viewer, a static site
generator, a Digital Asset Management tool, a chatbot, etc.):

`Collection`, `Collections`, `Person`, `Persons`, `Person Parents`, `Person Children`,
`Person Spouses`, `Ancestry Results`, `Descendancy Results`, `Relationship`,
`Relationships`, `Place Description`, `Place Descriptions`, `Source Description`,
`Source Descriptions`, `Artifacts` (backed by `MultimediaTable` -- scanned
certificates, photos, and similar), `Events` / `Event` (backed by `EventTable` +
`WitnessTable` + `RoleTable` -- shared events with multiple participants, like a
marriage with witnesses), `Person Search Results`, `Place Search Results`
(Atom/JSON-based query search -- see their own sections below for what's actually
supported).

Each `Person` embeds its conclusions (names, gender, facts) directly in the same
response, per the spec's fallback rule in Section 4.10.5 ("If no link to
`conclusions` is provided, the list of conclusions MUST be included in the original
request"). This avoids needing separate `/persons/{id}/conclusions` endpoints for a
read-only server.

## Multiple databases / Collections

The `Collection` state (RS spec Section 4.5, data type defined in the
[GEDCOM X Record Extensions](https://github.com/FamilySearch/gedcomx-record/blob/master/specifications/record-specification.md#collection))
is the intended discovery root of a GEDCOM X RS API: it's the one state formally
specified to carry `persons`, `relationships`, `source-descriptions`, and
`subcollections` links (Section 4.5.4's transitions table), which is exactly the
set of top-level resources this server exposes. It was left out of the first cut
of this project on the assumption that "a collection of genealogical data" was a
better fit for a hosted, multi-tree, multi-contributor service than for a single
RootsMagic file opened directly off disk -- but that reasoning didn't hold up: a
`Collection` doesn't have to be a big, sprawling archive. The spec's `Collection`
data type is just `id` / `title` / `content` (counts by resource type) / `links`,
which maps onto a single RootsMagic file perfectly well, and a compliant client
has nowhere else to start from -- without it, there's no spec-defined way to
discover that this server has `persons`, `relationships`, `place-descriptions`,
and `source-descriptions` at all.

**One RootsMagic file == one `Collection`.** `-db` is repeatable -- pass it
multiple times to serve several databases in one process, each as its own,
fully independent `Collection`:

```sh
./rmgedcomx -db Tree1.rmtree -db Tree2.rmtree -db royal92.rmtree
```

Every resource URL is scoped under its collection: `/collections/{id}/persons/P1`,
`/collections/{id}/relationships/F3`, and so on -- **not** the bare
`/persons/P1` an earlier version of this server used. That change was made
deliberately, after realizing the bare form was a real design flaw once more
than one database could be open at a time: two different databases' `P1`
would otherwise be indistinguishable, represented by the identical URL,
which is a much sharper problem than it sounds -- it means a client (or a
person) has no way to tell which family `P1` even refers to without
separately tracking which server instance or session they got that URL
from. Scoping every resource under its collection's id fixes that
structurally: the id is part of the URL, not implicit context. Discovery
states span every collection:

- `GET /` -- the root, for a client that only knows the base URL. Serves the
  `Collections` list (see below), not a single `Collection` -- with
  potentially more than one collection open, there's no longer a single one
  for the root to unambiguously be.
- `GET /collections` -- the formal `Collections` (list) state: every
  collection currently open, in the order their `-db` flags were given.
- `GET /collections/{id}` -- the formal `Collection` (single) state for one
  of them.

Architecturally, this is deliberately *not* implemented by threading a
collection id through every handler and builder function. Each `*api.Server`
(in `internal/api/server.go`) stays exactly what it always was: scoped to
one database, unaware any other might exist. `internal/api/multi.go`
(`NewMultiCollectionHandler`) is the only thing that knows there can be
more than one -- it owns the cross-collection `Collections`/`Collection`
handlers, and mounts each `Server`'s own resource routes
(`Server.resourceHandler()`) under `/collections/{id}/` via
`http.StripPrefix`. `Server.url()` builds every resource link against a
precomputed, collection-scoped base URL (`BaseURL + "/collections/" + ID`),
so `convert.go` and `handlers.go` needed no changes at all to produce
correctly-scoped links -- they were already only ever building links
relative to "this collection's base," which now simply includes the
`/collections/{id}` prefix. (`Server.globalURL()` is the one exception,
used only for the `subcollections` link, which intentionally points outside
the collection's own scope, at the global `/collections` list.)

`content` counts (`CollectionStats` in `internal/rmdb/queries.go`) are
computed with plain `COUNT(*)` SQL, not by materializing the full resource
lists, so listing collections stays cheap even with several large trees
open. The relationship count deliberately mirrors the exact logic
`handleRelationships` uses to build the real list (one `Couple` relationship
per family with both parents present, plus one `ParentChild` relationship
per parent-child pair) so the number a client sees here always matches what
`GET /collections/{id}/relationships` actually returns.

One link is a deliberate, documented departure from the spec:
`place-descriptions`. The formal transitions table for `Collection`
(Section 4.5.4) doesn't define a plural rel for the Place Descriptions list
state, and neither does the master link-relation table (Section 5.2) --
there's a singular `description` rel for one place description, but nothing
for the list. Rather than leave `/places` completely undiscoverable from the
Collection, `place-descriptions` is added anyway, following the
`source-descriptions` naming convention, under Section 4.5.4's explicit
allowance: "other transitions... is RECOMMENDED where applicable." A strict
client that only walks formally-specified rels won't find `/places` this
way; it'll need to know the URL, same as before this change.

### Collection ids and titles: human-recognizable, not persistent

`internal/collectionid` derives each collection's id and title from two
things: the RootsMagic **Home Person** (`RootPerson` in `ConfigTable`,
read via `rmdb.RootPersonDisplayName()` -- a plain integer PersonID inside
`ConfigTable`'s Database Configuration record, which is itself plain,
human-readable XML, not an opaque format; see "SQLite driver" below for
the same kind of discovery about a different `ConfigTable` record) and the
database's filename. Both are combined deliberately, not just as a
fallback: the Home Person's name is what a human actually recognizes a
family tree by, but the filename is what disambiguates between multiple
exports/backups of the *same* tree over time, where the Home Person is
identical between them and only the filename differs. RootsMagic's own
auto-backup naming embeds a timestamp in the filename for exactly this
reason (`royal92 - 2024 06 24 09-29.rmtree`), and rather than parsing out
"the timestamp" specifically -- fragile, and only correct for RootsMagic's
own naming convention -- `Derive` just uses the whole filename stem, which
captures that case for free and degrades gracefully for any other naming.
A trivial numeric-suffix pass (`Dedupe`) runs across the whole batch of
collections at startup as a last resort, only ever visible in the rare
case two collections would otherwise land on the exact same id.

**This server makes no promise that a given database is represented by the
same Collection id across restarts, and deliberately doesn't try to.** A
few concrete ways it can't be, no matter how the id is derived: the Home
Person is a user-editable setting in RootsMagic, not a fixed property of
the file; files get renamed, moved, or restored from backup; and which
`-db` flags are passed, and in what order, is under the user's control
each time the server starts. A technically stable identifier *does* exist
-- RootsMagic assigns a `UniqueID` per database at creation
(`ConfigTable`'s XML again) that never changes -- but it's a meaningless
opaque string to a human, and the whole point of the id is to be something
a person glances at and recognizes. Chasing strict persistence would have
meant either accepting an unrecognizable id, or maintaining a local
id-to-file mapping across runs -- state this server otherwise has no
reason to keep, for a guarantee that isn't actually achievable end-to-end
regardless (a human can always rename a file or change the Home Person
between one run and the next).

So the design leans the other way entirely: derive the friendliest id
reasonably possible, accept that it can drift, and make that fact
impossible to miss. At startup, the server prints a table mapping every
collection id to its title and source file (see `cmd/server/main.go`,
`printCollectionTable`); a person reads that table and connects a client
to the right id for that session. **No client should persist a collection
id across sessions** -- discover fresh via `GET /collections` every time a
client starts, the way `gedcomx_browser.py` already does. The README
states this plainly, not just here.

The `-title` flag from an earlier version of this server is gone --
title is now always derived the same way as the id (there's no longer a
single collection for a standalone override to unambiguously apply to).

## Multimedia

`GET /artifacts` and `GET /artifacts/{id}` implement the RS spec's `Artifacts`
state (Section 4.3), backed by RootsMagic's `MultimediaTable`. Per Section 4.3.3,
the data returned is a list of the same `SourceDescription` data type used by
`/source-descriptions` (`resourceType` is set to the more specific
`http://gedcomx.org/DigitalArtifact` to distinguish artifacts from bibliographic
sources -- see the doc comment on `ResourceTypeDigitalArtifact` in
`internal/gedcomx/model.go`), so `internal/api/handlers.go` reuses the same
`SourceDescriptionsDocument`/`SourceDescriptionDocument` JSON envelope types for
both endpoints; that's spec-correct, not a shortcut, since the JSON member name
(`sourceDescriptions`) is a property of the *data type*, not of which state
returned it.

There's no formally-specified state for "download the actual bytes" -- the
spec's mechanism for that is `SourceDescription.about`, "a URI for the resource
being described." This server points `about` (and a non-spec `digital-artifact`
link, for convenience) at `GET /artifacts/{id}/content`, which streams the raw
file with a `Content-Type` inferred from the filename and supports HTTP range
requests (via `http.ServeContent`) -- useful for a client previewing large
images or seeking within video.

### How photos/certificates attach to people: citations, not just facts

The naive design -- "look up media attached directly to a person or a fact,
via `MediaLinkTable`" -- turns out to badly undercount real files. Inspecting
an actual multi-thousand-item RootsMagic database during development showed
`MediaLinkTable.OwnerType` breaking down roughly as: ~10% attached directly to
persons, a handful to events, and **the large majority (roughly 90%) attached
to *citations*** (`OwnerType = 4`, `OwnerID = CitationTable.CitationID`) --
e.g. a scanned 1911 census image lives on the "1911 Census" citation attached
to a residence fact, not on the fact itself.

So `buildSourceReferences` in `internal/api/convert.go` (which populates the
`sources` array on every `Person` and `Fact`) does three lookups and merges
them, deduplicated: bibliographic sources cited directly, media attached
directly to the person/family/event/name, and media attached to *that owner's
citations*. All three show up together in the same `sources` array, as
`SourceReference`s pointing at either `/source-descriptions/S{id}` or
`/artifacts/M{id}` depending on which kind of thing they are -- a client
doesn't need to know which case it is; the URI shape distinguishes them, and
GEDCOM X doesn't require a `sources` array to point at only one data type.
This is currently done for `Person` and `Fact` (the common cases); it isn't
yet done for `Name` (`OwnerType = 7`), which would need the same treatment if
you find media specifically attached to alternate names rather than to the
person or a fact.

### Resolving a file to an actual path on disk

RootsMagic's `MultimediaTable.MediaPath` isn't a plain path -- the data
dictionary documents a leading-symbol convention (`?` = the "Media Folder"
configured in RootsMagic's Folder Settings window, `~` = home directory, `*` =
the folder containing the database file), and in practice you'll also see
absolute paths with no symbol at all (`C:\Users\...`, `G:\My Drive\...` --
often a cloud-sync-mapped drive letter). `internal/rmdb/mediapath.go`
(`ResolveMediaPath`) implements this, normalizing backslashes and handling
each case; `internal/rmdb/mediapath_test.go` covers all of them against
concrete examples, including bugs this decoder had and no longer has (an
early version silently dropped the leading `/` off absolute paths, and
separately misdetected a Windows drive letter like `C:` as a URI scheme).

Two real limits worth knowing about, not glossed over:

- **The `?` (Media Folder) symbol can't be resolved automatically -- but not
  for the reason you might expect.** `ConfigTable.DataRec` (`RecType = 1`)
  isn't some opaque binary format; it's plain, readable XML (confirmed by
  dumping it: `<Root><Version>9000</Version>...`), with ~160 tags covering
  UI column widths, name/place formatting rules, FamilySearch/MyHeritage
  hint settings, and so on -- including, as it turns out, `<RootPerson>`,
  which this server *does* use (see "Multiple databases / Collections"
  above). It was checked exhaustively against two real files for anything
  resembling a media folder path, and found nothing. That's not a gap in
  this server's parsing -- the Media Folder setting genuinely isn't part
  of the `.rmtree` file's data model at all, and its real location has
  since been confirmed directly: `%APPDATA%\RootsMagic\Version 9\RootsMagicUser.xml`,
  under `<Folders><Media>` -- a separate, per-Windows-user application
  settings file (also plain XML), entirely outside any database file. That
  makes sense once you think about it: a folder path is inherently
  specific to the machine it's configured on, so it can't sensibly travel
  with a database file that gets copied, shared, or opened on a different
  computer -- and it also confirms the setting is genuinely one value per
  RootsMagic installation, not per database, which is exactly why
  `-media-folder` is a single flag shared across every `-db` given, rather
  than something you'd configure per collection. No amount of
  `.rmtree`-blob-parsing could ever have recovered this value, since it
  was never written there in the first place -- `-media-folder` isn't a
  workaround for an unparsed format, it's the only way this information
  can reach this server at all. If any `MediaPath` in your file uses `?`,
  pass the folder explicitly with `-media-folder`; without it, those items
  resolve with a clear error (`GET .../content` returns 500 naming the
  problem) rather than silently pointing at the wrong place.
- **A Windows absolute path (a drive letter) can't be resolved on a
  non-Windows host, full stop** -- `G:\My Drive\...` means nothing on Linux or
  macOS regardless of how cleverly it's parsed. This server passes such paths
  through as-is (best effort: if you're running the server on Windows itself,
  or the drive is genuinely mounted at that letter, it'll work) and returns a
  clear 404 naming the exact resolved path it tried, rather than a confusing
  generic error, when the file isn't actually there.

### What RootsMagic's own UI requires, confirmed by actually using it

Everything above about the `?`/`~`/`*` symbols came from the data
dictionary and from reading real `MediaPath` values already sitting in
existing files. Actually adding a new piece of media through RootsMagic's
own interface (to get `royal92.rmtree`'s wedding painting attached) surfaced
a few things about the write side that were worth confirming rather than
assuming, since they matter to anyone setting up their own database to work
well with this server:

- **RootsMagic's own file-chooser dialog has no "store as relative path"
  option.** Selecting a file always records its full, absolute path.
  Getting a `*`-relative path at all means manually editing the `MediaPath`
  field afterward, by hand, in RootsMagic's own UI.
- **A bare, symbol-less relative path is rejected by RootsMagic itself**
  ("Media file not found") -- confirming, not just assuming, that
  RootsMagic never writes that form under normal use. This server's
  resolver still has a fallback for it (treating a symbol-less `MediaPath`
  as relative to the database's own directory) purely as defensive
  robustness -- it's very unlikely to ever fire against a file RootsMagic
  itself produced, but costs nothing to keep.
- **`*` requires a path separator immediately after it.** `*royal92` (no
  separator) was rejected the same way; `*\royal92` (or `*/royal92`,
  presumably -- only the backslash form was actually tried) was accepted.
  This server's own resolver was never actually affected by this either
  way -- `ResolveMediaPath` already trimmed a leading separator uniformly,
  so it treats `*royal92` and `*\royal92` identically and correctly -- but
  it's a real, confirmed fact about what RootsMagic itself will accept,
  worth knowing if you're hand-editing a `MediaPath` yourself rather than
  going through this server.
- **RootsMagic's UI displays the expanded absolute path even after
  accepting and storing the symbolic form.** The database itself still
  holds `*\royal92` (confirmed directly: that's the literal, unmodified
  `MediaPath` value in `royal92.rmtree`), but RootsMagic's own interface
  shows the fully expanded path back to the user -- the symbol is a
  storage-and-portability convenience, transparent to the person using the
  software, not something RootsMagic expects a user to keep looking at.

One thing above is confirmed only for `*`, not verified the same way for
`~`: it's a reasonable inference that `~` behaves identically (same
symbol-expansion mechanism, presumably the same code path inside
RootsMagic), but that's inference, not something this server's development
directly exercised the way `*` was. Worth confirming directly before
relying on it, the same way `*` was confirmed here rather than assumed.

### Items that are links, not files

Not every `MultimediaTable` row is a local file. Databases built partly from
online-search integrations can have rows where `MediaPath` is already a
URL-shaped value from an external provider (a real, observed example:
`MediaPath = http:\search.findmypast.com{0}\transcript?id=...`, `MediaFile` a
number that's presumably meant to be substituted into the `{0}` placeholder).
That substitution rule isn't documented anywhere this server could verify, so
rather than guess and risk presenting a broken link as if it worked,
`rmdb.LooksLikeExternalReference` just detects the pattern (a URI-scheme-like
prefix) and, for those items, `buildArtifactDescription` skips `about` and the
content link entirely and adds a note explaining why. `GET
/artifacts/{id}/content` for one of these returns a clear 404 rather than
trying to open `http:\...` as a local file path. The item's other metadata
(caption, description, citation) is still returned normally -- only the
"fetch the bytes" part is unavailable.

### MIME type inference

`MediaType` isn't reliably useful for this (RootsMagic's own `MediaType`
column is a coarse 4-value enum -- Image/File/Sound/Video -- and its `URL`
column, which sounds like it'd help, is documented as "Not implemented" and
was empty in every real file used during development). Instead,
`gedcomx.MediaTypeForFilename` infers a MIME type from the file extension,
checking a small built-in table first (covering every extension actually
observed: jpg/jpeg/png/gif/bmp/tif/pdf/doc/docx/htm/html and a few others)
before falling back to Go's `mime.TypeByExtension`, so behavior doesn't
depend on the deployment environment having a populated `/etc/mime.types` --
fine on a typical dev machine, not guaranteed on a minimal container image.

### Sources versus media

`Person`, `Relationship`, `Event`, and `PlaceDescription` -- everything that
extends the conceptual model's `Subject` data type -- expose two separate
arrays, `sources` and `media`, not one combined list. `Fact` (a
`Conclusion`, not a `Subject`) only ever gets `sources`.

This wasn't the original design. An earlier version of this server
combined bibliographic citations and attached artifacts into one `sources`
array everywhere, on the reasoning that both "evidence" a conclusion in
some sense. That reasoning doesn't survive contact with the spec's own
text, which draws the line explicitly: `Subject.media` is defined as
references to multimedia "intended to provide additional context or
illustration for the subject and *not* considered evidence supporting the
identity of the subject or its supporting conclusions" -- a direct,
deliberate contrast with `sources`, not a stylistic one. Checked against
two independent implementations, not just the spec's prose, to make sure
this wasn't a single source's idiosyncratic reading: `gedcomx-js`
(`Subject.js`) and `gedcomx-rs` (`person.rs`, `relationship.rs`,
`event.rs`, `placedescription.rs`) both have a distinct `media` field
alongside `sources`, with doc comments quoting the same spec language.
Neither contradicts the other, or the spec.

`buildSourcesAndMedia` (`internal/api/convert.go`) replaced the earlier
`buildSourceReferences`, returning both arrays from the same underlying
query rather than one merged list -- bibliographic citations go in
`sources`; artifacts (attached directly via `MediaLinkTable`, or via the
owner's citations -- see above) go in `media`. `Fact` calls this and
deliberately discards the `media` return value: a `Fact` has nowhere to
put it, but the same `EventTable` row's corresponding standalone `Event`
(same id, see "Events" below) does, and that's where it actually surfaces
instead -- not dropped, just relocated to the one place the spec actually
allows it. `PlaceDescription` gained a query for this that didn't exist at
all before (`rmdb.OwnerTypePlace`), since a place's own citations/media
were never being surfaced prior to this.

## Events

`GET /events` and `GET /events/{id}` implement the RS spec's `Events`/`Event`
states (Sections 4.7, 4.8), backed by RootsMagic's `EventTable` +
`WitnessTable` + `RoleTable`. This is a genuinely different GEDCOM X concept
from the `Fact`s already embedded on every `Person` and `Relationship` --
worth being precise about, since both are built from the exact same
underlying RootsMagic rows.

### Event versus Fact

The conceptual model spec draws this distinction explicitly (Section
2.5.2, "Events Versus Facts"): a `Fact` belongs to, and is meaningless
outside the context of, one `Person` or `Relationship` -- "facts do not
exist outside the scope of the subject to which they apply." An `Event`
exists independently and can have multiple participants in different
roles, "described independently" of any one person. The spec's own
illustrating example is almost exactly this project's motivating one: "a
birth record that provides information about biological parents, adoptive
parents, additional witnesses, etc. might justify a description of the
event in addition to descriptions of any facts provided by the record."
RootsMagic's `WitnessTable` -- additional participants beyond an event's
own owner, each with a role -- is precisely that "additional witnesses"
case, and a marriage is the clearest instance of it: the event has (at
least) two principals and, often, witnesses who aren't the couple
themselves.

So `buildFact` (unchanged) and `buildEvent` (new) both start from the same
`rmdb.Event` (an `EventTable` row) and deliberately produce two different
resources, at two different URLs, not one resource wearing two hats. They
share an id on purpose: an `Event`'s id is `E{EventID}`, the identical
scheme `factRef` already used for the corresponding `Fact`'s id nested
inside a `Person` or `Relationship` (see `parseEventID`'s doc comment) --
so if a client sees `"id": "E5049"` in a `Relationship`'s `facts` (this is
real, verified data: it's the Marriage fact on `F1`, the couple
relationship between Victoria Hanover and Albert in the `royal92.rmtree`
sample), it already knows `GET /events/E5049` will resolve to the fuller,
multi-participant picture of that same occurrence, with no separate
lookup needed to make the connection.

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
Additional participants come from `WitnessTable` rows for that
`EventID`, with `EventRole.type` resolved from `RoleTable.RoleName`
(free text the user assigns via RootsMagic's "Edit Role Type" window)
through `gedcomx.EventRoleType` -- a conservative, small table of common
English terms ("witness" → `Witness`, "officiant"/"minister"/"clergy" →
`Official`, etc.) mapping to Section 3.15.1's four known role types, with
a `http://rootsmagic.local/event-role/...` custom-URI fallback for
anything else, following the same convention as fact types
(`CustomFactType`) and event types (`CustomEventType`, below) -- rather
than guessing at what an arbitrary user-defined role name like "Best Man"
or "Bridesmaid" should map to.

`Event.type` itself is resolved by a *separate* function, `EventType`,
not by reusing `FactType`. The two tables mostly agree where the concepts
overlap (birth, death, marriage, divorce, burial, christening, census all
resolve to the identical URI either way) but not entirely: RootsMagic's
"ADOP" fact type is `http://gedcomx.org/AdoptiveParent` as a *fact* (a
fact about being an adoptive parent) but `http://gedcomx.org/Adoption` as
an *event* (the adoption event itself) -- confirmed against the spec's
"known event types" (Section 2.5.1) and "known fact types" tables, not
assumed. Reusing one mapping for both would have silently mislabeled that
one case, so `gedcomTagToEventType` is its own table, and
`CustomEventType`'s fallback URI namespace (`event-type` vs `fact-type`)
is kept distinct too, even though in practice most events and their
corresponding facts share the same underlying RootsMagic fact type name.

### Witnesses who aren't in the database

`WitnessTable.PersonID` can be `0`, meaning the witness isn't a person
recorded in this database at all -- RootsMagic stores their name as free
text instead (`WitnessTable.Given`/`Surname`). This isn't a hypothetical
edge case: `royal92.rmtree`'s own marriage event for Victoria and Albert
(`E5049`) has both kinds side by side -- twelve witnesses who are real
`Person`s already in the database (family members like Queen Adelaide,
`P219`), and Victoria's twelve bridesmaids (Mary Howard, Caroline
Gordon-Lennox, and ten others), who aren't.

`EventRole.person` is REQUIRED by the spec and MUST resolve to a real
`Person` resource. A `PersonID=0` witness structurally cannot satisfy
that -- there is no `Person` resource to reference, and inventing one
(synthesizing a fake `Person` from just a name, or fabricating a
resolvable-looking URI that doesn't actually resolve) would misrepresent
what's actually in the source database, which runs against this project's
whole approach (see, for a concrete precedent, how unresolvable
`MediaPath` values are handled in "Multimedia" above). So these witnesses
are simply left out of `roles`, but -- deliberately, not as an
afterthought -- not dropped from the response altogether: they're
collected into an `Event`-level note instead. The real, current output for
`E5049`:

> Additional participants recorded by name only, not as persons in this
> database: Mary Howard (Bridesmaid); Caroline Gordon-Lennox (Bridesmaid);
> Adelaide Paget (Bridesmaid); Eleanora Paget (Bridesmaid); Elizabeth
> Howard (Bridesmaid); Wilhelmina Stanhope (Bridesmaid); Sarah Villiers
> (Bridesmaid); Elizabeth Sackville-West (Bridesmaid); Ida Hay
> (Bridesmaid); Frances Cowper (Bridesmaid); Mary Grimston (Bridesmaid);
> Jane Pleydell-Bouverie (Bridesmaid)

That "(Bridesmaid)" comes from `RoleTable`, not `WitnessTable.Note`, and
that distinction was a real, working-with-real-data lesson, not a design
call made up front. RootsMagic's own UI offers exactly one built-in role
per fact type (`Witness`, for `Marriage`) -- anything more specific has to
be added manually as a new `RoleTable` row, which is genuinely how
`royal92.rmtree` ended up with a `Bridesmaid` role at all. The first
attempt at this sample data used the free-text `Note` field to record
"Bridesmaid" instead, since at the time that looked like the obvious place
for it -- but `Note` is a multi-line free-text area meant for substantive
commentary, not a categorical label, and RootsMagic's own UI reflects that
distinction. So the code initially got this backwards (preferring `Note`
over the role name whenever `Note` was present), which happened to produce
correct-looking output only because the two were never populated at the
same time in the test data. Once bridesmaids got a proper `Bridesmaid`
role instead, that bug would have silently reverted every one of them back
to "(Witness)". The fix: the role name (resolved through
`gedcomx.EventRoleType`, same as it always was) is always shown when set,
full stop, and `Note` -- on the rare chance it's ever populated
*alongside* a role, for genuine supplementary commentary -- is appended
separately (`"Name (Role): note text"`) rather than overriding or blending
with it. This mirrors, as closely as a witness without a `Person` behind
them can, how `EventRole.details` already works for the twelve witnesses
who *are* real `Person`s (below): the role type and its free-text details
are always two distinct pieces of information, never one replacing the
other.

## Embedded relationship states on the `Person` state

Prompted by direct review, not found independently: the RS spec's
Section 4.10.5, "Embedded States," lists `child-relationships`,
`parent-relationships`, and `spouse-relationships` as each `MUST` for
the `Person` state -- *"If no link to `child-relationships` is
provided, the list of child relationships MUST be included"* in the
same response (and correspondingly for the other two). The separate
"Link Relation Types" appendix (Section 5.2) confirms the same three
rel names as "embedded link[s]." This server previously provided
neither -- no `links.child-relationships`/etc., and `PersonDocument`
had no field to embed the data in at all -- so a single `GET
.../persons/{id}` never surfaced a person's own relationships, only
links to the separate `.../parents`, `.../children`, `.../spouses`
endpoints (which return lists of `Person`s, not `Relationship`s, and
are a different rel name entirely from the three the spec requires
here).

Fixed by embedding rather than linking: `PersonDocument`
(`internal/gedcomx/model.go`) gained a `Relationships` field,
deliberately without `omitempty` -- an absent field would be
indistinguishable from a person who genuinely has none, which is the
whole ambiguity the spec's own `MUST` is there to resolve. `handlePerson`
(`internal/api/handlers.go`) populates it with every `ParentChild`
relationship where this person is a child, every `ParentChild`
relationship where this person is a parent, and every `Couple`
relationship this person is part of.

Rather than duplicate that computation a fourth time, it reuses the
identical logic `GET .../persons/{id}/parents`, `.../children`, and
`.../spouses` already needed for their own, pre-existing
`Relationships` fields (`PersonRelativesDocument`, which already
embedded relationships correctly -- this gap was specific to the single
`Person` state, not those three). Each of the three handlers'
relationship-computation logic was extracted into its own
`personParentRelationships`/`personChildRelationships`/
`personSpouseRelationships` helper, with the three original handlers
refactored to call their own helper rather than duplicate the logic
inline -- checked against the full existing test suite immediately
after the refactor, before adding anything new, to confirm this was a
genuine extraction and not an accidental behavior change.

Verified directly against real data, not just reasoned about: `GET
.../persons/P1` (Victoria) now embeds exactly 12 relationships -- her
two parents, all nine of her real children with Albert, and her own
Couple relationship to Albert (complete with its `Marriage` fact,
sources, and all) -- confirmed against the live response, not assumed
from the code. The existing golden-file test for this exact endpoint
(`cmd/server/testdata/get_person_expected.json`) was regenerated from a
real server response rather than hand-edited, to avoid a transcription
error in a file this large. A dedicated test also confirms a person
with genuinely zero relationships gets `"relationships":[]`, not
`null` or an absent field -- the specific failure mode the spec's
`MUST` requirement is there to prevent, and the reason the new field
was deliberately not marked `omitempty`.

## `collection` link on the `Person` and `Relationship` states

Also prompted by direct review: the RS spec's own "Transitions" tables
for both states list a `collection` transition -- Section 4.10.4 for
`Person` ("Link to the collection that contains this person"), Section
4.21.4 for `Relationship` ("Link to the collection that contains this
relationship"). Neither state produced one; `Person` had links to
`parents`/`children`/`spouses`/`ancestry`/`descendancy`/itself, and
`Relationship` had only a self-link, `relationship`.

Fixed directly using the existing `s.collectionBaseURL` field
(`internal/api/server.go`) -- already exactly `cfg.BaseURL +
"/collections/" + cfg.ID`, i.e. the collection's own URL, computed once
at server startup and already used (via the `s.url` helper, which
appends a path to it) for every other link this server builds. The new
`collection` link uses `s.collectionBaseURL` directly, with no path
appended, since the collection state's own URL is exactly that value. In
`internal/api/convert.go`: `buildPerson` gains it alongside `person`;
`buildCoupleRelationship` and `buildParentChildRelationship` both gain
it alongside `relationship`. `RelationshipDocument.Links` already
mirrors a relationship's own `Links` field directly (`Links: rel.Links`
in `handleRelationship`), so no separate change was needed there for it
to propagate to the top level.

Verified as a genuine round trip, not just a string match: fetched
`.../persons/P1`'s and `.../relationships/F1`'s own `collection` link
`href` values directly and confirmed each one resolves to this same
collection's real `Collection` resource (`"id":"victoria-hanover-royal92"`
in the response), not merely present with a plausible-looking value.
Six existing golden files needed regenerating as a result
(`get_persons_expected.json`, `get_person_expected.json`,
`get_person_ancestry_expected.json`, `get_person_descendancy_expected.json`,
`get_relationships_expected.json`, `get_relationship_expected.json`) --
each diffed programmatically against its own prior version afterward
(comparing the full set of JSON key names present, not just running the
test suite) to confirm `collection`/`href` were the *only* keys added
and nothing was accidentally removed, before trusting the regeneration
and replacing the files.

## `DisplayProperties`: `marriageDate`/`marriagePlace` were never implemented, `birthPlace`/`deathPlace` were implemented but never wired up

Prompted by a direct report -- "`marriageDate`/`marriagePlace` appear to
be omitted." Checked directly against the RS spec's own `DisplayProperties`
properties table (Section 2.2) before touching anything: `name`, `gender`,
`lifespan`, `birthDate`, `birthPlace`, `deathDate`, `deathPlace`,
`marriageDate`, `marriagePlace`, `ascendancyNumber`, `descendancyNumber`,
`familiesAsParent`, `familiesAsChild`. `marriageDate`/`marriagePlace`
weren't just unpopulated -- the `DisplayProperties` struct
(`internal/gedcomx/model.go`) had no fields for them at all, added as
part of this fix. While investigating, `birthPlace`/`deathPlace` turned
out to have the same underlying gap one level less visible: the struct
fields already existed, but `buildDisplayProperties`
(`internal/api/convert.go`) never actually populated either one --
fixed alongside `marriageDate`/`marriagePlace`, since it's the exact
same shape of gap against the same spec table, not a separate report.
`familiesAsParent`/`familiesAsChild` are a real, separate gap too (also
unpopulated) but are a meaningfully larger feature -- each `FamilyView`
needs its own `parent1`/`parent2`/`children` construction across every
family a person is in, both as parent and as child -- and weren't part
of the report; left for their own turn rather than folded in here.

`birthPlace`/`deathPlace` come directly from this same person's own
Birth/Death facts -- `buildPerson` already computes these as
`[]gedcomx.Fact` before calling `buildDisplayProperties`, so the
already-built facts are reused rather than re-fetched. There's only
ever one Birth and one Death fact per real person in practice, so no
"which one" ambiguity the way marriage has.

`marriageDate`/`marriagePlace` needed a real design decision the spec
itself doesn't make: a person can have more than one marriage, and
`DisplayProperties` has room for exactly one of each. Resolved the same
way this project has resolved other "which one, when there are several"
questions with no other spec guidance -- take the first, consistently
ordered (`FamiliesAsParent`'s own query, `ORDER BY FamilyID`, matching
the existing convention already used for a person's primary name and
other "the first one" choices), and skip to the next family only if the
first has no Marriage fact at all, rather than treating a family known
not to have one as this person's answer. Verified against real,
non-obvious data, not a constructed example: `royal92.rmtree`'s own
person 21 (William II) has two real families where exactly this
happens -- the first (FamilyID 136) has no Marriage fact, the second
(146) does (5 Nov 1922) -- and this server correctly reports the
second, not an empty result from the first. This required
`buildDisplayProperties`'s own signature to change (it previously only
took `names`/`sex`; now also takes `personID` and the already-built
`facts`, and returns an error, since it now queries the database for
this person's own families).

Four golden files needed regenerating as a result
(`get_persons_expected.json`, `get_person_expected.json`,
`get_person_ancestry_expected.json`, `get_person_descendancy_expected.json`)
-- each diffed programmatically against its own prior version, the same
way as the `collection` link fix above, confirming `birthPlace`/
`deathPlace`/`marriageDate`/`marriagePlace` were the only keys added.

## `familiesAsParent`/`familiesAsChild`

Flagged as a real, separate gap at the time `marriageDate`/
`marriagePlace` were fixed (both fields already existed on the
`DisplayProperties` struct and were already correctly `nil`/omitted
when empty -- `FamilyView` itself, and both of these fields referencing
it, were already modeled -- but neither was ever actually populated) and
implemented here, in the dedicated turn that was flagged for rather than
folded in earlier.

Unlike `marriageDate`/`marriagePlace`, these carry no "which one, when
there's more than one" ambiguity to resolve: both are `OPTIONAL`,
"Order is preserved" *lists* (Section 2.2's own properties table), so
every family a person is a parent or a child in belongs in the result,
not just one. `buildFamilyView` (`internal/api/convert.go`) builds a
single `FamilyView` from an `rmdb.Family` -- checked directly against
the separate `FamilyView` data type (Section 2.3) before writing
anything: `parent1`/`parent2` are each individually `OPTIONAL` ("up to
two parents"), and `children` is "a list of references to the children
... who have that set of parents in common." The spec is deliberately
silent on which of `parent1`/`parent2` is which -- the same ambiguity
already resolved for `Relationship.person1`/`person2` on a `Couple`
relationship -- so `parent1`/`parent2` are assigned Father/Mother
respectively, matching `buildCoupleRelationship`'s own existing
`Person1`=Father/`Person2`=Mother convention for the exact same
`FatherID`/`MotherID` pair, rather than introducing a second, different
convention for the same underlying data. This one helper is shared by
both `familiesAsParent` (built from `FamiliesAsParent`'s own
already-fetched results, reused rather than re-queried a second time)
and `familiesAsChild` (a new query, `ChildRowsAsChild`, one per family
this person is a child in).

A `familiesAsChild` entry for the person's own family as a child
correctly includes that same person among `children` (alongside any
siblings) -- not a special case, but a direct, correct consequence of
`buildFamilyView` fetching *every* child of the family via
`ChildRowsOfFamily`, which this person is trivially one of, by the
spec's own definition of a family "view" as parents plus every child
who shares them.

Verified against real, non-obvious data: `royal92.rmtree`'s Victoria
(`P1`) has exactly one `familiesAsChild` entry (her own two real
parents, herself as the sole listed child -- no recorded siblings in
this database) and one `familiesAsParent` entry (herself and Albert as
parents -- Albert in `parent1`, confirming the Father/Mother convention
holds in practice, not just in the code) with all nine of her and
Albert's real children, in order.

Two of the three new permanent tests (`cmd/server/main_test.go`,
`TestDisplayPropertiesFamiliesAsParentAndChild`) initially failed for a
revealing reason worth recording, not just fixing quietly: the test
itself, not `buildFamilyView`, had the bug. Linking two children to only
their shared father -- no mother at all -- correctly produced two
separate single-parent families, not one shared family, because a bare,
single-parent `ParentChild` request never assumes an unnamed second
parent (see `CreateParentChildRelationship`'s own comment, and "A real
design mistake, corrected" earlier in this document, for the full,
previously-established reasoning this test had briefly forgotten).
Corrected by linking each child to both parents separately, the same
requirement already established for every other multi-child scenario in
this project.

Four golden files needed regenerating as a result of implementing this
(the same four as the `marriageDate`/`marriagePlace` fix, since all four
embed a full `Person` with its own `display`), diffed the same way,
confirming `familiesAsParent`/`familiesAsChild`/`parent1`/`parent2`/
`children`/`resource`/`resourceId` were the only keys added.

### A real, separate bug this surfaced: `Couple` relationship `facts` were silently discarded

Building a self-contained test for the fix above (rather than relying
only on `royal92.rmtree`'s existing data) required posting a `Couple`
relationship with a `Marriage` fact through the real HTTP API -- which
is when this was found: `handleCreateRelationships`
(`internal/api/createrelationship.go`) read a relationship's own
`Facts` for `ParentChild` (`relTypeFromFacts`, to detect
`Adoptive`/`Biological`/etc.) but never even looked at `Facts` for
`Couple` at all. `rmdb.NewCoupleRelationship` already has a `Facts`
field the storage layer already knows how to write -- it just never
received anything, since nothing on the API layer's `Couple` branch
ever populated it. The request itself gave no indication anything was
wrong: `POST /relationships` with a `Marriage` fact returned a normal
`201 Created`; the fact was simply never written to `EventTable` at
all, confirmed directly by querying the resulting database rather than
just re-reading the API's own response.

This had gone undetected because the two layers were each tested in
isolation: `internal/rmdb`'s own tests construct
`rmdb.NewCoupleRelationship{..., Facts: [...]}` directly, which
correctly exercises `CreateCoupleRelationship`'s own fact-writing
logic but never touches `handleCreateRelationships` at all; no existing
HTTP-level test had ever posted a `Couple` relationship with `Facts`
and then checked they actually round-tripped back on `GET`.

Fixed by generalizing `buildNewPersonFact` (renamed `buildNewFact`,
`internal/api/createperson.go`) to accept the fact's *expected* owner
type as a parameter (`rmdb.OwnerTypePerson` or `rmdb.OwnerTypeFamily`)
rather than hardcoding `Person` -- the conversion logic itself (date
parsing and its own fallback chain, place handling, `Details`/`Value`)
is identical either way; only the `FactTypeTable` ownership check
differs. `handleCreateRelationships`'s `Couple` branch now converts
`rel.Facts` the same way `ParentChild` already read them for
`relTypeFromFacts`, and passes the result through to
`CreateCoupleRelationship`. Verified with a dedicated regression test
(`cmd/server/main_test.go`, `TestCreateRelationshipsHTTP`) posting a
`Couple` relationship with a real `Marriage` fact and confirming it
reads back correctly via `GET` -- deliberately placed and named
independently of the `DisplayProperties` test that surfaced it, since
this is a genuinely separate bug a future reader should be able to find
by name.

## `Location` on a single `ParentChild` creation identified the wrong resource

Reported directly, precisely: a successful single `ParentChild`
creation returned `Location: /relationships/F{id}` unconditionally --
`coupleRef`'s own shape -- regardless of what was actually created.
Confirmed exactly as described before changing anything:
`handleCreateRelationships`'s final response construction
(`internal/api/createrelationship.go`) called `coupleRef(familyID)`
unconditionally for every successful single creation, `Couple` or
`ParentChild` alike -- there was no branch on which type had actually
been created at all.

This is wrong in the two distinct ways named in the report, both
checked directly rather than assumed. First, per the RS spec's own
Section 4.20.2, a single-create response's `Location` has to identify
the relationship actually created -- and `coupleRef` ("`F{id}`") and
`parentChildRef` ("`F{id}-FC{child}`" / "`F{id}-MC{child}`") name
genuinely different resources, not two equally-valid spellings of the
same one: `GET` on a `ParentChild`'s `coupleRef`-shaped URL 404s, since
that URL denotes a `Couple` relationship, which a newly-created
single-parent family isn't and was never claimed to be -- confirmed
directly, not asserted. Second, even for a family that *does* also have
a matching `Couple` resource (both parents present), `coupleRef` alone
doesn't identify *which* parent-child edge was actually created --
`Location` naming the family a relationship happens to belong to isn't
the same as naming the relationship itself.

Fixed by determining, for each created relationship, its own correct
ref immediately after creation -- `coupleRef(familyID)` unchanged for
`Couple`; for `ParentChild`, `parentChildRef(familyID, childID,
isFather)`, with `isFather` determined by fetching the just-created
family fresh (`FatherID == parentID`) rather than re-deriving it a
second, separate way (e.g. re-querying the parent's own sex
independently) -- directly authoritative about which role this specific
parent actually ended up in, including the idempotent case (linking an
already-linked parent again), rather than assuming a separately-run
computation agrees with what `CreateParentChildRelationship` itself
determined.

This also required correcting an existing test whose own assertion had
been relying on the bug without meaning to: it linked a child's
biological father, then biological mother, and asserted their two
`Location` values were `Equal` -- true only because the old, buggy
behavior collapsed both a father-child and a mother-child edge on the
same family down to the same `coupleRef` value, masking that they were
always two distinct relationships. Fixed to compare each `Location`'s
own family-id prefix (its actual, real intent -- "did these two land in
the same family") rather than the full ref, and strengthened with an
explicit check that the father-child and mother-child refs are
themselves different, which is now something meaningful to verify
rather than something the old bug accidentally made true either way.

Verified as a full round trip for both parent roles, not just that the
returned string looks right: `cmd/server/main_test.go` gained a
dedicated test that creates a father-child and a separate mother-child
relationship, fetches each `Location` afterward and confirms `200` with
the correct relationship (not just a plausible-looking string), and
confirms the old, buggy `coupleRef`-shaped URL for that same family
still correctly 404s -- reproducing the exact symptom in the original
report, not a stand-in for it.

## Person Search Results: the 10 direct search parameters

Prompted by a direct request, discussed in two parts -- an effort
assessment first, then the implementation once its two open design
questions (response format, non-exact matching) had actual answers
rather than assumptions.

This section (and its two follow-ups below, covering the
`"{relation}"`-prefixed parameters and Place Search Results)
supersedes an earlier design note in this document. Early in this
project, real Atom-based search looked like enough effort on its own
to be out of scope entirely -- a plain, non-Atom `GET
/persons?name=...` substring filter (`ListPersons`'s own `nameFilter`
parameter, `internal/rmdb/queries.go`) was added as a stand-in, with
this same section of `SCOPE.md` explicitly noting it as "a natural
place to grow real search later." That real search now exists, so the
stand-in it was explicitly meant to be temporary until has been
removed outright -- not deprecated or left running alongside the real
thing, since a client with any reason to prefer the old, far weaker
filter over the real thing implemented below shouldn't exist. `GET
/persons?name=...` is inert now: `name` is simply an unrecognized
query parameter `handlePersons` never looks at, the same as any other
one would be, silently ignored rather than erroring, consistent with
how that endpoint has always treated a query parameter it doesn't
recognize.

**The response format turned out to be far smaller than "Atom" first
suggests.** Checked directly against the RS spec before assuming a full
RFC 4287 XML serializer was needed: `application/x-gedcomx-atom+json`
(the GEDCOM X Atom Extensions specification's own JSON representation)
is the `MUST`-support media type for this state (Section 4.11.1); full
`application/atom+xml` is only `RECOMMENDED`. Matching this project's
own existing choice not to build XML support for the rest of the API
(`gedcomXMediaType`'s own comment), only the JSON representation is
implemented here either. That representation turned out to be a thin,
flat envelope (`internal/gedcomx/atom.go`: `AtomFeed`/`AtomEntry`/
`AtomContent`) whose `content.gedcomx` member is exactly the same
`PersonDocument` every other Person-returning endpoint already
produces -- checked against the Atom Extensions spec's own JSON member
table and the Content Processing Model section (3.2) before writing
any of this, not assumed. `ID`/`Title`/`Updated` are deliberately not
`omitempty` on either type, matching this project's own established
"don't let an omitted field be ambiguous with a genuinely absent value"
principle (`PersonDocument.Relationships`'s own precedent) -- checked
directly against RFC 4287's own RELAX NG grammar for `atomFeed`/
`atomEntry` (`atomId & atomTitle & atomUpdated`, none of them optional
or repeatable) before deciding this, since RFC 4287 itself requires
all three, exactly once.

**Non-exact ("~") matching is a plain SQL substring match**, per
direct instruction, after confirming there's no free, better option:
`NameTable.GivenMP`/`SurnameMP` turned out to be accent-folded
(`FoldAccents`) copies of `Given`/`Surname`, not a phonetic (Metaphone/
Soundex) encoding despite the column name -- RootsMagic has no fuzzy-
matching infrastructure to build on here. `internal/rmdb/search.go`'s
own `textCondition` applies `LIKE '%value%'` for non-exact, `=` for
exact, both sides wrapped in `LOWER(...)` throughout (not relying on an
implicit per-column collation, since `PersonTable.Sex` isn't even text
and `PlaceTable.Name`/`NameTable.Given` aren't guaranteed to share
one).

**Date matching reuses `gedcomx.ParseGedcom5Date`** -- the same GEDCOM
5.x date grammar this project already parses `Date.original` with for
write support -- rather than inventing a second date parser, since a
search client typing `birthDate:"30 June 1900"` is producing exactly
that kind of text. The parsed (year, month, day) becomes a `SortDate`
range (`ComputeSortDate`, already used for write support's own sort
values): exact match narrows to only the precision actually given (a
bare year widens to the whole year, a month+year to the whole month,
never narrower than what was specified); non-exact always widens to
the whole year regardless of how precise the given date was -- a
plain, defensible reading of "less precise" consistent with the
project's own preference for a simple, stated rule over an invented
fuzzy-date scheme this data has no real support for either. An
unparseable date is rejected with `400`, not silently matched against
nothing -- this project's established "reject clearly rather than
silently produce wrong results" principle, applied here as much as
anywhere else.

**Fact-based criteria (birth/death/marriage) use `EXISTS` subqueries,
not `JOIN`s** -- deliberately, since a person can have more than one
marriage (and, in principle, more than one recorded Birth/Death fact);
a `JOIN` would multiply `PersonID` rows in a way a bare `SELECT
DISTINCT` could mask incorrectly specifically for the separate `COUNT`
query this needs for `gx:results` (RS spec's own paging semantics).
Marriage criteria join through `FamilyTable` (`FatherID = ? OR
MotherID = ?`) to each of a person's own families, matching every
other "which of this person's families" query already built for
`buildDisplayProperties` and the embedded-relationships work.

**`atom:updated` needed a real, verified unit conversion this project
hadn't needed before.** `PersonTable.UTCModDate` is stored as days
since 1899-12-30 (the OLE Automation epoch, confirmed via
`utcModDateExpr`'s own definition, `internal/rmdb/writes.go`), but the
Atom Extensions JSON format requires milliseconds since the Unix
epoch. The 25569-day gap between the two epochs was verified directly
via date arithmetic, not assumed from memory, and the resulting
conversion (`GetPersonUTCModDate`, `internal/rmdb/queries.go`) was
checked against a real value from `royal92.rmtree` before trusting it
-- confirming a plausible, sane date, not just a number that compiled.
Kept as its own, separate query rather than added to the shared
`Person` struct every other query already populates, since this is
specifically a Person Search Results need, not something
`GetPerson`/`ListPersons`/etc. have ever required.

**A real correctness issue caught while writing the handler, not after:**
an entry's own `content.gedcomx` reuses `PersonDocument` directly, whose
`Relationships` field is deliberately never `omitempty` (see its own
comment) specifically so an empty result can't be mistaken for "not
computed." An earlier draft of this handler populated that field with a
bare `[]gedcomx.Relationship{}` for every search result rather than
actually computing it, which would have silently, incorrectly implied
every matching person has no relationships at all. Fixed by reusing
`personParentRelationships`/`personChildRelationships`/
`personSpouseRelationships` (the same helpers `handlePerson` itself
uses) for each search result too, rather than shipping a document that
happened to satisfy the type system while quietly lying about its own
content.

**Routing**: `GET /persons/search` is registered as its own, literal
route alongside the existing `GET /persons/{id}` wildcard -- verified
directly, not assumed, that Go 1.22's `net/http.ServeMux` correctly
prioritizes the literal pattern for a request to exactly `/persons/search`
rather than treating "search" as a `{id}` value (a small, standalone Go
program confirmed this precisely before relying on it in the real
router).

**Content negotiation**: this endpoint's required media type
(`gedcomXAtomMediaType`) differs from every other endpoint in this
server (`gedcomXMediaType`), so it's exempted from the global
`withContentNegotiation` middleware (`server.go`) the same way
`.../content` already is, with its own Accept-header check and
`Content-Type` inside `handlePersonSearch` itself
(`internal/api/personsearch.go`).

**Discovery**: the `Collection` state gained a `person-search`
templated link (RS spec's own master transitions table, Section 5.2:
"Templated Link to the query used to search for persons") using
`gedcomx.Link`'s existing, already-modeled but previously-unused
`Template` field. `q` is the spec's own template variable name
(Section 6); `limit`/`offset` (not the spec's generic `count`/`start`)
match this server's own existing paging parameter names used
everywhere else, for consistency with the rest of this API rather than
a second, different naming convention for this one endpoint.

`Place Search Results` (a separate, similarly Atom-based RS state) is
not addressed here.

## Person Search Results: the "{relation}"-prefixed search parameters

The follow-up to the 10 direct parameters above. The relation search
parameters table (RS spec Section 6) was re-checked directly before
starting this, not worked from the earlier summary of it: `{relation}`
substitutes `father`/`mother`/`spouse`/`parent`, each with **9** fields
(`Name`, `GivenName`, `Surname`, `BirthDate`, `BirthPlace`, `DeathDate`,
`DeathPlace`, `MarriageDate`, `MarriagePlace`), for 36 total -- the
project's own earlier running total of "32, 8 each" had simply missed
`MarriagePlace` from the table; caught and corrected before writing any
code against the wrong count, not after.

**The central design question the spec doesn't answer directly**: do
several fields for the same relation (`fatherGivenName:John
fatherSurname:Smith`) have to be satisfied by *one* father, or could a
father named John and a separately-recorded father named Smith each
satisfy one field independently? Resolved by the spec's own wording --
"the given name of **the** father" (singular, definite article) is only
consistent with one specific person's own facts, not a disjunction
across everyone who has ever held that role for the searched person.
`RelationCriteria` (`internal/rmdb/search.go`) and `relativeConditions`
enforce this directly: every field in one relation group is matched
against the *same* candidate relative's row, via one combined,
AND-joined condition, not independent EXISTS subqueries the way the 10
direct parameters (deliberately) are. Verified as a real behavioral
distinction, not just reasoned about: a test with a father whose given
name is correct but whose surname is a value that belongs to nobody
correctly returns no match -- confirming this isn't silently treated as
"OR across different relatives," which would have been the wrong,
easier-to-accidentally-implement reading.

**Resolving each relation to a candidate relative and family**:
`father`/`mother` go through `ChildTable`/`FamilyTable` -- the same
"which family is this person a child in" relationship
`buildDisplayProperties`'s own `familiesAsChild` already uses -- and
match if *any* of the person's families-as-child qualifies (the same
"any of several, not all" precedent the direct `marriageDate` parameter
already established for someone with more than one marriage).
`parent` is father OR mother -- but, per the same-relative rule above,
each side of that OR still has to satisfy every field of the `parent`
group internally by itself; a father half-matching and a mother
half-matching the rest doesn't combine into a match either. `spouse`
resolves the other way, through `FamilyTable` directly (families where
the searched person is one of the two parents), matching whichever of
`FatherID`/`MotherID` isn't them -- two symmetric branches, since which
column holds "the other one" depends on which role the searched person
themselves occupies.

**`{relation}MarriageDate`/`{relation}MarriagePlace` are tied to the
specific family that established the relation**, not just any marriage
the relative has ever had -- a real, considered choice among more than
one reasonable reading. For `father`/`mother`/`parent`, that's the
marriage of the specific family the searched person was found to be a
child of; for `spouse`, it's the searched person's own marriage to that
specific spouse. This was available without extra cost, since that
family is already resolved by the time any of `relativeConditions`'s
own fields are being matched, and it avoids the same "which one, if
there's more than one" ambiguity direct `marriageDate` doesn't have to
answer (there, "any of the person's own marriages" is a reasonable
default; here, tying it to a specific, already-identified family is
more precise and just as available). Verified against real,
non-obvious data: `royal92.rmtree`'s own Victoria has a real
`fatherMarriageDate`/`fatherMarriagePlace` distinct from her own
`marriageDate`/`marriagePlace` (her parents' 1818 marriage at Kew
Palace, versus her own 1840 marriage to Albert) -- confirmed both
resolve to the correct, different family, not accidentally the same
one.

**A real mistake caught during manual verification, before it became a
permanent test**: an initial check of `spouseGivenName:Albert` failed
to match Victoria, which looked at first like a bug in the `spouse`
resolution logic. It wasn't -- Albert's actual stored given name in
`royal92.rmtree` is "Albert Augustus Charles," not "Albert," and the
test used exact matching against an unverified guess at the value
rather than the real one. Re-run against the correct, verified value
(and separately with `~` for a substring match against just "Albert"),
both matched correctly, confirming the `spouse` logic itself was right
all along -- the value worth recording isn't the mistake itself so much
as the discipline that caught it: treating an unexpected result as a
reason to verify the real data before concluding the code was wrong.

**Argument-order care**: since `relativeIDExpr`/`familyIDExpr` are
inlined raw SQL fragments (e.g. `"f.FatherID"`), not bind parameters,
each relation-condition builder has to keep its Go-side argument slice
in exactly the order its own `?` placeholders appear in the assembled
SQL text -- particularly for `spouse`'s and `parent`'s two OR'd
branches, where getting this wrong wouldn't produce an error, just
silently wrong results (a value bound to the wrong placeholder).
Verified empirically against real data for every relation and every
field, not just by inspection, specifically because a mismatch here is
exactly the kind of bug that wouldn't announce itself.

Deterministic SQL text, not Go map iteration, is used to combine the
four possible relation groups in `SearchPersons` -- each `(condition,
args)` pair is self-contained and appended atomically regardless of
processing order, so map iteration's randomization wouldn't have
actually misaligned anything, but non-deterministic SQL text is worth
avoiding for predictable logging and debugging on its own merits, not
just where correctness strictly requires it.

Verified end to end through the real HTTP API against `royal92.rmtree`
for every one of the 9 fields across all 4 relations, then locked down
with a comprehensive, self-contained permanent test suite
(`cmd/server/main_test.go`, `TestPersonSearchHTTP`) covering all 9
fields for `father` plus representative coverage of `mother`/`spouse`/
`parent`, the same-relative-not-different-relatives distinction, two
different relation groups genuinely AND'ed together, and an
unrecognized relation-field name rejected with its own specific
message.

## Place Search Results

The other Atom-based search state the RS spec defines (Section 4.17),
extending the same infrastructure Person Search Results already built
rather than duplicating it.

**Media type and content negotiation are identical to Person Search
Results** -- checked directly, not assumed: Section 4.17.1 states the
same `application/x-gedcomx-atom+json` MUST / `application/atom+xml`
RECOMMENDED requirement, word for word, as Section 4.11.1. `handlePlaceSearch`
reuses `gedcomXAtomMediaType`/`acceptsGedcomXAtomJSON` directly rather
than a second, parallel copy, and `GET .../places/search` is exempted
from the global `withContentNegotiation` middleware (`server.go`) the
same way `.../persons/search` already is.

**A real, surprising spec gap, found and confirmed before writing any
code against it**: unlike Person Search Results, the RS spec's own "q"
template variable documentation (Section 5.3) defines search parameters
*exclusively* for persons -- the direct parameters and all 36
relation-prefixed ones this project has already implemented. No
"Place Search Parameters" table exists anywhere in the specification;
the `Place Search Results` state itself, its media type, its
operations, and its data elements are all fully specified, but which
query fields are actually valid against it is left entirely undefined.
Resolved by supporting exactly one parameter, `name` -- the one
reasonable, minimal choice available without inventing spec text that
doesn't exist: `PlaceDescription` (`internal/gedcomx/model.go`) has
essentially one searchable text attribute at all (`Names`;
`Latitude`/`Longitude` aren't meaningfully "searched" the same way),
and reusing the field name `name` keeps this consistent with Person
Search Results' own `name` parameter for the same underlying concept
rather than inventing a different name for it. Any other field is
rejected outright with a message naming `name` as the only one
supported, not silently ignored.

**`AtomContent.GedcomX` was generalized from `*PersonDocument` to
`any`** (`internal/gedcomx/atom.go`) specifically to support this:
Section 4.17.3 ("Data Elements") requires each entry's content to be "a
GEDCOM X document that MUST contain at least one instance of the
`PlaceDescription` Data Type," the same "reuse the real document shape
this server already produces" approach Person Search Results already
established, just for a different existing type
(`PlaceDescriptionsDocument`, the same one `GET /places` already
returns) rather than a second, place-specific content type invented
for identical data. The spec's further requirement -- if more than one
`PlaceDescription` were provided, the "main" one must be first -- is
trivially satisfied here, since exactly one is always provided per
entry, never more.

**Each entry links via `description`, not `person`** -- checked
directly against Section 4.17.4's own Transitions table, the one
transition it defines for this state, rather than assumed to mirror
Person Search Results' own `person` rel.

**`atom:updated` reuses the identical `UTCModDate` conversion** Person
Search Results already established, applied to `PlaceTable` instead
of `PersonTable` -- confirmed directly that `PlaceTable` has the exact
same `UTCModDate FLOAT` column, storing the same OLE Automation epoch,
before assuming the existing conversion logic could be reused as-is
(`GetPlaceUTCModDate`, `internal/rmdb/queries.go`).

**Routing**: `GET /places/search` is registered as its own literal
route alongside the existing `GET /places/{id}` wildcard, the same
pattern (and the same already-verified Go 1.22 `net/http.ServeMux`
literal-over-wildcard precedence) as `/persons/search`.

**Discovery**: the `Collection` state gained a `place-search` templated
link, alongside `person-search`, using the same `q`/`limit`/`offset`
template variables for the same reasons already established there.

Verified end to end through the real HTTP API against `royal92.rmtree`
(an exact match against a real place's full name, a substring match
against "Kensington" correctly returning all five real places
containing it, and confirming the routing and Collection-discovery
behavior), then locked down with a self-contained permanent test suite
(`cmd/server/main_test.go`, `TestPlaceSearchHTTP`) covering exact and
non-exact matching, empty results, the single-parameter restriction,
the `description` rel, routing, and Collection discovery. Two of the
new tests initially failed for a reason worth recording -- not a bug in
the implementation, but a mistaken assumption in the test itself: newly
created places in `testdata/empty.rmtree` don't start at `PL1`, since
that database already ships with 205 pre-loaded LDS temple places
(`PL1`-`PL205`); a fresh place created during a test gets `PL206`
onward. Fixed by asserting on the place's own name in the response
rather than a hardcoded, assumed ID -- both more correct and more
robust against that count ever changing.

## Write support

Off by default. `-write` enables it; without it, this server behaves
exactly as it always has -- every write attempt gets a `405 Method Not
Allowed` with a correct `Allow` header (see "HTTP semantics" below), the
same as any resource this server doesn't implement writes for at all.

This is being built in deliberately small, independently-testable stages,
not as one large change, specifically so problems surface against a small
diff rather than a large one. Each stage below is a real, separate unit of
work; the ones marked done have been built, and verified against a real
RootsMagic database, not just written and assumed correct.

### Why this is risky enough to be careful about

A `.rmtree` file is frequently years of someone's actual research. Getting
this wrong isn't like getting a read endpoint wrong -- a read bug returns
bad data; a write bug can destroy real data, permanently, in a file most
people don't rigorously version-control. Two things follow from that,
both decided before any real write code existed:

- **RootsMagic must not be running at the same time.** Two writers on one
  SQLite file -- RootsMagic's own desktop app and this server -- is a real
  corruption risk, not a hypothetical one. `-write` refuses to start at all
  if `RootsMagic.exe` appears to be running (checked via `tasklist`, a
  built-in Windows command -- no new dependency). This is enforced as a
  hard precondition, not a warning: the server exits with a clear error
  rather than proceeding. See `cmd/server/rootsmagic_running_check.go` for
  the exact mechanism and its real limits, both worth knowing:
  - It only checks at startup. It cannot, and does not try to, protect
    against someone opening RootsMagic *after* rmgedcomx has already
    started with `-write`. That gap is real and currently unaddressed.
  - It's meaningful only on Windows, where RootsMagic actually runs (a
    no-op everywhere else).
  - Unlike everything else in this project, this piece could not be
    verified empirically during development -- it was written and tested
    from Linux, which can run neither `tasklist` nor RootsMagic itself.
    The code comment says this plainly. If you're relying on this check,
    actually test it once on a real Windows machine (start `-write` with
    RootsMagic open and confirm it refuses; close RootsMagic and confirm
    it proceeds) rather than trusting it on the strength of the code
    reading correctly.
- **A backup happens automatically before this server's first write.**
  `DB.EnsureBackup()` (`internal/rmdb/backup.go`) copies the source file to
  a timestamped sibling (`royal92-backup-20260806-091724.rmtree`) the first
  time it's called on a given connection -- once per server session, not
  once per write, via `sync.Once`, so every write handler can call it
  unconditionally without worrying about redundant copies. It defaults to
  the same directory as the source file; that's a placeholder for a better
  default (RootsMagic's own configured Backup folder, from
  `RootsMagicUser.xml` -- see "Multimedia" above for the sibling
  discussion about that file's Media Folder setting, which applies here
  too), not a permanent design decision. This isn't a substitute for
  RootsMagic's own backup feature, and doesn't try to be -- it's a
  narrower, automatic safety net specifically for changes made by this
  server, so a mistake here (a bug, a bad request, this server writing
  something RootsMagic doesn't expect) can always be undone by restoring
  one file, without depending on anyone having remembered to make their
  own backup first.

### Stage 0 -- plumbing only, no capability (done)

`-write` threads a `readOnly bool` all the way down to `rmdb.Open`, which
now opens `mode=rw` (with a 5-second `busy_timeout`, so a brief, incidental
lock -- e.g. RootsMagic autosaving, if it were open, which the check above
means it shouldn't be -- causes a short wait-and-retry rather than an
immediate failure) instead of `mode=ro`. `DB.ReadOnly()` and `DB.Path()`
expose that state and the source path for later stages to use. With
`-write` off, this changes nothing observable; with it on, the underlying
connection can write, but nothing yet asks it to -- no HTTP write handlers
existed at this stage. Verified directly at the SQL level (not just
assumed from the code): a raw `UPDATE` against a read-only connection to a
real database failed with `attempt to write a readonly database`; the
identical `UPDATE` against a write-mode connection to the same file
succeeded.

The startup table (`cmd/server/main.go`, `printCollectionTable`) also
gained a `UNIQUE ID` column (RootsMagic's own per-database identifier --
see "Multiple databases / Collections" above for what it is and why it's
useful alongside the human-recognizable but unstable Collection id) and a
prominent read-only/`*** WRITE MODE ENABLED ***` banner, since write mode
is a whole-server setting, not a per-collection one.

### Stage 1 -- UPDATE, `Place` and `Source` (done)

`POST /collections/{id}/places/{id}` and `POST
/collections/{id}/source-descriptions/{id}`, per RS spec Sections 4.16.2
and 4.23.2 ("Update a place description" / "Update a source description",
both `OPTIONAL`) and Section 8 ("Updating Application States": a data
element supplying its own `id` is an update candidate for that id). A
successful update returns `204 No Content`, exactly as the spec says it
SHOULD; an invalid request returns `400` with an
[RFC 7807](https://www.rfc-editor.org/rfc/rfc7807) body explaining what
was wrong, also per spec ("a `400` response code is RECOMMENDED").

These two were chosen first specifically *because* they're structurally
simple (`PlaceTable`/`SourceTable` are single tables, with none of
`Person`'s or `Relationship`'s cross-table consistency concerns) -- the
point of this stage was to prove out the reusable plumbing (request
parsing, the write-route registration pattern, transactions, response
codes, the backup call) against the lowest-risk case, not because these
two resources matter most. They may not even end up mattering much in
practice: GEDAM (the DAM tool this server was originally built to support)
doesn't need to modify `Place` or `Source` at all -- but since they were
cheap to add correctly once the plumbing existed, they're here anyway.

**Write route registration is gated by `Server.resourceHandler()` checking
`!s.db.ReadOnly()` directly** -- not a separately-tracked "is this
collection writable" setting that could drift out of sync with the
database connection's own actual state. When false, `POST
/places/{id}`/`POST /source-descriptions/{id}` simply aren't registered at
all, so a request there gets the same automatic `405` (with the correct
`Allow` header) as any other write this server doesn't implement -- there
is no code path by which a write can reach the database in read-only mode;
it's not merely checked at runtime, the handler function doesn't exist to
be called.

**What's writable, and what deliberately isn't yet:**

- `Place`: `names[0].value` -> `PlaceTable.Name`; `latitude`/`longitude`
  together -> `PlaceTable.Latitude`/`Longitude` (converted to RootsMagic's
  own decimal-degrees-times-1e7 integer encoding); `notes[0].text` ->
  `PlaceTable.Note`. Providing exactly one of `latitude`/`longitude`
  without the other is rejected with `400` rather than silently storing a
  nonsensical half-coordinate.

  **The decimal-to-integer conversion uses `math.Round`, not a bare
  `int64(...)` conversion** -- found from a real golden-file mismatch, not
  caught in advance: `44.817778 * 1e7` evaluates to
  `448177779.9999999404` in float64 arithmetic, not exactly `448177780.0`
  (most decimal fractions have no exact binary representation), and
  `int64(...)` truncates toward zero rather than rounding, so a bare
  conversion silently rounded real coordinates down by up to 1 in the
  last digit (roughly a centimeter) depending on which specific decimal
  values happened to land on the wrong side of a float64 rounding
  boundary -- confirmed directly against this exact value, not a
  generic/theoretical concern.

  **A coordinates change also sets `LatLongExact = 1`** (`PlaceTable`),
  confirmed against a real captured diff -- deliberately different from
  the `0` `UpdatePlace` writes on a `Name` change (see its own doc
  comment for the full reasoning): this server has just as much basis to
  assert "these coordinates are exact" as RootsMagic's own manual
  coordinate-entry UI does, since a client explicitly provided them
  through this API, the same kind of deliberate, explicit value either
  way. If a single request changes both `Name` and coordinates, the
  coordinates' `LatLongExact = 1` is appended after (and therefore wins
  over) the `Name` branch's `LatLongExact = 0` -- confirmed empirically
  that SQLite resolves a column set twice in one `UPDATE` by taking the
  last occurrence, so this ordering is load-bearing, not incidental.

  **`LatLongExact` turned out to belong with `fsID`/`anID`/`IsPrivate`
  after all -- excluded from the `sqldiff` golden-file comparison
  entirely, not compared directly.** An earlier version of this section
  said the opposite: that a coordinates change makes it a real,
  observable transition `sqldiff` can verify, unlike the always-`0`
  fields. That reasoning held for a coordinates-only change, but two
  otherwise-identical real captures -- the same "change every field at
  once" edit, applied to two different places -- disagreed with each
  other on `LatLongExact` alone; everything else about them matched
  exactly. RootsMagic's own value for it is downstream of the same
  non-deterministic FamilySearch/Ancestry lookup as `fsID`/`anID` (see
  TESTING.md's "Non-deterministic fields" section for the full captured
  evidence), so there's no single correct value in a golden file to
  compare against when `Name` and coordinates change together -- trying
  to match one specific capture risks chasing what's actually just
  network timing, not a real behavioral difference. This server's own
  value is still fully deterministic and still verified, just directly
  (`fieldCheck`/`verifyFields` in `cmd/server/main_test.go`) rather than
  through `sqldiff`.
- `Source`: `titles[0].value` -> `SourceTable.Name`; `notes[0].text` ->
  `SourceTable.Comments`. **`citations` is deliberately rejected with
  `400`** if present at all, rather than silently accepted and ignored, or
  guessed at: this API's own `citations` output is `ActualText` and
  `RefNumber` concatenated into one string (see "Fact type mapping"'s
  sibling reasoning elsewhere in this file about not reusing ambiguous
  mappings), and there's no way to safely split an arbitrary string back
  into those two original fields. Getting this wrong would mean silently
  corrupting a citation, which is worse than refusing outright. A future
  revision could expose `ActualText`/`RefNumber` as genuinely separate
  fields (on both the read and write sides) to resolve this properly,
  rather than guessing now.

**Every write handler decodes its request body via `decodeStrictJSON`
(`internal/api/server.go`), not `json.Decoder.Decode` directly** -- it
sets `DisallowUnknownFields`, so a field name a target type doesn't
recognize is a `400`, not silently dropped. Found the hard way: a request
using `{"value": "..."}` instead of `{"text": "..."}` on a `Note` decodes
without error by default (the mistyped field is just ignored, leaving
`Text` at its zero value), and if that happens to be the only field in
the update, the request looks like a legitimate no-op and returns a
misleading `204` -- the client's intended write never took effect, with
no signal that anything went wrong. Confirmed directly: the exact request
that caused this now returns `400` naming the specific unrecognized
field (`json: unknown field "value"`), and a legitimate GET-then-echo-back
request (real fields like `id`/`links`/`notes` all present together)
still works normally -- `DisallowUnknownFields` only rejects names a type
genuinely doesn't have, not the ordinary pattern of a client sending back
more of a resource's own fields than strictly changed.

**Update semantics: a field that's absent, or present-but-empty, is left
unchanged -- there is currently no way to explicitly clear a field back to
empty via this API.** This is a real, deliberate limitation, not an
oversight: cleanly distinguishing "the client omitted this key" from "the
client explicitly wants to blank it" requires either JSON presence
detection against the raw request body (parse into
`map[string]json.RawMessage` first, check key existence, then decode) or
restructuring the existing output types to use pointers throughout, and
Stage 1's whole point was to keep the first real write endpoint simple
enough to get right on the first attempt. `latitude`/`longitude` are the
one exception, and get this for free: `PlaceDescription.Latitude`/
`Longitude` are already `*float64` (nil when a place has no coordinates,
for output purposes), so Go's JSON decoding already distinguishes "key
absent" from "key present" for those two fields specifically, without any
extra code. If explicit field-clearing turns out to matter in practice,
that's the point to revisit this, once there's a real use case driving the
choice rather than a hypothetical one.

**Fields RootsMagic itself touches as a side effect, that this server
deliberately handles differently -- confirmed against real captured
diffs, not assumed:**

- **`fsID`/`anID`/`LatLongExact` (Place, only when `Name` changes)** are
  reset to `0` -- see `UpdatePlace`'s own doc comment in
  `internal/rmdb/writes.go` for the full account (why `0` is the correct
  "never looked up" sentinel, confirmed exhaustively against all 922
  places in `royal92.rmtree`; why clearing is more honest than leaving a
  stale match once the name has changed).
  - **A second real captured diff showed RootsMagic re-running its
    FamilySearch/Ancestry lookup on *any* field edit -- a `Note`-only
    change, not just `Name`.** Reasonable on RootsMagic's side (a fresh
    match adds value regardless of which field triggered the save). This
    server deliberately does *not* replicate that broader trigger: the
    justification for clearing these fields is specifically that a stale
    match against the *old name* is misleading once the name changes --
    that reasoning doesn't apply when the name hasn't changed at all, so
    `UpdatePlace` only touches `fsID`/`anID`/`LatLongExact` inside the
    `Name != nil` branch, never on a `Note`-only or coordinates-only
    update.
  - **`fsID` can be negative** (a real captured value:
    `fsID=-1184254214`) -- worth remembering if this area is touched
    again, since it's an easy thing to get wrong in a regex or validation
    check (and once was -- see the git history around
    `cmd/server/main_test.go`'s `familySearchIDRegex`).
- **`IsPrivate` (Source, unconditionally on every update)** is set to `0`.
  The data dictionary documents this field as "not implemented," noting
  only `0` has ever been observed. A real captured diff for this project
  showed it flipping to `1` during a name-only edit, which contradicts
  that documentation and doesn't have an obvious causal explanation --
  quite possibly an artifact of that specific RootsMagic edit session
  rather than deterministic behavior tied to the edit itself. Given a
  field documented as unimplemented, with one observation that
  contradicts the only documentation available, writing the
  well-evidenced, only-ever-observed value is the more defensible choice
  than reproducing an unexplained one-off.
- **Verifying these specific fields (plus `LatLongExact` -- see below)
  doesn't go through the `sqldiff` golden-file comparison at all**,
  unlike every other field Stage 1 writes. `sqldiff` (like any
  before/after diff) only reports columns whose value actually
  *changed* -- and every place/source in `royal92.rmtree` already has
  `fsID`/`anID`/`IsPrivate` at the same value (`0`) this server always
  writes, so a same-value write is invisible to a diff regardless of
  whether this server did anything right. `LatLongExact` is excluded for
  a related but distinct reason (see below): this server's value for it
  genuinely does vary by context, but RootsMagic's own real value is
  itself non-deterministic, so there's no single correct captured value
  to compare against. `cmd/server/main_test.go`'s golden `.sql` files
  strip all four fields out of the comparison entirely (regex match
  replaced with an empty string, not masked with a placeholder), and
  `fieldCheck`/`verifyFields` query the resulting database directly
  instead, asserting each one's actual expected value -- independent of
  whatever the "before" state happened to be, or what RootsMagic itself
  happened to produce in one particular capture.

### `ConfigTable.DataRec` -- deliberately never written

A real captured diff, for a plain "add a comment to a Source" edit,
showed RootsMagic rewriting the whole of `ConfigTable.DataRec` -- a
~15KB, undocumented XML blob (see "Multiple databases / Collections"
above for the two other things this same blob holds: `UniqueID` and
`RootPerson`). This server does not, and will not, write to it.

Investigated before deciding that, not assumed: decoded the blob from
both the golden file's captured "after" state and a completely unrelated
reference copy of `royal92.rmtree`, and diffed them byte-for-byte --
identical, all 15,192 bytes. That's the key piece of evidence: it means
the one tag that had actually changed in the golden file's own
before/after (`<MediaCollapsed_Citations>true</MediaCollapsed_Citations>`
to `...false...`) matched whatever an entirely separate, previously
untouched copy of the same file happened to already have, for reasons
having nothing to do with the edit that was captured. Combined with what
the tag name plainly suggests, and independent confirmation that
RootsMagic's citation-editing UI does have a collapsible "Media list
panel" (checked directly against RootsMagic's own published
documentation, not just inferred) -- this is UI window/panel state, not
genealogical data, and not a deterministic consequence of the specific
edit that triggered its capture.

Three separate reasons this stays out of scope, not just one:

- **It's not data this server has any business asserting.** rmgedcomx is
  a headless server with no UI of its own -- there's no panel to be
  collapsed or expanded on its behalf, so there's nothing true to write
  here even in principle.
- **RootsMagic's own value for it isn't reliable evidence of anything**,
  the same conclusion already reached for `IsPrivate` and the
  `fsID`/`anID`/`LatLongExact` non-determinism (see above and TESTING.md's
  "Non-deterministic fields" section) -- just reached here by a different
  route (UI session state rather than a network race).
- **The mechanism would be categorically riskier than anything else this
  server writes.** Every other write is a single, well-understood column.
  Touching `DataRec` at all would mean parsing an entire undocumented XML
  document, mutating one element among 160+ others never individually
  investigated, and re-serializing the whole thing byte-for-byte
  correctly -- real risk of corrupting settings this project doesn't
  understand, in service of a value that (per the two points above)
  there's no reason to get right in the first place.

`cmd/server/main_test.go`'s `configTableDataRecRegex` strips this column
from *both* sides of the golden-file comparison, not just this server's
actual output (unlike `fsID`/`anID`/`LatLongExact`/`IsPrivate`, which rely
on whoever captured the golden file having removed them by hand) --
deliberately more defensive, since a multi-kilobyte hex blob is a much
easier thing to leave only partially cleaned up in a future capture than
a short numeric field.

Every write is wrapped in an explicit SQL transaction
(`internal/rmdb/writes.go`), even though a single `UPDATE` statement is
already atomic on its own without one -- introducing the pattern now,
against the simplest possible case, means it's already proven correct by
the time a later stage genuinely needs it (a multi-table `Person` write
touching both `PersonTable` and `NameTable`, for instance, where a partial
failure partway through would be a real problem without one).

### Stage 2 -- Artifact location updates (done)

GEDAM's actual requirements, clarified during Stage 1: updating a digital
asset's stored path, and creating/editing/deleting links between media and
the person/fact/event it documents -- notably, **not** creating new
`Person`/`Relationship`/`Event` records, and not a general "source record"
concept (GEDAM handles that independently of RootsMagic's own data model).
That meaningfully narrowed what full write support actually needs to
cover: `Artifacts` UPDATE (this stage) and `MediaLinkTable` CRUD (next)
are the resources with a real, driving use case behind them, not
`Person`/`Relationship` CREATE, which may end up out of scope entirely
rather than merely a later stage.

**`POST /collections/{id}/artifacts/{id}`** updates a multimedia item's
stored location. The request body's `mediaPath` (a new, write-only,
non-spec field on `SourceDescription` -- see its own doc comment in
`internal/gedcomx/model.go`) is a real, absolute filesystem path, exactly
as it exists on disk; the client never constructs RootsMagic's own path
syntax itself. This server encodes it into RootsMagic's `?`-relative
notation (`internal/rmdb/encodemediapath.go`), the same way `UpdatePlace`
computes `Reverse` rather than expecting a client to.

#### Why the Media Folder has to come from RootsMagic itself, not a flag

`?` is the only one of RootsMagic's three path symbols (`*`/`~`/`?`) this
server will ever *write* -- reasoned through directly, not just by
elimination:

- An absolute path is only meaningful on whichever machine typed it. Once
  the client sending a write request is potentially a different machine
  from the one running this server (which is potentially yet another
  machine from wherever RootsMagic itself runs), an absolute path stops
  meaning anything portable the moment it's written down.
- `*` (relative to the database's own directory) and `~` (relative to a
  home directory) both depend on machine-specific context a remote client
  has no way to see or reconstruct -- it doesn't know, and structurally
  can't know, where the database file lives on disk or whose home
  directory is relevant.
- `?` is different in kind, not just in degree: it isn't relative to a
  filesystem location at all, it's relative to a named, centrally
  configured setting. That's the one piece of context that can be resolved
  on the server side alone, without the client needing to know anything
  about the server's filesystem.

But resolving `?` requires actually knowing the Media Folder's value, and
the only place that value exists is `RootsMagicUser.xml` -- not the
database, not something a client can reliably supply (see "Multimedia"
above for where and how this was confirmed: `%APPDATA%\RootsMagic\Version
N\RootsMagicUser.xml`, `<Folders><Media>`). For *reading*, a wrong
`-media-folder` just means this server fails to find a file -- a
contained, visible failure. For *writing*, a wrong assumption means
writing a `?`-relative path that resolves correctly for this server but
doesn't match what RootsMagic itself believes the Media Folder is --
silently corrupting the link from RootsMagic's own point of view, not
surfacing until someone opens the file in RootsMagic later and finds it
broken. That asymmetry is why this isn't treated as a flag-level detail:

- **`-write` and `-media-folder` are mutually exclusive.** Passing both is
  refused at startup with a clear error, not silently resolved by picking
  one -- supplying both suggests confusion about which is actually in
  effect, not a deliberate override.
- **`-write` reads the Media Folder itself**, straight from
  `RootsMagicUser.xml` (`cmd/server/mediafolder_discovery.go`,
  `discoverMediaFolder`), and refuses to start if it can't. Two real
  locations are supported:
  - **Windows**: `%APPDATA%\RootsMagic\Version N\RootsMagicUser.xml` --
    confirmed directly against a real installation.
  - **macOS**: `~/RootsMagic/Version N\RootsMagicUser.xml` -- based on
    two community reports (RootsMagic's own forum: ["How to Change the
    Location of RootsMagic settings folder on
    Mac"](https://community.rootsmagic.com/t/how-to-change-the-location-of-rootsmagic-settings-folder-on-mac/15774),
    and a support thread with a staff reply giving the exact path,
    ["RM10 on Mac keeps closing
    suddenly"](https://community.rootsmagic.com/t/rm10-on-mac-keeps-closing-suddenly/12794)).
    Treat this with a bit more caution than the Windows location: it's a
    community report, including one direct quote from RootsMagic's own
    support staff, not something confirmed against a real Mac installation
    the way the Windows path was.
  - Anywhere else, `-write` refuses to start with a clear error explaining
    why -- not a silent fallback to some guessed behavior.
- **Multiple RootsMagic versions**: `RootsMagicUser.xml` lives under a
  per-version folder (`Version 9`/`Version 10` in the confirmed examples),
  so someone who's used more than one RootsMagic version could have more
  than one, on either platform. The highest version number found is used
  -- RootsMagic's schema migrations are understood to be one-directional,
  so the highest version installed is presumed to be the one actually in
  current use. If the found configurations' Media Folder values disagree
  with each other, that's logged in detail (which versions, which values)
  but isn't fatal; the highest version's value is used regardless. This
  logic (`discoverMediaFolderIn`) is identical across Windows and macOS --
  only the base directory differs between them, everything about
  interpreting what's inside it is shared.
- **`-bypass-os-check`** is a hidden flag -- deliberately not registered
  via the `flag` package at all (see `extractBypassOSCheckFlag` in
  `main.go`), so it never appears in `-h`/`--help` output. It forces
  `discoverMediaFolder` to use the macOS-style discovery path regardless
  of the actual platform. This is meaningful, not a no-op stand-in,
  because `os.UserHomeDir()` returns a real, usable directory on any
  platform -- so this is the genuine macOS convention, pointed at
  whatever the current platform's actual home directory is, not a
  simulated one. It exists specifically so write mode's Media Folder
  discovery -- including the version-conflict handling above -- can be
  exercised for real, end to end, from a development environment that's
  neither Windows nor macOS, rather than only being testable by
  constructing the `api`/`rmdb` layers directly and skipping
  `discoverMediaFolder` entirely (which is what an earlier version of
  this verification had to resort to). Confirmed working exactly this
  way: two fake `RootsMagicUser.xml` files under `~/RootsMagic/Version
  9/` and `~/RootsMagic/Version 10/` on a Linux machine, with
  deliberately different Media Folder values, correctly produced the
  version-conflict warning and picked Version 10's value; a subsequent
  real `POST /artifacts/{id}` request, using that genuinely-discovered
  folder, correctly updated a real database row. `-write` + `-media-folder`
  together is still refused even with `-bypass-os-check` present -- the
  bypass changes *how* the Media Folder is discovered, not whether a
  manually supplied one can substitute for discovery. This is a
  development/testing aid, not a supported way to run write mode in
  production on an unsupported platform, and nothing else about write
  mode is affected by it (the `RootsMagic.exe` check and the backup
  mechanism are both untouched).

#### `encodeMediaPath`: the reverse of reading, and why it's not `path/filepath`

`ResolveMediaPath` (see "Multimedia" above) turns RootsMagic's `?`
notation into a real path, for reads. `encodeMediaPath`
(`internal/rmdb/encodemediapath.go`) does the reverse for writes: given a
real path and the Media Folder, compute the `?`-relative `MediaPath` and
`MediaFile` RootsMagic itself would produce. Deliberately implemented with
explicit backslash normalization and manual string manipulation rather
than the standard `path/filepath` package -- the same reasoning already
applied to `ResolveMediaPath` and `collectionid.fileStem`: `path/filepath`
behaves according to the *build* platform, not a chosen one, but this
needs Windows path semantics specifically (backslashes, case-insensitive
comparison) regardless of what platform this code happens to be compiled
on. It's also what makes this function fully unit-testable on Linux
(`encodemediapath_test.go`), including the real path pattern confirmed
earlier against `royal92.rmtree` (`*\royal92\marriage-of-queen-victoria.jpg`)
as one of the test cases, not just invented examples.

A real path that isn't actually under the Media Folder is rejected
(`ErrPathNotUnderMediaFolder`, surfaced as `400`) rather than written
anyway as an absolute path -- writing anything else would break the one
guarantee this whole mechanism exists to provide. The prefix check
requires a genuine path-separator boundary, not just a string prefix match
(`C:\tmp2\...` correctly does NOT match a Media Folder of `C:\tmp`) --
confirmed with a dedicated test case, since this is exactly the kind of
boundary bug that's easy to get subtly wrong.

#### Verification, and what's still genuinely unverified

Full, real, end-to-end verification (server startup through the HTTP
request, using the real `-write ./rmgedcomx` binary, not a constructed
test harness) is now possible on Linux, via `-bypass-os-check` -- this
closes a gap from an earlier version of this section, which could only
exercise the `api`/`rmdb` layers directly with a manually supplied Media
Folder, skipping `discoverMediaFolder` (and its version-conflict handling)
entirely. Confirmed this way, against real `royal92.rmtree` data and two
deliberately-conflicting fake `RootsMagicUser.xml` files: the
version-conflict warning fires correctly and picks the higher version's
value; `M1`'s real `*\royal92\marriage-of-queen-victoria.jpg` correctly
became `?\Weddings\victoria-albert.jpg` after a real HTTP request, using
the genuinely-discovered Media Folder end to end, not a hand-supplied one;
a path outside the Media Folder correctly `400`s with a clear explanation;
a nonexistent artifact `404`s; a missing `mediaPath` and a body/URL id
mismatch both `400` before any write is attempted; read-only mode still
`405`s the identical request; `-write` + `-media-folder` together is
still refused even with `-bypass-os-check` present; a missing
`~/RootsMagic` directory produces a clear error rather than a confusing
one; `-bypass-os-check` doesn't appear in `-h` output and is a silent
no-op without `-write`.

What's still genuinely unverified, and can't be from here: the *real*
locations on a *real* Windows or macOS machine -- `%APPDATA%\RootsMagic\...`
was confirmed against a real Windows installation earlier in this
project, but `~/RootsMagic\...` on macOS is still only a community report
(see above), not independently confirmed. `-bypass-os-check` proves the
discovery *mechanism* is correct wherever it looks; it can't prove *where*
it should be looking on a platform this project doesn't have direct
access to. Same caveat as `rootsmagic_running_check.go`'s own doc comment
-- please confirm the macOS location specifically on a real Mac before
relying on it.

### Stage 2b -- `MediaLinkTable` CRUD for `Person` (done)

Deliberately split from the rest of Stage 2 into one entity at a time --
`Person`, `Event`, and `Relationship` all need this, but doing all three
at once would compound whatever issues came up in any one of them. This
covers `Person` only.

**`POST /collections/{id}/persons/{id}`** now accepts an updated `media`
array, diffed against `MediaLinkTable` rather than replaced wholesale --
entries newly present get a new row, entries newly absent get their row
removed, entries in both are left completely untouched, including
columns this server doesn't otherwise touch at all (see below). The
shared diffing logic (`rmdb.UpdateOwnerMedia`) is parameterized by owner
type/id specifically so `Event` and `Relationship` can reuse it directly
when their turn comes, rather than reimplementing it.

**The real `MediaLinkTable` schema turned out to have more in it than
the earlier planning discussion assumed** -- `IsPrimary`, `Include1-4`,
`RectLeft/Top/Right/Bottom`, `Comments`, on top of the core
`MediaID`/`OwnerType`/`OwnerID`. Checked against the data dictionary
before deciding anything: `Include2-4` and all four `Rect*` columns are
documented as "Not implemented," safe to leave at `0` unconditionally.
`IsPrimary` and `Include1` are real, though:

- `IsPrimary`: "Primary Photo checkbox... Determines image displayed in
  reports, the Pedigree view, and the People Side View pane." New links
  always get `0` -- this server has no basis to assert a newly-created
  link should be someone's primary photo, the same reasoning as `fsID`/
  `anID` in `UpdatePlace`: it's a real editorial choice, not something to
  claim on a user's behalf without evidence.
- `Include1`: "Include in Scrapbook." Also always `0` on new links. There
  is no GEDCOM X data type conceptually similar to RootsMagic's Scrapbook
  at all -- checked the conceptual model specifically looking for one --
  so this is **documented here as RootsMagic-only functionality this API
  doesn't expose**, not a gap to close later. A newly-linked artifact
  simply won't appear in the Scrapbook, or be treated as anyone's primary
  photo, until a person sets that manually in RootsMagic itself.

**Duplicate links**: `MediaLinkTable` has no uniqueness constraint on
`(MediaID, OwnerType, OwnerID)` -- nothing stops the same artifact being
linked to the same owner more than once (confirmed by deliberately
creating that state and testing against it, not just reasoning about
whether the schema allows it). Removing a media id removes *every*
matching row, not just one, so a removal can't leave an orphaned
duplicate behind.

**Scoped to `media` only, not general `Person` editing.** `names`/
`gender`/`facts`/`sources` aren't writable through this endpoint. A
request that includes any of them isn't rejected -- a client following
the ordinary GET-then-modify-then-POST pattern will naturally send
whatever it got back, unchanged, and refusing that over fields this
endpoint doesn't touch would just make the natural client pattern
unusable for the one thing it does support -- but it is logged
(`log.Printf` naming exactly which fields were present), specifically so
there's a visible trail of real demand if this needs expanding later,
rather than a silent gap nobody notices until someone asks why it doesn't
work.

**Doesn't bump the owner's own `UTCModDate`** (e.g. `PersonTable`'s), on
purpose, not by oversight: unlike a `Name`/`Note`/coordinate edit, there's
no real captured RootsMagic diff yet confirming whether attaching or
detaching media touches the owner row's own timestamp the way editing one
of its own fields does. Asserting that without evidence would be exactly
the kind of unverified claim this project has consistently avoided
elsewhere -- `ConfigTable`'s own `UTCModDate` is still bumped, since
that's confirmed to happen on every write regardless of what changed.

**Verified directly, not just unit-tested**: attaching `M1` (real data,
previously only linked to the marriage event) to a person correctly
creates a new row with `IsPrimary=0`/`Include1=0`, while the pre-existing
event link survives completely untouched; detaching correctly removes
only the intended row; a request naming a nonexistent artifact is
rejected atomically -- confirmed the *other*, valid artifact in the same
request does **not** get linked either, not just that the invalid one is
rejected; a nonexistent person 404s; a body/URL id mismatch 400s;
read-only mode still 405s the identical request; sending `names` alongside
`media` succeeds and produces the expected log line, not a rejection; an
actually-unknown field still 400s via `decodeStrictJSON`.

### Stage 2c -- `MediaLinkTable` CRUD for `Event` (done)

The small increment Stage 2b's own "What's next" anticipated: `POST
/collections/{id}/events/{id}`, same shape as `Person`'s, reusing
`rmdb.UpdateOwnerMedia` unchanged (`OwnerTypeEvent` instead of
`OwnerTypePerson` is the only difference at the data layer). Unsupported
fields for `Event` are `type`/`date`/`place`/`roles`/`sources`/`notes` --
logged, not rejected, same reasoning as `Person`.

Verified the same way, against real data: `M1` (already linked to Event
5049, the marriage, in `royal92.rmtree`) moved to a different,
previously-unlinked event via a real HTTP request; Event 5049's own
original link independently detached by a separate request and confirmed
untouched by the first one; re-sending an already-linked media id doesn't
create a duplicate row (confirmed by `LinkID` staying the same across
both calls, not just that the end state looked right); nonexistent
artifact/event, id mismatch, read-only mode, and the unknown-field
rejection all behave identically to `Person`'s. A dedicated
`rmdb`-level test (`TestUpdateOwnerMediaWorksForEventOwners`) exists
specifically to confirm the shared core isn't accidentally
`Person`-specific, not just that `Event`'s own HTTP layer happens to
work.

### GEDAM specification review

A working draft of GEDAM's own specification (the DAM client this project
exists to serve) was reviewed directly against rmgedcomx's actual current
behavior, not against what either side assumed about the other. Two
things came out of that review as real, concrete follow-on work (below);
a few other findings were spec-side issues (`sources`/`media`
terminology predating the split, `Fact`/`Event` naming) with nothing for
rmgedcomx to change, and a couple of GEDAM's stated "enhancement needed"
asks (the startup table's `UniqueID` column, `Collection.identifiers`)
turned out to already be shipped.

### Write availability now re-checked periodically, not just at startup

**The problem, precisely stated**: SQLite's own file locking already
prevents genuine data corruption from two processes writing at once --
that was never the actual risk. The real risk is RootsMagic itself
receiving a `SQLITE_BUSY` error in a code path it was never written to
expect, with unknown consequences -- and the original `-write` design
(`checkRootsMagicNotRunning`, checked exactly once, in `main()`, before
this server starts accepting any requests) can't protect against
RootsMagic being opened *after* this server already started in write
mode. That gap matters specifically because GEDAM (see above) is a
long-running background service, potentially running for days at a
stretch, not a short CLI invocation -- a startup-only check has no way
to ever notice RootsMagic showing up partway through that lifetime.

**The fix**: every write handler now goes through `requireWriteAllowed`
(`internal/api/server.go`), consulting a new `WriteGuard` interface on
top of (not instead of) the existing `db.ReadOnly()` gate. The concrete
implementation (`cmd/server/writeguard.go`) re-checks whether
RootsMagic is running, rate-limited to once per 10 seconds and only
triggered by an actual write attempt (not a background timer) -- by
explicit design: writes from GEDAM are expected to be infrequent but
occasionally bulk (e.g. relocating every artifact's path at once), and
10 seconds is judged fast enough to catch RootsMagic before it's itself
ready to attempt a write, without shelling out to `tasklist` on every
single request regardless of whether anything is actually happening.

**Once tripped, it latches permanently** -- every write for the rest of
this server process's life gets `405`, even after RootsMagic later
closes again, requiring a restart to resume. Deliberately the simpler of
two reasonable designs (the alternative, re-checking and auto-recovering
once RootsMagic closes, was explicitly set aside for now): a person
should never be left wondering whether a write might silently start
working again on its own while they're still unsure what happened.
`isRootsMagicRunning` (`cmd/server/rootsmagic_running_check.go`) was
factored out as the shared detection primitive behind both the original
startup check and this new one, so each can build its own contextually
correct message rather than sharing text written for only one of the two
situations.

The tripped response reuses the exact shape a genuinely read-only
server already returns for the identical request -- `405`, `Allow: GET,
HEAD`, the same RFC 7807 error body -- deliberately, so "this server
started read-only" and "this server was writable but RootsMagic showed
up" look identical from a client's point of view and need no second
error-handling path. A real, if easy-to-miss, bug was caught before this
shipped: passing a nil `*writeGuard` directly into `Config.WriteGuard`
(an interface field) does **not** produce a nil interface in Go -- a
classic trap, confirmed by testing it directly rather than trusting the
reasoning. `cmd/server/main.go` only assigns the field when the concrete
guard is genuinely non-nil, keeping `Config.WriteGuard` a true nil
interface (and therefore correctly bypassed by `requireWriteAllowed`) in
read-only mode. One shared guard instance is constructed once and passed
into every collection's `Config`, not one per collection -- RootsMagic
running is a whole-machine condition, so every collection needs to see
the same tripped state at the same moment, not learn about it on its own
independently-timed schedule.

Verified with a fake `WriteGuard` standing in for the real
process-checking logic (which can't itself be exercised outside a real
Windows machine): confirmed a nil guard doesn't panic, an allowing guard
lets a write through normally, and a tripped guard returns exactly the
expected `405`/`Allow`/error-body shape. The rate-limiting and latching
state machine itself (`cmd/server/writeguard_test.go`) is unit-tested
with an injectable check function, deterministically -- confirmed
directly, not just reasoned about: three rapid calls trigger exactly one
underlying check; waiting past the interval triggers a second; a tripped
guard stays tripped and never re-checks again even after the interval
passes and even though the underlying condition is still "found running"
each time; a failure in the check itself fails open rather than blocking
writes over an unrelated problem.

### Reverse lookup: what references a given artifact

GEDAM's own role-resolution algorithm (for computing its `Family`/
`Individual` folder views) needs to answer "which people, relationships,
events, and places reference this specific artifact" -- and there was no
way to answer that efficiently. `buildSourcesAndMedia` only ever
traverses forward (a given owner -> its sources/media); nothing let a
client start from an artifact and find its owners without enumerating
every `Person`/`Relationship`/`Event` in a collection and checking each
one's own `media` array by hand, which doesn't scale past a small
sample file.

Three new, non-spec extension endpoints close that gap: `GET
/artifacts/{id}/subjects` (every `Person`, `Relationship`, `Event`, and
`PlaceDescription` referencing this artifact), and `/persons`/`/events`/
`/relationships` (the same lookup, filtered to one type). Response shape
is a lightweight reference list (`gedcomx.SubjectReference`/
`SubjectReferencesDocument`), not embedded full resources -- deliberately,
matching GEDAM's own stated pattern of resolving each distinct context
independently: a caller that needs full details fetches them separately
via each reference's `href`. `resourceType` reuses the existing
`ResourceType*` URI constants already defined for `CollectionContent`,
rather than inventing a second vocabulary for the same four data types.

The underlying traversal (`rmdb.OwnersOfMedia`) is the reverse of
`buildSourcesAndMedia`'s own two-hop walk, structurally: direct
`MediaLinkTable` rows naming the artifact, plus (since a real file's
media is more often attached to a *citation* than directly to what it
documents -- see "Multimedia" above) a second hop through
`CitationLinkTable` for any citation the artifact is attached to. Two
owner types get special handling before a result is ever returned, not
passed through as-is:

- **`OwnerTypeName`** isn't a Subject with its own resource in this API
  (a name is a sub-part of a `Person`) -- resolved up to its owning
  `Person` via `NameTable.OwnerID`, confirmed against real data to
  actually hold the owning `PersonID` despite the generic column name.
  An orphaned name reference is skipped, not treated as a request
  failure.
- **`OwnerTypeSource`** (media attached directly to a bibliographic
  source record) is dropped outright -- not a `Subject` type this API
  exposes a `media` field for at all.

Verified thoroughly against real and deliberately-constructed data, not
simulated: `M1`'s existing real link to `royal92.rmtree`'s marriage
event, confirmed correctly returned before touching anything; attached
to a person via the real `POST /persons/{id}` write path, confirmed both
old and new links appear together and each type-filtered endpoint
correctly narrows the result; a nonexistent artifact 404s. The
citation/name-resolution path needed real care: an initial test using
one of `royal92.rmtree`'s two real citations produced thousands of
results in the raw response, which turned out to be correct, not a
bug -- that citation turned out to be a widely-shared "base import"
citation referenced by 11,698 separate rows, confirmed by direct count
rather than assumed. A
clean, deliberately small, synthetic scenario (one citation, cited by
exactly one name) gave an unambiguous, readable confirmation instead.
`internal/rmdb/reverselookup_test.go` covers the direct-link, via-citation
plus Name resolution, direct-Family-link, Source-exclusion, and
deduplication (the same Subject reached via two separate citation paths
must appear exactly once) cases directly against real data, independent
of the HTTP layer.

### Stage 2d -- `MediaLinkTable` CRUD for `Relationship` (done)

The gap GEDAM's own specification explicitly flagged (§9.4, §14 of its
v0.10 draft): `Person` and `Event` media linking existed; `Relationship`
(a couple with no specific fact attached) didn't, leaving §7.6's
`Relationship` reference case readable via `/artifacts/{id}/relationships`
but not creatable. Deferred back at Stage 2b for lack of a confirmed
need -- GEDAM's spec now confirms the need directly, so this closes that
gap.

**`POST /relationships/{id}`**, reusing `rmdb.UpdateOwnerMedia` unchanged
(`OwnerTypeFamily` instead of `OwnerTypePerson`/`OwnerTypeEvent` is the
only difference at the data layer -- confirmed by a dedicated test,
`TestUpdateOwnerMediaWorksForFamilyOwners`, the same pattern already used
to confirm this for `Event`). Unsupported fields: `facts`/`sources` --
logged, not rejected, same reasoning as `Person`/`Event`.

**One restriction specific to `Relationship`, with no equivalent in
`Person`/`Event`: only the "couple" relationship kind is writable, never
a parent-child relationship.** A relationship id like `F1-FC2` (a
specific parent-child pair) is rejected with a `400`, not silently
redirected to the family it belongs to. This isn't an arbitrary
restriction -- RootsMagic's own schema has no identity for "this specific
parent-child pair" to attach anything to at all; `MediaLinkTable`'s
`OwnerType=Family` is scoped to the family as a whole, the same identity
the "couple" relationship already represents. Silently redirecting a
parent-child request to the family would mean attaching media to a
*different* relationship than the one actually named in the URL.

A related, correctly-handled edge case: a family with only one recorded
partner (`FatherID` or `MotherID` is `0` -- `royal92.rmtree` has several,
e.g. `F70`, and the "Romanov - UNK" example in GEDAM's own §7.1) has no
valid "couple" relationship for this endpoint to target either, and
correctly `404`s -- the same existence check `GET /relationships/{id}`
already applies for reading, reused rather than reimplemented for
writing.

**`Type`/`Person1`/`Person2` are deliberately excluded from the
ignored-fields log**, unlike every other field on every other write
handler's equivalent check. Checked directly against
`buildCoupleRelationship`: all three are always populated on every real
`Relationship` this server returns (`Person1`/`Person2` are required by
the RS spec itself, not optional), so an ordinary GET-then-modify-then-
POST client will always send back non-empty values for them regardless
of actual intent to change anything. Logging their presence would be
noise on every single request, not a signal of anything -- unlike
`facts`/`sources`, which are genuinely optional and only present when
there's real data behind them.

Verified against real data: `F1` (Victoria and Albert's actual marriage
relationship in `royal92.rmtree`, discovered incidentally while testing
-- confirmed by its `facts`/`sources` already containing `E5049` and the
citations already documented elsewhere in this file) accepted a real
media attachment, confirmed via a follow-up `GET` that it actually took,
and confirmed via `/artifacts/{id}/relationships` that the reverse-lookup
correctly reflects a link created through the real write path, not just
raw SQL. `F1-FC2` (parent-child) rejected with `400`; `F70`
(single-partner) and a nonexistent family both `404`; a nonexistent
artifact `400`s; read-only mode still `405`s the identical request;
`facts` alongside `media` succeeds and logs as expected; an actually-
unknown field still `400`s. `TestRelationshipMediaWrite`
(`cmd/server/main_test.go`) makes this permanent -- deliberately a
separate test function from `TestWriteOperations`' own table, since that
table is specifically for sqldiff-golden-file comparisons against a real
RootsMagic capture, and media-linking touches no field a golden file
would show at all (see `rmdb.UpdateOwnerMedia`'s own tests for that
layer). Unlike `Person`/`Event`, whose own handler-level branching was
only ever verified manually and not left with a permanent test, this
one's non-trivial branching (couple vs. parent-child, both-partners-
present) earned a permanent regression test on its own merits.

## Stage 3 -- creating People and Relationships (done)

The natural next step after Stage 2's media-linking work, driven by a
concrete need this time rather than the original generic "eventually
cover CRUD" plan (see the "What's next" notes throughout Stage 2): GEDAM
wants to be able to add new people and relationships, not just link
media to existing ones.

Built from a real, systematically captured reference: a whole family (the
Brontës, from a public-domain GEDCOM file) entered into an initially
empty RootsMagic 8+ database one step at a time, with `sqldiff` output
captured and reviewed at every step -- the same methodology as every
other write feature in this project, just at a larger scale (15 golden
files covering person creation, alias addition, marriage/family creation,
and child linking).

### Three encoding schemes, all independently verified against real data before being trusted

Creating a `Person` touches several fields RootsMagic computes or encodes
in ways not otherwise documented -- each of the three below was checked
against real captured values before being implemented, not implemented
first and hoped to be correct:

**`PersonTable.UniqueID`** -- a standard 128-bit version 4 UUID (32 hex
characters, hyphens stripped) followed by a 4-character checksum. The
checksum is a Fletcher-16-style running sum over the 16 raw UUID bytes
(`sum1 += byte; sum2 += sum1`, both mod 256, formatted as `%02X%02X`)--
not derived from the GEDCOM 7 UID algorithm's own published description
(which has a confirmed transcription error in its algebraic description
of the second byte), but transcribed directly from a community member's
own SQL implementation
(https://sqlitetoolsforrootsmagic.com/forum/topic/uniqueid-in-persontable/#postid-1800)
and independently confirmed against 21 real `UniqueID` values pulled
from real RootsMagic-generated data (7 from the first captured golden
files, 14 more from a full `PersonTable` snapshot of the finished Brontë
database) -- every one checksums correctly. See
`internal/rmdb/generateuniqueid.go` and its tests.

**`EventTable.SortDate` / `NameTable.SortDate`** -- a 64-bit integer
encoding, bit-packed rather than a simple day-count, documented at
https://sqlitetoolsforrootsmagic.com/dates-sortdate-algorithm/:
`SortDate = 2^49*(Y+10000) + 2^45*M + 2^39*D + 17178820620`. Confirmed
against 18 real (year, month, day, SortDate) values captured from the
Brontë database, covering full dates, month-only dates, year-only
dates, and a date carrying RootsMagic's "About" qualifier (confirmed
separately that the qualifier does *not* change this value -- it only
affects the separate `Date` string, below). `9223372036854775807`
(max int64) is RootsMagic's own sentinel for "no date to sort by" --
confirmed directly: every `NameTable` row for a name with no date
entered uses exactly this value, with no exceptions. See
`internal/rmdb/sortdate.go` and its tests.

**`EventTable.Date` / `NameTable.Date`** (the encoded date *string*,
distinct from `SortDate`) -- this project already had a fully confirmed,
tested decoder for this format from earlier work
(`internal/gedcomx/rmdate.go`'s `ParseRMDate`, confirmed against a
purpose-built RootsMagic test database exercising every modifier; see
its own comment and this file's "Dates" section elsewhere for the full
grammar). `EncodeRMDate` is its inverse, added for this stage --
deliberately narrower in scope than `ParseRMDate`'s own read-side
coverage, for two confirmed reasons rather than caution alone:

- Only a plain date and an "About"-qualified date can be *encoded* at
  all, even though `ParseRMDate` can *decode* five different qualifiers
  (`About`/`Calculated`/`Estimated`/`Circa`/`Say`). This isn't an
  arbitrary limitation -- checked directly against `ParseRMDate`'s own
  mapping that only the plain and "About" cases ever produce a GEDCOM X
  `Formal` value in the first place; the other four qualifiers have no
  formal-date representation to encode *from* in GEDCOM X's own Date
  Format specification, so there's nothing for `EncodeRMDate` to invert
  for them even in principle.
- Before/after/range dates aren't supported for encoding, and this
  surfaced a genuine, confirmed ambiguity worth recording: GEDCOM X's
  formal date grammar represents both "Between X and Y" (RootsMagic's
  `R` directional code) and "From X to Y" (`S`) with the *identical*
  string shape, `+YYYY-MM-DD/+YYYY-MM-DD` -- confirmed directly via
  `ParseRMDate`'s own test cases, which produce that same formal string
  for both. A formal date in that shape genuinely cannot be mapped back
  to one specific RootsMagic directional code without guessing which one
  was meant, so `EncodeRMDate` declines rather than picks one arbitrarily.

Every one of these limits returns a clear error (surfaced as a `400` at
the API layer once the create handler is built) rather than a best
guess -- deliberate, not merely cautious: a wrong *read* is a cosmetic
display problem, but a wrong *write* creates a real, permanent,
incorrect record in someone's family tree. `internal/gedcomx/
rmdate_encode_test.go` round-trips every real `Date` string captured for
this stage (`ParseRMDate` -> `Formal` -> `EncodeRMDate` -> compare
against the original) rather than asserting hand-written expected
output, and separately confirms the before/after/range/malformed cases
are rejected.

### Two RootsMagic quirks found in the reference capture, confirmed and deliberately not replicated

- **`FamilyTable.ChildID`** turned out to be pure UI state, not data --
  confirmed directly from the data dictionary: *"PersonID of Child last
  active as the root person in Pedigree view."* A real capture showed it
  retroactively set on a family unrelated to the operation that
  triggered the capture, evidently because a person from that family had
  been viewed in Pedigree view earlier in the same RootsMagic session.
  The same category of finding as `MediaCollapsed_Citations` several
  stages back: this server has no Pedigree view to have a root person
  for, so there's nothing true to write here. Left at its default (`0`)
  always.
- **`UTCModDate` during marriage/child creation is inconsistent in a way
  confirmed not worth replicating.** Adding a spouse bumps `UTCModDate`
  on only one of the two `PersonTable` rows (whichever partner the
  operation was performed *from*, confirmed directly against raw,
  unredacted values in the actual snapshot databases -- not just the
  golden files, which have this field redacted); adding a child bumps
  neither parent's nor the child's own `UTCModDate` at all. The value
  that *is* written during a marriage is also truncated to midnight
  (confirmed directly: `46246.2471460764` before, exactly `46247.0`
  after) rather than carrying the full-precision timestamp used
  everywhere else, including by RootsMagic itself elsewhere in the very
  same transaction. Decided (see conversation) not to replicate either
  behavior: `UTCModDate` is set on exactly the row(s) each create/update
  handler directly writes, always full-precision, never cascaded to
  unrelated records -- consistent with how every UPDATE handler already
  built in this project behaves, and how this project has already
  chosen not to replicate other confirmed-but-unhelpful RootsMagic
  quirks elsewhere (`fsID`/`anID`/`LatLongExact`, `IsPrivate`).

### API design decisions, per the RS spec directly rather than invented

- **`Person` creation**: `POST /persons`, per RS spec Section 4.9.2 --
  `201` + `Location` header if exactly one person was created, `204` if
  a request created several at once.
- **Child-linking**: `POST /relationships` with `type=http://gedcomx.org/ParentChild`,
  matching how this server's *read* side already models a parent-child
  link as its own `Relationship` kind (see the "Relationships" section
  above) -- not a new, separate mechanism.

### `POST /persons` -- the create handler itself (done)

Built on top of the encoding layer above. `internal/rmdb.CreatePerson`
does the actual multi-table transaction (`PlaceTable` resolution/creation,
`EventTable` per fact, one `NameTable` row per name, `PersonTable`,
`ConfigTable`'s `UTCModDate` bump); `internal/api`'s
`handleCreatePersons` and its `buildNewPerson`/`buildNewPersonName`/
`buildNewPersonFact` helpers translate an incoming GEDCOM X request into
that call, resolving type URIs via the new reverse lookups added for
this (`gedcomx.GedcomTagForFactType`, `GenderCode`, `NameTypeCode` --
each the inverse of an existing read-side mapping, not a fresh guess).

**`SurnameMP`/`GivenMP`/`NicknameMP`** turned out not to need a new
dependency: `golang.org/x/text` (the usual way to do this in Go) isn't
reachable from this sandbox's network allowlist, so the mapping table
was generated once, locally, using Python's `unicodedata` (real NFD
decomposition, not a hand-picked subset) and embedded as static Go data
(`internal/rmdb/accentfold_table.go`, 488 entries covering the Latin-1
Supplement, Latin Extended-A/B, and Latin Extended Additional Unicode
blocks). Checked against the one real confirmed example first
(`Brontë` -> `Bronte`) before trusting the broader table. Deliberately
excludes ligatures/special letters (`ø`, `æ`, `œ`, `ß`) -- NFD
decomposition doesn't touch these at all, since they aren't a base
letter plus a combining accent mark to begin with, and this project has
no confirmed data point for what RootsMagic does with them.

**ID assignment, place deduplication, and the primary-name-only
`BirthYear`/`DeathYear` behavior** are all confirmed directly against
the real capture, field-by-field: `CreatePerson`'s output for recreating
Patrick Brontë from an empty database matches the real golden file
exactly, on every field this server controls (the only two that
necessarily differ are `UTCModDate`, a timestamp, and `UniqueID`,
randomly generated per person). Place deduplication confirmed
separately across two independent `CreatePerson` calls sharing a place
name. See `internal/rmdb/createperson_test.go`.

**Scoped narrower than the full GEDCOM X conceptual model, by design**:
only built-in GEDCOM X fact types with a confirmed RootsMagic
`FactTypeTable` mapping can be created; a custom fact-type URI is
rejected rather than either matched fuzzily or used to silently create a
new `FactTypeTable` row (a materially bigger, unverified feature). A
`Fact.place` must carry `original` text; resolving a place from a bare
`resource` reference isn't supported. A `NameForm` with neither `parts`
nor `fullText` (see below for the case where `fullText` alone is
present) is rejected -- there's no real-world case motivating support
for a name that says nothing at all about the person's name. Every
rejection in this list returns a clear `400` naming what was wrong, not
a best guess -- consistent with this whole stage's own principle (see
above): a wrong write is a permanent, real mistake in someone's family
tree, not a cosmetic display issue.

### A real spec-compliance gap, found via a real user report: `fullText`-only names

Originally, a `NameForm` was required to carry structured `parts`
(explicit `Given`/`Surname`/...) -- a bare `fullText` was rejected
outright, reasoning that splitting free text into surname/given is
inherently ambiguous, the same reasoning `EncodeRMDate` applies to
ambiguous dates. That reasoning wasn't wrong on its own terms, but it
produced a real bug: a user reported a `400` for a request built from
real GEDCOM data (their own `royal92.ged` test file, imported by
RootsMagic itself) that turned out to be fully spec-compliant. Checked
directly against the GEDCOM X conceptual model spec (Section 3.19, not
assumed or half-remembered) before accepting that: `fullText` and
`parts` are *independently* `OPTIONAL` on `NameForm` -- a `fullText`-only
name is a completely valid representation the spec explicitly
anticipates, not an edge case this server had any business rejecting.

The fix isn't "attempt the ambiguous split after all." It's a
deterministic fallback with real evidence behind it, not a guess:
when `parts` is absent, the whole `fullText` is stored in
`NameTable.Given`, with `NameTable.Surname` left empty -- confirmed to
be RootsMagic's own actual behavior, not invented for this server.
GEDCOM 5.x's own convention encloses a surname in slashes
(`"Given /Surname/"`); when that slash-delimited portion is empty (a
real line from the user's own `royal92.ged`: `"1 NAME Albert Augustus
Charles//"`), RootsMagic stores the whole preceding text in `Given` and
leaves `Surname` empty -- checked directly against the real
`royal92.rmtree` database this file produces (`NameID 2:
Given="Albert Augustus Charles", Surname=""`), and confirmed
consistently across several more of the same pattern in the same file
(`Victoria Adelaide Mary`, `Alice Maud Mary`, ...), not just the one
example. This server doesn't parse GEDCOM 5.x or its slash convention at
all -- it's GEDCOM X JSON in, RootsMagic schema out -- but the
*resulting* rule (`fullText`-only -> whole thing in `Given`, empty
`Surname`) is the same rule RootsMagic itself already applies to exactly
this situation, adopted here specifically because it's evidence-backed
rather than because it happens to be convenient.

This is a genuinely different kind of decision than the
before/after/range date rejection nearby, worth being precise about why
both stand: those were a real coin flip between two specific, equally
plausible interpretations, with real risk of silently storing the wrong
one and no way for anyone to notice. This has a single, deterministic,
externally-verified answer -- there was never a choice to guess wrong.

Verified with a real HTTP round trip reproducing both the user's exact
original request (`"Victoria  Hanover"`, double space preserved exactly
as sent) and the `royal92.ged` `Albert Augustus Charles` case, confirming
the stored `NameTable` values directly, not just the response status.
`cmd/server/main_test.go`'s `TestCreatePersonsHTTP` gained a permanent
test for the fallback -- replacing an existing test that had specifically
asserted the old, now-incorrect rejection behavior, not left alongside
it. (The "neither `parts` nor `fullText`" case mentioned in that test's
original description turned out to need the same treatment -- see below,
this wasn't the end of this particular story.)

### The same gap, one layer further in: a name with genuinely nothing in it at all

A third real report, from the same `royal92.ged` file, on the very next
individual tested: `I785`, entered with only a title, sex, and death
year -- no name content of any kind. The request this produces has a
`NameForm` with neither `parts` nor `fullText` (an empty string, not
absent -- JSON can't distinguish the two for a plain `string` field
either way), which the fix above still rejected. Checked directly against
the real `royal92.rmtree` row this individual produces before accepting
that the rejection was wrong: `Given=""`, `Surname=""`, `IsPrimary=1` --
still one real `NameTable` row, not zero. Confirmed consistently against
a second individual with the identical GEDCOM shape (`I788`, `"1 NAME
//"`) before treating it as a general rule rather than a one-off.

Fixed the same way as the `fullText`-only case: no split is attempted (
there's nothing to split), `Given` ends up `""` simply because
`form.FullText` already is empty in this case -- the existing assignment
handles both outcomes without a separate branch.

This also meant reconsidering `Person.names` itself, checked directly
against the conceptual model spec (Section 2.1) rather than assumed:
`OPTIONAL`, same as `fullText`/`parts` on `NameForm`. This server was
also rejecting a `Person` with no `names` field at all, or an empty
`names` array -- both now accepted, falling back to exactly one empty,
primary `NewPersonName`, matching RootsMagic's own confirmed "always at
least one `NameTable` row" behavior from the case above rather than
diverging from it by creating zero.

**A fourth issue, found while testing the third, not reported
separately**: the real request for `I785` doesn't mark its (only) name
`preferred` -- GEDCOM X's own spec permits this, treating list order as
the preference signal instead ("names are assumed to be given in order
of preference, with the most preferred name in the first position").
This server wasn't honoring that convention at all: every name got
created with `IsPrimary=0` unless a client explicitly set `preferred:
true`, which produced `IsPrimary=0` on `I785`'s only name -- diverging
from every real `royal92.rmtree` row checked throughout this entire
project, all of which have `IsPrimary=1` on the first or only name, with
no exceptions found. Fixed in `buildNewPerson`: if nothing in the list
was explicitly marked preferred, the first name defaults to primary
instead. Verified specifically that an explicit `preferred: true` later
in the list still overrides the default, not just that the default
itself works.

**A fifth issue, found but deliberately not fixed here**: `I785`'s death
year (`1870`) doesn't appear in the created record either. Checked why
directly: the request carries only `Date.Original` (`"       1870"`,
whitespace-padded), never `Date.Formal`, and `buildNewPersonFact` only
ever consults `Formal` -- `Original` is silently ignored entirely,
regardless of its content. Unlike the four fixes above, this isn't a
narrow, deterministic gap with a single evidence-backed answer sitting
right there -- `Original` is explicitly free text with no defined
grammar at all, and while this particular example happens to be a bare,
parseable year, `Original` can just as easily hold `"abt 1870"`, `"MAY
1870"`, or something with no extractable date at all. Solving this
properly is a real, open-ended parsing problem in its own right, not a
quick addition to this fix -- flagged for its own dedicated pass rather
than rushed in alongside four narrower, already-verified fixes. (It got
that dedicated pass -- see "`Date.original` as a fallback" below.)

### Four corrections to bring write behavior in line with RootsMagic, prompted by direct review

All found by directly checking real RootsMagic data rather than trusting
this project's own earlier claims about it -- two of the four turned out
to be wrong, not just incomplete.

**`BirthYear`/`DeathYear`: duplicated across every name, not just the
primary.** An earlier version of `CreatePerson`
(`internal/rmdb/createperson.go`) set these only on a person's primary
`NameTable` row, with an explicit comment claiming this was "confirmed
against a real captured diff." That claim was wrong, not just stale --
checked directly against real multi-name people in two separate real
RootsMagic databases (neither `royal92.rmtree` nor this project's other
established fixtures happen to contain a multi-name person at all, which
is presumably why this was never caught earlier) and found every
non-primary name row carrying the *same* `BirthYear`/`DeathYear` as its
person's primary name, with no exceptions. Corrected to duplicate the
values onto every name row; the incorrect comment was corrected in
place, not just removed, and the existing test
(`TestCreatePersonNonPrimaryNameHasNoYears`, whose own name asserted the
wrong premise) was replaced with
`TestCreatePersonNonPrimaryNameHasSameYears`.

**`ChildOrder`: 0-indexed, not 1-indexed.** Both places
`internal/rmdb/createparentchild.go` starts a new family's first child
(`nextID`-style "one past the current value" logic, defaulting to `1`
when no children exist yet) used a 1-indexed default. Checked directly
against real `ChildTable` data across three real RootsMagic databases:
two independently confirmed every multi-child family starts at
`ChildOrder = 0`, with only a third, differently-sourced database (of
unclear provenance, and the outlier against the other two) showing
1-indexed. Corrected in both the "new family" and "merge into an
existing family" code paths; the real six-children capture test's own
`ChildOrder` expectation was corrected too, since it had been asserting
the wrong (1-indexed) values without anyone having actually checked them
against the real capture's own `ChildOrder` column specifically.

**`PersonTable.SpouseID` and `PersonTable.ParentID`: neither is set by
this server at all.** Checked directly against the RM4-11 data
dictionary's own description before reconsidering either field, not
assumed: each holds the `FamilyID` of whichever family was last *viewed*
for this person in RootsMagic's own UI -- as a spouse, or as a child,
respectively (`0` if none ever was) -- a UI navigation state, not a
genealogical fact, and a value that has no principled correct answer for
a record created through this API, which was never viewed in that UI at
all. `ParentID` was found second, in direct follow-up review after
`SpouseID`, using the identical reasoning and the identical dictionary
wording (down to the "last displayed in Pedigree, Family, or Descendent
View" phrasing) -- confirmed empirically too, not just from the
dictionary's own text: in `royal92.rmtree`, 1964 of the 2018 people
(97%) who genuinely do belong to a family as a child via a real
`ChildTable` row still have `ParentID = 0`. The authoritative source of
the real relationship is `ChildTable`, never either of these two
`PersonTable` columns.

What actually prompted revisiting `SpouseID` first wasn't the definition
alone, though -- it was a real, concrete symptom: a real Brontë test
database showed a person's `SpouseID` referencing `FamilyID = 7` when
only 4 families existed. Tracing this down surfaced a genuine, separate
bug in `CreateParentChildRelationship`'s own merge logic (see "A real
design mistake, corrected" above): when a temporary single-parent family
gets merged into a pre-existing one and deleted, nothing updated the
*other* parent's own `SpouseID` if it had been pointing at that
now-deleted family -- left dangling, referencing a `FamilyID` that no
longer existed. Given six children each create-then-merge-away a
temporary family in a typical multi-child scenario, this reliably
produces exactly the kind of gap-in-the-sequence value reported.

Rather than add more bookkeeping to keep a UI-state field correct across
every merge -- for a value this server never had genuine information
for in the first place -- neither field is touched at all, by either
`CreateCoupleRelationship` or `CreateParentChildRelationship`
(`CreatePerson`'s own `PersonTable` insert already defaulted both to
`0`, unchanged). This removes the dangling-reference bug as a direct
consequence of the design change, not as a separate patch alongside it,
and removes the equivalent (if never separately reported) bug `ParentID`
would have had for exactly the same reason. Real-capture tests that had
asserted a specific value for either field (matching what RootsMagic's
own UI produced when the golden files were originally captured --
which inherently involved viewing the family, unlike an API-created
one) were updated to confirm both now stay `0` instead.

All four fixes verified together, not just individually: reproduced
this exact scenario end to end through the real HTTP API (Patrick and
Maria's family, all six real Brontë children, each linked via two
separate `ParentChild` requests) and confirmed directly against the
resulting database -- a single, correctly-merged family; `ChildOrder`
running `0` through `5` across the six children in order; `BirthYear`/
`DeathYear` duplicated across both of Patrick's own names; and every
`PersonTable.SpouseID` in the database at `0`, with no dangling values
anywhere.

### `Fact.value` was never reaching `EventTable.Details`

A real, reported gap, not found independently: a value-only fact --
`Occupation`, `Education`, `Religion`, and similar types that carry a
free-text `value` rather than (or alongside) a date or place -- created
successfully and produced a real `EventTable` row, but with `Details`
always empty. `buildNewPersonFact` (`internal/api/createperson.go`)
simply never read `f.Value` at all, and `NewPersonFact`
(`internal/rmdb/createperson.go`) had no field to carry it even if it
had. This was a one-directional gap specifically -- the read side
(`buildFact`, `internal/api/convert.go`) already reversed this exact
mapping (`e.Details` -> `f.Value`) correctly, so a fact created any other
way (or inspected directly in the database) always showed the value
correctly; only this server's own write path never wrote it.

Checked directly against the conceptual model spec before wiring
anything up, not assumed: `Fact.value`, Section 3.14 -- "the value of
the fact," string, `OPTIONAL`. Fixed by adding a `Details` field to
`NewPersonFact`, setting it directly from `f.Value` in
`buildNewPersonFact`, and binding it in both places an `EventTable` row
gets created from a `NewPersonFact` (`CreatePerson` and
`CreateCoupleRelationship`, the latter for family-owned facts like a
`Marriage` that might equally carry a `value` -- both had the exact same
hardcoded-empty gap, even though only the person-level path had a real
report against it). Verified against the real request this was reported
with directly: a person with three ordinary date/place facts (`Birth`,
`Death`, `Burial`) and three value-only ones (`Occupation`, `Education`,
`Religion`) in the same request, confirming each `Details` value lands
on the correct fact and neither kind of fact interferes with the other.
`internal/rmdb/createperson_test.go` gained a storage-layer test
confirming the write directly; `cmd/server/main_test.go` gained an
HTTP-level test reproducing the exact reported request end to end.


### Nicknames: a real GEDCOM-vs-GEDCOM-X structural mismatch

Prompted by a direct question, not found independently: GEDCOM 5.x nests
a nickname *within* a `NAME` record (`"2 NICK"` under `"1 NAME"`), but
GEDCOM X models a nickname as its own, separate `Name` -- checked
directly against the conceptual model spec's "Known Name Types" (Section
3.13.1: `BirthName`/`MarriedName`/`AlsoKnownAs`/`Nickname`/`AdoptiveName`/
`FormalName`/`ReligiousName`), not assumed. A `Name` with `type=Nickname`
was already a recognized value before this (`gedcomx.NameTypeCode` maps
it to RootsMagic's own `NameType` 6, matching how an `AlsoKnownAs` or
`MarriedName` gets its own `NameTable` row), so it was never rejected --
but that's the wrong mechanism for a GEDCOM-5.x-sourced nickname
specifically: `NameTable.Nickname`/`NicknameMP` are a single pair of
columns on *one* name record, not a slot for an arbitrary number of
alternate nickname entities the way a genuinely separate `AlsoKnownAs`
name is. A client converting a real GEDCOM 5.x `NICK` value into a
separate `Name(type=Nickname)` -- the natural, faithful translation,
given GEDCOM X has no "nested sub-field of another Name" concept at all
-- would previously have gotten a second, spurious `NameTable` row
instead of the `Nickname` column RootsMagic itself would have used for
an equivalent GEDCOM 5.x import.

Fixed in `buildNewPerson` (`internal/api/createperson.go`): a `Name`
with `type=Nickname` is filtered out of the normal "each `Name` becomes
its own `NameTable` row" loop before it runs, rather than being passed
through it. Its text (`nicknameText`: `nameForms[0].fullText`, falling
back to every part's value concatenated with spaces -- mirroring the
conceptual model spec's own description of how `fullText` MAY be
derived from `parts`, since a nickname has no meaningful Given/Surname
breakdown of its own to preserve) is attached to whichever name ends up
primary, determined *after* the existing preferred/first-name-default
logic has already run -- so this works correctly regardless of whether
the primary name is `names[0]` or a later one explicitly marked
`preferred`. Given the rarity of the case and RootsMagic's own schema
only having room for one, a second `Name(type=Nickname)` is dropped
rather than rejected -- this was floated as the working assumption
before building anything, confirmed rather than just accepted:
logged at `Info` level (which nickname was used, which were dropped)
so it's not silently lost, matching this project's own established
"log, don't reject" precedent for other recognized-but-necessarily-
narrowed request content (`logIgnoredFields`). A `Name(type=Nickname)`
with no `nameForms` at all is still rejected -- `nameForms` is REQUIRED
on every `Name` regardless of type (Section 3.13), and this server
doesn't relax that requirement just because a particular `Name`
happens to be handled differently downstream.

**The read side gained matching support**, not left as a write-only,
one-way feature: `buildPerson` (`internal/api/convert.go`) synthesizes a
second, separate `Name(type=Nickname)` from `NameTable.Nickname`
whenever it's non-empty, alongside the real `Name` built from that same
row -- the direct reverse of the write-side absorption. Deliberately
given no `id` of its own: it isn't a real, separately addressable
`NameTable` row, and assigning one (e.g. reusing the parent name's)
would misleadingly imply it were addressable when it isn't. Verified as
a genuine round trip, not assumed: posted a person with a nickname,
`GET` it back, confirmed the nickname reappears as its own synthesized
`Name` in the response.

**`NicknameMP` is computed via `FoldAccents`**, the same transformation
already confirmed for `SurnameMP`/`GivenMP` -- by analogy, not
independently verified against a real captured example, since no
`NICK` value exists anywhere in this project's own `royal92.ged`
reference file. Worth naming precisely why the analogy is the best
available evidence rather than treating it as equivalent to a real
capture: the RM4-11 data dictionary's own description of `NicknameMP`
turned out, on inspection, to be a copy-paste artifact -- `SurnameMP`,
`GivenMP`, and `NicknameMP` all carry the *identical* description text
("Version of User Defined NameTable.Surname"), which is clearly a
mistake in RootsMagic's own documentation (referencing `Surname` for
all three), not a real semantic claim that `NicknameMP` derives from
`Surname`. The `Nickname`/`NicknameMP` write path itself (writing
whatever `NewPersonName.Nickname` is given, computing its `MP` value)
was already present in `internal/rmdb/createperson.go` before this
change -- built at some earlier point but never actually reachable,
since nothing on the API layer populated the field until now.
`internal/rmdb/createperson_test.go` gained a dedicated test confirming
both the verbatim write and the folded value (`"Ańné"` verbatim,
`"Anne"` in `NicknameMP`) directly, and `cmd/server/main_test.go` gained
five HTTP-level tests: attaches to the primary row rather than creating
one of its own, attaches correctly when the primary name isn't
`names[0]`, a second nickname is dropped rather than rejected, and a
nickname with no `nameForms` is still rejected.

One incidental correction made alongside this: `buildNewPersonName`'s
own comment previously said nickname was "its own NamePart type" --
checked directly against the "Known Name Part Types" list (Section
3.18.1: `Prefix`/`Suffix`/`Given`/`Surname` only) while building this
feature and found to be wrong; nickname was never a `NamePart` type at
all, only a `Name` type. Fixed in place rather than left standing next
to the correct behavior it was actually (if inadvertently) describing.

### `Date.original` as a fallback when `Date.formal` is absent

The dedicated pass the previous section deferred to. Prompted by the
same `I785` report, but as its own, explicit request this time: this
server should accept `Date.original` for populating `EventTable.Date`,
not assume every client computes `Formal`.

**Grounded in the actual GEDCOM 5.5.1 specification directly**
(`gedcom.io/specifications/ged551.pdf`, fetched and read in full, not
recalled or assumed), not in any client-side conversion tooling this
project's own test data happened to come from -- tooling like that is
expected to keep changing as it's developed further, so it isn't a
sound basis for what this server itself needs to support; the
specification is the actual, stable contract. `Date.original` values
aren't arbitrary human-typed text in general, but they also aren't
required to follow any particular grammar at all -- what grounds this
feature's scope is GEDCOM 5.5.1's own `DATE_VALUE` grammar (page
45-47), since a `DATE` tag's line value in a real GEDCOM file is
exactly this grammar, and a client passing that value through into
`Date.original` verbatim (a reasonable, common thing to do) means this
server can reasonably expect to see it.

The full `DATE_VALUE` grammar, read directly from the specification:

```
DATE_VALUE:= [ <DATE> | <DATE_PERIOD> | <DATE_RANGE> |
               <DATE_APPROXIMATED> | INT <DATE> (<DATE_PHRASE>) |
               (<DATE_PHRASE>) ]
DATE_APPROXIMATED:= [ ABT <DATE> | CAL <DATE> | EST <DATE> ]
DATE_RANGE:= [ BEF <DATE> | AFT <DATE> | BET <DATE> AND <DATE> ]
DATE_PERIOD:= [ FROM <DATE> | TO <DATE> | FROM <DATE> TO <DATE> ]
DATE_GREG:= [ <YEAR_GREG>[B.C.] | <MONTH> <YEAR_GREG> |
              <DAY> <MONTH> <YEAR_GREG> ]
YEAR_GREG:= [ <NUMBER> | <NUMBER>/<DIGIT><DIGIT> ]
```

**`internal/gedcomx/gedcom5date.go`'s new `ParseGedcom5Date`** covers
`<DATE_GREG>` (day/month/year, month/year, or year-only precision) and
`<DATE_APPROXIMATED>`/the two single-date halves of `<DATE_RANGE>`
(`ABT`/`CAL`/`EST` qualitative, `BEF`/`AFT` directional -- both
confirmed, already-supported modifier categories on the read side,
`rmdate.go`, just not previously reachable from this write path).
Reuses `EncodeRMDate`'s own RM-date-string construction (extracted into
a small shared `buildRMDateString` helper rather than duplicated) but
built from this different source grammar. `royal92.ged`'s own 4018 real
`DATE` values (now a permanent project fixture, `testdata/royal92.ged`)
were used to confirm this scope actually holds up against a real file,
not to define the scope in the first place: 99.5% (3998) parse
successfully.

**Not yet supported, named here precisely because the specification
defines them, not because a test file happened to lack examples**:
`BET <DATE> AND <DATE>` and the `<DATE_PERIOD>` forms (`FROM`/`TO`) --
both involve two dates, and `BET...AND...` in particular reproduces the
same ambiguity already documented for `EncodeRMDate`'s own formal-date
direction (GEDCOM X's formal grammar can't distinguish "between" from
"from...to" even though GEDCOM 5.5.1's own grammar can); `INT <DATE>
(<DATE_PHRASE>)` and the bare `(<DATE_PHRASE>)` form -- a free-text
phrase in parentheses, which the specification itself describes as "any
statement offered as a date when the year is not recognizable to a date
parser," i.e. deliberately unstructured by design, not something a
grammar-based parser should be reaching for; the `B.C.` suffix on
`YEAR_GREG`; and `YEAR_GREG`'s own double-dating form
(`<NUMBER>/<DIGIT><DIGIT>`, e.g. `"1743/44"`, for the pre-1752
Julian/Gregorian new-year-date discrepancy). Each of these needs its own
real design decision -- what does `SortDate` mean for a date that's
fundamentally a range, not a point? does double-dating sort by the old-
style or new-style year? -- not a quick addition alongside the cases
above just because the grammar exists. One data point cross-checks two
completely separate parts of this project regardless:
`ParseGedcom5Date("abt 1808")` produces
`"D.+18080000.A+00000000.."`, the exact value independently confirmed
against real captured RootsMagic data during a much earlier
investigation into a different individual entirely.

**Returns `ok bool`, not an error** -- a deliberate difference from
`EncodeRMDate`. `Original` is inherently free text with no single
defined grammar it's required to follow (GEDCOM 5.5.1's own
`DATE_VALUE` grammar is the common case, not a universal guarantee), so
not matching it is a normal outcome here, not a client mistake to
report back as a `400`. `buildNewPersonFact` falls back to no date
(matching the existing behavior for a fact with no date information at
all), logs the unparsed value at `Info` level, and -- added on direct
request, see below -- preserves the original text itself in
`EventTable.Note`, prefixed with `"rmgedcomx was unable to parse this
text as a date: "` so it's clearly this server's own annotation and not
data RootsMagic itself wrote. `NewPersonFact` gained a `Note` field for
this (`internal/rmdb/createperson.go`), and the previously-hardcoded
empty `Note` in both `CreatePerson`'s and `CreateCoupleRelationship`'s
own `EventTable` inserts now binds it properly -- the latter isn't
currently reachable through the HTTP API (`handleCreateRelationships`
doesn't build `Facts` for a `Couple` relationship at all yet, a
separate, pre-existing gap noted here rather than fixed in the same
change), but keeping both write paths consistent was worth doing
regardless of which one a request can currently reach.

### A real bug this uncovered: an invalid `Date.formal` rejected the whole request even with a perfectly good `Date.original` right there

A real request to create Charlemagne failed outright:
`Date.formal="+742-04-02"` (his real birth year, 742 AD) alongside
`Date.original=" 2 APR  742"`. Checked directly against the actual
GEDCOM X Date Format specification
(`github.com/FamilySearch/gedcomx/blob/master/specifications/date-format-specification.md`,
Section 5.2.2.1) before concluding which side was actually wrong: *"The
year component is defined as a REQUIRED [+] or [-] and four digits,
left-padded with zeros as needed."* `"742"` needed to be `"0742"` --
the request's own `Formal` value is genuinely not spec-compliant, and
`EncodeRMDate`'s rejection of it was, strictly, correct. That's not
where this actually stopped, though: the previous version of
`buildNewPersonFact` treated *any* `Formal` parse failure as an
immediate, hard rejection of the whole request -- even though the very
same fact's `Original` value was sitting right there, unambiguous, and
perfectly parseable by `ParseGedcom5Date`. A `Formal` that's present but
invalid was, in effect, being treated as a *harder* failure than
`Formal` being absent entirely, which makes little sense: both cases
end up needing the same fallback.

Fixed by extending the fallback `buildNewPersonFact` already had for a
missing `Formal` to also cover an *invalid* one: if `EncodeRMDate`
fails and `Original` is present, fall back to `ParseGedcom5Date` on
`Original` before giving up, the same as the missing-`Formal` case
already did. `EncodeRMDate`'s own validation is untouched -- still
exactly as strict, still correctly rejecting `"+742-04-02"` on its own
terms; what changed is only what `buildNewPersonFact` does in response.
A genuinely unrecoverable case (`Formal` invalid, and no `Original` to
fall back to at all) still returns a `400` -- the client explicitly
opted into `Formal`'s strict, machine-readable contract in that case
and failed it with nothing else offered, which is different from
`Formal` simply being invalid but recoverable via `Original`.

Verified against the real request directly: both of Charlemagne's facts
(birth `+742-04-02`, death `+814`, neither zero-padded) now create
successfully, and a subsequent `GET` shows each correctly re-encoded
with a properly zero-padded `Formal` (`+0742-04-02`, `+0814`) --
confirming the value round-trips correctly through `ParseGedcom5Date`
and back out through the read side's own `ParseRMDate`, not just that
the request no longer fails.
`cmd/server/main_test.go`'s `TestCreatePersonsHTTP` gained this exact
scenario as a permanent regression test, alongside two more: `Formal`
invalid with `Original` *also* unparseable (falls back to no date plus
`Note`, still not a rejection) and `Formal` valid with `Original` also
present (confirms `Formal` still takes priority, unchanged).

`internal/gedcomx/gedcom5date_test.go`'s
`TestParseGedcom5DateAgainstRealRoyal92GedFile` runs every single one of
`royal92.ged`'s 4018 real dates through `ParseGedcom5Date`, asserting both
the overall 99.5% coverage rate and the *exact* set of 20 values that
don't match -- a future change narrowing or widening real-world coverage
gets caught here specifically, not just discovered later against a real
request. Verified end to end through the real HTTP stack too, not just
the parser in isolation: `cmd/server/main_test.go` reproduces the exact
`I785` request this bug was reported with, confirming the created
record's `Date`/`SortDate`/`DeathYear` match the real `royal92.rmtree`
values for this same individual, plus separate cases confirming an
unparseable `Original` -- both arbitrary free text and a real,
specification-defined form this server doesn't parse yet (`BET...AND...`)
-- is preserved in `Note` with the exact prefix, rather than rejected or
silently dropped.

**Per RS spec Section 4.9.2 exactly**: `201` + `Location` header when a
request creates exactly one person, `204` when it creates several. A
request creating multiple persons is explicitly *not* all-or-nothing
across those persons -- each `CreatePerson` call is its own transaction,
so a failure partway through a multi-person request can leave earlier
persons in that same request already committed. The error response in
that case says so explicitly, naming which persons already exist,
rather than leaving the client to discover it. Making a whole
multi-person request atomic would need one transaction spanning all of
them; not built, since nothing in this project's reference data
exercises multi-person creation as a single atomic unit in the first
place.

Verified through the real HTTP stack end to end, not just at the
storage layer: `cmd/server/main_test.go`'s `TestCreatePersonsHTTP`
covers the `201`/`Location`/follow-up-`GET` happy path, the `204`
multi-person path, every rejection case above, malformed JSON, an
actually-unknown field (still caught by `decodeStrictJSON`, same as
every other write handler), and read-only mode still correctly
returning `405`.

### `POST /relationships` -- Couple and ParentChild creation (done)

Built on the same `nextID`/`resolveOrCreatePlace`/`bumpConfigTableModDate`
pattern `CreatePerson` established -- confirmed to carry over directly,
as anticipated.

**`CreateCoupleRelationship`** (`internal/rmdb/createcouple.go`) creates
a new `FamilyTable` row plus zero or more family-owned facts (e.g. a
Marriage) -- RootsMagic itself supports single-parent
families (confirmed against real data: several families in
`royal92.rmtree` have one of the two at `0`), so only one of
`FatherID`/`MotherID` is required, not both. Matched field-by-field
against the real captured golden file for Patrick and Maria's marriage
-- exact match on every field this server controls, including
`EventTable.FamilyID` staying `0` on the marriage fact itself (a
family-owned fact identifies its owner via `OwnerType`/`OwnerID`, not
this column -- the same finding already documented for `CreatePerson`'s
own facts). Two deliberate divergences from the real capture, both
consistent with `CreatePerson`'s own established policy: both spouses'
`UTCModDate` get bumped here, not just one -- RootsMagic's own real
capture only bumped whichever spouse the operation was performed *from*
(confirmed directly against raw, unredacted values, not just the golden
file), which this project has already decided not to replicate, for
both `Person` and `Relationship` writes, since there's no principled
reason to prefer one spouse's timestamp over the other's; and
`PersonTable.SpouseID` is left untouched entirely -- the real capture did
show it set, but see "SpouseID: removed entirely" below for why that
was reconsidered and reversed.

**`CreateParentChildRelationship`** (`internal/rmdb/createparentchild.go`)
was the harder design problem in this whole stage, and went through a
real, corrected design mistake before landing where it is now -- see
"A real design mistake, corrected" below for the full account. The
short version of where it landed: RootsMagic's own schema has nowhere
to attach a bare (parent, child) pair directly. A child belongs to a
*family* (`ChildTable.FamilyID`), and that family separately has a
`FatherID`/`MotherID` -- the father-child and mother-child relationships
this server's read side already exposes as two distinct `Relationship`
resources (see "Relationships" above) are really two views onto the
same underlying family membership, not two facts RootsMagic stores
independently. Creating one has to resolve which family is actually
meant, and the resolution only ever does so based on something already
established about the *child* specifically -- never based on what the
named parent alone happens to already have on file:

1. If the child already belongs to a matching-kind family (see RelType
   below) that already has this exact parent in the matching role, this
   is a no-op (idempotent) -- confirmed directly with a dedicated test,
   not just reasoned about.
2. If the child already belongs to a matching-kind family with that role
   empty, it's completed with this parent. If completing it would create
   a second family record for parents already paired elsewhere (a real
   case: the other parent's own `ParentChild` request, or a `Couple`
   relationship, may have already established that exact pairing under
   a different `FamilyID`), the child's link is *moved* to the
   pre-existing family instead, and the now-redundant one is removed.
3. If the child has no matching family at all, a new one is created for
   this parent alone (matching `CreateCoupleRelationship`'s own support
   for single-parent families) -- regardless of how many *other*
   families the named parent already has on file for other children.
4. If more than one of the child's existing families could match, this
   is genuinely ambiguous (a real, schema-supported case -- a child can
   belong to more than one family at once, e.g. biological and adoptive
   -- see "RelType" below) and rejected rather than guessed at.

Sex "Unknown" on the parent is rejected outright, the same reasoning
`CreateCoupleRelationship` already applies via `resolveCoupleRoles`
(below) to a couple where either person's sex isn't Male/Female. A real
gap was caught and fixed while building the HTTP layer on top of this:
the first version never validated `ChildID` actually existed before
creating a dangling `ChildTable` reference to it -- caught before
shipping, not after.

Checked field-by-field against the real six-children capture -- and this
specific check is itself worth being honest about, not just stated:
the original comparison used a `ChildOrder` expectation (`1` through
`6`) that turned out to be wrong (see "`ChildOrder`: 0-indexed, not
1-indexed" below), so "exact match" wasn't actually true until that was
corrected. `RecID`/`ChildID`/`FamilyID`/`ChildOrder` now genuinely match
across all six children, in order -- requiring two `ParentChild`
requests per child (one per parent) rather than one, per the corrected
design above; the test itself was updated to send both rather than
relying on the old, incorrect single-request shortcut.

### A real design mistake, corrected

An earlier version of this function resolved a bare (parent, child)
pair by checking whether the named parent already had exactly one
family on file, and used it directly if so. This was a real, if
understandable, mistake, caught during design discussion rather than
after shipping: "the parent happens to have one family recorded" is a
fact about this database's *current contents*, not a fact about the
parent's real life. If Mary's only recorded family happens to be with
Patrick, a bare `ParentChild(Mary, Child)` request says nothing at all
about whether Patrick is Child's other parent -- it could just as
easily be Robert, someone not yet in the database at all. Linking the
child into Patrick's family anyway would silently assert a co-parent
that was never actually named.

Corrected to the design above, which never reuses a family based on
the parent's own existing state -- only ever based on the child's.
Verified directly, not just reasoned about: a person with two real,
distinct partners (Mary, with children by both Patrick and Robert) was
constructed and confirmed each child lands in the correct, distinct
family, never confused with the other, even though Mary already had
one family on file by the time the second child's link arrived.

Two further, real bugs were found and fixed while updating the existing
real-data test to send both `ParentChild` requests per child (matching
the corrected design) rather than just discovering them by accident
later:

- **`ChildOrder` wasn't recomputed on merge.** Each child, created in
  its own temporary single-parent family before being merged into the
  shared one (case 2 above), always started at `ChildOrder = 1` in that
  temporary family (this project's own indexing at the time this bug
  was found and fixed -- later itself corrected to start at `0` instead;
  see "`ChildOrder`: 0-indexed, not 1-indexed" below) -- and the merge
  only moved the `ChildTable` row's `FamilyID`, never recomputed
  `ChildOrder` against the *target* family's own existing children. All
  six children would have silently collided at the same starting value
  instead of getting distinct positions in order.
- **`PersonTable.ParentID` wasn't updated on merge either** -- left
  pointing at the now-deleted temporary family instead of the
  pre-existing one actually kept.

Both are now fixed as part of the merge step itself, and both are
covered by the same real-data test, which would fail again if either
regressed.

### RelType: distinguishing biological, adoptive, step, foster, and guardian relationships

Prompted by the ambiguity case above having a real, named counterpart:
`ChildTable.RelFather`/`RelMother` (confirmed against the RM4-11 data
dictionary) aren't flags -- they're an eight-value relationship-kind
code (0=Birth, 1=Adopted, 2=Step, 3=Foster, 4=Related, 5=Guardian,
6=Sealed, 7=Unknown), meaning a person genuinely can belong to more
than one family as a child at once (biological parents and adoptive
parents both on file is a real, supported case, though not one
`royal92.rmtree` happens to contain an example of).

**Checked the actual GEDCOM X specification for a matching mechanism
before building anything, and initially checked the wrong document**:
the conceptual model spec's own "Known Fact Types" table does list
`http://gedcomx.org/Adoption`, but that table's own "scope" column marks
it `person`, not `relationship` -- and there is a *separate, dedicated*
specification (`fact-types-specification.md`, not
`conceptual-model-specification.md`) with its own "2.3 Parent-Child
Relationship Fact Types" section, found only after this project's own
first attempt used the wrong one and was corrected. That section defines
ten fact types explicitly scoped to a parent-child relationship, five of
which have a direct, one-to-one RootsMagic counterpart: `BiologicalParent`
(RelType 0), `AdoptiveParent` (1), `StepParent` (2), `FosterParent` (3),
`GuardianParent` (5). `Related` (4), `Sealed` (6, an LDS-temple-ordinance
concept specific to RootsMagic with no GEDCOM X counterpart at all), and
`Unknown` (7) are left unmapped -- nothing in GEDCOM X's own vocabulary
corresponds to them cleanly, and the remaining five GEDCOM X fact types
in that section (`ChildOrder`, `EnteringHeir`, `ExitingHeir`,
`SociologicalParent`, `SurrogateParent`) don't correspond to any
RootsMagic `RelFather`/`RelMother` value at all. `relTypeFromFacts`
(`internal/api/createrelationship.go`) does the actual mapping, checked
in the request's own `Facts` order (first match wins), defaulting to
`RelTypeBirth` when none are present -- matching RootsMagic's own
default for an unspecified GEDCOM 5.x `PEDIGREE_LINKAGE_TYPE`, and
`BiologicalParent` exists so a client *can* say so explicitly, not so
every client *must*.

This isn't just metadata -- it's load-bearing for case 4's own
ambiguity resolution above: a candidate family is only a match if the
target role is empty *and* the other role (if already filled) is either
also empty or the same `RelType`. This is what lets a child who already
has an incomplete biological family and a separate incomplete adoptive
family still resolve correctly: a new birth-type link only considers
birth-type candidates, an adopted-type link only considers adopted-type
ones -- confirmed directly, end to end through the real HTTP request
shape a client actually sends, not just at the `rmdb` layer.

**Not yet verified against a real captured adoption case** -- unlike
almost everything else in this stage, which was checked against real
RootsMagic data before being trusted. Flagged clearly rather than
implied: this is real, spec-grounded, internally-tested design, but the
next real GEDCOM file with an actual adoption in it should be treated as
a genuine verification step, not just another test run.

**`internal/api/createrelationship.go`**'s `handleCreateRelationships`
resolves which of the two supported types a request is (`Couple` or
`ParentChild`; anything else is rejected) and which person plays which
role. For `Couple` specifically, `resolveCoupleRoles` determines
Father/Mother from each person's own recorded `Sex` rather than trusting
`person1`/`person2`'s order -- confirmed the RS spec's own `Couple`
relationship type doesn't define which is which -- and rejects a pair
that isn't exactly one Male and one Female, the same principle as
`CreateParentChildRelationship`'s own unknown-sex rejection. Per RS spec
Section 4.20.2 (mirroring `Persons`' own `POST`): `201` + `Location` for
exactly one relationship created, `204` for several.

`CreateCoupleRelationship` (`internal/rmdb/createcouple.go`) also gained
an eager existing-family check as part of this same redesign -- unlike
`CreateParentChildRelationship`, this is safe to do eagerly, since a
`Couple` relationship explicitly names both people; there's no unstated
"who's the other parent" that reusing an existing family might silently
assume incorrectly. If a family already exists with exactly this
Father/Mother pairing, it's reused (idempotent) rather than duplicated;
if either person already has an existing family with the other role
empty, it's completed rather than a new one created -- the real case
this covers being a `Couple` relationship arriving *after* both parents
were already independently established via separate `ParentChild`
requests, making it fully redundant except for whatever `Facts` it
carries (e.g. a `Marriage` date), which still get attached to whichever
family id ends up in play either way -- verified directly, not assumed,
since an early draft of this returned before reaching the fact-attaching
code at all when reusing an existing family, silently dropping any
`Facts` the request carried.

Verified through the real HTTP stack end to end:
`cmd/server/main_test.go`'s `TestCreateRelationshipsHTTP` covers the
`Couple` happy path (in both person1/person2 orderings), the
`ParentChild`-after-`Couple` case confirming both derived relationships
appear on a subsequent `GET`, the `204` multi-relationship path, a
same-sex couple rejection, a nonexistent person rejection (confirmed
`400`, not a raw `500`), an unsupported relationship type, an empty
request, and read-only mode.

### What's next

`Person`, `Couple`, and `ParentChild` creation are all done and tested.
Nothing currently identified as the next piece -- this closes out the
create-side work this stage set out to do (see "Stage 3" above for
where this started).

## RootsMagic version handling

RootsMagic 8 or later is required. The data dictionary shows that `PersonTable`,
`NameTable`, `FamilyTable`, `ChildTable`, `EventTable`, `FactTypeTable`,
`PlaceTable`, `SourceTable`, `CitationTable`, `CitationLinkTable`, and `RoleTable`
are unchanged between RootsMagic 8 and RootsMagic 10/11 for every column this
server reads. So rather than branching logic on a detected version number,
`internal/rmdb` does two things:

1. **Discovers columns dynamically** with `PRAGMA table_info(...)` at startup, and
   only selects columns it knows how to use. If a future RootsMagic version adds
   columns, nothing breaks. If a column this server wants is missing, it's treated
   as absent/zero-value rather than causing an error.
2. **Reports a best-effort version string** in the startup log line (based on which
   optional tables exist, e.g. `DNATable`, `FamilySearchTable`, `AncestryTable` are
   later additions) -- this is purely informational and doesn't gate functionality.

If a required table or column is missing, `Open` fails at startup with a clear
error naming what's missing, rather than silently returning incomplete or wrong
data.

### RootsMagic 7 was dropped, not just never added

Originally "RootsMagic 7 or later," narrowed to 8 after a real, specific
finding, not a general tightening of scope. A community blog post
(https://sqlitetoolsforrootsmagic.com/date-last-edited/) documented that
RM8 replaced `EditDate` with `UTCModDate` on `PersonTable`/`EventTable`/
`NameTable` -- but those aren't the tables this server's write support
(see "Write support" below) actually touches. Checked directly against
the RM4-11 data dictionary's own version-specific sheets (a `4-7` sheet
and an `8` sheet, covering exactly the tables that matter here) rather
than assuming the blog's examples generalized: `PlaceTable`,
`SourceTable`, `MultimediaTable`, `MediaLinkTable`, and `ConfigTable` --
the five tables every write handler touches -- have no modification-
timestamp column *at all* in RM7, under either name. It isn't a rename
this server failed to keep up with; for these five tables, RM7 tracked
no modification timestamp, period.

Every write handler unconditionally sets `UTCModDate` on one or more of
these tables. Against a real RM7 file, that's not a silently-missing
nice-to-have -- it's `"no such column: UTCModDate"` on the very first
write attempt, a raw SQL error a person would have had to decode
themselves, discovered only when they actually tried to write, not at
startup.

The option actually taken wasn't the only one considered. Gating only
`-write` mode to RM8+ (leaving RM7 usable read-only, since the read path
never queries this column at all) was the initial proposal and would
have worked. Decided against it for reasons broader than this one bug:
legacy RootsMagic version support was never one of this project's actual
goals, just something that came along with reading columns dynamically
rather than hard-coding a schema version. `internal/rmdb`'s own package
doc comment already says as much. And narrowing now avoids doing this
twice -- future write support is planned for `PersonTable`, `NameTable`,
`FamilyTable`, and `EventTable` (see "What's next" throughout this
document), which will need their own correct-timestamp handling
regardless of this decision.

**Enforced structurally, not just documented**: `requiredTablesAndColumns`
(`internal/rmdb/db.go`) now requires `UTCModDate` specifically on the five
write-relevant tables, plus adds `ConfigTable` to the required-tables list
for the first time (it was implicitly depended on for `UniqueID`/
`RootPerson` reads and the `UTCModDate` bump on every write, but was
never actually checked at startup before this). A pre-RM8 file now fails
`Open` immediately, for both read-only and write access, with every
missing column named in one error -- not just the first one encountered.
Verified directly, not just reasoned about: took a real copy of
`royal92.rmtree` and used `ALTER TABLE ... DROP COLUMN` to reconstruct
what an RM7 file's schema actually looks like for these five tables,
confirmed `Open` rejects it with the expected message naming all five
columns, and confirmed the real, unmodified file is still accepted
normally. `internal/rmdb/rm8_required_test.go` makes both permanent.

Testing against a real RM7 database (rather than the reconstructed
approximation used here) is worth doing at some point, as its own
separate test concern -- not done as part of this change, since no real
RM7 file was available to test against directly.

## Fact type mapping

RootsMagic's `FactTypeTable` has built-in fact types (IDs below 1000) and can have
user-defined ones (1000+). Built-in types generally carry a real GEDCOM tag
(`BIRT`, `DEAT`, `MARR`, ...); user-defined types usually have `GedcomTag = "EVEN"`.
`internal/gedcomx/facttypes.go` maps the common GEDCOM tags to their GEDCOM X
Conceptual Model fact-type URIs (`http://gedcomx.org/Birth`, etc.). Anything that
doesn't match a known tag is emitted as a custom fact type URI built from the
RootsMagic fact type name, so no fact is silently dropped, e.g.:
`http://rootsmagic.local/fact-type/Occupation`.

## Date qualifier encoding

The date-layout description above (two fixed-width `sign+YYYYMMDD+qualifier`
groups) was originally inferred from public documentation. The two
single-byte qualifier codes were then **confirmed against a purpose-built
RootsMagic test database** exercising every modifier RootsMagic's UI
exposes for a single date and a date range (see
`internal/gedcomx/rmdate_test.go`, which encodes exactly these cases as a
regression test):

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

Note the two `A` bytes are in different positions and mean different
things (`After` as the directional byte, `About` as the qualitative byte)
-- `decodeRMDate` never confuses them because they're captured from
different regex groups.

RootsMagic's own documentation (https://help.rootsmagic.com, "Date
formats") lists further modifiers this decoder doesn't have confirmed byte
codes for: the single-date directional modifiers By, To, Until, Since; the
range modifiers dash ("–") and Or; and the qualitative modifiers Circa and
Say. Dates using those still get their year/month/day decoded correctly
(the digit positions are reliable regardless of qualifier); they just don't
get an English modifier word, on the principle that guessing wrong would
misrepresent the record. If you want to fill these in, the fastest way is
the same one used here: create a couple of test people in RootsMagic,
enter dates with those specific modifiers, and inspect `EventTable.Date` --
`sqlite3 yourfile.rmtree "SELECT Date FROM EventTable"`.

GEDCOM X formal dates (`Date.formal`) are populated for the confirmed cases
where the GEDCOM X Date Format profile has a clean representation (plain,
About via the `A` approximate prefix, Before/After/Between/From-To via the
`/` range syntax) and left empty otherwise (BC dates, Calculated,
Estimated, and any unconfirmed modifier) -- `Date.original` always has the
best available human-readable text regardless.

## RMNOCASE collation

RootsMagic declares several indexed text columns (`PlaceTable.Name`,
`SourceTable.Name`, etc.) `COLLATE RMNOCASE`, a custom collation RootsMagic
registers at the application level to emulate Windows' Unicode
case-insensitive string comparison. Without that collation registered,
SQLite fails any query that touches those columns (including implicitly,
via `ORDER BY` or an index) with `no such collation sequence: RMNOCASE`.

This server registers an approximation: Go's Unicode-aware
`strings.ToLower` comparison (this handles non-ASCII case folding, e.g.
"É" vs "é", not just ASCII). What it doesn't reproduce is Windows'
accent/diacritic-insensitivity -- on Windows, RootsMagic likely treats
"café" and "cafe" as equal for sorting/searching purposes; here they sort
as distinct. That only affects sort order and place/source name matching,
never which rows exist or their content, so it doesn't affect correctness
of any data returned. [unifuzz](https://github.com/mooredan/unifuzz)
reimplements RMNOCASE more precisely (via Wine's collation logic, as a
loadable SQLite extension) if exact Windows-parity sorting matters for your
use case; the same idea (accent-stripping before comparison, e.g. via
`golang.org/x/text/unicode/norm`) could be ported into
`registerCollation()` in `internal/rmdb/db.go` if needed.

## SQLite driver, and why it's read-only after all

This server uses [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite), a
CGo-free, pure-Go SQLite implementation, so building doesn't require a C
compiler and cross-compilation works normally.

An earlier version of this document said this driver couldn't do true
read-only access and fell back to enforcing it with `PRAGMA query_only = 1`
alone. That was a real mistake, not a deliberate tradeoff -- it came from
only checking the Go wrapper's driver-specific DSN handling (`_pragma=`,
`_time_format`, `vfs`) and concluding "no `mode` handling here, so no
read-only support." But `modernc.org/sqlite` doesn't reimplement SQLite's
URI-filename parsing -- it transpiles the actual SQLite C source (via
`ccgo`), and that C code has its own well-established handling of
`mode=ro` as a query parameter (see
[sqlite.org/uri.html](https://sqlite.org/uri.html)), which takes effect
before the Go wrapper's `flags` argument to `sqlite3_open_v2` even enters
the picture. This was confirmed empirically (not just re-read from docs)
by round-tripping the exact DSN pattern this server uses -- `file:path?
mode=ro` -- against a database, and separately confirming Python's
built-in `sqlite3` module, which links the same real SQLite engine and
exhibits the identical override behavior, rejects writes and refuses to
create a missing file the same way.

So: `Open()` uses `file:%s?mode=%s` where `%s` is `ro` or `rw` -- `mode=ro`
gives genuine, OS/engine-level read-only access (a write fails with
`SQLITE_READONLY`, and a missing path fails to open rather than silently
creating an empty file). This is functionally equivalent to what a cgo-based
driver like `mattn/go-sqlite3` gives you with the same DSN convention --
there's no read-only tradeoff for choosing the pure-Go driver here after all.
An earlier version of this server also set `PRAGMA query_only = 1` as
"defense in depth" on top of `mode=ro`; that's been removed as redundant now
that `mode=ro` is confirmed to genuinely enforce read-only at the engine
level on its own, and because it split "is this connection read-only?"
across two mechanisms instead of one.

Which mode gets used is decided in exactly one place: the unexported `open`
function in `internal/rmdb/db.go` takes a `readOnly bool`; the exported
`Open` always calls it with `true`. There's no `-write` flag yet -- write
support isn't implemented (see "Why 'core resources, read-only'" above) --
but when one is added, it should thread a bool through to `open` rather than
introduce a second, separate read/write mechanism.

Custom collations (RMNOCASE) are registered once, globally, via the
package-level `sqlite.RegisterCollationUtf8`, rather than per-connection.

**A note on verification:** `modernc.org/sqlite` and its dependencies
(`modernc.org/libc`, `modernc.org/mathutil`, etc.) are hosted on
`gitlab.com`, which the sandboxed environment this server was developed in
cannot reach, so the real `modernc.org/sqlite` build specifically could
not be compiled end-to-end there. What *was* verified in that environment,
end-to-end, against both a purpose-built qualifier-test database and a
real multi-generation family tree file: every HTTP endpoint, the
read-only/missing-file behavior described above, and the RMNOCASE
collation -- all via a small local stub that implements
`modernc.org/sqlite`'s exact documented API surface (`RegisterCollationUtf8`,
driver name `"sqlite"`, native DSN passthrough for `mode=ro`) backed by a
different, reachable engine (`mattn/go-sqlite3`) underneath. That stub is
scaffolding for this project's own development, not a submission artifact,
and isn't part of the delivered code. Independently, the DSN/collation-registration approach
was checked directly against `modernc.org/sqlite`'s real source at tag
`v1.34.1` (fetched via its read-only GitHub mirror,
github.com/modernc-org/sqlite) rather than guessed from memory. On a
normal machine with unrestricted internet access, `go mod tidy && go build
./cmd/server` should just work -- if it doesn't, the most likely culprit is
the pinned `v1.34.1` version in `go.mod` being retracted or superseded;
check `https://pkg.go.dev/modernc.org/sqlite?tab=versions` for the current
recommended version and bump it.

## HTTP semantics

An external audit of this server (see repo history around the date of this
section) raised several genuine issues with how it used HTTP -- status
codes, content negotiation, error bodies, and paging links -- that this
section addresses. One point from that audit, custom link relations
(`place-descriptions`), was a conscious, already-documented choice (see
"Multiple databases / Collections" above) and wasn't changed; everything
else here was.

### Status codes: 405/404, not 501, for what this server doesn't do

An earlier version of this server explicitly registered handlers for
`POST`/`PUT`/`PATCH`/`DELETE` on every resource, and for whole unimplemented
resource families (`Records`, `Agents`, `Events`, `Person Matches`,
`/oauth2/token`), all returning a custom `501 Not Implemented` body. That
was a genuine misuse of `501`, which per RFC 7231 is about the server not
supporting a request's functionality *at all* (classically, an unrecognized
method) -- not the correct code for "this resource exists and I understood
your request perfectly, I just won't do that here" (`405 Method Not
Allowed`, which the spec requires to carry an `Allow` header listing what
*is* supported) or "this URL doesn't correspond to anything on this server"
(plain `404`).

The fix ended up being to delete code, not add it. Verified empirically
(not assumed) against Go 1.22's `net/http.ServeMux`: a path registered only
for `GET` automatically returns `405` with a correct `Allow: GET, HEAD`
header for any other method, `HEAD` requests are automatically answered
from the `GET` handler with the body discarded, and a path that was never
registered at all returns a plain `404` -- all with zero custom code. So
`internal/api/server.go`'s `resourceHandler()` and
`internal/api/multi.go`'s top-level routes now register `GET` only, for
exactly the resources this server implements, and nothing else --
including no explicit `/oauth2/token` route at all. That's not an
oversight: RS spec Section 9 makes authentication a `MAY`, this server has
no protected states to gate behind it, and nothing in this server's own
`links` ever advertises that URL to a client, so there's nothing for a
stub to usefully guard against. A client that goes looking for it anyway
gets a plain 404, same as any other URL this server was never going to
recognize -- genuinely, not just nominally: `net/http`'s router treats a
bare `"/"` pattern as a catch-all for every unmatched path, not just the
literal root, so `GET /` is registered as `"GET /{$}"` (Go 1.22's
exact-match syntax) specifically so a typo'd path doesn't quietly get
served the Collections list instead of a 404.

One consequence worth being explicit about: this server no longer
distinguishes, in its HTTP responses, "a real GEDCOM X RS feature we
haven't built" from "not a thing at all" -- both are now a plain 404/405.
That distinction still exists, just relocated to documentation (this file
and the README) rather than runtime response bodies, which is the more
correct place for it: a spec-aware client should be consulting a server's
stated capabilities, not probing error responses to reverse-engineer them.

### Content negotiation: honest about the one format on offer

This server has always produced exactly one representation,
`application/x-gedcomx-v1+json` -- there's no XML support (a full
dual-format implementation is disproportionate for what this project
needs, and nothing has ever asked for it). The gap the audit correctly
identified: it forced that `Content-Type` on every response regardless of
what the client's `Accept` header actually asked for, and never sent
`Vary`. `withContentNegotiation` in `internal/api/server.go` now checks the
`Accept` header (a plain yes/no check against the one representation this
server has -- there's nothing to rank with q-values when there's only one
option) and responds `406 Not Acceptable` if none of it can be satisfied,
and sets `Vary: Accept` on every response, since the `Content-Type` now
genuinely does depend on that header. (Not `Vary: Accept-Encoding` too, as
the audit also suggested -- this server doesn't negotiate encodings at
all, so claiming it varies on one it doesn't touch would be inaccurate,
not just incomplete.)

`GET .../artifacts/{id}/content` is deliberately exempt from both checks.
It isn't a GEDCOM X RS state -- it's this server's own extension for
serving whatever a `SourceDescription`'s `about` points at (see
"Multimedia" above) -- and its entire purpose is to return the artifact's
own real `Content-Type` (`image/jpeg`, `application/pdf`, ...), which has
nothing to do with this server's GEDCOM X JSON profile.

### Error bodies: RFC 7807, not a bespoke shape

GEDCOM X RS doesn't define an error body schema of its own -- error
responses are outside the spec's scope, left to general HTTP/REST
convention. This server's own ad hoc `{"error": "..."}` (and, before that,
"reason"/"seeAlso" fields on 501s) had no standard behind it. Every error
response is now [RFC 7807](https://www.rfc-editor.org/rfc/rfc7807) Problem
Details (`internal/api/server.go`'s `problemDetails`, `application/problem+json`):
`title` (from `http.StatusText`), `status`, and `detail` (the specific,
human-readable explanation this server always provided anyway). `type` is
deliberately omitted, which RFC 7807 defines as meaning `about:blank` --
this server doesn't have a taxonomy of distinct problem-type URIs worth
inventing and maintaining, just a status code and a message per
occurrence, and the spec's own default fallback says exactly that
honestly.

### Paging: `first`/`last` too, not just `prev`/`next`

RS spec Section 7 defines four paging rels: `first`, `next`, `prev`,
`last`. This server's `pagingLinks` originally only ever produced `first`
alongside `prev` (so never on the first page, where `first` is arguably
most useful for a client to confirm it's looking at) and never `last` at
all. `first` and `last` are now included whenever a resource has more than
one page, on every page (not just relative to a page boundary) -- unlike
`prev`/`next`, they mark the fixed ends of the whole list, not a position
relative to where the client currently is. `last`'s offset is computed
from `total`, which the caller already has (`((total-1)/limit)*limit`).

This is still a simpler mechanism than the spec's full paging-as-links
model overall (`?limit=`/`?offset=` query parameters, not opaque
server-chosen page tokens), just now complete against what Section 7
itself defines.

## Logging

Converted from a mix of the bare `log` package and direct `fmt.Fprintln`
calls to `log/slog`, prompted by a real, concrete need: a `405` with no
visible explanation for why. Both packages that log anything
(`internal/api`, `cmd/server`) call `slog`'s package-level functions
(`slog.Info`, `slog.Warn`, ...) directly against the default logger,
rather than threading a `*slog.Logger` instance through every function
call -- there's exactly one process, one log stream, and one place
(`cmd/server/main.go`'s `setupLogging`, run first thing in `main`) that
decides its level and format; a per-call-site logger parameter would add
plumbing this server has no actual use for.

**Two new flags** (`cmd/server/main.go`): `-log-level` (`trace`/`debug`/`info`/
`warn`/`error`, default `info`) and `-log-format` (`text`/`json`, default
`text`). Logs go to stderr, matching the `log` package's own former
default and every existing `fmt.Fprintln(os.Stderr, ...)` startup-error
call -- kept separate from the collection table below specifically so
the two remain independently redirectable.

**`log.Fatalf` has no direct slog equivalent** -- slog's levels are
`Debug`/`Info`/`Warn`/`Error` only, no `Fatal`. Replaced with a small
`fatalf` helper (`slog.Error` then `os.Exit(1)`) so every startup-fatal
call site does both consistently, rather than some remembering the exit
and some not.

**The startup collection table (`printCollectionTable`) deliberately
was *not* converted**, and stays direct `fmt.Fprint*` to stdout -- see
its own doc comment for the reasoning in full, briefly: it's a
human-readable report a person reads once at a terminal, not a
diagnostic log line, and an aligned table (via `tabwriter`) has no
sensible representation as structured log attributes without losing the
alignment for no real benefit.

### Debug-level logging for 4xx/5xx responses -- the actual point of this work

`internal/api/server.go`'s request-logging middleware (`withLogging`)
now emits two things per request, not one:

- An `Info` line for every request: method, path, status, duration --
  the direct replacement for what used to be a bare
  `log.Printf("%s %s -> %d (%s)", ...)` line.
- A separate `Debug` line, *only* when the response status is `>= 400`,
  carrying both the request body that produced it and the actual
  response body. For this API, the response body is always one of two
  things, and telling them apart is itself the diagnosis: this server's
  own detailed RFC 7807 reason (e.g. `"RootsMagic.exe was detected
  running..."`), or -- if the request never reached this server's own
  handler code at all, e.g. `POST /persons` when `-write` wasn't passed
  and that route was never registered -- Go's own bare `"Method Not
  Allowed"` text. Seeing the second kind is itself the answer: the
  problem isn't in a specific handler's logic, it's that the route
  doesn't exist under the server's current configuration. The request
  body was added second, prompted directly by a real report: the
  response alone explains *that* something was rejected, but the natural
  next question -- *what* did the client actually send -- needed the
  request alongside it.

At `trace`, that same `Debug` detail line is emitted for every request,
including successful responses.

Deliberately two separate log lines at two separate levels, not one line
with the bodies as always-present attributes: slog's level filtering is
per-call, not per-attribute, so a body attribute on the `Info` line would
always render regardless of the configured level -- a separate `Debug`
call is the only way to make the extra detail actually optional.

Mechanically, `statusRecorder` (already capturing the status code for
the existing log line) also captures a copy of the response body via its
own `Write`; `withLogging` itself reads and re-wraps `r.Body` to capture
the request body the same way, since an `io.ReadCloser` can otherwise
only be read once and the downstream handler still needs to read it
normally. Both captures are gated on whether `Debug` is actually enabled
(`slog.Default().Enabled(...)`, checked once up front) -- reading and
re-wrapping `r.Body` has a real, if small, cost, and there's no reason to
pay it, on every single request, on a server run at the default
`-log-level=info` where nothing will ever look at the result.

Confirmed the capture actually works, not just compiles, against real
request scenarios run through the real HTTP stack, in two separate
passes: a route genuinely unregistered in read-only mode (`405`, body
`"Method Not Allowed\n"`, Go's own text); a real handler-level `404`
(body this server's own RFC 7807 JSON, `"person P999 not found"`); a
write-guard rejection with a fake tripped guard (`405`, body this
server's own JSON, `"RootsMagic.exe was detected running..."`); a
genuine `204` success producing only the `Info` line, no `Debug` line at
all; and, for the request-body addition specifically, a request carrying
a distinctive marker value confirmed to appear in the captured debug
line alongside the response's own distinct rejection detail --
`cmd/server/main_test.go`'s `TestDebugLoggingCapturesRequestAndResponseBody`
makes this last one permanent, installing a real `slog` handler writing
to a buffer rather than asserting anything about the middleware's source
directly.

### A real bug this surfaced: `POST /relationships` returning 500 for a client-resolvable ambiguity

While adding the request-body capture above, a real user report showed
this exact response:

```
status=500 detail="parent 24 already belongs to 2 different families as
FatherID -- which one this child belongs to can't be determined from a
parent-child relationship alone; create or identify the specific couple
relationship first"
```

`500` is wrong here, and worth being precise about why: it signals "this
server has a bug, something went wrong on our end" -- but this is a
perfectly well-formed request, referencing persons that genuinely exist,
that this server correctly declined to guess about at the time (the
original design's own case 3 -- since superseded; see "A real design
mistake, corrected" in the "Stage 3" section above for why, and for what
replaced it). That's a client-resolvable situation (the error message
itself says how: create or identify the specific couple relationship
first), which is what `400` means, not `500`.

The root cause: `handleCreateRelationships`'s status-code logic only
checked `errors.Is(err, rmdb.ErrNotFound)` to decide between `400` and
`500`, defaulting to `500` for everything else -- and neither of
`CreateParentChildRelationship`'s two "this server won't guess" errors
(an unknown-sex parent, or a parent already in more than one matching-role
family) were wrapped in anything that check could recognize. Unlike
`Person` creation's own "at least one name" validation (caught earlier,
during request-building, well before any database call -- see
`buildNewPerson`, already correctly `400`), this specific ambiguity can
only be discovered by actually querying the database during the create
call itself, so there was no earlier validation step it could have been
caught by instead.

Fixed by adding a second sentinel, `rmdb.ErrAmbiguous` (`internal/rmdb/writes.go`,
alongside `ErrNotFound`), wrapping both of `CreateParentChildRelationship`'s
"won't guess" errors and `resolveCoupleRoles`'s equivalent (a couple
request where the two persons' sexes don't resolve to one Father and one
Mother -- already correctly `400` before this fix, since it's caught
during request-building the same way `Person`'s name check is, but
wrapped for consistency regardless), and checking for it alongside
`ErrNotFound` in `handleCreateRelationships`'s actual status-code
decision. Checked whether this same gap exists anywhere else in the
`~55` other `StatusInternalServerError` call sites across `internal/api`
before concluding it doesn't: every one of them is either a genuine
database/encoding failure (correctly `500`) or a validation check that
already happens before any database call, the same way `Person`'s does
-- `Relationship` creation is the only place in this server where an
error can only be discovered *during* the write itself and still mean
"the request needs to change," not "something broke."

Verified both with a real HTTP round trip reproducing the original
report exactly (confirmed `400`, not `500`, with the same detail message)
and with permanent tests: `internal/rmdb/createparentchild_test.go`'s
existing ambiguity tests were strengthened to assert `errors.Is(err,
ErrAmbiguous)` specifically, not just that *some* error occurred, and
`cmd/server/main_test.go`'s `TestCreateRelationshipsHTTP` gained a
dedicated regression test recreating this exact scenario end to end.

The `ErrAmbiguous`/`400` mapping fixed here remains correct and
unchanged. The specific scenario quoted above -- a parent already in two
families, linked to a *new* child with no other information -- no
longer produces this error at all, though: it was the direct symptom of
the design mistake corrected later in this same section (see "A real
design mistake, corrected," above) and now correctly creates a new
family instead of guessing or rejecting. `ErrAmbiguous`/`400` still
applies, just to the narrower, still-genuinely-ambiguous case that
replaced it -- a child already belonging to more than one existing
family.

### Other messages, converted in place

`log.Printf("warning: ...")` calls throughout `cmd/server`
(`mediafolder_discovery.go`, `writeguard.go`,
`rootsmagic_running_check.go`) became `slog.Warn` with the specifics
(path, error, version numbers, ...) as structured attributes rather than
interpolated into the message string. The three
"ignoring unsupported field(s)" notices in `internal/api/handlers.go`
(`Person`/`Relationship`/`Event` write handlers -- see "Write support"
above) were consolidated into one shared `logIgnoredFields` helper,
`slog.Info`, so the message shape stays consistent across all three
rather than drifting independently.
