# rmgedcomx

A lightweight, read-only RESTful API server, written in Go, that exposes the contents of a
[RootsMagic](https://rootsmagic.com/) genealogy database (SQLite) through a subset of the
[GEDCOM X RS](https://github.com/FamilySearch/gedcomx-rs) specification.

## Scope

This server implements the **core genealogy resources** of GEDCOM X RS, as a read-only
(`GET`-only) API:

- `Collections` / `Collection`
- `Persons` / `Person`
- `Person Parents` / `Person Children` / `Person Spouses`
- `Ancestry Results` / `Descendancy Results`
- `Relationships` / `Relationship`
- `Place Descriptions` / `Place Description`
- `Source Descriptions` / `Source Description`
- `Artifacts` (scanned certificates, photos, and other multimedia)

Not implemented (out of scope for this build): OAuth2 authentication,
`Records`, `Agents`, `Events`, Atom search-result feeds, and any write operations (`POST`/`DELETE`).
See [SCOPE.md](./SCOPE.md) for details and rationale, and for notes on extending the server
later if you need any of this.

## RootsMagic schema

RootsMagic 7 or later is required. The table and column layout is effectively unchanged
from RootsMagic 7 through RootsMagic 10/11 for the tables this server reads
(`PersonTable`, `NameTable`, `FamilyTable`, `ChildTable`, `EventTable`,
`FactTypeTable`, `PlaceTable`, `SourceTable`, `CitationTable`, `CitationLinkTable`, `RoleTable`).
The server queries columns by name (not position) and only requires the columns it actually
uses, so it works unmodified against RootsMagic 7–11 files. See [SCOPE.md](./SCOPE.md) for
what happens if you point it at an older file.

## Build

Requires Go 1.22+. No C compiler needed — this uses
[`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite), a CGo-free, pure-Go
SQLite implementation (mirrored on GitHub at
[modernc-org/sqlite](https://github.com/modernc-org/sqlite)), so
cross-compiling and building on machines without a C toolchain both just
work.

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
./rmgedcomx -db Gould.rmtree -db Family.rmtree -db royal92.rmtree
```

(`royal92.rmtree` is included in this repo as a ready-to-try sample -- a
public-domain GEDCOM of European royalty, imported into RootsMagic.)

On startup, the server prints a table mapping each collection's id to its
title and source file:

```
Collections available this session:
COLLECTION ID                     TITLE                               DATABASE FILE
jean-valerie-gould-gould          Jean Valerie Gould (Gould)          Gould.rmtree
charlotte-mae-henry-family        Charlotte Mae Henry (Family)        Family.rmtree
victoria-hanover-royal92          Victoria Hanover (royal92)          royal92.rmtree
```

**A collection's id is not guaranteed to be the same across restarts** -- it's
derived from RootsMagic's "Home Person" setting (which a user can change) and
the filename (which can be renamed, copied, or restored from backup), chosen
to be human-recognizable rather than durable. **No client should persist a
collection id across sessions** -- discover fresh via `GET /collections` every
time a client starts (as the example Python client does), and use the startup
table above to confirm, as a human, which id corresponds to which database for
the session about to start. See [SCOPE.md](./SCOPE.md#multiple-databases--collections)
for the full reasoning.

Then browse, e.g.:

```
curl http://localhost:8080/
curl http://localhost:8080/collections
curl http://localhost:8080/collections/jean-valerie-gould-gould
curl http://localhost:8080/collections/jean-valerie-gould-gould/persons?limit=20
curl http://localhost:8080/collections/jean-valerie-gould-gould/persons/P1
curl http://localhost:8080/collections/jean-valerie-gould-gould/persons/P1/ancestry?generations=4
curl http://localhost:8080/collections/jean-valerie-gould-gould/relationships/F3
curl http://localhost:8080/collections/jean-valerie-gould-gould/places/12
curl http://localhost:8080/collections/jean-valerie-gould-gould/source-descriptions/5
curl http://localhost:8080/collections/jean-valerie-gould-gould/artifacts?limit=20
curl http://localhost:8080/collections/jean-valerie-gould-gould/artifacts/M1
curl http://localhost:8080/collections/jean-valerie-gould-gould/artifacts/M1/content -o photo.jpg
```

All responses are `application/x-gedcomx-v1+json` (GEDCOM X JSON, with the GEDCOM X RS
extensions such as `living`, `display`, and `links`), except `.../artifacts/{id}/content`,
which streams the raw file with its actual `Content-Type`. `GET /` returns the `Collections`
list (the discovery root -- see [SCOPE.md](./SCOPE.md#multiple-databases--collections) for
why), so a client that only knows the base URL can find everything else from there. Every
`Person`'s and `Fact`'s `sources` array includes attached photos/certificates alongside
bibliographic sources -- see [SCOPE.md](./SCOPE.md#multimedia) for how RootsMagic actually
attaches media (it's usually via the citation, not the person or fact directly) and for the
real limits of resolving a `MediaPath` to a file on disk (cloud-drive letters, RootsMagic's
"Media Folder" setting, and items that turn out to be external links rather than local
files).

Anything in the GEDCOM X RS spec this server intentionally doesn't implement
(writes, `Records`, `Agents`, `Events`, `Person Matches`, OAuth2) returns
`501 Not Implemented` rather than a bare `404` -- see [SCOPE.md](./SCOPE.md)
for the full list.

### Flags

| flag | default | description |
|------|---------|--------------|
| `-db` | *(required, repeatable)* | Path to a RootsMagic `.rmtree`/`.rmgc` SQLite file; pass multiple times to serve multiple databases, each as its own Collection |
| `-addr` | `:8080` | Address to listen on |
| `-base-url` | `http://localhost:8080` | Base URL used to build absolute links in responses |
| `-media-folder` | *(none)* | RootsMagic's configured "Media Folder", if any multimedia paths use it (see [SCOPE.md](./SCOPE.md#multimedia)); shared by every `-db`, since it's a RootsMagic-installation-wide setting, not a per-database one |
| `-default-generations` | `4` | Default number of generations for ancestry/descendancy queries |
| `-max-page-size` | `200` | Maximum number of entries returned by a single paged request |

The database is never written to: `Open()` uses SQLite's native `mode=ro` URI
parameter, which `modernc.org/sqlite` honors natively since it transpiles the
real SQLite engine rather than reimplementing URI handling -- a write attempt
fails at the SQL engine level regardless of file permissions. See
[SCOPE.md](./SCOPE.md) for how this was verified.
