# rmgedcomx tests

For testing the write functionality of the rmgedcomx server a suite of tests have been run manually
in RootsMagic, as shown in the table below. Any changes to the database have been captured (using
the [sqldiff](https://sqlite.org/sqldiff.html) tool) as a "Golden file" in the `testdata` directories.

The Go tests will perform the same writes via the rmgedcomx API, use sqldiff to compare the modified
database to the original database, and then compare the sqldiff output to the "Golden files" to find
any issues.

Dynamic data -- timestamps and RootsMagic's own opaque FamilySearch/Ancestry identifiers -- can't be
expected to match between these tests, so it's handled two different ways depending on what it means
for this server to get it right:

- **Timestamps** (`UTCModDate`) are masked with a placeholder (`[TIMESTAMP_UPDATED]`) rather than
  compared literally -- this server is expected to write *some* current timestamp, just not
  necessarily the exact same one RootsMagic wrote at capture time.
- **`fsID`/`anID`/`LatLongExact` (Place) and `IsPrivate` (Source)** are stripped from the comparison
  entirely, not masked. This server always writes `0` for these (it doesn't call out to
  FamilySearch/Ancestry, and doesn't reproduce `IsPrivate`'s undocumented behavior -- see
  `internal/rmdb/writes.go`'s own comments, and SCOPE.md's "Write support" section, for the full
  reasoning). Since every place/source in `royal92.rmtree` already has these fields at `0`, a
  before/after diff can't tell whether this server wrote `0` or didn't touch the field at all --
  masking wouldn't fix that, since it would still require the field to appear in the diff at all,
  which it structurally can't when the value never changes. So these fields are verified a different
  way instead: `TestWriteOperations` queries the resulting database directly after each write and
  asserts the value is exactly `0`, independent of `sqldiff` altogether. See `zeroFieldCheck` and
  `verifyZeroFields` in `cmd/server/main_test.go`.

## Requirements

Running the write tests (`TestWriteOperations`) requires `sqldiff` to be available on `PATH`.
It's part of the [SQLite command-line tools](https://sqlite.org/download.html) -- download the
"Precompiled Binaries" for your platform and put `sqldiff`/`sqldiff.exe` somewhere on `PATH`. (Read
tests, `TestReadOperations`, don't need it -- they don't touch a database's contents at all.)

Comparing `RMNOCASE`-collated columns (`PlaceTable.Name`, `SourceTable.Name`, and others) also
requires the [unifuzz](https://github.com/mooredan/unifuzz) collation library, loaded via `sqldiff
--lib`. `unifuzz.dll` (Windows) / `unifuzz.so` (Linux) is checked into `cmd/server/testdata/` in
this repo, so no separate download is needed for it specifically -- just `sqldiff` itself.

The test code picks the right `sqldiff` executable name and `unifuzz` library name for the platform
it's running on automatically (`cmd/server/main_test.go`'s `sqldiffCommand`/`unifuzzLibPath`).
macOS isn't specifically handled -- not tested against, not a supported claim either way.

## Test list

| Test ID | Test Description | Test output |
|---|---|---|
| 1 | `-write` not passed: every existing `GET` endpoint still works, and every write attempt still returns `405 Method Not Allowed` with a correct `Allow` header. | No golden file -- HTTP status/header check only, no database write occurs. |
| 2 | Startup table shows the correct `UNIQUE ID` column value, cross-checked against `ConfigTable`'s `<UniqueID>` independently (e.g. via `sqlite3`). | Manual -- console output at startup, not something `TestReadOperations`/`TestWriteOperations` currently capture. |
| 3 | `-write` passed: `*** WRITE MODE ENABLED ***` banner appears; without it, the plain "Read-only" line appears instead. | Manual -- console output at startup. |
| 4 | `-write` passed while `RootsMagic.exe` is running: server refuses to start with a clear error. Closing RootsMagic and retrying: server starts normally. | Manual -- confirmed working already. |
| 5 | `-write` passed, one write performed: exactly one timestamped backup file appears, byte-identical to the pre-write database (checksum/diff against a copy made before starting the server). | Manual/Go-native -- file existence and checksum check, no golden file. |
| 6 | A second write in the same running session: no second backup file appears. | Manual/Go-native -- file existence check, no golden file. |
| 7 | `POST` a place, `names` only. | **Done** -- `testdata/post_places_name_expected.sql` (currently covers this exact case: name-only change on `PL423`). |
| 8 | `POST` a place, `notes` only. | Needs golden file, e.g. `testdata/post_places_note_expected.sql`. |
| 9 | `POST` a place, `latitude`/`longitude` only. Also confirm the decimal-to-integer conversion is exact (`value × 10,000,000`, no rounding) for a coordinate with several decimal places. | Needs golden file, e.g. `testdata/post_places_coordinates_expected.sql`. |
| 10 | `POST` a place, all four fields (`names`, `notes`, `latitude`, `longitude`) at once. | Needs golden file, e.g. `testdata/post_places_all_fields_expected.sql`. |
| 11 | Partial-update semantics: set a note (one request), then a second request that only sends `names` (omitting `notes`) -- confirm the note from the first request survives untouched. | No golden file needed -- Go-native: two sequential API calls plus a `GET` to confirm, not a RootsMagic-comparable single edit. |
| 12 | `latitude` without `longitude` (or vice versa): `400`, and the place is confirmed unchanged afterward. | No golden file -- request rejected before any write. |
| 13 | Body `id` doesn't match the URL's id: `400`, nothing written. | No golden file. |
| 14 | `POST` to a `PlaceID` that doesn't exist: `404`. | No golden file. |
| 15 | Malformed JSON body: `400`, not a `500` or crash. | No golden file. |
| 16 | Same request as #7, but without `-write`: `405`, database confirmed untouched. | No golden file. |
| 17 | `POST` a source, `titles` only. | **Done** -- `testdata/post_sources_expected.sql`. |
| 18 | `POST` a source, `notes` (→ `Comments`) only. | Needs golden file, e.g. `testdata/post_sources_comments_expected.sql`. |
| 19 | `POST` a source, both `titles` and `notes` at once. | Needs golden file, e.g. `testdata/post_sources_all_fields_expected.sql`. |
| 20 | `POST` a source with a non-empty `citations` array: `400` with the explanatory message; confirm `titles`/`notes` in the *same* request weren't partially applied either (clean all-or-nothing rejection). | No golden file -- request rejected before any write. |
| 21a | `POST` to a `SourceID` that doesn't exist: `404`. | No golden file. |
| 21b | Body `id` doesn't match the URL's id: `400`. | No golden file. |
| 21c | Malformed JSON body: `400`. | No golden file. |
| 21d | Same request as #17, but without `-write`: `405`, database confirmed untouched. | No golden file. |
| 22 | With more than one `-db` open at once, `-write` governs all of them uniformly (one flag, no per-collection override), and a write to collection A doesn't touch collection B's file at all. | No golden file -- confirmed by checksumming/inspecting collection B's file, not by comparing to a RootsMagic edit. |
| 23 | Update a place/source name to something differing only in case from another existing value (`RMNOCASE` round-trip) -- confirm the write and a subsequent read both behave sanely. | Low priority; needs a real case-collision example in the data to be meaningful, may not be worth a dedicated golden file. |

Test IDs match the numbering from the original test list (see project chat history) so they stay
cross-referenceable with that discussion. 7 and 17 already have golden files and Go test cases
wired up (`TestWriteOperations` in `cmd/server/main_test.go`); everything else in the "Needs golden
file" rows above needs the corresponding RootsMagic edit captured and a new test case added
alongside the existing two, following the same pattern.
