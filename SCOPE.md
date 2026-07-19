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
- **`Collections` / `Records` / `Artifacts`** — these model a hosted archive of
  scanned records and user-contributed collections. RootsMagic doesn't have an
  equivalent concept; your multimedia is closer to `SourceDescription`s than to
  `Records`.
- **Atom search-result feeds** (`Person Search Results`, `Place Search Results`) — a
  real search implementation (indexing, ranking, paging as Atom/JSON feeds) is a
  project in itself. `GET /persons?name=...` is provided instead, as a simpler
  non-Atom filter, and is a natural place to grow real search later.
- **Write operations** — you asked for read-only. RootsMagic's own file locking and
  UI assumptions make concurrent external writes risky; if you want write support
  later, it should go through RootsMagic's documented update patterns and probably
  a `-write` flag that's off by default.

### What is included

The resources that map directly onto what's actually in a RootsMagic file, and that
are useful for read access from another tool (a family tree viewer, a static site
generator, a chatbot, etc.):

`Person`, `Persons`, `Person Parents`, `Person Children`, `Person Spouses`,
`Ancestry Results`, `Descendancy Results`, `Relationship`, `Relationships`,
`Place Description`, `Place Descriptions`, `Source Description`, `Source Descriptions`.

Each `Person` embeds its conclusions (names, gender, facts) directly in the same
response, per the spec's fallback rule in Section 4.10.5 ("If no link to
`conclusions` is provided, the list of conclusions MUST be included in the original
request"). This avoids needing separate `/persons/{id}/conclusions` endpoints for a
read-only server.

## RootsMagic version handling

The data dictionary shows that `PersonTable`, `NameTable`, `FamilyTable`,
`ChildTable`, `EventTable`, `FactTypeTable`, `PlaceTable`, `SourceTable`,
`CitationTable`, `CitationLinkTable`, and `RoleTable` are unchanged between
RootsMagic 7 and RootsMagic 10/11 for every column this server reads. So rather than
branching logic on a detected version number, `internal/rmdb` does two things:

1. **Discovers columns dynamically** with `PRAGMA table_info(...)` at startup, and
   only selects columns it knows how to use. If a future RootsMagic version adds
   columns, nothing breaks. If a column this server wants is missing, it's treated
   as absent/zero-value rather than causing an error.
2. **Reports a best-effort version string** (`GET /` includes a `rootsMagicSchema`
   hint) based on which optional tables exist (e.g. `DNATable`, `FamilySearchTable`,
   `AncestryTable` are later additions) — this is informational only and doesn't
   gate functionality.

RootsMagic 4–6 files: the dictionary marks several columns used here (`ChildTable`,
`CitationLinkTable.OwnerType`, `PlaceTable.Reverse`, etc.) as introduced later than
RM4-6, and pre-RM7 RootsMagic used a different citation-linking model entirely
(`BiblioTable`/`Master Source` structure rather than `CitationTable`/
`CitationLinkTable`). Rather than silently returning incomplete or wrong data
against those older files, the server does a startup capability check and exits
with a clear error naming the missing table/column if it opens a pre-RM7 file. If
you need RM4–6 support, that's a distinct schema mapping and would be a good
follow-up.

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

So: `Open()` uses `file:%s?mode=ro&_pragma=query_only(1)` --
`mode=ro` gives genuine, OS/engine-level read-only access (a write fails
with `SQLITE_READONLY`, and a missing path fails to open rather than
silently creating an empty file), and `_pragma=query_only(1)` is kept on
top purely as defense in depth. This is functionally equivalent to what a
cgo-based driver like `mattn/go-sqlite3` gives you with the same DSN
convention -- there's no read-only tradeoff for choosing the pure-Go driver
here after all.

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
driver name `"sqlite"`, native DSN passthrough for `mode=ro`, the
`_pragma=` convention) backed by a different, reachable engine
(`mattn/go-sqlite3`) underneath. That stub is scaffolding for this
project's own development, not a submission artifact, and isn't part of
the delivered code. Independently, the DSN/collation-registration approach
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
  this server reads (`Person`, `Relationship`, `Place Description`,
  `Source Description`) -- the full spec defines create/update/delete
  transitions for these; this server is read-only by design (see "Why
  'core resources, read-only'" above), so a write attempt is a
  deliberately-unimplemented feature, not a malformed request.
- **Resource families never read or written at all**: `Collections`,
  `Records`, `Artifacts`, `Agents`, `Events`, `Person Matches`, and
  OAuth2 (`/oauth2/token`). These get explicit stub routes at their
  conventional paths.

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
