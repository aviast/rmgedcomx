# rmgedcomx

A lightweight, read-only RESTful API server, written in Go, that exposes the contents of a
[RootsMagic](https://rootsmagic.com/) genealogy database (SQLite) through a subset of the
[GEDCOM X RS](https://github.com/FamilySearch/gedcomx-rs) specification.

## Scope

This server implements the **core genealogy resources** of GEDCOM X RS, as a read-only
(`GET`-only) API:

- `Persons` / `Person`
- `Person Parents` / `Person Children` / `Person Spouses`
- `Ancestry Results` / `Descendancy Results`
- `Relationships` / `Relationship`
- `Place Descriptions` / `Place Description`
- `Source Descriptions` / `Source Description`

Not implemented (out of scope for this build): OAuth2 authentication, `Collections`,
`Records`, `Artifacts`, Atom search-result feeds, and any write operations (`POST`/`DELETE`).
See [SCOPE.md](./SCOPE.md) for details and rationale, and for notes on extending the server
later if you need any of this.

## RootsMagic schema

RootsMagic has stored its data in a SQLite database since RootsMagic 4. The table and column
layout is effectively unchanged from RootsMagic 7 through RootsMagic 10/11 for the tables this
server reads (`PersonTable`, `NameTable`, `FamilyTable`, `ChildTable`, `EventTable`,
`FactTypeTable`, `PlaceTable`, `SourceTable`, `CitationTable`, `CitationLinkTable`, `RoleTable`).
The server queries columns by name (not position) and only requires the columns it actually
uses, so it works unmodified against RootsMagic 7–11 files. See [SCOPE.md](./SCOPE.md) for how
older files (RootsMagic 4–6) are handled.

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

Then browse, e.g.:

```
curl http://localhost:8080/
curl http://localhost:8080/persons?limit=20
curl http://localhost:8080/persons/P1
curl http://localhost:8080/persons/P1/ancestry?generations=4
curl http://localhost:8080/relationships/F3
curl http://localhost:8080/places/12
curl http://localhost:8080/source-descriptions/5
```

All responses are `application/x-gedcomx-v1+json` (GEDCOM X JSON, with the GEDCOM X RS
extensions such as `living`, `display`, and `links`).

Anything in the GEDCOM X RS spec this server intentionally doesn't implement
(writes, `Collections`, `Records`, `Artifacts`, `Agents`, `Events`, `Person
Matches`, OAuth2) returns `501 Not Implemented` rather than a bare `404` --
see [SCOPE.md](./SCOPE.md) for the full list.

### Flags

| flag | default | description |
|------|---------|--------------|
| `-db` | *(required)* | Path to the RootsMagic `.rmtree`/`.rmgc` SQLite file |
| `-addr` | `:8080` | Address to listen on |
| `-base-url` | `http://localhost:8080` | Base URL used to build absolute links in responses |
| `-default-generations` | `4` | Default number of generations for ancestry/descendancy queries |
| `-max-page-size` | `200` | Maximum number of entries returned by a single paged request |

The database is never written to. `modernc.org/sqlite` doesn't support the
`mode=ro` DSN flag some other drivers do, so this is enforced instead with
`PRAGMA query_only = 1`, set immediately on connect — any write attempt
fails at the SQL engine level regardless of file permissions.
