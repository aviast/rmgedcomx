# rmgedcomx

A lightweight RESTful API server, written in Go, that exposes the contents of a
[RootsMagic](https://rootsmagic.com/) genealogy database (SQLite) through a subset of the
[GEDCOM X RS](https://github.com/FamilySearch/gedcomx-rs) specification. Read-only by
default; write support for a small, deliberately-limited set of resources is available
via `-write` -- see [SCOPE.md](./SCOPE.md#write-support).

## Scope

This server implements the **core genealogy resources** of GEDCOM X RS. Almost
everything is `GET`-only:

- `Collections` / `Collection`
- `Persons` / `Person`
- `Person Parents` / `Person Children` / `Person Spouses`
- `Ancestry Results` / `Descendancy Results`
- `Relationships` / `Relationship`
- `Place Descriptions` / `Place Description`
- `Source Descriptions` / `Source Description`
- `Artifacts` (scanned certificates, photos, and other multimedia)
- `Events` / `Event` (shared events with multiple participants, e.g. a marriage with witnesses)

Not implemented (out of scope for this build): OAuth2 authentication,
`Records`, `Agents`, Atom search-result feeds, and any write operations (`POST`/`DELETE`).
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
./rmgedcomx -db Tree1.rmtree -db Tree2.rmtree -db royal92.rmtree
```

(`royal92.rmtree` is included in this repo as a ready-to-try sample -- a
public-domain GEDCOM of European royalty, imported into RootsMagic. It's the
only sample data included, and every example in this README and in
[SCOPE.md](./SCOPE.md) is generated from it, deliberately -- there's no
privacy concern in publishing exact ids or example responses for people who
died over a century ago.)

On startup, the server prints a table mapping each collection's id to its
title and source file -- one row per `-db` flag. Here's the real output for
`royal92.rmtree` on its own:

```
Collections available this session:
COLLECTION ID             TITLE                       DATABASE FILE
victoria-hanover-royal92  Victoria Hanover (royal92)  royal92.rmtree
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

Then browse, e.g. (all real, verified against `royal92.rmtree` -- P1 is
Victoria Hanover; F1 is her marriage to Albert, whose Marriage fact
`E5049` is the same id as `.../events/E5049` -- see
[SCOPE.md](./SCOPE.md#events). That event's `roles` include real witnesses
already in the database, like P219, Queen Adelaide, alongside her
bridesmaids, who aren't; and its `sources` include `M1`, an actual scan of
the painting of the wedding):

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

Writing to a resource that exists (anything other than `GET`) gets a `405 Method Not
Allowed` with a correct `Allow` header; a URL this server doesn't implement at all
(`Records`, `Agents`, `Person Matches`, OAuth2) gets a plain `404` -- see
[SCOPE.md](./SCOPE.md#http-semantics) for the reasoning. Error bodies follow
[RFC 7807](https://www.rfc-editor.org/rfc/rfc7807) Problem Details (`application/problem+json`:
`title`, `status`, `detail`), not a bespoke shape.

### Flags

| flag | default | description |
|------|---------|--------------|
| `-db` | *(required, repeatable)* | Path to a RootsMagic `.rmtree`/`.rmgc` SQLite file; pass multiple times to serve multiple databases, each as its own Collection |
| `-addr` | `:8080` | Address to listen on |
| `-base-url` | `http://localhost:8080` | Base URL used to build absolute links in responses |
| `-media-folder` | *(none)* | RootsMagic's configured "Media Folder", if any multimedia paths use it (see [SCOPE.md](./SCOPE.md#multimedia)); shared by every `-db`, since it's a RootsMagic-installation-wide setting, not a per-database one. **Cannot be combined with `-write`** -- write mode determines the Media Folder itself, from RootsMagic's own configuration (see below) |
| `-write` | `false` | Enable write support (see [SCOPE.md](./SCOPE.md#write-support) for exactly what that covers at any given point -- it's being built in small, tested stages, not all at once). Refuses to start if RootsMagic itself appears to be running. **Windows and macOS only**: reads the Media Folder from RootsMagic's own `RootsMagicUser.xml` (the macOS location is based on community reports, not independently confirmed -- see SCOPE.md) |
| `-default-generations` | `4` | Default number of generations for ancestry/descendancy queries |
| `-max-page-size` | `200` | Maximum number of entries returned by a single paged request |

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
