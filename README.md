# rmgedcomx

A RESTful API server, written in Go, that exposes the contents of a
[RootsMagic](https://rootsmagic.com/) genealogy database (SQLite) through a subset of the
[GEDCOM X RS](https://github.com/FamilySearch/gedcomx-rs) specification. Read-only by
default; a growing set of write operations is available via `-write` -- see
[SCOPE.md](./SCOPE.md#write-support) for the full account of what's implemented, how it was
verified against real RootsMagic data, and every design decision behind it.

## Scope

### Reading

This server implements the **core genealogy resources** of GEDCOM X RS as `GET`-only
resources:

- `Collections` / `Collection`
- `Persons` / `Person`
- `Person Parents` / `Person Children` / `Person Spouses`
- `Ancestry Results` / `Descendancy Results`
- `Relationships` / `Relationship`
- `Place Descriptions` / `Place Description`
- `Source Descriptions` / `Source Description`
- `Artifacts` (scanned certificates, photos, and other multimedia)
- `Events` / `Event` (shared events with multiple participants, e.g. a marriage with witnesses)
- `Person Search Results` / `Place Search Results` (Atom/JSON-based query search)

### Writing (`-write`)

- **Create a person or several** (`POST /persons`) -- names (including multiple names per
  person, an explicit `preferred` name or the GEDCOM X convention of the first name in the
  list being preferred, and a nickname attached to the primary name rather than a row of its
  own), facts (with a `formal` date, a `Date.original` fallback parsed from GEDCOM 5.x syntax
  when `formal` is absent or invalid, a place, and/or a free-text `value` -- e.g. `Occupation`
  or `Religion`), and gender. See [SCOPE.md](./SCOPE.md#creating-person-records) for the
  full account of what's supported, and [HISTORY.md](./HISTORY.md) for several real bugs
  found and fixed by checking actual RootsMagic behavior rather than assuming it.
- **Create a `Couple` or `ParentChild` relationship, or several** (`POST /relationships`) --
  resolves Father/Mother roles from each person's own recorded sex, reuses or completes an
  existing family rather than creating a duplicate when the same two people are already
  paired, and never guesses which of a parent's existing families a bare parent-child link
  belongs to (a real design mistake, corrected during development -- see
  [HISTORY.md](./HISTORY.md#a-design-mistake-in-relationship-creation-corrected)). Supports marking a `ParentChild`
  relationship as adoptive, step, or foster via GEDCOM X's own dedicated fact types
  (`AdoptiveParent`, `StepParent`, `FosterParent`, `GuardianParent`, `BiologicalParent`).
- **Attach or replace linked media** on an existing `Person`, `Event`, `Relationship`
  (`Family`), `Place Description`, or `Source Description` (`POST /persons/{id}`,
  `POST /events/{id}`, `POST /relationships/{id}`, `POST /places/{id}`,
  `POST /source-descriptions/{id}`) -- this is the *only* thing these five endpoints support
  writing; no other fields on an existing resource can be changed through this API yet.
- **Correct an artifact's file path** (`POST /artifacts/{id}`) -- for when RootsMagic's own
  stored `MediaPath` no longer resolves to a real file (moved, renamed, or on a different
  machine).

Every write is validated before anything is written, rejects unrecognized JSON fields
outright rather than silently ignoring them, and is verified against real, captured
RootsMagic behavior wherever that was possible -- see [SCOPE.md](./SCOPE.md) for the specifics
and the evidence behind each one, including a few places where this project's own earlier
claims about RootsMagic's behavior turned out to be wrong and were corrected in place rather
than left standing.

**Not implemented**, and out of scope for this build: `DELETE` of any kind; changing a
`Person`'s or `Event`'s own names, facts, or gender once created (only linked media can be
updated on an existing one); OAuth2 authentication; `Records`, `Agents`, and `Person
Matches`. See [SCOPE.md](./SCOPE.md) for details, rationale, and notes on
extending the server later if you need any of this.

## RootsMagic schema

RootsMagic 8 or later is required. The table and column layout is effectively unchanged
from RootsMagic 8 through RootsMagic 10/11 for the tables this server reads and writes
(`PersonTable`, `NameTable`, `FamilyTable`, `ChildTable`, `EventTable`, `FactTypeTable`,
`PlaceTable`, `SourceTable`, `CitationTable`, `CitationLinkTable`, `RoleTable`,
`MultimediaTable`, `MediaLinkTable`, `ConfigTable`). The server queries columns by name
(not position) and only requires the columns it actually uses, so it works unmodified
against RootsMagic 8–11 files. RootsMagic 7 is rejected specifically because it has no
modification-timestamp column at all on several of these tables, which this server's write
support depends on -- see [SCOPE.md](./SCOPE.md) for the full account, and for what happens
if you point it at an older file.

## Build

Requires Go 1.25 or later (a dependency of the test-only `testify` module this project
uses requires it; the server itself has no 1.25-specific requirement of its own). No C
compiler needed -- this uses [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite), a
CGo-free, pure-Go SQLite implementation (mirrored on GitHub at
[modernc-org/sqlite](https://github.com/modernc-org/sqlite)), so cross-compiling and
building on machines without a C toolchain both just work.

```sh
go mod tidy   # first time only: fetches modernc.org/sqlite and friends, fills in go.sum
go build -o rmgedcomx ./cmd/server
```

## Run

```sh
./rmgedcomx -db /path/to/YourTree.rmtree -addr :8080
```

Or serve several databases at once -- `-db` is repeatable, and each one becomes
its own, fully independent `Collection`:

```sh
./rmgedcomx -db Tree1.rmtree -db Tree2.rmtree -db royal92.rmtree
```

(`royal92.rmtree` is included in this repo as a ready-to-try sample -- a
public-domain GEDCOM of European royalty, imported into RootsMagic. It's the
only sample data included, and every example in this README and in
[SCOPE.md](./SCOPE.md) is generated from it, deliberately -- there's no
privacy concern in publishing exact ids or example responses for people who
died over a century ago. The original GEDCOM file it was imported from is
also included, at `testdata/royal92.ged`, and has independently been a
repeated, genuine source of real edge cases this project's write support was
built and corrected against -- see [SCOPE.md](./SCOPE.md) for several of
them.)

On startup, the server prints a table mapping each collection's id to its
title, source file, and RootsMagic's own database `UniqueID` -- one row per
`-db` flag. Here's the real output for `royal92.rmtree` on its own:

```
Read-only (pass -write to enable write support).

Collections available this session:
COLLECTION ID             TITLE                       DATABASE FILE   UNIQUE ID
victoria-hanover-royal92  Victoria Hanover (royal92)  royal92.rmtree  1474AA04B27542E9B980E4DDBD107FFAC8BD
```

**A collection's id is not guaranteed to be the same across restarts** -- it's
derived from RootsMagic's "Home Person" setting (which a user can change) and
the filename (which can be renamed, copied, or restored from backup), chosen
to be human-recognizable rather than durable. **No client should persist a
collection id across sessions** -- discover fresh via `GET /collections` every
time a client starts, and use the startup table above to confirm, as a human,
which id corresponds to which database for the session about to start. See
[SCOPE.md](./SCOPE.md#multiple-databases--collections) for the full reasoning.

### Reading

All real, verified against `royal92.rmtree` -- P1 is Victoria Hanover; F1 is her marriage
to Albert, whose Marriage fact `E5049` is the same id as `.../events/E5049` -- see
[SCOPE.md](./SCOPE.md#events). That event's `roles` include real witnesses already in the
database, like P219, Queen Adelaide, alongside her bridesmaids, who aren't; and its
`sources` include `M1`, an actual scan of the painting of the wedding:

```
curl http://localhost:8080/
curl http://localhost:8080/collections
curl http://localhost:8080/collections/victoria-hanover-royal92
curl http://localhost:8080/collections/victoria-hanover-royal92/persons?limit=20
curl http://localhost:8080/collections/victoria-hanover-royal92/persons/P1
curl http://localhost:8080/collections/victoria-hanover-royal92/persons/P1/ancestry?generations=4
curl http://localhost:8080/collections/victoria-hanover-royal92/relationships/F1
curl http://localhost:8080/collections/victoria-hanover-royal92/places/PL261
curl http://localhost:8080/collections/victoria-hanover-royal92/source-descriptions/S1
curl http://localhost:8080/collections/victoria-hanover-royal92/events?limit=20
curl http://localhost:8080/collections/victoria-hanover-royal92/events/E5049
curl http://localhost:8080/collections/victoria-hanover-royal92/artifacts?limit=20
curl http://localhost:8080/collections/victoria-hanover-royal92/artifacts/M1
curl http://localhost:8080/collections/victoria-hanover-royal92/artifacts/M1/content -o wedding.jpg
```

Responses use `application/x-gedcomx-v1+json` (GEDCOM X JSON), the one representation this
server produces -- a request with an `Accept` header that excludes it gets `406 Not
Acceptable` rather than JSON anyway, and every response carries `Vary: Accept` to say so.
The one exception is `.../artifacts/{id}/content`, which streams the raw file with its own
real `Content-Type` and isn't part of the GEDCOM X JSON profile at all. `GET /` returns the
`Collections` list (the discovery root -- see [SCOPE.md](./SCOPE.md#multiple-databases--collections)
for why), so a client that only knows the base URL can find everything else from there. Every
`Person`'s and `Fact`'s `sources` array includes attached photos/certificates alongside
bibliographic sources -- see [SCOPE.md](./SCOPE.md#multimedia) for how RootsMagic actually
attaches media (it's usually via the citation, not the person or fact directly) and for the
real limits of resolving a `MediaPath` to a file on disk (cloud-drive letters, RootsMagic's
"Media Folder" setting, and items that turn out to be external links rather than local
files).

### Writing (`-write`)

Create a person with two facts, one carrying only a free-text `value` rather than a date or
place (`Occupation`):

```sh
curl -X POST http://localhost:8080/collections/victoria-hanover-royal92/persons \
  -H 'Content-Type: application/x-gedcomx-v1+json' \
  -d '{
    "persons": [{
      "names": [{"nameForms": [{"parts": [
        {"type": "http://gedcomx.org/Given", "value": "Joseph Patrick"},
        {"type": "http://gedcomx.org/Surname", "value": "Kennedy"}
      ]}]}],
      "gender": {"type": "http://gedcomx.org/Male"},
      "facts": [
        {"type": "http://gedcomx.org/Birth", "date": {"formal": "+1888-09-06"}, "place": {"original": "Boston, MA"}},
        {"type": "http://gedcomx.org/Occupation", "value": "Bank President, Ambassador"}
      ]
    }]
  }'
```

A single created resource gets `201 Created` with a `Location` header pointing at it; several
in one request get `204 No Content` instead, per the RS spec's own paging/collection
semantics for a multi-resource `POST` -- see [SCOPE.md](./SCOPE.md) for the exact reasoning.
Link two existing people as a couple, then link a child to each of them separately (a bare,
single-parent link never assumes which of a parent's existing families it belongs to -- see
[SCOPE.md](./SCOPE.md#creating-relationship-records-couple-and-parentchild)):

```sh
curl -X POST http://localhost:8080/collections/victoria-hanover-royal92/relationships \
  -H 'Content-Type: application/x-gedcomx-v1+json' \
  -d '{"relationships": [{
    "type": "http://gedcomx.org/Couple",
    "person1": {"resourceId": "P1"},
    "person2": {"resourceId": "P2"}
  }]}'
```

Writing to a resource in a way this server doesn't support (`POST` to a resource whose
own fields can't be changed, or any method this server doesn't implement at all for that
path) gets a `405 Method Not Allowed` with a correct `Allow` header; a URL this server
doesn't implement at all (`Records`, `Agents`, `Person Matches`, OAuth2) gets a plain `404`
-- see [SCOPE.md](./SCOPE.md#http-semantics) for the reasoning. Error bodies follow
[RFC 7807](https://www.rfc-editor.org/rfc/rfc7807) Problem Details
(`application/problem+json`: `title`, `status`, `detail`), not a bespoke shape, and a request
this server rejects for any reason names the actual problem in `detail` rather than a generic
message.

### Flags

| flag | default | description |
|------|---------|--------------|
| `-db` | *(required, repeatable)* | Path to a RootsMagic `.rmtree`/`.rmgc` SQLite file; pass multiple times to serve multiple databases, each as its own Collection |
| `-addr` | `:8080` | Address to listen on |
| `-base-url` | `http://localhost:8080` | Base URL used to build absolute links in responses |
| `-media-folder` | *(none)* | RootsMagic's configured "Media Folder", if any multimedia paths use it (see [SCOPE.md](./SCOPE.md#multimedia)); shared by every `-db`, since it's a RootsMagic-installation-wide setting, not a per-database one. **Cannot be combined with `-write`** -- write mode determines the Media Folder itself, from RootsMagic's own configuration (see below) |
| `-write` | `false` | Enable write support -- see [SCOPE.md](./SCOPE.md#write-support) for exactly what that covers. Refuses to start if RootsMagic itself appears to be running. **Windows and macOS only**: reads the Media Folder from RootsMagic's own `RootsMagicUser.xml` (the macOS location is based on community reports, not independently confirmed -- see SCOPE.md) |
| `-default-generations` | `4` | Default number of generations for ancestry/descendancy queries |
| `-max-page-size` | `200` | Maximum number of entries returned by a single paged request |
| `-log-level` | `info` | `trace`, `debug`, `info`, `warn`, or `error`. `debug` additionally logs request and response bodies for every failed (4xx/5xx) request; `trace` logs them for every request. The response body is normally the fastest way to see why a failure happened, since it is either this server's own detailed reason, or (if the request never reached this server's own handler code at all, e.g. a write route that doesn't exist because `-write` wasn't passed) Go's own bare text response, which is itself the diagnostic |
| `-log-format` | `text` | `text` or `json`. Logs go to stderr; the startup collection table (above) goes to stdout, so the two are independently redirectable if that's useful |

By default, the database is never written to: `Open()` uses SQLite's native `mode=ro` URI
parameter, which `modernc.org/sqlite` honors natively since it transpiles the
real SQLite engine rather than reimplementing URI handling -- a write attempt
fails at the SQL engine level regardless of file permissions. See
[SCOPE.md](./SCOPE.md) for how this was verified. With `-write`, this server
makes an automatic timestamped backup of your database before its first write
each session -- but that's a safety net for mistakes *this server* makes, not
a substitute for your own backups, and it does not, and cannot, protect
against RootsMagic itself running at the same time (which `-write` refuses to
start alongside, on Windows).

## Python tools

Two standalone Python scripts live alongside the server in this repo. Neither
is part of rmgedcomx itself or required to run it -- both are separate,
optional clients that talk to a running instance over HTTP, and are useful
for exploring and populating one while testing.

### `gedcomx_browser.py` -- a GUI hypermedia browser

A Tkinter desktop app that browses a running server the way a real GEDCOM X
RS client is meant to: it follows the server's own hypermedia links
(`collections`, `persons`, `parents`/`children`/`spouses`, `person-search`,
...) rather than hardcoding URLs, so using it against this server also
exercises those links for real. It provides a paged, filterable list of
whatever collection/resource you're browsing (the filter box uses this
server's own Atom-based search, `person-search`/`place-search`, when the
current list supports it, falling back to a client-side, non-exact filter
otherwise -- and says plainly which of the two it did, and why, rather than
silently working around a server that's missing or has broken a state the
spec defines), a person detail view, interactive ancestry and descendancy
tree visualizations, and a place tab with an optional embedded OpenStreetMap
view. Back/forward navigation works like a regular browser.

Set up its dependencies (just `tkintermapview`, for the optional map view --
the app still runs fine without it, just without maps) with either:

```sh
pip install -r requirements.txt
```

or, for a reproducible conda environment:

```sh
conda env create -f environment.yml
conda activate rmgedcomx
```

Then run it and point it at a running server from its own connection dialog
(`http://localhost:8080` by default):

```sh
python gedcomx_browser.py
```

### `gedcom_to_gedcomx.py` -- import a GEDCOM 5.x file over the write API

A command-line importer: parses a standard GEDCOM 5.x (`.ged`) file and
uploads it to a running server via `POST /persons` and `POST /relationships`
-- the same write API described above, not a separate mechanism, so it needs
`-write` enabled on the target server. It discovers those two endpoints the
same hypermedia way the browser does (fetches the server's root, then reads
the first collection's own `persons`/`relationships` links), rather than
assuming a fixed URL shape. Per family, it creates a `Couple` relationship
(carrying `Marriage`/`Divorce`/`Annulment`/`Engagement`/`Separation` facts,
where present in the file) and a `ParentChild` relationship to each child
from each parent separately -- one `POST` per parent, matching how this
server's own `ParentChild` creation is designed to be used (see
[SCOPE.md](./SCOPE.md#creating-relationship-records-couple-and-parentchild))
-- carrying an `AdoptiveParent`/`BiologicalParent`/`FosterParent`/
`StepParent`/`GuardianParent` fact when the file records a pedigree type for
that child.

```sh
pip install requests
python gedcom_to_gedcomx.py <gedcom-file> [server-url]
```

`server-url` defaults to `http://localhost:8080/`. This project's own
`testdata/royal92.ged` -- the original GEDCOM file `royal92.rmtree` was
imported from -- is a ready-to-try example; point it at a fresh, empty
database (`testdata/empty.rmtree`) running with `-write` to see it populate
one end to end.
