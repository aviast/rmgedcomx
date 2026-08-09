package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/require"
	_ "modernc.org/sqlite"
)

func TestReadOperations(t *testing.T) {
	tests := []struct {
		name               string   // Name of the test case
		method             string   // HTTP method (POST, PUT, DELETE)
		endpoint           string   // The API route to hit
		reqBody            string   // The JSON payload to send
		goldenFile         string   // Path to the expected JSON response (leave blank if expecting an error code)
		expectedStatus     int      // Expected HTTP response code
		dbPaths            []string // Paths to the database files
		baseURL            string   // Base URL for the API
		mediaFolder        string   // Path to the media folder
		write              bool     // Whether the server is in write mode
		defaultGenerations int      // Default number of generations for ancestry/descendancy queries
		maxPageSize        int      // Maximum page size for paginated responses
	}{
		// --- Collection Endpoints ---
		{
			name:               "GET Root Collection",
			method:             "GET",
			endpoint:           "/",
			reqBody:            ``,
			goldenFile:         "testdata/get_root_expected.json",
			expectedStatus:     http.StatusOK,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "POST Root Collection (Read-only check)",
			method:             "POST",
			endpoint:           "/",
			reqBody:            `{"name": "Attempt to modify"}`,
			goldenFile:         "",
			expectedStatus:     http.StatusMethodNotAllowed,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},

		// --- Persons Endpoints ---
		{
			name:               "GET Persons Collection",
			method:             "GET",
			endpoint:           "/collections/victoria-hanover-royal92/persons",
			reqBody:            ``,
			goldenFile:         "testdata/get_persons_expected.json",
			expectedStatus:     http.StatusOK,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "POST Persons Collection (Read-only check)",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/persons",
			reqBody:            `{"names":[{"nameForms":[{"fullText":"New Person"}]}]}`,
			goldenFile:         "",
			expectedStatus:     http.StatusMethodNotAllowed,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "GET Single Person",
			method:             "GET",
			endpoint:           "/collections/victoria-hanover-royal92/persons/P1",
			reqBody:            ``,
			goldenFile:         "testdata/get_person_expected.json",
			expectedStatus:     http.StatusOK,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "PUT Single Person (Read-only check)",
			method:             "PUT",
			endpoint:           "/collections/victoria-hanover-royal92/persons/P1",
			reqBody:            `{"id":"P1"}`,
			goldenFile:         "",
			expectedStatus:     http.StatusMethodNotAllowed,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "DELETE Single Person (Read-only check)",
			method:             "DELETE",
			endpoint:           "/collections/victoria-hanover-royal92/persons/P1",
			reqBody:            ``,
			goldenFile:         "",
			expectedStatus:     http.StatusMethodNotAllowed,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},

		// --- Person Relationships & Trees ---
		{
			name:               "GET Person Ancestry",
			method:             "GET",
			endpoint:           "/collections/victoria-hanover-royal92/persons/P1/ancestry?generations=2",
			reqBody:            ``,
			goldenFile:         "testdata/get_person_ancestry_expected.json",
			expectedStatus:     http.StatusOK,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "GET Person Descendancy",
			method:             "GET",
			endpoint:           "/collections/victoria-hanover-royal92/persons/P1/descendancy?generations=2",
			reqBody:            ``,
			goldenFile:         "testdata/get_person_descendancy_expected.json",
			expectedStatus:     http.StatusOK,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},

		// --- Relationships Endpoints ---
		{
			name:               "GET Relationships Collection",
			method:             "GET",
			endpoint:           "/collections/victoria-hanover-royal92/relationships",
			reqBody:            ``,
			goldenFile:         "testdata/get_relationships_expected.json",
			expectedStatus:     http.StatusOK,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "POST Relationships Collection (Read-only check)",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/relationships",
			reqBody:            `{"type":"http://gedcomx.org/Couple"}`,
			goldenFile:         "",
			expectedStatus:     http.StatusMethodNotAllowed,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "GET Single Relationship",
			method:             "GET",
			endpoint:           "/collections/victoria-hanover-royal92/relationships/F1",
			reqBody:            ``,
			goldenFile:         "testdata/get_relationship_expected.json",
			expectedStatus:     http.StatusOK,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "DELETE Single Relationship (Read-only check)",
			method:             "DELETE",
			endpoint:           "/collections/victoria-hanover-royal92/relationships/F1",
			reqBody:            ``,
			goldenFile:         "",
			expectedStatus:     http.StatusMethodNotAllowed,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},

		// --- Source Descriptions Endpoints ---
		{
			name:               "GET Source Descriptions Collection",
			method:             "GET",
			endpoint:           "/collections/victoria-hanover-royal92/source-descriptions",
			reqBody:            ``,
			goldenFile:         "testdata/get_source_descriptions_expected.json",
			expectedStatus:     http.StatusOK,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "POST Source Descriptions Collection (Read-only check)",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/source-descriptions",
			reqBody:            `{"about":"http://example.com/source"}`,
			goldenFile:         "",
			expectedStatus:     http.StatusMethodNotAllowed,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "GET Single Source Description",
			method:             "GET",
			endpoint:           "/collections/victoria-hanover-royal92/source-descriptions/S1",
			reqBody:            ``,
			goldenFile:         "testdata/get_source_description_expected.json",
			expectedStatus:     http.StatusOK,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "PATCH Single Source Description (Read-only check)",
			method:             "PATCH",
			endpoint:           "/collections/victoria-hanover-royal92/source-descriptions/S1",
			reqBody:            `{"about":"http://example.com/updated"}`,
			goldenFile:         "",
			expectedStatus:     http.StatusMethodNotAllowed,
			dbPaths:            []string{"../../royal92.rmtree"},
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              false,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
	}

	for _, tc := range tests {
		// t.Run creates a sub-test. This shows up nicely formatted in the CLI output.
		t.Run(tc.name, func(t *testing.T) {
			// ### Start rmgedcomx ###
			// Call the function from your main code that builds your router.
			// If your router needs the database path, you would pass it in here.
			router, cleanup := SetupRouter(
				tc.dbPaths,
				tc.baseURL,
				tc.mediaFolder,
				tc.write,
				tc.defaultGenerations,
				tc.maxPageSize,
			)
			defer cleanup()

			// Pass the router directly to httptest
			testServer := httptest.NewServer(router)
			defer testServer.Close()
			defer cleanup()

			req, err := http.NewRequest(tc.method, testServer.URL+tc.endpoint, bytes.NewBufferString(tc.reqBody))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, tc.expectedStatus, resp.StatusCode)

			if tc.goldenFile != "" {
				expectedBytes, err := os.ReadFile(tc.goldenFile)
				require.NoError(t, err, "Failed to read golden file")

				actualBytes, err := io.ReadAll(resp.Body)
				require.NoError(t, err, "Failed to read response body")

				require.JSONEq(t, string(expectedBytes), string(actualBytes))
			}
		})
	}
}

// Pre-compile regex to replace dynamic timestamps in sqldiff output
var utcModDateRegex = regexp.MustCompile(`UTCModDate=[0-9.]+`)
var familySearchIDRegex = regexp.MustCompile(`,\s*fsID=-?[0-9]+`)
var ancestryIDRegex = regexp.MustCompile(`,\s*anID=-?[0-9]+`)
var isPrivateRegex = regexp.MustCompile(`,\s*IsPrivate=[0-9]+`)

func TestWriteOperations(t *testing.T) {
	tests := []struct {
		name               string           // Name of the test case
		method             string           // HTTP method (POST, PUT, DELETE)
		endpoint           string           // The API route to hit
		reqBody            string           // The JSON payload to send
		goldenFile         string           // Path to the expected sqldiff output (.sql)
		verifyZero         []zeroFieldCheck // Fields to verify directly are 0 -- see zeroFieldCheck's own comment for why sqldiff can't be trusted for these
		expectedStatus     int              // Expected HTTP response code
		baseURL            string           // Base URL for the API
		mediaFolder        string           // Path to the media folder
		write              bool             // Whether the server is in write mode
		defaultGenerations int              // Default number of generations
		maxPageSize        int              // Maximum page size
	}{
		{
			name:               "POST Place Name Change",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/places/PL423",
			reqBody:            `{"places":[{"id":"PL423","names":[{"value":"Belgrade, Serbia"}]}]}`,
			goldenFile:         "testdata/post_places_name_expected.sql",
			verifyZero:         []zeroFieldCheck{{table: "PlaceTable", idCol: "PlaceID", idVal: "423", columns: []string{"fsID", "anID", "LatLongExact"}}},
			expectedStatus:     http.StatusNoContent,
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              true,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "POST Place Note Change",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/places/PL423",
			reqBody:            `{"places":[{"id":"PL423","notes":[{"text":"Updated note"}]}]}`,
			goldenFile:         "testdata/post_places_note_expected.sql",
			verifyZero:         []zeroFieldCheck{{table: "PlaceTable", idCol: "PlaceID", idVal: "423", columns: []string{"fsID", "anID", "LatLongExact"}}},
			expectedStatus:     http.StatusNoContent,
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              true,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			// LatLongExact is deliberately NOT in verifyZero here -- it's
			// 1 after this update, not 0 (see UpdatePlace's own doc
			// comment), and the golden file asserts that value directly
			// via the normal sqldiff comparison rather than a separate
			// direct check, since a coordinates change makes it a real,
			// observable transition sqldiff can actually verify.
			name:               "POST Place Coordinates Change",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/places/PL423",
			reqBody:            `{"places":[{"id":"PL423","latitude":44.817778,"longitude":20.456944}]}`,
			goldenFile:         "testdata/post_places_coordinates_expected.sql",
			expectedStatus:     http.StatusNoContent,
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              true,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "POST Place All Fields Change",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/places/PL882",
			reqBody:            `{"places":[{"id":"PL882","names":[{"value":"Odessa, Ukraine"}],"notes":[{"text":"Odesa, also spelled Odessa, is the third-most populous city and municipality in Ukraine and a major seaport and transport hub located in the south-west of the country, on the northwestern shore of the Black Sea."}],"latitude":44.817778,"longitude":20.456944}]}`,
			goldenFile:         "testdata/post_places_all_fields_expected.sql",
			verifyZero:         []zeroFieldCheck{{table: "PlaceTable", idCol: "PlaceID", idVal: "882", columns: []string{"fsID", "anID"}}},
			expectedStatus:     http.StatusNoContent,
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              true,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			name:               "POST Source Name Change",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/source-descriptions/S1",
			reqBody:            `{"sourceDescriptions":[{"id":"S1","titles":[{"value":"Public Domain GEDCOM file imported on 22 July 2026"}]}]}`,
			goldenFile:         "testdata/post_sources_expected.sql",
			verifyZero:         []zeroFieldCheck{{table: "SourceTable", idCol: "SourceID", idVal: "1", columns: []string{"IsPrivate"}}},
			expectedStatus:     http.StatusNoContent,
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              true,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 1. Create a isolated copy of royal92.rmtree in a temporary directory
			tempDir := t.TempDir()
			tempDBPath := filepath.Join(tempDir, "royal92.rmtree")
			copyFile(t, "../../royal92.rmtree", tempDBPath)

			// 2. Initialize router using the temporary database copy
			router, cleanup := SetupRouter(
				[]string{tempDBPath},
				tc.baseURL,
				tc.mediaFolder,
				tc.write,
				tc.defaultGenerations,
				tc.maxPageSize,
			)

			testServer := httptest.NewServer(router)

			// 3. Send HTTP Request
			req, err := http.NewRequest(tc.method, testServer.URL+tc.endpoint, bytes.NewBufferString(tc.reqBody))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			resp.Body.Close()

			require.Equal(t, tc.expectedStatus, resp.StatusCode)

			// 4. CRITICAL: Close server and DB connections BEFORE running sqldiff
			// This releases SQLite file locks on Windows
			testServer.Close()
			cleanup()

			// 5. Compare database diff against golden SQL file
			if tc.goldenFile != "" {
				expectedBytes, err := os.ReadFile(tc.goldenFile)
				require.NoError(t, err, "Failed to read golden file")

				// Run sqldiff comparing the clean original against our modified temp copy
				actualDiff := runSqlDiff(t, "../../royal92.rmtree", tempDBPath)

				// TrimSpace both sides symmetrically -- comparing the golden
				// file's raw bytes (which may or may not end in a trailing
				// newline, depending on how the file was saved) against only
				// the actual side being trimmed (inside normalizeSQL) was a
				// real, if easy to miss, source of spurious failures: two
				// strings that are identical except for trailing whitespace
				// aren't equal to require.Equal, and that's not the kind of
				// difference this test is meant to catch.
				normalizedExpected := strings.TrimSpace(string(expectedBytes))
				// Normalize line endings and mask/strip dynamic fields in test output
				normalizedActual := normalizeSQL(actualDiff)

				require.Equal(t, normalizedExpected, normalizedActual)
			}

			// 6. Directly verify the fields step 5's comparison deliberately
			// excludes (see normalizeSQL's own comment, and zeroFieldCheck's,
			// for the full reasoning): confirm they're actually 0, not just
			// "didn't fail to change." sqldiff can only ever tell us whether
			// a value changed between two database states, never what that
			// value actually is -- for fields already at 0 in every place
			// and source in royal92.rmtree, that makes it structurally
			// incapable of answering the question that actually matters
			// here. This is the check that does.
			for _, check := range tc.verifyZero {
				verifyZeroFields(t, tempDBPath, check)
			}
		})
	}
}

// zeroFieldCheck names a single row and a set of columns on it that this
// server always writes as 0 (see internal/rmdb/writes.go's own comments on
// UpdatePlace/UpdateSource for the full reasoning: fsID/anID/LatLongExact
// on Place, IsPrivate on Source -- fields this server has no basis to set
// to anything other than a known-safe default, since it doesn't do the
// external verification, or reimplement the undocumented behavior, that
// would justify any other value).
//
// These are checked directly, by querying the resulting database after
// the write, rather than through the sqldiff-based golden-file comparison
// every other field goes through. That's deliberate, not a shortcut:
// sqldiff (like any before/after diff) only reports columns whose value
// actually *changed* -- and every place and source in royal92.rmtree
// already has these specific fields at 0, the same value this server
// writes, so a diff can never observe whether this server wrote anything
// at all. A direct query is the only way to actually confirm the value,
// independent of whatever it happened to be beforehand.
type zeroFieldCheck struct {
	table   string
	idCol   string
	idVal   string
	columns []string
}

func verifyZeroFields(t *testing.T, dbPath string, check zeroFieldCheck) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err, "opening database to verify zero fields")
	defer db.Close()

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s",
		strings.Join(check.columns, ", "), check.table, check.idCol, check.idVal)
	row := db.QueryRow(query)

	values := make([]int, len(check.columns))
	scanTargets := make([]any, len(values))
	for i := range values {
		scanTargets[i] = &values[i]
	}
	require.NoError(t, row.Scan(scanTargets...), "querying %s", query)

	for i, col := range check.columns {
		require.Equal(t, 0, values[i], "%s.%s should be 0 after this server's write, was %d", check.table, col, values[i])
	}
}

// sqldiffCommand returns the sqldiff executable name for the current
// platform: "sqldiff.exe" on Windows, "sqldiff" everywhere else. Either
// way, it's expected to be on PATH -- see TESTING.md for where to get it.
//
// macOS isn't specifically handled -- deliberately out of scope for now,
// not an oversight (this project doesn't currently target it). It falls
// through to the non-Windows case, which will find a real "sqldiff" on
// PATH if one happens to be installed the same way as on Linux, but
// that's untested, not a supported claim.
func sqldiffCommand() string {
	if runtime.GOOS == "windows" {
		return "sqldiff.exe"
	}
	return "sqldiff"
}

// unifuzzLibPath returns the path to the unifuzz collation library
// (needed for sqldiff to correctly compare RMNOCASE-collated columns) for
// the current platform: testdata/unifuzz.dll on Windows,
// testdata/unifuzz.so everywhere else. Same macOS caveat as
// sqldiffCommand.
func unifuzzLibPath() string {
	name := "unifuzz.so"
	if runtime.GOOS == "windows" {
		name = "unifuzz.dll"
	}
	return filepath.Join("testdata", name)
}

// Helper to run sqldiff with the unifuzz collation library
func runSqlDiff(t *testing.T, dbOriginal, dbModified string) string {
	cmd := exec.Command(sqldiffCommand(), "--lib", unifuzzLibPath(), dbOriginal, dbModified)
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()
	require.NoError(t, err, "sqldiff execution failed: %s", errOut.String())

	return out.String()
}

// Helper to sanitize dynamic values and normalize line endings
func normalizeSQL(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = utcModDateRegex.ReplaceAllString(s, "UTCModDate=[TIMESTAMP_UPDATED]")
	// fsID, anID, and IsPrivate are stripped entirely, not masked with a
	// placeholder like UTCModDate is. The difference: a placeholder still
	// requires the field to appear in the diff at all, which only
	// happens if its value actually changed from what it already was --
	// and every place/source in royal92.rmtree already has these fields
	// at the same value (0) this server always writes for them, so a
	// same-value write is invisible to a before/after diff no matter what
	// this server does or doesn't do. That makes sqldiff comparison
	// fundamentally the wrong tool for confirming these specific fields:
	// it can tell us whether a value changed, not what the value actually
	// is. So they're excluded from this comparison entirely, and verified
	// directly instead -- see the direct assertions in TestWriteOperations
	// itself, right after this comparison, which query the resulting
	// database for these exact fields.
	//
	// LatLongExact is deliberately NOT in this list, even though it was
	// at one point: unlike fsID/anID/IsPrivate, this server doesn't
	// always write the same value for it -- UpdatePlace writes 0 on a
	// Name change, 1 on a coordinates change (see its own doc comment in
	// internal/rmdb/writes.go) -- so a coordinates change produces a
	// real, observable 0 -> 1 transition sqldiff genuinely can verify.
	// Stripping it unconditionally would have hidden exactly the kind of
	// regression it exists to catch.
	s = familySearchIDRegex.ReplaceAllString(s, "")
	s = ancestryIDRegex.ReplaceAllString(s, "")
	s = isPrivateRegex.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// Helper to copy files
func copyFile(t *testing.T, src, dst string) {
	source, err := os.Open(src)
	require.NoError(t, err)
	defer source.Close()

	destination, err := os.Create(dst)
	require.NoError(t, err)
	defer destination.Close()

	_, err = io.Copy(destination, source)
	require.NoError(t, err)
}
