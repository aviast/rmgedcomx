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
- **Atom search-result feeds** (`Person Search Results`, `Place Search Results`) — a
  real search implementation (indexing, ranking, paging as Atom/JSON feeds) is a
  project in itself. `GET /persons?name=...` is provided instead, as a simpler
  non-Atom filter, and is a natural place to grow real search later.
- **Write operations** — off by default, and the vast majority of resources are
  still read-only even when write support is enabled. See the "Write support"
  section below for what's actually implemented, staged incrementally, and why.

`Collections` / `Collection`, `Artifacts`, and `Events` / `Event` **are**
implemented -- see the "Multiple databases / Collections", "Multimedia", and
"Events" sections below for why and how.

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
marriage with witnesses).

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

## RootsMagic version handling

RootsMagic 7 or later is required. The data dictionary shows that `PersonTable`,
`NameTable`, `FamilyTable`, `ChildTable`, `EventTable`, `FactTypeTable`,
`PlaceTable`, `SourceTable`, `CitationTable`, `CitationLinkTable`, and `RoleTable`
are unchanged between RootsMagic 7 and RootsMagic 10/11 for every column this
server reads. So rather than branching logic on a detected version number,
`internal/rmdb` does two things:

1. **Discovers columns dynamically** with `PRAGMA table_info(...)` at startup, and
   only selects columns it knows how to use. If a future RootsMagic version adds
   columns, nothing breaks. If a column this server wants is missing, it's treated
   as absent/zero-value rather than causing an error.
2. **Reports a best-effort version string** in the startup log line (based on which
   optional tables exist, e.g. `DNATable`, `FamilySearchTable`, `AncestryTable` are
   later additions) -- this is purely informational and doesn't gate functionality.

If a required table or column is missing -- which in practice means a pre-RM7
file, since pre-RM7 RootsMagic used a substantially different schema -- `Open`
fails at startup with a clear error naming what's missing, rather than silently
returning incomplete or wrong data. RootsMagic 6 and earlier are out of scope for
this server and not a planned addition.

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
