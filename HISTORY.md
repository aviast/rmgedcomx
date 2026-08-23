# History

This document records how rmgedcomx got to its current state: real bugs
found and fixed, design mistakes made and corrected, and the staged
rollout of write support. For what the server actually does today, see
[SCOPE.md](./SCOPE.md).

Development was driven substantially by the requirements of GEDAM, a
Digital Asset Management client this server was built to support --
GEDAM's own needs prioritized which parts of the GEDCOM X RS
specification got implemented, and in what order.

## Early structural decisions

**Bare resource URLs became collection-scoped.** An early version of this
server used bare URLs (`/persons/P1`) with no notion of more than one
database being open. Once multiple `-db` flags became possible, this was
recognized as a real design flaw: two different databases' `P1` would be
indistinguishable, represented by the identical URL. Every resource URL
was moved under `/collections/{id}/...`, and the `Collection`/`Collections`
states were added as the formal discovery root (see SCOPE.md's "Multiple
databases / Collections"). The `-title` flag from the single-collection
era was removed at the same time -- title is now always derived the same
way as the id, since there's no longer a single collection for a
standalone override to unambiguously apply to.

**`sources` and `media` were originally one combined array.** An early
version of this server merged bibliographic citations and attached
artifacts into a single `sources` array everywhere, reasoning that both
"evidence" a conclusion in some sense. That reasoning didn't survive
contact with the spec's own text, which draws an explicit line:
`Subject.media` is "not considered evidence supporting the identity of
the subject," a deliberate contrast with `sources`. Checked against two
independent implementations (`gedcomx-js`, `gedcomx-rs`), both of which
have a distinct `media` field alongside `sources`. `buildSourceReferences`
was replaced with `buildSourcesAndMedia`, returning both arrays from the
same underlying query. `PlaceDescription` gained a query for this that
didn't exist at all before (its own citations/media were never being
surfaced prior to this).

## Read-side fixes found through direct review

**Embedded relationship states on `Person`.** RS spec Section 4.10.5
requires `child-relationships`/`parent-relationships`/`spouse-relationships`
to be embedded directly in the `Person` state if not linked. This server
provided neither -- `GET .../persons/{id}` never surfaced a person's own
relationships at all, only links to the separate `.../parents`,
`.../children`, `.../spouses` endpoints. Fixed by adding a
`Relationships` field to `PersonDocument` (deliberately without
`omitempty`, so an empty result can't be mistaken for "not computed"),
reusing the same relationship-computation logic the three separate
endpoints already had (extracted into shared
`personParentRelationships`/`personChildRelationships`/
`personSpouseRelationships` helpers). Verified against real data:
Victoria (`royal92.rmtree`'s `P1`) now embeds exactly 12 relationships --
her two parents, all nine of her children with Albert, and her own
`Couple` relationship to Albert.

**`collection` link on `Person` and `Relationship`.** Both states'
"Transitions" tables (Sections 4.10.4, 4.21.4) list a `collection`
transition that neither state produced. Fixed using the already-existing
`s.collectionBaseURL`. Six golden files needed regenerating as a result,
each diffed programmatically against its own prior version to confirm
only the new link was added.

**`DisplayProperties`: `marriageDate`/`marriagePlace` were never
implemented at all; `birthPlace`/`deathPlace` were implemented but never
wired up.** Reported directly as "`marriageDate`/`marriagePlace` appear
to be omitted." Investigating turned up a second, one-level-less-visible
gap: `birthPlace`/`deathPlace`'s struct fields already existed but
`buildDisplayProperties` never populated either one. Fixed together.
`marriageDate`/`marriagePlace` needed a real design decision (which
marriage, if a person has more than one) -- resolved by taking the first
family (by `FamilyID`) that actually has a Marriage fact, verified
against a real, non-obvious case: `royal92.rmtree`'s William II has two
families where the first has no Marriage fact and the second does, and
this server correctly reports the second.

**`familiesAsParent`/`familiesAsChild`** were flagged as a separate,
larger gap at the time of the `marriageDate` fix above (the struct
fields and `FamilyView` type already existed, but were never populated)
and implemented in a follow-up pass. Two of the three new permanent
tests initially failed for a revealing reason: the *test*, not the
implementation, had the bug -- it linked two children to only their
shared father, which correctly produces two separate single-parent
families rather than one shared family (a bare, single-parent
`ParentChild` request never assumes an unnamed second parent; see "A
design mistake in relationship creation, corrected" below). Fixed by
linking each child to both parents, the same requirement already
established elsewhere.

**A separate bug surfaced while testing the above: `Couple` relationship
`facts` were silently discarded.** Building a self-contained test for
`familiesAsParent` required posting a `Couple` relationship with a
`Marriage` fact through the real HTTP API -- which is when this was
found: `handleCreateRelationships` read a relationship's own `Facts` for
`ParentChild` (to detect Adoptive/Biological/etc.) but never even looked
at `Facts` for `Couple` at all. The request itself gave no indication
anything was wrong -- `POST /relationships` with a `Marriage` fact
returned a normal `201 Created`; the fact was simply never written to
`EventTable`. This had gone undetected because the two layers were
tested in isolation: `internal/rmdb`'s own tests construct
`rmdb.NewCoupleRelationship{..., Facts: [...]}` directly, bypassing the
API layer entirely. Fixed by generalizing the person-fact converter
(renamed `buildNewFact`) to accept an expected owner type
(`OwnerTypePerson` or `OwnerTypeFamily`) rather than hardcoding `Person`.

**`Location` on a single `ParentChild` creation identified the wrong
resource.** Reported directly: a successful single `ParentChild`
creation returned `Location: /relationships/F{id}` (the `Couple`
resource's own shape) unconditionally, regardless of what was actually
created -- `GET` on that URL 404s for a `ParentChild`-only family, since
it denotes a `Couple` relationship the family isn't. Fixed by determining
each created relationship's own correct ref (`parentChildRef` with the
right `isFather`, fetched fresh from the created family) rather than
always using `coupleRef`. This also required correcting an existing test
whose own assertion had been relying on the bug: it linked a child's
biological father, then mother, and asserted their two `Location` values
were `Equal` -- true only because the old, buggy behavior collapsed both
a father-child and a mother-child edge on the same family down to the
same value.

## Search

**A plain `?name=` substring filter was an early stand-in for real
search.** Early in this project, real Atom-based search looked like
enough effort on its own to be out of scope entirely. `GET
/persons?name=...` was added instead, explicitly noted at the time as
"a natural place to grow real search later." Real search was
subsequently discussed (an effort assessment first, covering the actual
scope of the RS spec's Atom-based `Person Search Results`/`Place Search
Results` states) and then implemented in three stages: the 10 direct
Person Search parameters, the 36 relation-prefixed parameters, and Place
Search Results. Once the real search existed, the stand-in filter was
removed outright, not deprecated or left running alongside it -- `GET
/persons?name=...`'s `name` parameter is now simply unrecognized and
ignored, the same as any other query parameter this endpoint doesn't
look at.

**The response format turned out to be far smaller than "Atom" first
suggested.** The RS spec's `MUST`-support media type for both search
states is `application/x-gedcomx-atom+json`, not XML -- full
`application/atom+xml` is only `RECOMMENDED`. The JSON envelope turned
out to be thin and flat, reusing the exact same `PersonDocument`/
`PlaceDescriptionsDocument` shapes every other endpoint already produces.

**Non-exact (`~`) matching was specified directly as a plain SQL
substring match**, after confirming there was no better option for
free: `NameTable.GivenMP`/`SurnameMP` turned out to be accent-folded
copies of `Given`/`Surname`, not a phonetic (Metaphone/Soundex) encoding
despite the column name -- RootsMagic has no fuzzy-matching
infrastructure to build on here.

**The relation-parameter count was corrected before implementation, not
after.** The RS spec's relation search parameters table was re-checked
directly before starting that stage: `{relation}` (father/mother/spouse/
parent) has 9 fields each, not 8 -- an earlier running total had missed
`MarriagePlace`.

**A real mistake caught during manual verification of the relation
parameters, before it became a permanent test**: an initial check of
`spouseGivenName:Albert` failed to match Victoria in `royal92.rmtree`,
which looked at first like a bug in the `spouse` resolution logic. It
wasn't -- Albert's actual stored given name is "Albert Augustus Charles,"
not "Albert," and the check used exact matching against an unverified
guess at the value. Re-run against the correct, verified value, it
matched correctly.

**Two Place Search tests initially failed for a similar reason**: newly
created places in `testdata/empty.rmtree` don't start at `PL1`, since
that database ships with 205 pre-loaded LDS temple places (`PL1`-`PL205`)
-- a fresh place created during a test gets `PL206` onward. Fixed by
asserting on the place's own name rather than a hardcoded, assumed ID.

## Write support: staged rollout

Write support was built in deliberately small, independently-testable
stages, specifically so problems would surface against a small diff
rather than a large one. Each stage was verified against a real
RootsMagic database before being considered done.

**Stage 0 -- plumbing only.** `-write` threaded a `readOnly bool` down to
`rmdb.Open` (opening `mode=rw` instead of `mode=ro`), with no HTTP write
handlers yet. Verified directly at the SQL level: a raw `UPDATE` against
a read-only connection failed with `attempt to write a readonly
database`; the identical `UPDATE` against a write-mode connection to the
same file succeeded.

**Stage 1 -- `Place` and `Source Description` UPDATE.** Chosen first
because they're structurally simple (single tables, no cross-table
consistency concerns), to prove out the reusable plumbing (request
parsing, the write-route registration pattern, transactions, response
codes, the backup call) against the lowest-risk case. GEDAM itself didn't
need to modify `Place`/`Source` at all, but since they were cheap to add
correctly once the plumbing existed, they were included anyway.

A real bug surfaced here: the decimal-to-integer conversion for
`latitude`/`longitude` used a bare `int64(...)` conversion rather than
`math.Round`. `44.817778 * 1e7` evaluates to `448177779.9999999404` in
float64 arithmetic, not exactly `448177780.0`, and `int64(...)` truncates
toward zero -- so real coordinates were being silently rounded down by up
to 1 in the last digit, depending on which specific decimal values
happened to land on the wrong side of a float64 rounding boundary. Found
from a real golden-file mismatch, not caught in advance.

Also found: `decodeStrictJSON`'s `DisallowUnknownFields` requirement
came from a concrete failure mode -- a request using `{"value": "..."}`
instead of `{"text": "..."}` on a `Note` previously decoded without
error (the mistyped field silently ignored), and if that happened to be
the only field in the update, the request looked like a legitimate no-op
and returned a misleading `204` with the intended write never having
taken effect.

`ConfigTable.DataRec` (a ~15KB undocumented XML blob RootsMagic itself
rewrites on nearly every edit) was investigated and deliberately excluded
from ever being written: decoding and diffing it against an unrelated
reference copy of the same file showed the one changed tag
(`MediaCollapsed_Citations`) was UI window/panel state unrelated to the
edit that triggered its capture, not genealogical data.

**Stage 2 -- `Artifact` location updates.** GEDAM's actual requirements,
clarified at this point: updating a digital asset's stored path, and
creating/editing/deleting media links -- not creating new
`Person`/`Relationship`/`Event` records (that expectation later changed;
see Stage 3 below). `encodeMediaPath` was built as the reverse of the
existing `ResolveMediaPath`. Full end-to-end verification (including
`RootsMagicUser.xml` discovery) wasn't possible from the Linux sandbox
this project was developed in until a hidden `-bypass-os-check` flag was
added specifically to exercise the macOS discovery code path against a
real home directory on any platform, closing a real verification gap an
earlier pass of this work had to work around by testing the `api`/`rmdb`
layers directly instead.

**Stage 2b/2c/2d -- `MediaLinkTable` CRUD for `Person`, `Event`, and
`Relationship`.** Deliberately split into one entity at a time rather
than all three at once, so problems in any one wouldn't compound. The
real `MediaLinkTable` schema turned out to have more in it than initial
planning assumed (`IsPrimary`, `Include1-4`, `Rect*`, `Comments`) --
checked against the data dictionary before deciding how to handle each.
`Relationship` (Stage 2d) was deferred at Stage 2b for lack of a
confirmed need, then built once GEDAM's own specification draft (§9.4,
§14) explicitly flagged the gap.

A GEDAM specification review at this point (a working draft reviewed
directly against this server's actual behavior) surfaced two concrete
pieces of follow-on work -- the periodic write-availability re-check
below, and the artifact reverse-lookup endpoints -- plus confirmed two
of GEDAM's own "enhancement needed" asks (the startup table's `UniqueID`
column, `Collection.identifiers`) had already shipped.

**Write availability re-checked periodically, not just at startup.** The
original design (`checkRootsMagicNotRunning`, checked once in `main()`)
couldn't protect against RootsMagic being opened *after* this server
already started in write mode -- a real gap, since GEDAM is a
long-running background service, not a short CLI invocation. Fixed with
`WriteGuard`, re-checking on each write attempt (rate-limited to once
per 10 seconds) and latching permanently once tripped. A real, if
easy-to-miss, Go bug was caught before this shipped: assigning a nil
`*writeGuard` directly into an interface field does not produce a nil
interface -- `main.go` was fixed to only assign the field when the
concrete guard is genuinely non-nil.

**Reverse lookup endpoints** (`GET /artifacts/{id}/subjects` and
type-filtered variants) were added because GEDAM's own role-resolution
algorithm needed to answer "which people/relationships/events/places
reference this artifact," and nothing in this server's existing,
forward-only traversal could answer that without enumerating every
resource by hand. Testing this needed real care: an initial test using
one of `royal92.rmtree`'s two real citations produced thousands of
results, which turned out to be correct, not a bug -- that citation was
a widely-shared "base import" citation referenced by 11,698 separate
rows. A small, deliberately synthetic scenario was used for a readable
test instead.

**Stage 3 -- creating `Person` and `Relationship` records.** Driven by a
concrete need (GEDAM wanted to add new people and relationships, not
just link media to existing ones), built from a real, systematically
captured reference: a whole family (the Brontës, from a public-domain
GEDCOM file) entered into an initially empty RootsMagic 8+ database one
step at a time, with `sqldiff` output captured and reviewed at every
step (15 golden files).

Several real, direct-review-prompted corrections came out of checking
this server's own earlier claims against real data rather than trusting
them: `BirthYear`/`DeathYear` had been set only on a person's primary
name, with a comment incorrectly claiming this was "confirmed against a
real captured diff" -- neither `royal92.rmtree` nor this project's other
fixtures happened to contain a multi-name person, so this had never
actually been checked. `ChildOrder` had been 1-indexed by default,
confirmed wrong against two of three real databases checked.
`PersonTable.SpouseID`/`ParentID` had been getting set (per the RM4-11
data dictionary, each actually holds the `FamilyID` of whichever family
was last *viewed* for that person in RootsMagic's own UI, not a
genealogical fact) -- what actually prompted revisiting this was a
concrete symptom, a real test database showing `SpouseID` referencing a
`FamilyID` that had since been deleted during a family-merge operation,
traced back to a genuine bug in the merge logic (see "A design mistake
in relationship creation, corrected" below) and resolved by not writing
either field at all, since neither has a principled correct value for a
record this server creates.

Three separate real user reports, all against the same `royal92.ged`
file, drove a sequence of related name-handling fixes: a `NameForm` with
only `fullText` (no structured `parts`) was originally rejected outright
on the reasoning that splitting free text into surname/given is
inherently ambiguous -- but the conceptual model spec explicitly
permits a `fullText`-only name, so this was a real spec-compliance gap,
not a defensible caution. Fixed to store the whole `fullText` in `Given`
with `Surname` empty, matching RootsMagic's own confirmed behavior for
the equivalent GEDCOM 5.x case. The very next individual in the same
file (`I785`) had *no* name content at all, needing the same fix
extended to the "neither `parts` nor `fullText`" case, and to
`Person.names` itself being accepted as absent/empty (also `OPTIONAL`
per the spec). While testing that fix, a fourth issue was found: this
server wasn't honoring GEDCOM X's "first name in the list is preferred
by default" convention, producing `IsPrimary=0` on every real
`royal92.rmtree` name checked. `I785`'s death year not appearing in the
created record was found at the same time but deliberately deferred to
its own pass (see below), since `Date.original` is open-ended free text
with no defined grammar, not a narrow, deterministic gap like the four
name issues.

**`Fact.value` never reaching `EventTable.Details`** was a real, reported
gap -- a value-only fact (`Occupation`, `Education`, `Religion`, etc.)
created successfully but with `Details` always empty, since
`buildNewPersonFact` (as it was then named) never read `f.Value` at all.
The read side had already correctly reversed this exact mapping, so the
gap was purely one-directional, on the write path only.

**Nickname handling** was prompted by a direct question about a real
structural mismatch: GEDCOM 5.x nests a nickname within a `NAME` record,
but GEDCOM X models it as its own separate `Name`, while RootsMagic's
schema has only a single `Nickname`/`NicknameMP` column pair on one name
record. A client converting a GEDCOM 5.x `NICK` value the natural way
(into a separate `Name(type=Nickname)`) would previously have gotten a
spurious second `NameTable` row. Fixed by absorbing a `Name(type=Nickname)`
into the primary name's own `Nickname` column instead, with the read
side synthesizing it back as a separate `Name` for round-trip
correctness.

**`Date.original` as a fallback when `Date.formal` is absent** was the
dedicated pass the `I785` report above deferred -- prompted by the same
report, but as its own explicit request. Grounded directly in the actual
GEDCOM 5.5.1 specification (fetched and read in full) rather than any
particular client-side conversion tooling, since tooling was expected to
keep changing. `royal92.ged`'s own 4018 real `DATE` values were used to
confirm the resulting scope held up against a real file (99.5% parse
successfully), not to define the scope in the first place.

A related real bug surfaced immediately after: a request to create
Charlemagne failed outright because `Date.formal="+742-04-02"` wasn't
zero-padded to four digits (a genuine, spec-required violation) -- but
the same fact's `Date.original` ("2 APR 742") was sitting right there,
perfectly parseable, and the existing code treated any `Formal` parse
failure as an immediate hard rejection without ever falling back to
`Original` the way a *missing* `Formal` already did. Fixed by extending
the existing missing-`Formal` fallback to also cover an invalid one.

**`POST /relationships` -- `Couple` and `ParentChild` creation** reused
the same patterns `CreatePerson` established. `CreateParentChildRelationship`
was the harder design problem in this whole stage, and went through a
real, corrected design mistake before landing on its current algorithm
(see next section).

### A design mistake in relationship creation, corrected

An earlier version of `CreateParentChildRelationship` resolved a bare
(parent, child) pair by checking whether the named parent already had
exactly one family on file, and used it directly if so. This was a real,
if understandable, mistake caught during design discussion rather than
after shipping: "the parent happens to have one family recorded" is a
fact about the database's *current contents*, not a fact about the
parent's real life. If Mary's only recorded family happens to be with
Patrick, a bare `ParentChild(Mary, Child)` request says nothing at all
about whether Patrick is Child's other parent. Linking the child into
Patrick's family anyway would silently assert a co-parent that was never
actually named.

Corrected to the algorithm SCOPE.md now describes, which never reuses a
family based on the parent's own existing state -- only ever based on the
child's. Verified with a person having two real, distinct partners
(children by both Patrick and Robert), confirming each child lands in
the correct, distinct family.

Two further real bugs were found while updating the existing test to
match the corrected design: `ChildOrder` wasn't being recomputed on a
family merge (all children would have silently collided at the same
starting value), and `PersonTable.ParentID` wasn't updated on merge
either (left pointing at the now-deleted temporary family). Both are now
fixed as part of the merge step itself.

**`RelType`** was prompted by the ambiguity case above having a real,
named counterpart in RootsMagic's own schema. The correct GEDCOM X
mapping required finding the right specification document on a second
attempt -- the conceptual model spec's "Known Fact Types" table does list
`http://gedcomx.org/Adoption`, but scoped to `person`, not
`relationship`; the actual match is a separate, dedicated
"Parent-Child Relationship Fact Types" section in a different
specification document entirely, found only after the first attempt
used the wrong one.

### RootsMagic 7 dropped, not just never added

Originally "RootsMagic 7 or later," narrowed to 8 after a specific
finding, not a general tightening of scope: a community blog post
documented that RM8 replaced `EditDate` with `UTCModDate` on
`PersonTable`/`EventTable`/`NameTable` -- but those aren't the tables
write support actually touches. Checked directly against the RM4-11 data
dictionary's own version-specific sheets: `PlaceTable`, `SourceTable`,
`MultimediaTable`, `MediaLinkTable`, and `ConfigTable` -- the five tables
every write handler touches -- have no modification-timestamp column at
all in RM7, under either name. Every write handler unconditionally sets
`UTCModDate` on one or more of these, so against a real RM7 file this
wasn't a missing nice-to-have, it was a raw `"no such column"` SQL error
on the very first write attempt. Gating only `-write` mode to RM8+
(leaving RM7 usable read-only) was considered and rejected: legacy
version support was never an actual project goal, and narrowing now
avoided needing to make the same decision again once write support
extended further.

## HTTP semantics and logging: external audit fixes

An external audit of this server raised several genuine issues with how
it used HTTP -- status codes, content negotiation, error bodies, and
paging links.

**Status codes.** An earlier version of this server explicitly registered
handlers for `POST`/`PUT`/`PATCH`/`DELETE` on every resource, and for
whole unimplemented resource families (`Records`, `Agents`, `Events` at
the time, `Person Matches`, `/oauth2/token`), all returning a custom
`501 Not Implemented` body -- a genuine misuse of `501`, which per RFC
7231 is about the server not supporting a request's functionality *at
all*, not "this resource exists and I understood your request, I just
won't do that here" (`405`) or "this URL doesn't correspond to anything"
(`404`). The fix was to delete code, not add it: Go 1.22's
`net/http.ServeMux` already produces the correct `405`/`404` automatically
for a route that isn't registered.

**Content negotiation.** This server had always produced exactly one
representation, but forced that `Content-Type` on every response
regardless of the client's `Accept` header, and never sent `Vary`. Fixed
to check `Accept` and respond `406` if it can't be satisfied, with
`Vary: Accept` on every response.

**Error bodies.** This server's own ad hoc `{"error": "..."}` shape (and,
before that, `"reason"`/`"seeAlso"` fields on 501s) had no standard
behind it. Replaced with RFC 7807 Problem Details throughout.

**Paging.** `pagingLinks` originally only ever produced `first` alongside
`prev` (never on the first page, where it's arguably most useful) and
never `last` at all. Both are now included on every page whenever a
resource has more than one.

**Logging** was converted from a mix of the bare `log` package and direct
`fmt.Fprintln` calls to `log/slog`, prompted by a real, concrete need: a
`405` with no visible explanation for why. The debug-level
request/response body logging this enabled directly surfaced a real bug:
`POST /relationships` was returning `500` for a client-resolvable
ambiguity (a parent already belonging to two different families as
`FatherID`, which the request itself explained how to resolve). `500`
signals a server bug; this was a well-formed request this server
correctly declined to guess about. The root cause: `handleCreateRelationships`'s
status-code logic only checked for one sentinel error
(`rmdb.ErrNotFound`) and defaulted to `500` for everything else, and
neither of `CreateParentChildRelationship`'s "won't guess" errors were
wrapped in anything that check could recognize. Fixed by adding a second
sentinel, `rmdb.ErrAmbiguous`, and checking for it alongside
`ErrNotFound`. (The specific scenario in the original report -- a parent
in two families, linked to a new child with no other information -- no
longer produces this error at all: it was a direct symptom of the design
mistake corrected above, and now correctly creates a new family instead
of guessing or rejecting. `ErrAmbiguous`/`400` still applies to the
narrower, still-genuinely-ambiguous case that replaced it: a child
already belonging to more than one existing family.)

## A documentation mistake, corrected: the SQLite driver was never actually read-only-incapable

An earlier version of this project's own documentation claimed
`modernc.org/sqlite` (the pure-Go SQLite driver this server uses)
couldn't do true read-only access, and that read-only enforcement relied
solely on `PRAGMA query_only = 1`. That was a real mistake, not a
deliberate tradeoff -- it came from checking only the Go wrapper's
driver-specific DSN handling (`_pragma=`, `_time_format`, `vfs`) and
concluding "no `mode` handling here, so no read-only support." In fact
`modernc.org/sqlite` transpiles the actual SQLite C source rather than
reimplementing SQLite's own URI-filename parsing, and that C code
already handles `mode=ro` as a query parameter correctly, taking effect
before the Go wrapper's own `flags` argument even enters the picture.
Confirmed empirically by round-tripping the exact DSN pattern this
server uses against a database, and separately confirming Python's
built-in `sqlite3` module (linking the same real SQLite engine) exhibits
the identical behavior. The redundant `PRAGMA query_only = 1` "defense in
depth" was removed once `mode=ro` was confirmed to genuinely enforce
read-only at the engine level on its own.
