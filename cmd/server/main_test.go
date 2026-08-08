package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/require"
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
var familySearchIDRegex = regexp.MustCompile(`fsID=[0-9]+`)
var ancestryIDRegex = regexp.MustCompile(`anID=-?[0-9]+`)

func TestWriteOperations(t *testing.T) {
	tests := []struct {
		name               string // Name of the test case
		method             string // HTTP method (POST, PUT, DELETE)
		endpoint           string // The API route to hit
		reqBody            string // The JSON payload to send
		goldenFile         string // Path to the expected sqldiff output (.sql)
		expectedStatus     int    // Expected HTTP response code
		baseURL            string // Base URL for the API
		mediaFolder        string // Path to the media folder
		write              bool   // Whether the server is in write mode
		defaultGenerations int    // Default number of generations
		maxPageSize        int    // Maximum page size
	}{
		{
			name:               "POST Place Name Change",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/places/PL423",
			reqBody:            `{"places":[{"id":"PL423","names":[{"value":"Belgrade, Serbia"}]}]}`,
			goldenFile:         "testdata/post_places_expected.sql",
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

				normalizedExpected := string(expectedBytes)
				// Normalize line endings and mask dynamic fields in test output
				normalizedActual := normalizeSQL(actualDiff)

				require.Equal(t, normalizedExpected, normalizedActual)
			}
		})
	}
}

// Helper to run sqldiff with the unifuzz collation library
func runSqlDiff(t *testing.T, dbOriginal, dbModified string) string {
	dllPath := filepath.Join("testdata", "unifuzz.dll")

	cmd := exec.Command("sqldiff.exe", "--lib", dllPath, dbOriginal, dbModified)
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
	s = familySearchIDRegex.ReplaceAllString(s, "fsID=[FAMILYSEARCH_ID_UPDATED]")
	s = ancestryIDRegex.ReplaceAllString(s, "anID=[ANCESTRY_ID_UPDATED]")
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
