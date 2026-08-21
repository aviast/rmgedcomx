package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/aviast/rmgedcomx/internal/loglevel"
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

// TestPersonAndRelationshipHaveCollectionLink confirms both the Person
// state (RS spec Section 4.10.4, "Transitions") and the Relationship
// state (Section 4.21.4, "Transitions") carry a "collection" link back
// to the Collection state that contains them -- a real gap, prompted by
// direct review, not found independently: this server previously
// produced neither, only links to sub-resources under each (.../parents,
// .../children, .../spouses for Person; nothing at all for
// Relationship). Verified as a genuine round trip, not just a string
// match: the href is actually fetched and confirmed to return this same
// collection's own resource, not merely present with a plausible-looking
// value.
func TestPersonAndRelationshipHaveCollectionLink(t *testing.T) {
	router, cleanup := SetupRouter([]string{"../../royal92.rmtree"}, "http://localhost:8080", "testdata/media", false, 4, 200)
	defer cleanup()
	testServer := httptest.NewServer(router)
	defer testServer.Close()

	const wantCollectionURL = "http://localhost:8080/collections/victoria-hanover-royal92"

	fetchAndCheck := func(t *testing.T, path string) {
		t.Helper()
		resp, err := http.Get(testServer.URL + path)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
		require.Contains(t, string(body), `"collection":{"href":"`+wantCollectionURL+`"}`)

		collResp, err := http.Get(strings.Replace(wantCollectionURL, "http://localhost:8080", testServer.URL, 1))
		require.NoError(t, err)
		defer collResp.Body.Close()
		collBody, err := io.ReadAll(collResp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, collResp.StatusCode, "the collection link's own href should resolve: %s", collBody)
		require.Contains(t, string(collBody), `"id":"victoria-hanover-royal92"`)
	}

	t.Run("Person", func(t *testing.T) {
		fetchAndCheck(t, "/collections/victoria-hanover-royal92/persons/P1")
	})
	t.Run("Relationship", func(t *testing.T) {
		fetchAndCheck(t, "/collections/victoria-hanover-royal92/relationships/F1")
	})
}

func TestDisplayPropertiesBirthDeathMarriage(t *testing.T) {
	post := func(t *testing.T, testServer *httptest.Server, path, body string) (int, string, http.Header) {
		t.Helper()
		req, err := http.NewRequest("POST", testServer.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(respBody), resp.Header
	}
	get := func(t *testing.T, testServer *httptest.Server, location string) (int, string) {
		t.Helper()
		url := strings.Replace(location, "http://localhost:8080", testServer.URL, 1)
		resp, err := http.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(body)
	}
	newTestServer := func(t *testing.T) *httptest.Server {
		t.Helper()
		tempDir := t.TempDir()
		tempDBPath := filepath.Join(tempDir, "empty.rmtree")
		copyFile(t, "../../testdata/empty.rmtree", tempDBPath)
		router, cleanup := SetupRouter([]string{tempDBPath}, "http://localhost:8080", "testdata/media", true, 4, 200)
		t.Cleanup(cleanup)
		return httptest.NewServer(router)
	}

	t.Run("birthPlace and deathPlace are populated from the person's own facts", func(t *testing.T) {
		testServer := newTestServer(t)
		status, respBody, headers := post(t, testServer, "/collections/empty/persons", `{"persons":[{
			"names":[{"nameForms":[{"fullText":"Test Person"}]}],
			"facts":[
				{"type":"http://gedcomx.org/Birth","place":{"original":"Boston, MA"}},
				{"type":"http://gedcomx.org/Death","place":{"original":"Hyannis Port, MA"}}
			]
		}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, body := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", body)
		require.Contains(t, body, `"birthPlace":"Boston, MA"`)
		require.Contains(t, body, `"deathPlace":"Hyannis Port, MA"`)
	})

	t.Run("marriageDate and marriagePlace are populated from a Couple relationship's Marriage fact", func(t *testing.T) {
		testServer := newTestServer(t)
		_, respBody, h1 := post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Spouse One"}]}],"gender":{"type":"http://gedcomx.org/Male"}}]}`)
		require.Contains(t, h1.Get("Location"), "/persons/P1", "body: %s", respBody)
		_, respBody, h2 := post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Spouse Two"}]}],"gender":{"type":"http://gedcomx.org/Female"}}]}`)
		require.Contains(t, h2.Get("Location"), "/persons/P2", "body: %s", respBody)

		status, respBody, _ := post(t, testServer, "/collections/empty/relationships", `{"relationships":[{
			"type":"http://gedcomx.org/Couple",
			"person1":{"resourceId":"P1"},
			"person2":{"resourceId":"P2"},
			"facts":[{"type":"http://gedcomx.org/Marriage","date":{"original":"3 Jun 1972","formal":"+1972-06-03"},"place":{"original":"New York, NY"}}]
		}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, body := get(t, testServer, h1.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", body)
		require.Contains(t, body, `"marriageDate":"3 Jun 1972"`)
		require.Contains(t, body, `"marriagePlace":"New York, NY"`)
	})

	t.Run("a person with no marriage at all has marriageDate/marriagePlace absent, not empty strings", func(t *testing.T) {
		testServer := newTestServer(t)
		status, respBody, headers := post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Never Married"}]}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, body := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", body)
		require.NotContains(t, body, `"marriageDate"`)
		require.NotContains(t, body, `"marriagePlace"`)
	})

	t.Run("a family with no Marriage fact is skipped in favor of a later family that has one", func(t *testing.T) {
		testServer := newTestServer(t)
		_, respBody, hParent := post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Twice Married"}]}],"gender":{"type":"http://gedcomx.org/Male"}}]}`)
		require.Contains(t, hParent.Get("Location"), "/persons/P1", "body: %s", respBody)
		status, respBody, _ := post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"First Spouse"}]}],"gender":{"type":"http://gedcomx.org/Female"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody) // P2
		status, respBody, _ = post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Second Spouse"}]}],"gender":{"type":"http://gedcomx.org/Female"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody) // P3

		// First family: no Marriage fact at all.
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P2"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		// Second family: has one.
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{
			"type":"http://gedcomx.org/Couple",
			"person1":{"resourceId":"P1"},
			"person2":{"resourceId":"P3"},
			"facts":[{"type":"http://gedcomx.org/Marriage","date":{"original":"1 Jan 1990"},"place":{"original":"London, England"}}]
		}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, body := get(t, testServer, hParent.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", body)
		require.Contains(t, body, `"marriageDate":"1 Jan 1990"`)
		require.Contains(t, body, `"marriagePlace":"London, England"`)
	})
}

// TestDisplayPropertiesFamiliesAsParentAndChild confirms
// DisplayProperties.familiesAsParent/familiesAsChild (RS spec Section
// 2.2) are populated -- checked directly against the spec's own
// properties table and the separate FamilyView data type (Section 2.3)
// before implementing any of this, not assumed. Unlike
// marriageDate/marriagePlace, these carry no "which one" ambiguity --
// OPTIONAL, "Order is preserved" *lists*, so every family a person is a
// parent or a child in is included. Self-contained via the write API
// rather than depending on royal92.rmtree's specific data, matching
// this project's own established preference for a permanent regression
// test.
func TestDisplayPropertiesFamiliesAsParentAndChild(t *testing.T) {
	post := func(t *testing.T, testServer *httptest.Server, path, body string) (int, string, http.Header) {
		t.Helper()
		req, err := http.NewRequest("POST", testServer.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(respBody), resp.Header
	}
	get := func(t *testing.T, testServer *httptest.Server, location string) (int, string) {
		t.Helper()
		url := strings.Replace(location, "http://localhost:8080", testServer.URL, 1)
		resp, err := http.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(body)
	}
	newTestServer := func(t *testing.T) *httptest.Server {
		t.Helper()
		tempDir := t.TempDir()
		tempDBPath := filepath.Join(tempDir, "empty.rmtree")
		copyFile(t, "../../testdata/empty.rmtree", tempDBPath)
		router, cleanup := SetupRouter([]string{tempDBPath}, "http://localhost:8080", "testdata/media", true, 4, 200)
		t.Cleanup(cleanup)
		return httptest.NewServer(router)
	}

	t.Run("a person with no families at all has both fields absent, not empty arrays", func(t *testing.T) {
		testServer := newTestServer(t)
		status, respBody, headers := post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Solo Person"}]}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, body := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", body)
		require.NotContains(t, body, `"familiesAsParent"`)
		require.NotContains(t, body, `"familiesAsChild"`)
	})

	t.Run("familiesAsParent has parent1=father, parent2=mother, and every child", func(t *testing.T) {
		testServer := newTestServer(t)
		_, respBody, hFather := post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Father"}]}],"gender":{"type":"http://gedcomx.org/Male"}}]}`)
		require.Contains(t, hFather.Get("Location"), "/persons/P1", "body: %s", respBody)
		status, respBody, _ := post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Mother"}]}],"gender":{"type":"http://gedcomx.org/Female"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody) // P2
		status, respBody, _ = post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Child One"}]}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody) // P3
		status, respBody, _ = post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Child Two"}]}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody) // P4

		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P2"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		// Each child needs linking to BOTH parents separately -- a bare,
		// single-parent ParentChild request never assumes the child
		// belongs to that parent's existing family (see
		// CreateParentChildRelationship's own comment in
		// internal/rmdb/createparentchild.go); only linking to both
		// merges the child into the same family as the Couple above.
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P1"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P2"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P1"},"person2":{"resourceId":"P4"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P2"},"person2":{"resourceId":"P4"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, body := get(t, testServer, hFather.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", body)
		require.Contains(t, body, `"familiesAsParent":[{"parent1":{"resource":"http://localhost:8080/collections/empty/persons/P1","resourceId":"P1"},"parent2":{"resource":"http://localhost:8080/collections/empty/persons/P2","resourceId":"P2"},"children":[{"resource":"http://localhost:8080/collections/empty/persons/P3","resourceId":"P3"},{"resource":"http://localhost:8080/collections/empty/persons/P4","resourceId":"P4"}]}]`)
	})

	t.Run("familiesAsChild includes this person alongside any siblings", func(t *testing.T) {
		testServer := newTestServer(t)
		status, respBody, _ := post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Father"}]}],"gender":{"type":"http://gedcomx.org/Male"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody) // P1
		status, respBody, _ = post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Mother"}]}],"gender":{"type":"http://gedcomx.org/Female"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody) // P2
		status, respBody, hChild := post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"This Person"}]}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody) // P3
		status, respBody, _ = post(t, testServer, "/collections/empty/persons", `{"persons":[{"names":[{"nameForms":[{"fullText":"Sibling"}]}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody) // P4

		// Both children need linking to BOTH parents separately, the
		// same requirement as the familiesAsParent test above, for the
		// same reason -- without a second, shared parent confirming it,
		// nothing tells this server these two children actually belong
		// to the same family rather than merely sharing one parent in
		// common (a real, different scenario this project's own design
		// has already had to distinguish -- see
		// CreateParentChildRelationship's own comment).
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P1"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P2"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P1"},"person2":{"resourceId":"P4"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships", `{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P2"},"person2":{"resourceId":"P4"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, body := get(t, testServer, hChild.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", body)
		require.Contains(t, body, `"familiesAsChild":[{"parent1":{"resource":"http://localhost:8080/collections/empty/persons/P1","resourceId":"P1"},"parent2":{"resource":"http://localhost:8080/collections/empty/persons/P2","resourceId":"P2"},"children":[{"resource":"http://localhost:8080/collections/empty/persons/P3","resourceId":"P3"},{"resource":"http://localhost:8080/collections/empty/persons/P4","resourceId":"P4"}]}]`)
	})
}

// Pre-compile regex to replace dynamic timestamps in sqldiff output
var utcModDateRegex = regexp.MustCompile(`UTCModDate=[0-9.]+`)
var familySearchIDRegex = regexp.MustCompile(`,\s*fsID=-?[0-9]+`)
var ancestryIDRegex = regexp.MustCompile(`,\s*anID=-?[0-9]+`)
var latLongExactRegex = regexp.MustCompile(`,\s*LatLongExact=[0-9]+`)
var isPrivateRegex = regexp.MustCompile(`,\s*IsPrivate=[0-9]+`)

// configTableDataRecRegex strips ConfigTable's DataRec column -- an
// opaque, undocumented XML blob (~15KB in royal92.rmtree) holding
// RootsMagic's own UI layout/window state (panel collapsed/expanded
// flags, last-viewed person, and the like), not genealogical data. A
// real captured diff showed this whole blob rewritten by a plain "add a
// comment to a Source" edit -- turned out to be a red herring, not new
// functionality this server needs: the one specific value that had
// changed (MediaCollapsed_Citations) matched byte-for-byte between a
// completely unrelated reference copy and the "after" state here, which
// means it reflects whatever a UI panel was left at, not a deterministic
// consequence of the edit that was actually made. See SCOPE.md's "Write
// support" section for the full account. This server never writes
// DataRec at all (only ConfigTable's UTCModDate), so this regex is a
// no-op against this server's own output -- it exists to also strip the
// same pattern from a golden file's raw content at comparison time (see
// where this is used below), as a safety net against a future capture
// leaving a multi-kilobyte hex blob only partially cleaned up by hand.
//
// Matches with a *trailing* comma, not a leading one like the other
// strip regexes above -- confirmed directly against a real capture that
// DataRec is always the first column ConfigTable's SET clause touches
// (UTCModDate always follows it, never precedes it), the opposite
// position from fsID/anID/LatLongExact/IsPrivate, which are never first
// in their own SET clauses. A leading-comma pattern here would silently
// fail to match at all.
var configTableDataRecRegex = regexp.MustCompile(`DataRec=x'[0-9a-fA-F]*',\s*`)

func TestWriteOperations(t *testing.T) {
	tests := []struct {
		name               string       // Name of the test case
		method             string       // HTTP method (POST, PUT, DELETE)
		endpoint           string       // The API route to hit
		reqBody            string       // The JSON payload to send
		goldenFile         string       // Path to the expected sqldiff output (.sql)
		verifyFields       []fieldCheck // Fields whose expected value this server determines deterministically -- see fieldCheck's own comment for why sqldiff can't be trusted for these
		expectedStatus     int          // Expected HTTP response code
		baseURL            string       // Base URL for the API
		mediaFolder        string       // Path to the media folder
		write              bool         // Whether the server is in write mode
		defaultGenerations int          // Default number of generations
		maxPageSize        int          // Maximum page size
	}{
		{
			name:               "POST Place Name Change",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/places/PL423",
			reqBody:            `{"places":[{"id":"PL423","names":[{"value":"Belgrade, Serbia"}]}]}`,
			goldenFile:         "testdata/post_places_name_expected.sql",
			verifyFields:       []fieldCheck{{table: "PlaceTable", idCol: "PlaceID", idVal: "423", expected: map[string]int{"fsID": 0, "anID": 0, "LatLongExact": 0}}},
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
			verifyFields:       []fieldCheck{{table: "PlaceTable", idCol: "PlaceID", idVal: "423", expected: map[string]int{"fsID": 0, "anID": 0, "LatLongExact": 0}}},
			expectedStatus:     http.StatusNoContent,
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              true,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			// LatLongExact=1 is this server's own deterministic behavior
			// for a coordinates change (see UpdatePlace's own doc
			// comment) -- verified directly via verifyFields, not
			// through the golden-file/sqldiff comparison, since
			// RootsMagic's own value for this field is itself
			// non-deterministic (see TESTING.md's "Non-deterministic
			// fields" section) and stripped from that comparison
			// entirely, the same as fsID/anID.
			name:               "POST Place Coordinates Change",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/places/PL423",
			reqBody:            `{"places":[{"id":"PL423","latitude":44.817778,"longitude":20.456944}]}`,
			goldenFile:         "testdata/post_places_coordinates_expected.sql",
			verifyFields:       []fieldCheck{{table: "PlaceTable", idCol: "PlaceID", idVal: "423", expected: map[string]int{"LatLongExact": 1}}},
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
			reqBody:            `{"places":[{"id":"PL882","names":[{"value":"Odessa, Ukraine"}],"notes":[{"text":"Odesa, also spelled Odessa, is the third-most populous city and municipality in Ukraine and a major seaport and transport hub located in the south-west of the country, on the northwestern shore of the Black Sea."}],"latitude":46.485722,"longitude":30.743444}]}`,
			goldenFile:         "testdata/post_places_all_fields_expected.sql",
			verifyFields:       []fieldCheck{{table: "PlaceTable", idCol: "PlaceID", idVal: "882", expected: map[string]int{"fsID": 0, "anID": 0, "LatLongExact": 1}}},
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
			verifyFields:       []fieldCheck{{table: "SourceTable", idCol: "SourceID", idVal: "1", expected: map[string]int{"IsPrivate": 0}}},
			expectedStatus:     http.StatusNoContent,
			baseURL:            "http://localhost:8080",
			mediaFolder:        "testdata/media",
			write:              true,
			defaultGenerations: 4,
			maxPageSize:        200,
		},
		{
			// ConfigTable.DataRec was in RootsMagic's own real captured
			// diff for this exact edit -- an opaque ~15KB XML blob of UI
			// layout state (see configTableDataRecRegex's own comment)
			// that turned out to be unrelated to this edit at all, not
			// something this server needs to reproduce. Stripped from
			// the golden file itself and from comparison via
			// configTableDataRecRegex, same as fsID/anID/LatLongExact/
			// IsPrivate above.
			name:               "POST Source Comments Change",
			method:             "POST",
			endpoint:           "/collections/victoria-hanover-royal92/source-descriptions/S2",
			reqBody:            `{"sourceDescriptions":[{"id":"S2","notes":[{"text":"Added comment."}]}]}`,
			goldenFile:         "testdata/post_sources_comments_expected.sql",
			verifyFields:       []fieldCheck{{table: "SourceTable", idCol: "SourceID", idVal: "2", expected: map[string]int{"IsPrivate": 0}}},
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
				//
				// configTableDataRecRegex is also applied here, to the
				// golden file's own raw content, not just inside
				// normalizeSQL -- unlike the other stripped fields (which
				// rely on whoever captured the golden file having removed
				// them by hand, an established and so far reliable
				// convention), a ~15KB hex blob is exactly the kind of
				// thing that's easy to leave only partially cleaned up.
				// Stripping it from both sides means a future golden file
				// that still has a leftover DataRec clause won't cause a
				// spurious failure -- see configTableDataRecRegex's own
				// comment for the full story behind why this field is
				// excluded at all.
				normalizedExpected := normalizeSQL(string(expectedBytes))
				// Normalize line endings and mask/strip dynamic fields in test output
				normalizedActual := normalizeSQL(actualDiff)

				require.Equal(t, normalizedExpected, normalizedActual)
			}

			// 6. Directly verify the fields step 5's comparison deliberately
			// excludes (see normalizeSQL's own comment, and fieldCheck's,
			// for the full reasoning): confirm this server's own
			// deterministic value, not just "didn't fail to change" or
			// "matches one particular RootsMagic capture." sqldiff can
			// only ever tell us whether a value changed between two
			// database states, never what the value actually is or
			// should be -- and for fields downstream of a
			// non-deterministic external lookup on RootsMagic's own side,
			// there's no single "correct" captured value to compare
			// against in the first place. This is the check that
			// verifies what this server can actually promise.
			for _, check := range tc.verifyFields {
				verifyFields(t, tempDBPath, check)
			}
		})
	}
}

// TestRelationshipMediaWrite covers handleUpdateRelationship's own logic
// directly -- separate from TestWriteOperations' table above, which is
// specifically for sqldiff-golden-file comparisons against a real
// RootsMagic capture. There's no RootsMagic capture to compare against
// here (media linking doesn't touch any field a golden file would show;
// see rmdb.UpdateOwnerMedia's own tests in internal/rmdb for the
// underlying diffing logic), but handleUpdateRelationship has real,
// non-trivial branching of its own -- couple vs. parent-child id
// handling, the both-partners-present check -- worth guarding against
// regression with a permanent test, not just the one-off manual
// verification used to build it.
func TestRelationshipMediaWrite(t *testing.T) {
	post := func(t *testing.T, testServer *httptest.Server, path, body string) (int, string) {
		t.Helper()
		req, err := http.NewRequest("POST", testServer.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(respBody)
	}

	tempDir := t.TempDir()
	tempDBPath := filepath.Join(tempDir, "royal92.rmtree")
	copyFile(t, "../../royal92.rmtree", tempDBPath)

	router, cleanup := SetupRouter(
		[]string{tempDBPath},
		"http://localhost:8080",
		"testdata/media",
		true, // write
		4,
		200,
	)
	testServer := httptest.NewServer(router)
	defer testServer.Close()
	defer cleanup()

	prefix := "/collections/victoria-hanover-royal92/relationships/"

	// F1 is Victoria and Albert's real couple relationship in
	// royal92.rmtree -- both FatherID and MotherID present, a valid
	// target.
	status, body := post(t, testServer, prefix+"F1", `{"relationships":[{"id":"F1","media":[{"descriptionId":"M1"}]}]}`)
	require.Equal(t, http.StatusNoContent, status, "attaching media to a real couple relationship: %s", body)

	// Confirm it actually took, via GET, not just trusting the 204.
	getResp, err := http.Get(testServer.URL + prefix + "F1")
	require.NoError(t, err)
	defer getResp.Body.Close()
	getBody, err := io.ReadAll(getResp.Body)
	require.NoError(t, err)
	require.Contains(t, string(getBody), `"descriptionId":"M1"`, "expected the newly attached media to show up on a subsequent GET")

	// A parent-child relationship id must be rejected -- RootsMagic has
	// no place to attach media to a specific parent-child pair.
	status, body = post(t, testServer, prefix+"F1-FC2", `{"relationships":[{"id":"F1-FC2","media":[{"descriptionId":"M1"}]}]}`)
	require.Equal(t, http.StatusBadRequest, status, "parent-child relationship id should be rejected: %s", body)

	// F70 has MotherID=0 in royal92.rmtree -- a single-partner family,
	// not a valid couple relationship target.
	status, body = post(t, testServer, prefix+"F70", `{"relationships":[{"id":"F70","media":[{"descriptionId":"M1"}]}]}`)
	require.Equal(t, http.StatusNotFound, status, "single-partner family should 404, not be treated as a valid couple: %s", body)

	// A nonexistent family id.
	status, body = post(t, testServer, prefix+"F999999", `{"relationships":[{"id":"F999999","media":[{"descriptionId":"M1"}]}]}`)
	require.Equal(t, http.StatusNotFound, status, "nonexistent family should 404: %s", body)

	// A nonexistent artifact reference.
	status, body = post(t, testServer, prefix+"F2", `{"relationships":[{"id":"F2","media":[{"descriptionId":"M999"}]}]}`)
	require.Equal(t, http.StatusBadRequest, status, "nonexistent artifact should 400: %s", body)

	// facts present alongside media should succeed, not be rejected --
	// only logged as unsupported (verified in the one-off manual pass
	// this test formalizes; not re-asserted here since it's a log line,
	// not response-visible behavior).
	status, body = post(t, testServer, prefix+"F1", `{"relationships":[{"id":"F1","facts":[{"type":"http://gedcomx.org/Marriage"}],"media":[{"descriptionId":"M1"}]}]}`)
	require.Equal(t, http.StatusNoContent, status, "facts present alongside media should succeed, not be rejected: %s", body)

	// An actually-unknown field should still be rejected by
	// decodeStrictJSON, same as every other write handler.
	status, body = post(t, testServer, prefix+"F1", `{"relationships":[{"id":"F1","bogus":"field"}]}`)
	require.Equal(t, http.StatusBadRequest, status, "unknown field should be rejected: %s", body)
	require.Contains(t, body, "bogus", "error message should name the specific unrecognized field")
}

// fieldCheck names a single row and a set of columns on it whose expected
// value this server determines deterministically -- independent of
// whatever RootsMagic itself produced for the same fields when a golden
// file was captured. fsID/anID/LatLongExact on Place, IsPrivate on
// Source: all four are downstream, on RootsMagic's own side, of a
// non-deterministic external network lookup (FamilySearch/Ancestry), not
// a reliable success/fail signal -- see TESTING.md's "Non-deterministic
// fields" section for the real captured evidence this conclusion is
// based on, and internal/rmdb/writes.go's own comments on
// UpdatePlace/UpdateSource for what this server actually writes for each
// and why.
//
// These are checked directly, by querying the resulting database after
// the write, rather than through the sqldiff-based golden-file comparison
// every other field goes through. That's deliberate, not a shortcut:
// sqldiff (like any before/after diff) only reports columns whose value
// actually *changed* between two states, and can't tell us what RootsMagic
// "should" have produced independent of one specific, possibly-flaky
// capture -- a direct query is the only way to confirm this server's own
// value, independent of whatever RootsMagic happened to do.
type fieldCheck struct {
	table    string
	idCol    string
	idVal    string
	expected map[string]int // column name -> expected value
}

func verifyFields(t *testing.T, dbPath string, check fieldCheck) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err, "opening database to verify fields")
	defer db.Close()

	columns := make([]string, 0, len(check.expected))
	for col := range check.expected {
		columns = append(columns, col)
	}
	sort.Strings(columns) // deterministic query text and scan order

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s",
		strings.Join(columns, ", "), check.table, check.idCol, check.idVal)
	row := db.QueryRow(query)

	values := make([]int, len(columns))
	scanTargets := make([]any, len(values))
	for i := range values {
		scanTargets[i] = &values[i]
	}
	require.NoError(t, row.Scan(scanTargets...), "querying %s", query)

	for i, col := range columns {
		want := check.expected[col]
		require.Equal(t, want, values[i], "%s.%s should be %d after this server's write, was %d", check.table, col, want, values[i])
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
	// fsID, anID, LatLongExact, and IsPrivate are stripped entirely, not
	// masked with a placeholder like UTCModDate is, and not compared
	// against sqldiff's output against a golden file at all -- see
	// TESTING.md's "Non-deterministic fields" section for the full
	// account of why, and the real captured evidence behind it. Verified
	// directly instead, via fieldCheck/verifyFields below, which assert
	// this server's own deterministic behavior independent of whatever
	// RootsMagic happened to produce in one particular capture.
	//
	// LatLongExact briefly wasn't in this list -- reasoned, at the time,
	// that unlike fsID/anID/IsPrivate (always the same value regardless
	// of what changed) a coordinates change makes this server write a
	// different, real value (1 instead of 0), so sqldiff ought to be able
	// to verify that transition. That reasoning turned out to be
	// incomplete: RootsMagic's own value for it is ALSO downstream of the
	// same non-deterministic FamilySearch/Ancestry lookup as fsID/anID,
	// confirmed by two otherwise-identical "change every field at once"
	// captures against the same place (Belgrade) that disagreed with each
	// other on LatLongExact alone -- everything else about them matched.
	// A golden file capturing one specific run's LatLongExact value was
	// never something to chase in the first place.
	s = familySearchIDRegex.ReplaceAllString(s, "")
	s = ancestryIDRegex.ReplaceAllString(s, "")
	s = latLongExactRegex.ReplaceAllString(s, "")
	s = isPrivateRegex.ReplaceAllString(s, "")
	s = configTableDataRecRegex.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// TestCreatePersonsHTTP covers the full POST /persons flow through the
// real HTTP stack -- request parsing, fact-type/gender/name-type
// resolution, and the RS spec's own 201-vs-204 semantics (Section
// 4.9.2) -- starting from a genuinely empty database, the same starting
// point this feature's own reference capture used (see SCOPE.md's
// "Stage 3" section). Separate from TestCreatePersonMatchesRealCapturedData
// and its siblings in internal/rmdb, which cover CreatePerson's own
// storage-layer correctness field-by-field against the real capture;
// this one is specifically about the HTTP request/response contract on
// top of it.
func TestCreatePersonsHTTP(t *testing.T) {
	post := func(t *testing.T, testServer *httptest.Server, body string) (int, string, http.Header) {
		t.Helper()
		req, err := http.NewRequest("POST", testServer.URL+"/collections/empty/persons", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(respBody), resp.Header
	}

	get := func(t *testing.T, testServer *httptest.Server, location string) (int, string) {
		t.Helper()
		url := strings.Replace(location, "http://localhost:8080", testServer.URL, 1)
		resp, err := http.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(body)
	}

	newTestServer := func(t *testing.T, write bool) *httptest.Server {
		t.Helper()
		tempDir := t.TempDir()
		tempDBPath := filepath.Join(tempDir, "empty.rmtree")
		copyFile(t, "../../testdata/empty.rmtree", tempDBPath)
		router, cleanup := SetupRouter([]string{tempDBPath}, "http://localhost:8080", "testdata/media", write, 4, 200)
		t.Cleanup(cleanup)
		return httptest.NewServer(router)
	}

	// Collection id for an empty.rmtree-backed collection -- derived the
	// same way every other collection id in this project is (see
	// internal/collectionid), confirmed empirically here rather than
	// assumed: "empty" -- empty.rmtree has no root person (nothing to
	// derive a name-based id from), so this falls back to the filename
	// stem.
	t.Run("single person creates 201 with Location, GET confirms it", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{
			"gender":{"type":"http://gedcomx.org/Male"},
			"names":[{"preferred":true,"nameForms":[{"fullText":"Patrick Brontë","parts":[
				{"type":"http://gedcomx.org/Given","value":"Patrick"},
				{"type":"http://gedcomx.org/Surname","value":"Brontë"}
			]}]}],
			"facts":[
				{"type":"http://gedcomx.org/Birth","date":{"formal":"+1777-03-17"},"place":{"original":"County Down, Ireland"}},
				{"type":"http://gedcomx.org/Death","date":{"formal":"+1861-06-07"},"place":{"original":"Haworth, Yorks."}}
			]
		}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		loc := headers.Get("Location")
		require.Contains(t, loc, "/persons/P1", "Location header: %q", loc)

		getResp, err := http.Get(strings.Replace(loc, "http://localhost:8080", testServer.URL, 1))
		require.NoError(t, err)
		defer getResp.Body.Close()
		getBody, err := io.ReadAll(getResp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, getResp.StatusCode, "GET body: %s", getBody)
		require.Contains(t, string(getBody), `"fullText":"Patrick Brontë"`)
		require.Contains(t, string(getBody), `"formal":"+1777-03-17"`)
		require.Contains(t, string(getBody), `County Down, Ireland`)
	})

	// RS spec Section 4.10.5, "Embedded States": child-relationships,
	// parent-relationships, and spouse-relationships are each MUST for
	// the Person state -- either a link, or embedded directly. A person
	// with genuinely zero relationships (never linked to anyone via
	// Couple or ParentChild) still needs the field present as an empty
	// array, not absent or null -- an absent field would be
	// indistinguishable from a server that doesn't support this
	// embedded state at all.
	t.Run("a person with no relationships gets an empty relationships array, not null or absent", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[{"nameForms":[{"fullText":"Solo Person"}]}]}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"relationships":[]`)
	})

	t.Run("multiple persons in one request creates 204", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[
			{"names":[{"preferred":true,"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"Arthur Bell"},{"type":"http://gedcomx.org/Surname","value":"Nicholls"}]}]}]},
			{"names":[{"preferred":true,"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"Eleanor"},{"type":"http://gedcomx.org/Surname","value":"McClory"}]}]}]}
		]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusNoContent, status, "body: %s", respBody)
		require.Empty(t, headers.Get("Location"), "no single Location header when multiple persons were created")
	})

	// Note: a Person with no names field at all used to be rejected here
	// ("missing name is rejected"). That test is gone, not fixed in
	// place -- Person.names turned out to be OPTIONAL all along (see
	// buildNewPerson's own comment below), so the premise the test was
	// named for no longer holds. See "person with no names field at all
	// is accepted..." below for what replaced it.

	// A real bug found via a real user report: a NameForm with only
	// fullText (no parts) is fully spec-compliant -- GEDCOM X's own
	// conceptual model (Section 3.19) marks both fullText and parts
	// independently OPTIONAL, checked directly against the spec text --
	// and this server previously rejected it outright. Fixed to fall
	// back to storing the whole fullText in Given, Surname empty,
	// confirmed to be RootsMagic's own real, deterministic behavior for
	// exactly this situation (a GEDCOM 5.x name with no marked surname,
	// e.g. this project's own royal92.ged test file's real line "1 NAME
	// Albert Augustus Charles//") rather than a guessed split. See
	// buildNewPersonName's own comment in internal/api/createperson.go
	// for the full account, including the real royal92.rmtree row this
	// was checked against directly.
	t.Run("name with only fullText, no parts, falls back to Given=fullText, Surname empty", func(t *testing.T) {
		testServer := newTestServer(t, true)
		status, body, headers := post(t, testServer, `{"persons":[{"names":[{"nameForms":[{"fullText":"Albert Augustus Charles"}]}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", body)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"value":"Albert Augustus Charles"`)
	})

	// A second real bug, found via a second real report: a NameForm with
	// neither parts nor fullText -- a person with no usable name in the
	// source data at all (a real royal92.ged individual entered with
	// only a title, sex, and death year) -- was also being rejected,
	// for the same overreach as the fullText-only case above. RootsMagic's
	// own confirmed behavior for exactly this case is not "no NameTable
	// row" -- it's one NameTable row with Given="" and Surname="" (see
	// buildNewPersonName's own comment for the real royal92.rmtree row
	// this was checked against). This person also has no explicit
	// "preferred" name marker, which surfaced a third, related bug while
	// testing this exact request: IsPrimary came out 0 on the person's
	// only name, when every real royal92.rmtree row checked throughout
	// this project has IsPrimary=1 on the first/only name. Fixed
	// alongside this one -- see buildNewPerson's own comment.
	t.Run("name with neither parts nor fullText is accepted, empty Given/Surname, IsPrimary defaults to true", func(t *testing.T) {
		testServer := newTestServer(t, true)
		status, body, headers := post(t, testServer, `{"persons":[{"names":[{"nameForms":[{}]}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", body)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"preferred":true`)
	})

	t.Run("person with no names field at all is accepted, one empty primary name created", func(t *testing.T) {
		testServer := newTestServer(t, true)
		status, body, headers := post(t, testServer, `{"persons":[{"gender":{"type":"http://gedcomx.org/Male"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", body)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"preferred":true`)
	})

	t.Run("second name explicitly marked preferred overrides the first-name default", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[
			{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"Not"},{"type":"http://gedcomx.org/Surname","value":"Preferred"}]}]},
			{"preferred":true,"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"Is"},{"type":"http://gedcomx.org/Surname","value":"Preferred"}]}]}
		]}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"value":"Is"`)
	})

	// GEDCOM 5.x nests a nickname within the same NAME record ("2 NICK"
	// under "1 NAME"), but GEDCOM X models it as its own, separate Name
	// (type=Nickname) -- checked directly against the conceptual model
	// spec's "Known Name Types," Section 3.13.1, not assumed. RootsMagic's
	// own schema doesn't have a matching "separate nickname entity"
	// concept either -- NameTable.Nickname is a single column on one name
	// record. Prompted by a direct request, not found independently: the
	// first Nickname-type Name's text is attached to the *primary* name's
	// own record, not given a NameTable row of its own.
	t.Run("Nickname-type Name attaches to the primary name's Nickname column, not its own row", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[
			{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"Patrick"},{"type":"http://gedcomx.org/Surname","value":"Brontë"}]}],"preferred":true},
			{"type":"http://gedcomx.org/Nickname","nameForms":[{"fullText":"Paddy"}]}
		],"gender":{"type":"http://gedcomx.org/Male"}}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `{"type":"http://gedcomx.org/Nickname","nameForms":[{"fullText":"Paddy"}]}`, "the nickname should round-trip back as its own synthesized Name on GET")
	})

	t.Run("nickname attaches to whichever name ends up primary, not always names[0]", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[
			{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"Not"},{"type":"http://gedcomx.org/Surname","value":"Preferred"}]}]},
			{"preferred":true,"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"Is"},{"type":"http://gedcomx.org/Surname","value":"Preferred"}]}]},
			{"type":"http://gedcomx.org/Nickname","nameForms":[{"fullText":"Izzy"}]}
		]}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"fullText":"Izzy"`)
	})

	t.Run("a second Nickname-type Name is dropped, not an error, first one still used", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[
			{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"Anne"}]}],"preferred":true},
			{"type":"http://gedcomx.org/Nickname","nameForms":[{"fullText":"First"}]},
			{"type":"http://gedcomx.org/Nickname","nameForms":[{"fullText":"Second"}]}
		]}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"fullText":"First"`)
		require.NotContains(t, getBody, `"fullText":"Second"`, "the second nickname should be dropped, not silently kept somewhere")
	})

	t.Run("Nickname-type Name with no nameForms is rejected", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[
			{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"Anne"}]}],"preferred":true},
			{"type":"http://gedcomx.org/Nickname","nameForms":[]}
		]}]}`
		status, respBody, _ := post(t, testServer, body)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", respBody)
	})

	// A real reported gap: a value-only fact (Occupation, Education,
	// Religion -- no date or place of its own) never reached
	// EventTable.Details, even though the read side already reversed
	// this exact mapping the other way (buildFact: e.Details ->
	// f.Value). Reproduces the exact real request this was found with,
	// mixing value-only facts alongside ordinary date/place ones in the
	// same person, to confirm neither kind interferes with the other.
	t.Run("Fact.value is written to EventTable.Details", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons": [{"names": [{"nameForms": [{"fullText": "Joseph Patrick KENNEDY", "parts": [{"type": "http://gedcomx.org/Given", "value": "Joseph Patrick"}, {"type": "http://gedcomx.org/Surname", "value": "KENNEDY"}]}], "preferred": true}], "facts": [{"type": "http://gedcomx.org/Birth", "date": {"original": "6 SEP 1888", "formal": "+1888-09-06"}, "place": {"original": "Boston, MA"}}, {"type": "http://gedcomx.org/Death", "date": {"original": "18 NOV 1969", "formal": "+1969-11-18"}, "place": {"original": "Hyannis Port, MA"}}, {"type": "http://gedcomx.org/Burial", "place": {"original": "Holyhood Cemetery, Brookline, MA"}}, {"type": "http://gedcomx.org/Occupation", "value": "Bank President, Ambassador"}, {"type": "http://gedcomx.org/Education", "value": "Harvard Graduate"}, {"type": "http://gedcomx.org/Religion", "value": "Roman Catholic"}], "gender": {"type": "http://gedcomx.org/Male"}}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"value":"Bank President, Ambassador"`)
		require.Contains(t, getBody, `"value":"Harvard Graduate"`)
		require.Contains(t, getBody, `"value":"Roman Catholic"`)
	})

	t.Run("unrecognized fact type is rejected", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"X"}]}]}],
			"facts":[{"type":"http://gedcomx.org/NotARealFactType"}]}]}`
		status, respBody, _ := post(t, testServer, body)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", respBody)
	})

	t.Run("unsupported date form (before/after/range) is rejected, not guessed", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"X"}]}]}],
			"facts":[{"type":"http://gedcomx.org/Birth","date":{"formal":"/+1910"}}]}]}`
		status, respBody, _ := post(t, testServer, body)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", respBody)
	})

	// A real bug, found via a real user report: Date.formal is REQUIRED
	// by the actual GEDCOM X Date Format specification (Section 5.2.2.1)
	// to have a four-digit, zero-padded year -- checked directly against
	// the spec text before concluding the request, not this server, was
	// the one out of line. "+742-04-02" (Charlemagne's real birth year,
	// 742 AD) is missing that padding and is genuinely invalid Formal.
	// But this server was hard-rejecting the whole request over it, even
	// though the same fact's Date.original (" 2 APR  742") was right
	// there and perfectly parseable. Fixed: an invalid Formal now falls
	// back to Original the same way an absent Formal already did,
	// rather than being treated as a harder failure than having no
	// Formal at all. EncodeRMDate's own strict validation is unchanged --
	// this is entirely about what happens after it correctly rejects
	// something.
	t.Run("invalid Date.formal falls back to Date.original instead of rejecting the request (Charlemagne)", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons": [{"names": [{"nameForms": [{"fullText": "Charlemagne", "parts": [{"type": "http://gedcomx.org/Given", "value": "Charlemagne"}]}]}], "facts": [{"type": "http://gedcomx.org/Birth", "date": {"original": " 2 APR  742", "formal": "+742-04-02"}, "place": {"original": "Aachen,West Germany"}}, {"type": "http://gedcomx.org/Death", "date": {"original": "        814", "formal": "+814"}}], "gender": {"type": "http://gedcomx.org/Male"}}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"formal":"+0742-04-02"`, "birth date should have been parsed from Original and re-encoded with a correctly zero-padded year")
		require.Contains(t, getBody, `"formal":"+0814"`, "death date likewise")
	})

	t.Run("invalid Date.formal AND unparseable Date.original both fall back to no date, with Note, not a rejection", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"X"}]}]}],
			"facts":[{"type":"http://gedcomx.org/Birth","date":{"formal":"/+1910","original":"sometime, who knows"}}]}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `rmgedcomx was unable to parse this text as a date: sometime, who knows`)
	})

	t.Run("valid Date.formal still takes priority over Date.original when both are present", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"X"}]}]}],
			"facts":[{"type":"http://gedcomx.org/Birth","date":{"formal":"+1819-05-24","original":"this text is irrelevant and should be ignored"}}]}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"formal":"+1819-05-24"`)
	})

	// A real bug, found via a real user report: this server only ever
	// consulted Date.formal, silently recording no date at all when a
	// real client -- converting a real GEDCOM file -- sent only
	// Date.original (a real royal92.ged individual, entered with a
	// title, sex, and this exact death date, nothing else). Fixed to
	// fall back to gedcomx.ParseGedcom5Date when formal is absent -- see
	// its own comment in internal/gedcomx/gedcom5date.go for the full
	// account, including why this scope (not the full GEDCOM 5.5.1
	// grammar) was chosen. Verified here against the exact real request
	// this bug was reported with, checking the stored Date/SortDate/
	// DeathYear directly against the real royal92.rmtree values for
	// this same individual, not just that the request succeeds.
	t.Run("Date.original is used as a fallback when Date.formal is absent", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons": [{"names": [{"nameForms": [{"fullText": ""}]}], "facts": [{"type": "http://gedcomx.org/Death", "date": {"original": "       1870"}}], "gender": {"type": "http://gedcomx.org/Male"}}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"formal":"+1870"`, "the GET response should now show a Formal value derived from the stored date")
	})

	// The other side of the same fix: Original present but not in a
	// supported form should not reject the whole request -- the fact
	// (and person) still get created, just without a machine-readable
	// date, the same way a fact with no date at all works today. The
	// unparseable text itself isn't preserved anywhere (RootsMagic's own
	// EventTable.Date has no room for arbitrary free text once
	// ParseGedcom5Date can't interpret it as structured Y/M/D -- see
	// this test's own absence-of-a-date-field assertion below, and
	// SCOPE.md's own note on this as a real, separate possible
	// improvement: EventTable.Note is already unused by every write this
	// project makes and could hold it instead, not attempted here).
	// Added, per an explicit follow-up request: the unparseable text is
	// no longer just logged and lost -- it's preserved in
	// EventTable.Note, prefixed so it's clearly this server's own
	// annotation rather than data RootsMagic itself wrote. Two cases
	// here rather than one: arbitrary free text a client might send,
	// and separately, a form that's real, defined GEDCOM 5.5.1 grammar
	// (checked directly against the actual specification -- gedcom.io/
	// specifications/ged551.pdf, DATE_RANGE, page 47) but not one this
	// server's own ParseGedcom5Date supports yet (see its own comment
	// for why ranges specifically were left out). Both should behave
	// identically from this server's point of view -- neither is a
	// client mistake, both get preserved rather than silently dropped.
	t.Run("unparseable Date.original does not reject the request, original text preserved in Note", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"X"}]}]}],
			"facts":[{"type":"http://gedcomx.org/Birth","date":{"original":"sometime in the 1870s, maybe"}}]}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"type":"http://gedcomx.org/Birth"`, "the fact itself should still exist")
		require.NotContains(t, getBody, `"date"`, "RootsMagic's own Date field has no room for unparseable free text -- confirmed no \"date\" key appears at all, not a guessed or partial one")
		require.Contains(t, getBody, `rmgedcomx was unable to parse this text as a date: sometime in the 1870s, maybe`, "the original text should be preserved in Notes, with the specified prefix")
	})

	t.Run("a real GEDCOM 5.5.1 form this server doesn't support yet (a date range) is also preserved in Note, not rejected", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"names":[{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"X"}]}]}],
			"facts":[{"type":"http://gedcomx.org/Birth","date":{"original":"BET 1900 AND 1910"}}]}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		getStatus, getBody := get(t, testServer, headers.Get("Location"))
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `rmgedcomx was unable to parse this text as a date: BET 1900 AND 1910`)
	})

	t.Run("unrecognized gender type is rejected", func(t *testing.T) {
		testServer := newTestServer(t, true)
		body := `{"persons":[{"gender":{"type":"http://gedcomx.org/Alien"},
			"names":[{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"X"}]}]}]}]}`
		status, respBody, _ := post(t, testServer, body)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", respBody)
	})

	t.Run("empty persons array is rejected", func(t *testing.T) {
		testServer := newTestServer(t, true)
		status, body, _ := post(t, testServer, `{"persons":[]}`)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", body)
	})

	t.Run("malformed JSON is rejected", func(t *testing.T) {
		testServer := newTestServer(t, true)
		status, body, _ := post(t, testServer, `{not json`)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", body)
	})

	t.Run("unknown field is rejected", func(t *testing.T) {
		testServer := newTestServer(t, true)
		status, body, _ := post(t, testServer, `{"persons":[{"bogus":"field"}]}`)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", body)
		require.Contains(t, body, "bogus")
	})

	t.Run("read-only mode rejects with 405, nothing created", func(t *testing.T) {
		testServer := newTestServer(t, false)
		body := `{"persons":[{"names":[{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"X"}]}]}]}]}`
		status, respBody, headers := post(t, testServer, body)
		require.Equal(t, http.StatusMethodNotAllowed, status, "body: %s", respBody)
		require.Equal(t, "GET, HEAD", headers.Get("Allow"))
	})
}

// TestCreateRelationshipsHTTP covers the full POST /relationships flow
// through the real HTTP stack -- both relationship types this server's
// read side already models (Couple and ParentChild), including the
// specific behavior CreateParentChildRelationship's own comment
// documents: creating a couple relationship first, then a single
// ParentChild request naming either parent, correctly surfaces *both*
// father-child and mother-child relationships on a subsequent GET (they
// share one underlying ChildTable row -- see SCOPE.md's "Stage 3"
// section for the full account of why this isn't two separate writes).
func TestCreateRelationshipsHTTP(t *testing.T) {
	post := func(t *testing.T, testServer *httptest.Server, path, body string) (int, string, http.Header) {
		t.Helper()
		req, err := http.NewRequest("POST", testServer.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(respBody), resp.Header
	}
	get := func(t *testing.T, testServer *httptest.Server, path string) (int, string) {
		t.Helper()
		resp, err := http.Get(testServer.URL + path)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(body)
	}
	newTestServer := func(t *testing.T, write bool) *httptest.Server {
		t.Helper()
		tempDir := t.TempDir()
		tempDBPath := filepath.Join(tempDir, "empty.rmtree")
		copyFile(t, "../../testdata/empty.rmtree", tempDBPath)
		router, cleanup := SetupRouter([]string{tempDBPath}, "http://localhost:8080", "testdata/media", write, 4, 200)
		t.Cleanup(cleanup)
		return httptest.NewServer(router)
	}
	createPerson := func(t *testing.T, testServer *httptest.Server, gender, given, surname string) {
		t.Helper()
		body := fmt.Sprintf(
			`{"persons":[{"gender":{"type":%q},"names":[{"preferred":true,"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":%q},{"type":"http://gedcomx.org/Surname","value":%q}]}]}]}]}`,
			gender, given, surname)
		status, respBody, _ := post(t, testServer, "/collections/empty/persons", body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
	}

	t.Run("couple relationship creates 201, GET confirms it", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "Patrick", "Brontë")
		createPerson(t, testServer, "http://gedcomx.org/Female", "Maria", "Branwell")

		body := `{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P2"}}]}`
		status, respBody, headers := post(t, testServer, "/collections/empty/relationships", body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		require.Contains(t, headers.Get("Location"), "/relationships/F1")

		status, getBody := get(t, testServer, "/collections/empty/relationships/F1")
		require.Equal(t, http.StatusOK, status, "body: %s", getBody)
		require.Contains(t, getBody, `"type":"http://gedcomx.org/Couple"`)
	})

	t.Run("couple relationship works regardless of person1/person2 order", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "Patrick", "Brontë")
		createPerson(t, testServer, "http://gedcomx.org/Female", "Maria", "Branwell")

		// Mother listed first -- roles should still resolve correctly by Sex.
		body := `{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P2"},"person2":{"resourceId":"P1"}}]}`
		status, respBody, _ := post(t, testServer, "/collections/empty/relationships", body)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
	})

	t.Run("parent-child after couple surfaces both father-child and mother-child on GET", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "Patrick", "Brontë")
		createPerson(t, testServer, "http://gedcomx.org/Female", "Maria", "Branwell")
		createPerson(t, testServer, "http://gedcomx.org/Female", "Charlotte", "Brontë")

		status, respBody, _ := post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P2"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		// Both links are required under the corrected design (see
		// CreateParentChildRelationship's own comment) -- a bare,
		// single-parent link no longer assumes the child belongs to
		// that parent's existing family. The first creates a new
		// single-parent family for Charlotte; the second recognizes
		// that would duplicate the pre-existing Patrick+Maria family
		// and merges into it, landing on F1.
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P1"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		status, respBody, headers := post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P2"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		require.Contains(t, headers.Get("Location"), "/relationships/F1")

		status, getBody := get(t, testServer, "/collections/empty/relationships")
		require.Equal(t, http.StatusOK, status, "body: %s", getBody)
		require.Contains(t, getBody, `"id":"F1-FC3"`)
		require.Contains(t, getBody, `"id":"F1-MC3"`)
	})

	t.Run("multiple relationships in one request creates 204", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "A", "One")
		createPerson(t, testServer, "http://gedcomx.org/Female", "B", "Two")
		createPerson(t, testServer, "http://gedcomx.org/Male", "C", "Three")
		createPerson(t, testServer, "http://gedcomx.org/Female", "D", "Four")

		body := `{"relationships":[
			{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P2"}},
			{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P3"},"person2":{"resourceId":"P4"}}
		]}`
		status, respBody, headers := post(t, testServer, "/collections/empty/relationships", body)
		require.Equal(t, http.StatusNoContent, status, "body: %s", respBody)
		require.Empty(t, headers.Get("Location"))
	})

	t.Run("couple relationship with two males is rejected", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "A", "One")
		createPerson(t, testServer, "http://gedcomx.org/Male", "B", "Two")

		body := `{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P2"}}]}`
		status, respBody, _ := post(t, testServer, "/collections/empty/relationships", body)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", respBody)
	})

	// Regression test for a real bug found via a real user report: this
	// exact scenario originally returned 500, not 400. The parent-child
	// request itself is entirely well-formed and everything it
	// references genuinely exists -- the ambiguity (which of the
	// father's two families does this child belong to?) can only be
	// discovered by actually querying the database during the create
	// call, which is why it wasn't caught by earlier request-validation
	// the way "two males" above is. See rmdb.ErrAmbiguous's own comment.
	// Replaces an earlier version of this test that asserted the
	// opposite: a father with two real families (remarriage) and a
	// bare, single-parent ParentChild request for a new child. That
	// used to be rejected as ambiguous -- now it correctly creates a
	// new, third family instead of guessing which existing one (or
	// rejecting outright), since a bare (parent, child) pair carries no
	// information about which partner the child's other parent actually
	// was. See CreateParentChildRelationship's own comment (and this
	// project's SCOPE.md, "Stage 3" section) for the full account of
	// why this changed, including a real, corrected design mistake this
	// project made and caught before shipping.
	t.Run("parent with two existing families creates a new one for an unlinked child, not a guess", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "A", "Father")
		createPerson(t, testServer, "http://gedcomx.org/Female", "B", "Mother1")
		createPerson(t, testServer, "http://gedcomx.org/Female", "C", "Mother2")
		createPerson(t, testServer, "http://gedcomx.org/Male", "D", "Child")

		status, respBody, _ := post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P2"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		status, respBody, headers := post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P1"},"person2":{"resourceId":"P4"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		require.NotContains(t, headers.Get("Location"), "/relationships/F1", "should not have assumed the child belongs to the father's first existing family")
		require.NotContains(t, headers.Get("Location"), "/relationships/F2", "should not have assumed the child belongs to the father's second existing family either")
	})

	// The case that's still genuinely ambiguous under the corrected
	// design: a child already belonging to more than one family (a
	// real, schema-supported case -- see
	// TestCreateParentChildRelationshipRejectsChildAlreadyInMultipleFamilies
	// in internal/rmdb for the full account of why this can happen).
	t.Run("child already in multiple same-kind families is rejected with 400, not 500", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "A", "Father1")
		createPerson(t, testServer, "http://gedcomx.org/Male", "B", "Father2")
		createPerson(t, testServer, "http://gedcomx.org/Male", "C", "Child")
		createPerson(t, testServer, "http://gedcomx.org/Female", "D", "Mother")

		status, respBody, _ := post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P1"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		status, respBody, _ = post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P2"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)

		status, respBody, _ = post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P4"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", respBody)
	})

	// A child with a known biological parent and, separately, an
	// adoptive parent -- each family still missing its other role.
	// Without the RelType-aware disambiguation (relTypeFromFacts,
	// driven by GEDCOM X's own dedicated Parent-Child Relationship Fact
	// Types -- fact-types-specification.md, Section 2.3, a different
	// document from the person-scoped fact types this project checked
	// first, before finding the correct one), a new mother-role request
	// naming either family's remaining role would be genuinely
	// ambiguous between the two, since both have the target role empty
	// at the same time. Verified end to end through the real HTTP
	// request shape a client actually sends, not just the rmdb layer.
	t.Run("AdoptiveParent/BiologicalParent facts disambiguate a child's biological vs adoptive family", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "Robert", "Bio")
		createPerson(t, testServer, "http://gedcomx.org/Male", "Patrick", "Adoptive")
		createPerson(t, testServer, "http://gedcomx.org/Female", "Mary", "Bio")
		createPerson(t, testServer, "http://gedcomx.org/Female", "Jane", "Adoptive")
		createPerson(t, testServer, "http://gedcomx.org/Male", "Kid", "Smith")

		// familyOf strips a ParentChild Location's own "-FC{child}"/
		// "-MC{child}" suffix (see the dedicated Location-correctness
		// test below) down to just its family id, since this test's own
		// question is "did these two land in the same family," not
		// "are these the literal same relationship" -- a father-child
		// and a mother-child edge on the very same family are two
		// distinct, correctly-different relationships, not a case where
		// this test should expect equal Location values.
		familyOf := func(location string) string {
			t.Helper()
			return location[:strings.LastIndex(location, "-")]
		}

		status, respBody, headers := post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P1"},"person2":{"resourceId":"P5"},"facts":[{"type":"http://gedcomx.org/BiologicalParent"}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		bioFatherChildLoc := headers.Get("Location")
		require.Contains(t, bioFatherChildLoc, "-FC5", "Robert is male, so this must be a father-child ref")
		bioFamily := familyOf(bioFatherChildLoc)

		status, respBody, headers = post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P2"},"person2":{"resourceId":"P5"},"facts":[{"type":"http://gedcomx.org/AdoptiveParent"}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		adoptiveFamily := familyOf(headers.Get("Location"))
		require.NotEqual(t, bioFamily, adoptiveFamily, "biological and adoptive fathers must land in different families")

		status, respBody, headers = post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P3"},"person2":{"resourceId":"P5"},"facts":[{"type":"http://gedcomx.org/BiologicalParent"}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		bioMotherChildLoc := headers.Get("Location")
		require.Contains(t, bioMotherChildLoc, "-MC5", "Mary is female, so this must be a mother-child ref")
		require.Equal(t, bioFamily, familyOf(bioMotherChildLoc), "biological mother should complete the biological family, not be rejected as ambiguous")
		require.NotEqual(t, bioFatherChildLoc, bioMotherChildLoc, "the father-child and mother-child edges on the same family are two distinct relationships, not one")

		status, respBody, headers = post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P4"},"person2":{"resourceId":"P5"},"facts":[{"type":"http://gedcomx.org/AdoptiveParent"}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		require.Equal(t, adoptiveFamily, familyOf(headers.Get("Location")), "adoptive mother should complete the adoptive family, not be rejected as ambiguous")
	})

	// A real, previously-undetected bug: rel.Facts was read for
	// ParentChild (relTypeFromFacts, above) but never even looked at
	// for Couple -- a Marriage fact sent alongside a Couple relationship
	// got 201 Created with nothing actually written to EventTable, no
	// error at all. Existing rmdb-layer tests never caught this because
	// they construct rmdb.NewCoupleRelationship directly, bypassing this
	// exact API-layer gap entirely -- only a real HTTP round trip
	// through handleCreateRelationships itself exercises it.
	t.Run("a Marriage fact on a Couple relationship is actually written, not silently dropped", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "Patrick", "Brontë")
		createPerson(t, testServer, "http://gedcomx.org/Female", "Maria", "Branwell")

		status, respBody, headers := post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P2"},"facts":[{"type":"http://gedcomx.org/Marriage","date":{"original":"29 Dec 1812","formal":"+1812-12-29"},"place":{"original":"Guiseley, Yorks."}}]}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		require.Contains(t, headers.Get("Location"), "/relationships/F1")

		getStatus, getBody := get(t, testServer, "/collections/empty/relationships/F1")
		require.Equal(t, http.StatusOK, getStatus, "body: %s", getBody)
		require.Contains(t, getBody, `"type":"http://gedcomx.org/Marriage"`)
		require.Contains(t, getBody, `"original":"29 Dec 1812"`)
		require.Contains(t, getBody, `"original":"Guiseley, Yorks."`)
	})

	// A real, reported bug: a successful single ParentChild creation
	// always returned Location: /relationships/F{id} -- coupleRef's own
	// shape -- regardless of what was actually created. That's wrong in
	// two distinct ways checked directly against the RS spec before
	// fixing anything: per Section 4.20.2, a single-create response's
	// Location must identify the relationship actually created, and
	// coupleRef and parentChildRef name genuinely different resources
	// (GET on a ParentChild's coupleRef-shaped URL 404s, since that URL
	// denotes a Couple relationship, which a single-parent family isn't
	// and was never claimed to be). Verified as a full round trip for
	// both parent roles, not just that the string looks right: each
	// Location is actually fetched afterward and confirmed to return
	// 200 with the correct relationship, not just pattern-matched.
	t.Run("Location for a single ParentChild creation identifies the actual relationship, not just the family", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "Father", "Test")
		createPerson(t, testServer, "http://gedcomx.org/Female", "Mother", "Test")
		createPerson(t, testServer, "http://gedcomx.org/Male", "Child1", "Test")
		createPerson(t, testServer, "http://gedcomx.org/Male", "Child2", "Test")

		status, respBody, headers := post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P1"},"person2":{"resourceId":"P3"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		fatherChildLoc := headers.Get("Location")
		require.Contains(t, fatherChildLoc, "/relationships/F1-FC3", "father-child ref, not coupleRef's F1")
		getStatus, getBody := get(t, testServer, "/collections/empty/relationships/F1-FC3")
		require.Equal(t, http.StatusOK, getStatus, "the Location returned must itself resolve: %s", getBody)
		require.Contains(t, getBody, `"id":"F1-FC3"`)
		require.Contains(t, getBody, `"type":"http://gedcomx.org/ParentChild"`)

		status, respBody, headers = post(t, testServer, "/collections/empty/relationships",
			`{"relationships":[{"type":"http://gedcomx.org/ParentChild","person1":{"resourceId":"P2"},"person2":{"resourceId":"P4"}}]}`)
		require.Equal(t, http.StatusCreated, status, "body: %s", respBody)
		motherChildLoc := headers.Get("Location")
		require.Contains(t, motherChildLoc, "/relationships/F2-MC4", "mother-child ref, not coupleRef's F2")
		getStatus, getBody = get(t, testServer, "/collections/empty/relationships/F2-MC4")
		require.Equal(t, http.StatusOK, getStatus, "the Location returned must itself resolve: %s", getBody)
		require.Contains(t, getBody, `"id":"F2-MC4"`)

		// coupleRef's own shape (what the bug used unconditionally)
		// genuinely 404s for a ParentChild-only family -- confirming
		// this isn't just "a different but still-working URL."
		getStatus, getBody = get(t, testServer, "/collections/empty/relationships/F1")
		require.Equal(t, http.StatusNotFound, getStatus, "body: %s", getBody)
	})

	t.Run("nonexistent person is rejected with 400, not 500", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "A", "One")

		body := `{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P999"}}]}`
		status, respBody, _ := post(t, testServer, "/collections/empty/relationships", body)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", respBody)
	})

	t.Run("unsupported relationship type is rejected", func(t *testing.T) {
		testServer := newTestServer(t, true)
		createPerson(t, testServer, "http://gedcomx.org/Male", "A", "One")
		createPerson(t, testServer, "http://gedcomx.org/Female", "B", "Two")

		body := `{"relationships":[{"type":"http://gedcomx.org/EnslavedBy","person1":{"resourceId":"P1"},"person2":{"resourceId":"P2"}}]}`
		status, respBody, _ := post(t, testServer, "/collections/empty/relationships", body)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", respBody)
	})

	t.Run("empty relationships array is rejected", func(t *testing.T) {
		testServer := newTestServer(t, true)
		status, body, _ := post(t, testServer, "/collections/empty/relationships", `{"relationships":[]}`)
		require.Equal(t, http.StatusBadRequest, status, "body: %s", body)
	})

	t.Run("read-only mode rejects with 405", func(t *testing.T) {
		testServer := newTestServer(t, false)
		body := `{"relationships":[{"type":"http://gedcomx.org/Couple","person1":{"resourceId":"P1"},"person2":{"resourceId":"P2"}}]}`
		status, respBody, headers := post(t, testServer, "/collections/empty/relationships", body)
		require.Equal(t, http.StatusMethodNotAllowed, status, "body: %s", respBody)
		require.Equal(t, "GET, HEAD", headers.Get("Allow"))
	})
}

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

// TestDebugLoggingCapturesRequestAndResponseBody confirms withLogging's
// own debug-level line (internal/api/server.go) actually captures and
// emits both bodies when -log-level=debug is active -- prompted by a
// real request: seeing the response body alone explains *that*
// something was rejected, but not *what* was actually sent that
// triggered it. Verified by installing a real slog handler writing to a
// buffer (not by reading the middleware's source and reasoning about
// it), making a request guaranteed to fail with a distinctive, easy to
// search for value in both directions, and confirming both actually
// appear in the captured log text.
func TestDebugLoggingCapturesRequestAndResponseBody(t *testing.T) {
	originalLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	var logBuf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	tempDir := t.TempDir()
	tempDBPath := filepath.Join(tempDir, "empty.rmtree")
	copyFile(t, "../../testdata/empty.rmtree", tempDBPath)
	router, cleanup := SetupRouter([]string{tempDBPath}, "http://localhost:8080", "testdata/media", true, 4, 200)
	defer cleanup()
	testServer := httptest.NewServer(router)
	defer testServer.Close()

	// A distinctive, unlikely-to-appear-by-coincidence value in the
	// request, and an unsupported fact type guaranteed to produce a 400
	// with its own distinctive detail in the response.
	const requestMarker = "XYZZY-REQUEST-MARKER"
	body := `{"persons":[{"names":[{"nameForms":[{"parts":[{"type":"http://gedcomx.org/Given","value":"` + requestMarker + `"}]}]}],
		"facts":[{"type":"http://gedcomx.org/NotARealFactType"}]}]}`

	req, err := http.NewRequest("POST", testServer.URL+"/collections/empty/persons", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	logged := logBuf.String()
	require.Contains(t, logged, "level=DEBUG")
	require.Contains(t, logged, requestMarker, "the captured debug log should contain the actual request body sent")
	require.Contains(t, logged, "unrecognized or unsupported fact type", "the captured debug log should also still contain the response's own detail")
}

// TestTraceLoggingCapturesRequestAndResponseBodyForSuccessfulRequest confirms
// that trace extends debug's detail logging to successful requests too.
func TestTraceLoggingCapturesRequestAndResponseBodyForSuccessfulRequest(t *testing.T) {
	originalLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	var logBuf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: loglevel.Trace})))

	tempDir := t.TempDir()
	tempDBPath := filepath.Join(tempDir, "empty.rmtree")
	copyFile(t, "../../testdata/empty.rmtree", tempDBPath)
	router, cleanup := SetupRouter([]string{tempDBPath}, "http://localhost:8080", "testdata/media", false, 4, 200)
	defer cleanup()
	testServer := httptest.NewServer(router)
	defer testServer.Close()

	resp, err := http.Get(testServer.URL + "/collections/empty")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	logged := logBuf.String()
	require.Contains(t, logged, "msg=\"request details\"")
	require.Contains(t, logged, "status=200")
	require.Contains(t, logged, "requestBody=\"\"")
	require.Contains(t, logged, "responseBody=")
	require.Contains(t, logged, "empty", "the trace log should contain the successful response body")
}
