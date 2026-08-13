# rmgedcomx tests

For testing the write functionality of the rmgedcomx server a suite of tests have been run manually
in RootsMagic, as shown in the table below. Any changes to the database have been captured (using
the [sqldiff](https://sqlite.org/sqldiff.html) tool) as a "Golden file" in the `testdata` directories.

The Go tests will perform the same writes via the rmgedcomx API, use sqldiff to compare the modified
database to the original database, and then compare the sqldiff output to the "Golden files" to find
any issues.

Dynamic data -- timestamps, and a handful of fields downstream of a non-deterministic external
lookup (see "Non-deterministic fields" below) -- can't be expected to match between these tests, so
it's handled two different ways depending on what it means for this server to get it right:

- **Timestamps** (`UTCModDate`) are masked with a placeholder (`[TIMESTAMP_UPDATED]`) rather than
  compared literally -- this server is expected to write *some* current timestamp, just not
  necessarily the exact same one RootsMagic wrote at capture time.
- **`fsID`/`anID`/`LatLongExact` (Place) and `IsPrivate` (Source)** are stripped from the comparison
  entirely, not masked, and not compared against a specific RootsMagic capture at all -- see
  "Non-deterministic fields" below for why. This server's own value for each is verified directly
  instead: `TestWriteOperations` queries the resulting database after each write and asserts the
  exact value this server is supposed to produce, independent of `sqldiff` altogether. See
  `fieldCheck` and `verifyFields` in `cmd/server/main_test.go`.

## Non-deterministic fields

**When capturing a new golden file, strip `fsID`, `anID`, and `LatLongExact` (Place) and
`IsPrivate` (Source) out of it entirely, regardless of what RootsMagic's own `sqldiff` output shows
for them.** Don't try to make this server's behavior match one specific captured value for these
four fields -- there isn't a single correct value to match in the first place.

All four are downstream, on RootsMagic's own side, of a real-time lookup against FamilySearch's
and/or Ancestry's own services -- not something this server does at all (see SCOPE.md's "Write
support" section for why: a live, third-party network dependency is a fundamentally different kind
of feature than writing a field to SQLite, and deliberately out of scope). That lookup is a race
against a timeout, not a reliable success/fail signal, which makes RootsMagic's own resulting value
for these fields non-deterministic from one edit to the next -- confirmed directly, not assumed,
by capturing the exact same edit twice and getting different results:

| Capture | Fields edited | `fsID` matched | `anID` matched | `LatLongExact` |
|---|---|---|---|---|
| Belgrade, name only | Name | Yes | Yes | `1` |
| Belgrade, coordinates only | Latitude/Longitude | *(no match)* | *(no match)* | `1` |
| Odessa, all four fields at once | Name, Note, Latitude, Longitude | *(no match)* | Yes | *unchanged* |
| Belgrade, all four fields at once | Name, Note, Latitude, Longitude | Yes | Yes | `1` |

The last two rows are the clearest evidence: the exact same combination of fields, edited the same
way, on two different real captures -- one with a full match and `LatLongExact=1`, one with a
partial match and `LatLongExact` untouched. Everything else about those two captures agreed. Trying
to write conditional logic in `UpdatePlace` to reproduce this pattern would mean encoding a rule for
a value that depends on network timing at the moment of capture, not a real, reproducible RootsMagic
behavior -- indistinguishable, from inside a golden file, from an actual bug. Chasing it cost real
time before the pattern above made clear what was actually going on; stripping these fields from new
golden files up front avoids repeating that.

This server's own behavior for all four fields is fully deterministic regardless -- see
`internal/rmdb/writes.go`'s comments on `UpdatePlace`/`UpdateSource` for exactly what's written and
why -- it's specifically RootsMagic's own captured value that can't be treated as ground truth.

**`ConfigTable.DataRec` is a related but separate trap, worth calling out on its own: strip it from
new golden files too, whenever it shows up.** Unlike the four fields above, this isn't about network
timing -- `DataRec` is a ~15KB, undocumented XML blob holding RootsMagic's own UI window/panel
layout state, not genealogical data. A real capture for a plain "add a comment to a Source" edit
showed RootsMagic rewriting the entire blob over one changed tag
(`MediaCollapsed_Citations`) -- confirmed, by decoding and diffing it against a separate reference
copy, to be unrelated to the edit that triggered its capture (see SCOPE.md's "Write support" section
for the full account). This server doesn't and won't write `DataRec` at all. Unlike the other four
fields, you don't have to remember to strip this one by hand: `configTableDataRecRegex` in
`cmd/server/main_test.go` strips it automatically from *both* sides of the comparison (the golden
file's own raw content included, not just this server's actual output) -- deliberately more
defensive than the other four, since a multi-kilobyte hex blob is a much easier thing to leave only
partially cleaned up by hand than a short numeric field. Still worth removing it from a golden file
before committing it, for anyone reading the file later, even though the test itself doesn't
strictly require that.

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
| 8 | `POST` a place, `notes` only. | **Done** -- `testdata/post_places_note_expected.sql` (currently covers this exact case: note-only change on `PL423`). |
| 9 | `POST` a place, `latitude`/`longitude` only. Also confirm the decimal-to-integer conversion is exact (`value × 10,000,000`) for a coordinate with several decimal places -- a real bug here (float64 truncation instead of rounding) was found and fixed via this exact test; see SCOPE.md's "Write support" section. | **Done** -- `testdata/post_places_coordinates_expected.sql`. |
| 10 | `POST` a place, all four fields (`names`, `notes`, `latitude`, `longitude`) at once. | **Done** -- `testdata/post_places_all_fields_expected.sql`. `fsID`/`anID`/`LatLongExact` stripped from it -- see "Non-deterministic fields" above, this is the exact case that finding came from. |
| 11 | Partial-update semantics: set a note (one request), then a second request that only sends `names` (omitting `notes`) -- confirm the note from the first request survives untouched. | No golden file needed -- Go-native: two sequential API calls plus a `GET` to confirm, not a RootsMagic-comparable single edit. |
| 12 | `latitude` without `longitude` (or vice versa): `400`, and the place is confirmed unchanged afterward. | No golden file -- request rejected before any write. |
| 13 | Body `id` doesn't match the URL's id: `400`, nothing written. | No golden file. |
| 14 | `POST` to a `PlaceID` that doesn't exist: `404`. | No golden file. |
| 15 | Malformed JSON body: `400`, not a `500` or crash. | No golden file. |
| 16 | Same request as #7, but without `-write`: `405`, database confirmed untouched. | No golden file. |
| 17 | `POST` a source, `titles` only. | **Done** -- `testdata/post_sources_expected.sql`. |
| 18 | `POST` a source, `notes` (→ `Comments`) only. | **Done** -- `testdata/post_sources_comments_expected.sql`. `ConfigTable.DataRec` stripped from it -- see "Non-deterministic fields" above, this is the exact case that finding came from. |
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

## Testing across RootsMagic versions

Every golden file and every test database in this project (`royal92.rmtree` included) is
RM9-era. Nothing here has been tested against a real RM8, RM10, or RM11 database, or a real
RM7 file (`internal/rmdb/rm8_required_test.go`'s RM7 rejection test uses a real RM9 database
with columns dropped via `ALTER TABLE`, reconstructing what an RM7 schema looks like for the
tables that matter -- a reasonable approximation, confirmed against the real data dictionary,
but not the same as an RM7 file RootsMagic itself actually produced). Worth its own test suite
at some point -- a version-support concern is a different kind of testing than the sqldiff-based
write verification this document otherwise covers, and probably deserves its own document rather
than being folded into this one.
