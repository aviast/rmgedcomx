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
	roles     map[int64]rmdb.Role
	cfg       Config
	// collectionBaseURL is cfg.BaseURL + "/collections/" + cfg.ID,
	// precomputed once. Used by url() for every resource link this
	// collection's handlers build (persons, relationships, ...); see
	// globalURL() for the few links that intentionally point outside this
	// collection's own scope.
	collectionBaseURL string
}

// NewServer builds a Server, preloading the (small) FactTypeTable and
// RoleTable.
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
	roles, err := db.AllRoles()
	if err != nil {
		return nil, err
	}
	return &Server{
		db:                db,
		factTypes:         factTypes,
		roles:             roles,
		cfg:               cfg,
		collectionBaseURL: cfg.BaseURL + "/collections/" + cfg.ID,
	}, nil
}

// resourceHandler builds the route table for everything that belongs to
// this one collection: persons, relationships, places, source
// descriptions, artifacts, and events. It does NOT include the Collections/
// Collection discovery states (GET /, /collections, /collections/{id}) --
// those necessarily span every collection this server has open, not just
// this one, so they're assembled once, at the top level, by
// NewMultiCollectionHandler, which mounts this handler under
// /collections/{id}/ for each collection (via http.StripPrefix). Unwrapped
// by any middleware for the same reason: logging, content negotiation, and
// the default Content-Type are applied once, at the top level, not per
// collection.
//
// Only GET is ever registered, deliberately: this server is read-only, and
// resource families the spec defines but this server doesn't implement
// (Records, Agents, Person Matches, and this collection's own write
// transitions) are simply never registered at all, rather than wired up
// to custom "not implemented" handlers. That's not a gap --
// Go's net/http ServeMux (1.22+) already does exactly the right, standard
// thing on its own once you don't fight it: a request for a registered
// path with an unregistered method gets a 405 Method Not Allowed with a
// correct Allow header populated automatically, HEAD requests against a
// GET-only registration are answered automatically (body discarded, no
// separate handler needed), and a path that was never registered at all
// gets a plain 404. All three were verified empirically against Go 1.22's
// actual behavior during an audit of this server rather than assumed; see
// SCOPE.md's "HTTP semantics" section for the detail and for why an
// earlier version of this server did the opposite (custom 501 responses
// for all of the above) and was wrong to.
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

	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /events/{id}", s.handleEvent)

	return mux
}

// gedcomXMediaType is the one representation this server produces for
// every GEDCOM X RS state. There's no XML representation (see SCOPE.md) --
// full JSON+XML dual support is a large undertaking this personal-use
// server doesn't need, and no client of it has ever asked for XML.
const gedcomXMediaType = "application/x-gedcomx-v1+json"

// withContentNegotiation sets Vary: Accept (this response's Content-Type
// genuinely does depend on the Accept header, via the check below, so
// advertising that is accurate -- unlike Accept-Encoding, which this
// server doesn't negotiate on at all and so doesn't claim to) and rejects
// with 406 Not Acceptable any request whose Accept header excludes the one
// representation this server can produce, rather than silently sending
// GEDCOM X JSON regardless of what was asked for.
//
// GET .../artifacts/{id}/content is exempt from both checks: it isn't a
// GEDCOM X RS state at all (see SCOPE.md's "Multimedia" section) -- it's
// this server's own extension for serving whatever a SourceDescription's
// `about` points at, and its whole purpose is to return the artifact's own
// real Content-Type (image/jpeg, application/pdf, ...), not GEDCOM X JSON,
// so neither "must accept our JSON profile" nor "force our JSON
// Content-Type" applies to it.
func withContentNegotiation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Accept")
		if strings.HasSuffix(r.URL.Path, "/content") {
			next.ServeHTTP(w, r)
			return
		}
		if !acceptsGedcomXJSON(r.Header.Get("Accept")) {
			writeError(w, http.StatusNotAcceptable,
				"this server only produces "+gedcomXMediaType+"; none of the media types in your Accept header can be satisfied")
			return
		}
		w.Header().Set("Content-Type", gedcomXMediaType)
		next.ServeHTTP(w, r)
	})
}

// acceptsGedcomXJSON reports whether an Accept header value permits this
// server's one supported representation. A missing/empty header, or any
// entry that's "*/*", "application/*", "application/json", or the exact
// GEDCOM X JSON media type (parameters and case ignored), is accepted.
// This deliberately isn't full RFC 7231 content negotiation (no q-value
// ranking) -- with exactly one representation to offer, there's nothing to
// rank, only a yes/no of whether that one representation is acceptable.
func acceptsGedcomXJSON(accept string) bool {
	accept = strings.TrimSpace(accept)
	if accept == "" {
		return true
	}
	for _, part := range strings.Split(accept, ",") {
		mediaType := part
		if i := strings.IndexByte(mediaType, ';'); i >= 0 {
			mediaType = mediaType[:i]
		}
		switch strings.ToLower(strings.TrimSpace(mediaType)) {
		case "*/*", "application/*", "application/json", gedcomXMediaType:
			return true
		}
	}
	return false
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
	if err := enc.Encode(v); err != nil {
		log.Printf("error encoding response: %v", err)
	}
}

// problemDetails is the RFC 7807 "Problem Details for HTTP APIs" JSON
// representation (media type application/problem+json). GEDCOM X RS
// doesn't define its own error body schema -- error responses are outside
// the spec's own scope, left to general HTTP/REST convention -- so this
// server uses the standard one rather than a bespoke shape. `type` is
// deliberately omitted: RFC 7807 says a missing `type` means "about:blank"
// (no further semantics beyond the HTTP status code itself), which is an
// honest fit here -- this server doesn't have a taxonomy of distinct
// problem-type URIs to document and maintain, just a status code and a
// human-readable detail message for each occurrence.
type problemDetails struct {
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func writeError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	writeJSON(w, status, problemDetails{
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	})
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

// pagingLinks builds the first/prev/next/last links defined by RS spec
// Section 7 ("Paged Application States"). first/last are included
// whenever there's more than one page, regardless of which page you're
// currently on (they mark the ends of the whole list, not relative to the
// current position, unlike prev/next).
func pagingLinks(s *Server, base string, limit, offset, total int) gedcomx.Links {
	links := gedcomx.Links{}
	if offset > 0 {
		prevOffset := offset - limit
		if prevOffset < 0 {
			prevOffset = 0
		}
		links["prev"] = gedcomx.Link{Href: s.url(base + "?limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(prevOffset))}
	}
	if offset+limit < total {
		links["next"] = gedcomx.Link{Href: s.url(base + "?limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(offset+limit))}
	}
	if total > limit {
		links["first"] = gedcomx.Link{Href: s.url(base + "?limit=" + strconv.Itoa(limit) + "&offset=0")}
		lastOffset := ((total - 1) / limit) * limit
		links["last"] = gedcomx.Link{Href: s.url(base + "?limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(lastOffset))}
	}
	return links
}
