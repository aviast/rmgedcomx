// Package api implements the GEDCOM X RS HTTP handlers backed by a
// RootsMagic database (internal/rmdb), producing GEDCOM X JSON documents
// (internal/gedcomx).
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/rmgedcomx/internal/gedcomx"
	"github.com/example/rmgedcomx/internal/rmdb"
)

// Config holds server-wide settings.
type Config struct {
	BaseURL            string
	DefaultGenerations int
	MaxPageSize        int
}

// Server holds the shared state used by all HTTP handlers.
type Server struct {
	db        *rmdb.DB
	factTypes map[int64]rmdb.FactType
	cfg       Config
}

// NewServer builds a Server, preloading the (small) FactTypeTable.
func NewServer(db *rmdb.DB, cfg Config) (*Server, error) {
	if cfg.DefaultGenerations <= 0 {
		cfg.DefaultGenerations = 4
	}
	if cfg.MaxPageSize <= 0 {
		cfg.MaxPageSize = 200
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	factTypes, err := db.AllFactTypes()
	if err != nil {
		return nil, err
	}
	return &Server{db: db, factTypes: factTypes, cfg: cfg}, nil
}

// Handler builds the HTTP route table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.handleRoot)

	mux.HandleFunc("GET /persons", s.handlePersons)
	mux.HandleFunc("GET /persons/{id}", s.handlePerson)
	mux.HandleFunc("GET /persons/{id}/parents", s.handlePersonParents)
	mux.HandleFunc("GET /persons/{id}/children", s.handlePersonChildren)
	mux.HandleFunc("GET /persons/{id}/spouses", s.handlePersonSpouses)
	mux.HandleFunc("GET /persons/{id}/ancestry", s.handleAncestry)
	mux.HandleFunc("GET /persons/{id}/descendancy", s.handleDescendancy)

	mux.HandleFunc("GET /relationships", s.handleRelationships)
	mux.HandleFunc("GET /relationships/{id}", s.handleRelationship)

	mux.HandleFunc("GET /places", s.handlePlaces)
	mux.HandleFunc("GET /places/{id}", s.handlePlace)

	mux.HandleFunc("GET /source-descriptions", s.handleSourceDescriptions)
	mux.HandleFunc("GET /source-descriptions/{id}", s.handleSourceDescription)

	s.registerNotImplemented(mux)

	return withLogging(withGedcomXContentType(mux))
}

// registerNotImplemented wires up HTTP 501 responses for the parts of the
// GEDCOM X RS specification this server deliberately doesn't implement
// (see SCOPE.md), rather than letting them fall through to a generic 404.
//
// Two categories:
//
//  1. Write transitions (POST/PUT/DELETE/PATCH) on the resources this
//     server does read -- Person, Relationship, Place Description, Source
//     Description all define create/update/delete transitions in the full
//     spec; this server is read-only by design, so any non-GET on those
//     paths is a deliberately-unimplemented spec feature, not a bad
//     request. Each write method is registered explicitly (rather than a
//     bare, any-method pattern) since a bare pattern for an exact path
//     conflicts with the "GET /" catch-all registered for the root: Go's
//     ServeMux can't order "matches more methods" against "matches more
//     paths" and panics rather than guess.
//
//  2. Entire resource families the spec defines that this server never
//     reads or writes at all: Collections, Records, Artifacts, Agents,
//     Events, Person Matches, and OAuth2. These get explicit stub routes
//     at their conventional paths so a client gets a clear "not
//     implemented" rather than an ambiguous 404.
func (s *Server) registerNotImplemented(mux *http.ServeMux) {
	readOnlyResources := []string{
		"/persons",
		"/persons/{id}",
		"/relationships",
		"/relationships/{id}",
		"/places",
		"/places/{id}",
		"/source-descriptions",
		"/source-descriptions/{id}",
	}
	writeMethods := []string{"POST", "PUT", "PATCH", "DELETE"}
	handler := s.notImplemented(
		"this server is read-only; create/update/delete transitions on this resource are not implemented")
	for _, path := range readOnlyResources {
		for _, method := range writeMethods {
			mux.HandleFunc(method+" "+path, handler)
		}
	}

	unimplementedFamilies := map[string]string{
		"/collections":          "the Collections/Collection states are not implemented",
		"/collections/{id}":     "the Collections/Collection states are not implemented",
		"/records":              "the Records/Record states are not implemented",
		"/records/{id}":         "the Records/Record states are not implemented",
		"/artifacts/{id}":       "the Artifacts state is not implemented",
		"/agents":               "the Agents/Agent states are not implemented",
		"/agents/{id}":          "the Agents/Agent states are not implemented",
		"/events":               "the Events/Event states are not implemented",
		"/events/{id}":          "the Events/Event states are not implemented",
		"/persons/{id}/matches": "the Person Matches state is not implemented",
		"/persons/{id}/matches/{matchId}/working": "the Person Matches Query / Match state is not implemented",
		"/oauth2/token": "OAuth2 is not implemented; this server has no authentication",
	}
	allMethods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	for path, reason := range unimplementedFamilies {
		h := s.notImplemented(reason)
		for _, method := range allMethods {
			mux.HandleFunc(method+" "+path, h)
		}
	}
}

// notImplemented returns a handler that always responds 501, with a JSON
// body explaining why and pointing to SCOPE.md for the full list of what's
// implemented.
func (s *Server) notImplemented(reason string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.writeJSON(w, http.StatusNotImplemented, notImplementedBody{
			Error:   "not implemented",
			Detail:  reason,
			SeeAlso: "https://github.com/FamilySearch/gedcomx-rs -- see this server's SCOPE.md for the full list of implemented vs. unimplemented resources",
		})
	}
}

type notImplementedBody struct {
	Error   string `json:"error"`
	Detail  string `json:"detail"`
	SeeAlso string `json:"seeAlso"`
}

func withGedcomXContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-gedcomx-v1+json")
		next.ServeHTTP(w, r)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.RequestURI(), rec.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// --- shared response helpers ---

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("error encoding response: %v", err)
	}
}

type errorBody struct {
	Error string `json:"error"`
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, errorBody{Error: msg})
}

func (s *Server) notFound(w http.ResponseWriter, kind, id string) {
	s.writeError(w, http.StatusNotFound, kind+" "+id+" not found")
}

// pagingParams reads and clamps ?limit=&offset= query parameters.
func (s *Server) pagingParams(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > s.cfg.MaxPageSize {
		limit = s.cfg.MaxPageSize
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

func (s *Server) url(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return s.cfg.BaseURL + path
}

func pagingLinks(s *Server, base string, limit, offset, total int) gedcomx.Links {
	links := gedcomx.Links{}
	if offset > 0 {
		prevOffset := offset - limit
		if prevOffset < 0 {
			prevOffset = 0
		}
		links["prev"] = gedcomx.Link{Href: s.url(base + "?limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(prevOffset))}
		links["first"] = gedcomx.Link{Href: s.url(base + "?limit=" + strconv.Itoa(limit) + "&offset=0")}
	}
	if offset+limit < total {
		links["next"] = gedcomx.Link{Href: s.url(base + "?limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(offset+limit))}
	}
	return links
}
