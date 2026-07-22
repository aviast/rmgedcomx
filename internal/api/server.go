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

	"github.com/aviast/rmgedcomx/internal/gedcomx"
	"github.com/aviast/rmgedcomx/internal/rmdb"
)

// Config holds server-wide settings.
type Config struct {
	// ID is this collection's id, used both as its URL path segment
	// (/collections/{ID}/...) and its Collection.id in responses. See
	// internal/collectionid and SCOPE.md's "Multiple databases /
	// Collections" section for how it's derived and why it isn't (and
	// can't be) guaranteed stable across server restarts.
	ID string
	// BaseURL is the GLOBAL server root (e.g. "http://localhost:8080"),
	// shared by every collection -- not this collection's own URL prefix.
	// Server derives and stores its own collection-scoped base
	// (BaseURL + "/collections/" + ID) for building resource links; see
	// url() and globalURL().
	BaseURL            string
	Title              string
	DefaultGenerations int
	MaxPageSize        int
	Media              rmdb.MediaFolderConfig
}

// Server holds the shared state used by all HTTP handlers for one
// collection.
type Server struct {
	db        *rmdb.DB
	factTypes map[int64]rmdb.FactType
	cfg       Config
	// collectionBaseURL is cfg.BaseURL + "/collections/" + cfg.ID,
	// precomputed once. Used by url() for every resource link this
	// collection's handlers build (persons, relationships, ...); see
	// globalURL() for the few links that intentionally point outside this
	// collection's own scope.
	collectionBaseURL string
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
	return &Server{
		db:                db,
		factTypes:         factTypes,
		cfg:               cfg,
		collectionBaseURL: cfg.BaseURL + "/collections/" + cfg.ID,
	}, nil
}

// resourceHandler builds the route table for everything that belongs to
// this one collection: persons, relationships, places, source
// descriptions, artifacts, and the 501 stubs for what's deliberately
// unimplemented within a collection's scope. It does NOT include the
// Collections/Collection discovery states (GET /, /collections,
// /collections/{id}) or OAuth2 -- those necessarily span every collection
// this server has open, not just this one, so they're assembled once, at
// the top level, by NewMultiCollectionHandler, which mounts this handler
// under /collections/{id}/ for each collection (via http.StripPrefix).
// Unwrapped by any middleware for the same reason: logging and the
// default Content-Type are applied once, at the top level, not per
// collection.
func (s *Server) resourceHandler() http.Handler {
	mux := http.NewServeMux()

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

	mux.HandleFunc("GET /artifacts", s.handleArtifacts)
	mux.HandleFunc("GET /artifacts/{id}", s.handleArtifact)
	mux.HandleFunc("GET /artifacts/{id}/content", s.handleArtifactContent)

	registerNotImplemented(mux)

	return mux
}

// registerNotImplemented wires up HTTP 501 responses for the parts of the
// GEDCOM X RS specification this server deliberately doesn't implement
// within a collection's scope (see SCOPE.md), rather than letting them
// fall through to a generic 404. (Collections/Collection's own write
// methods, and OAuth2, are handled separately at the top level -- see
// NewMultiCollectionHandler -- since they aren't collection-scoped.)
//
// Two categories:
//
//  1. Write transitions (POST/PUT/DELETE/PATCH) on the resources this
//     server does read -- Person, Relationship, Place Description, Source
//     Description, and Artifacts all define create/update/delete
//     transitions in the full spec; this server is read-only by design,
//     so any non-GET on those paths is a deliberately-unimplemented spec
//     feature, not a bad request.
//
//  2. Entire resource families the spec defines that this server never
//     reads or writes at all: Records, Agents, Events, Person Matches.
//     These get explicit stub routes at their conventional paths so a
//     client gets a clear "not implemented" rather than an ambiguous 404.
func registerNotImplemented(mux *http.ServeMux) {
	readOnlyResources := []string{
		"/persons",
		"/persons/{id}",
		"/relationships",
		"/relationships/{id}",
		"/places",
		"/places/{id}",
		"/source-descriptions",
		"/source-descriptions/{id}",
		"/artifacts",
		"/artifacts/{id}",
		"/artifacts/{id}/content",
	}
	writeMethods := []string{"POST", "PUT", "PATCH", "DELETE"}
	handler := notImplemented(
		"this server is read-only; create/update/delete transitions on this resource are not implemented")
	for _, path := range readOnlyResources {
		for _, method := range writeMethods {
			mux.HandleFunc(method+" "+path, handler)
		}
	}

	unimplementedFamilies := map[string]string{
		"/records":              "the Records/Record states are not implemented",
		"/records/{id}":         "the Records/Record states are not implemented",
		"/agents":               "the Agents/Agent states are not implemented",
		"/agents/{id}":          "the Agents/Agent states are not implemented",
		"/events":               "the Events/Event states are not implemented",
		"/events/{id}":          "the Events/Event states are not implemented",
		"/persons/{id}/matches": "the Person Matches state is not implemented",
		"/persons/{id}/matches/{matchId}/working": "the Person Matches Query / Match state is not implemented",
	}
	allMethods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	for path, reason := range unimplementedFamilies {
		h := notImplemented(reason)
		for _, method := range allMethods {
			mux.HandleFunc(method+" "+path, h)
		}
	}
}

// notImplemented returns a handler that always responds 501, with a JSON
// body explaining why and pointing to SCOPE.md for the full list of what's
// implemented.
func notImplemented(reason string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotImplemented, notImplementedBody{
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	// HTTP forbids a body on 204 No Content (and a few other statuses) --
	// net/http enforces this and logs "request method or response status
	// code does not allow body" if you try anyway. All of this server's
	// call sites that pass StatusNoContent do so specifically because the
	// payload is an empty list, so there's nothing meaningful to encode
	// regardless.
	if status == http.StatusNoContent {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("error encoding response: %v", err)
	}
}

type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func notFound(w http.ResponseWriter, kind, id string) {
	writeError(w, http.StatusNotFound, kind+" "+id+" not found")
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

// url builds an absolute URL for a resource within THIS collection (e.g.
// "/persons/P1" -> "http://host/collections/{id}/persons/P1"). This is
// what every resource-building function in convert.go uses.
func (s *Server) url(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return s.collectionBaseURL + path
}

// globalURL builds an absolute URL relative to the server's global root,
// NOT this collection's own prefix -- for the handful of links that
// intentionally point outside this collection's scope (currently just
// "subcollections", which points at the top-level /collections list
// spanning every collection this server has open).
func (s *Server) globalURL(path string) string {
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
