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
- **Write operations** — you asked for read-only. RootsMagic's own file locking and
  UI assumptions make concurrent external writes risky; if you want write support
  later, it should go through RootsMagic's documented update patterns and probably
  a `-write` flag that's off by default. See "SQLite driver" below for how the
  current read-only enforcement is deliberately centralized in one place to make
  that easy to add later.

`Collections` / `Collection` and `Artifacts` **are** implemented -- see the
"Collection" and "Multimedia" sections below for why and how.

### What is included

The resources that map directly onto what's actually in a RootsMagic file, and that
are useful for read access from another tool (a family tree viewer, a static site
generator, a Digital Asset Management tool, a chatbot, etc.):

`Collection`, `Collections`, `Person`, `Persons`, `Person Parents`, `Person Children`,
`Person Spouses`, `Ancestry Results`, `Descendancy Results`, `Relationship`,
`Relationships`, `Place Description`, `Place Descriptions`, `Source Description`,
`Source Descriptions`, `Artifacts` (backed by `MultimediaTable` -- scanned
certificates, photos, and similar).

Each `Person` embeds its conclusions (names, gender, facts) directly in the same
response, per the spec's fallback rule in Section 4.10.5 ("If no link to
`conclusions` is provided, the list of conclusions MUST be included in the original
request"). This avoids needing separate `/persons/{id}/conclusions` endpoints for a
read-only server.

## Collection

The `Collection` state (RS spec Section 4.5, data type defined in the
[GEDCOM X Record Extensions](https://github.com/FamilySearch/gedcomx-record/blob/master/specifications/record-specification.md#collection))
is the intended discovery root of a GEDCOM X RS API: it's the one state formally
specified to carry `persons`, `relationships`, `source-descriptions`, and
`subcollections` links (Section 4.5.4's transitions table), which is exactly the
set of top-level resources this server exposes. It was left out of the first cut of
this project on the assumption that "a collection of genealogical data" was a
better fit for a hosted, multi-tree, multi-contributor service than for a single
RootsMagic file opened directly off disk -- but that reasoning doesn't hold up: a
`Collection` doesn't have to be a big, sprawling archive. The spec's `Collection`
data type is just `id` / `title` / `content` (counts by resource type) / `links`,
which maps onto a single RootsMagic file perfectly well, and a compliant client
has nowhere else to start from -- without it, there's no spec-defined way to
discover that this server has `persons`, `relationships`, `place-descriptions`, and
`source-descriptions` at all.

So: one RootsMagic file == one `Collection`, with a fixed id (`"main"`, since
there's never more than one). It's addressable three ways, all returning the same
content:

- `GET /` -- the root, for a client that only knows the base URL.
- `GET /collections` -- the formal `Collections` (list) state; always a single-item
  list, since this server never manages more than one file at a time.
- `GET /collections/main` -- the formal `Collection` (single) state, individually
  addressable per the spec.

`content` counts (`CollectionStats` in `internal/rmdb/queries.go`) are computed with
plain `COUNT(*)` SQL, not by materializing the full resource lists, so hitting `/`
stays cheap even on a large tree. The relationship count deliberately mirrors the
exact logic `handleRelationships` uses to build the real list (one `Couple`
relationship per family with both parents present, plus one `ParentChild`
relationship per parent-child pair) so the number a client sees here always matches
what `GET /relationships` actually returns.

One link is a deliberate, documented departure from the spec: `place-descriptions`.
The formal transitions table for `Collection` (Section 4.5.4) doesn't define a
plural rel for the Place Descriptions list state, and neither does the master
link-relation table (Section 5.2) -- there's a singular `description` rel for one
place description, but nothing for the list. Rather than leave `/places` completely
undiscoverable from the Collection, `place-descriptions` is added anyway, following
the `source-descriptions` naming convention, under Section 4.5.4's explicit
allowance: "other transitions... is RECOMMENDED where applicable." A strict client
that only walks formally-specified rels won't find `/places` this way; it'll need
to know the URL, same as before this change.

The `title` shown is the `-db` file's name (without extension) by default, or
whatever `-title` is set to.

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
  hint settings, and so on. It was checked exhaustively against two real
  files, and neither contains anything resembling a media folder path.
  That's not a gap in this server's parsing -- the Media Folder setting
  genuinely isn't part of the `.rmtree` file's data model at all. It makes
  sense once you think about it: a folder path is inherently specific to
  the machine it's configured on, so it almost certainly lives in a local,
  per-installation setting (most likely the Windows Registry, or an INI
  file next to the RootsMagic executable, given RootsMagic's Delphi/Windows
  heritage) rather than travelling with a database file that gets copied,
  shared, or opened on a different computer. No amount of blob-parsing
  could recover a value that was never written to the file -- `-media-folder`
  isn't a workaround for an unparsed format, it's the only way this
  information can reach this server at all. If any `MediaPath` in your file
  uses `?`, pass the folder explicitly with `-media-folder`; without it,
  those items resolve with a clear error (`GET .../content` returns 500
  naming the problem) rather than silently pointing at the wrong place.
- **A Windows absolute path (a drive letter) can't be resolved on a
  non-Windows host, full stop** -- `G:\My Drive\...` means nothing on Linux or
  macOS regardless of how cleverly it's parsed. This server passes such paths
  through as-is (best effort: if you're running the server on Windows itself,
  or the drive is genuinely mounted at that letter, it'll work) and returns a
  clear 404 naming the exact resolved path it tried, rather than a confusing
  generic error, when the file isn't actually there.

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

## HTTP 501 for unimplemented spec surface

Anything in the GEDCOM X RS spec that this server deliberately doesn't
implement returns `501 Not Implemented` with a small JSON body (`error`,
`detail`, `seeAlso`), rather than a generic `404`, so a client can tell
"this exists in the spec but isn't built here" apart from "this URL is
just wrong." Two cases, both wired up in
`internal/api/server.go:registerNotImplemented`:

- **Write transitions** (`POST`/`PUT`/`PATCH`/`DELETE`) on the resources
  this server reads (`Collection`, `Person`, `Relationship`, `Place
  Description`, `Source Description`, `Artifacts`) -- the full spec defines
  create/update/delete transitions for these; this server is read-only by
  design (see "Why 'core resources, read-only'" above), so a write attempt
  is a deliberately-unimplemented feature, not a malformed request.
- **Resource families never read or written at all**: `Records`,
  `Agents`, `Events`, `Person Matches`, and OAuth2 (`/oauth2/token`).
  These get explicit stub routes at their conventional paths.

A genuinely unrecognized path (anything not in the spec and not one of
these stubs) still returns a plain `404`, matching ordinary REST API
behavior -- this took one bug fix to get right: `net/http`'s router
treats a bare `"/"` pattern as a catch-all for every unmatched path, not
just the literal root, so the root handler is registered as `"GET /{$}"`
(Go 1.22's exact-match syntax) instead.

## Pagination

`Persons`, `Relationships`, `Place Descriptions`, and `Source Descriptions` support
`?limit=` and `?offset=`, capped by `-max-page-size`. This is a simpler mechanism
than the spec's full paging-as-links model (Section 7) but follows its spirit
(`first`/`next`/`prev` links are included when applicable).
