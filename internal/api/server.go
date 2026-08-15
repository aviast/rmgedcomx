// Package api implements the GEDCOM X RS HTTP handlers backed by a
// RootsMagic database (internal/rmdb), producing GEDCOM X JSON documents
// (internal/gedcomx).
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aviast/rmgedcomx/internal/gedcomx"
	"github.com/aviast/rmgedcomx/internal/loglevel"
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
	// WriteGuard is consulted before every write, in addition to (not
	// instead of) db.ReadOnly() -- see WriteGuard's own doc comment for
	// why a single startup-time check isn't enough for a long-running
	// server. nil means "no additional gating," which every collection
	// sharing one server process gets by construction, since exactly one
	// guard instance is built in cmd/server/main.go and shared across
	// every collection's Config -- RootsMagic.exe running is a
	// whole-machine condition, not a per-database one, and a shared
	// guard means one collection tripping it is immediately reflected
	// for every other collection this server is also serving, not just
	// the one collection whose own periodic check happened to run first.
	WriteGuard WriteGuard
}

// WriteGuard is consulted by every write handler before it does
// anything else, on top of (not instead of) the existing db.ReadOnly()
// gate that decides whether write routes are registered at all. Defined
// as an interface here, in the package that consumes it, rather than
// this package depending on cmd/server's concrete implementation (which
// shells out to Windows' tasklist -- OS-specific, and not something this
// package needs to know about to do its job). See
// cmd/server/writeguard.go for the concrete implementation and the full
// reasoning behind rate-limiting and latching, both of which are that
// implementation's concern, not this interface's.
type WriteGuard interface {
	// Allow reports whether a write should be permitted to proceed right
	// now. When ok is false, reason is a human-readable explanation
	// suitable for returning directly in an error response.
	Allow() (ok bool, reason string)
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
	mux.HandleFunc("GET /artifacts/{id}/subjects", s.handleArtifactSubjects)
	mux.HandleFunc("GET /artifacts/{id}/persons", s.handleArtifactPersons)
	mux.HandleFunc("GET /artifacts/{id}/events", s.handleArtifactEvents)
	mux.HandleFunc("GET /artifacts/{id}/relationships", s.handleArtifactRelationships)

	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /events/{id}", s.handleEvent)

	// Write routes are only registered at all when this collection's own
	// database connection is actually writable -- the single source of
	// truth for that is db.ReadOnly() (set by the -write flag, all the
	// way down in rmdb.Open), not a separately-tracked setting here, so
	// there's exactly one place this can ever be decided inconsistently
	// with the underlying connection. When not registered, a POST to
	// these paths gets the same automatic 405 as any other unimplemented
	// write (see the doc comment above) -- there's no behavioral
	// difference between "-write not passed" and "this particular write
	// isn't implemented yet." See SCOPE.md's "Write support" section for
	// the staged plan this is part of.
	if !s.db.ReadOnly() {
		mux.HandleFunc("POST /places/{id}", s.requireWriteAllowed(s.handleUpdatePlace))
		mux.HandleFunc("POST /source-descriptions/{id}", s.requireWriteAllowed(s.handleUpdateSourceDescription))
		mux.HandleFunc("POST /artifacts/{id}", s.requireWriteAllowed(s.handleUpdateArtifact))
		mux.HandleFunc("POST /persons/{id}", s.requireWriteAllowed(s.handleUpdatePerson))
		mux.HandleFunc("POST /persons", s.requireWriteAllowed(s.handleCreatePersons))
		mux.HandleFunc("POST /events/{id}", s.requireWriteAllowed(s.handleUpdateEvent))
		mux.HandleFunc("POST /relationships/{id}", s.requireWriteAllowed(s.handleUpdateRelationship))
		mux.HandleFunc("POST /relationships", s.requireWriteAllowed(s.handleCreateRelationships))
	}

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

// withLogging wraps every request with one slog.Info line (method, path,
// status, duration) -- the direct replacement for what used to be a bare
// log.Printf("%s %s -> %d (%s)", ...) line. When the response status
// indicates an error (>= 400), a second, separate slog.Debug line
// follows, carrying both the request body that produced this response
// and the response body itself. At trace level this second line is emitted
// for every request. For this API, the response body is
// always the RFC 7807 problem-details JSON naming exactly why the
// request failed (or, for a request that never reached this server's own
// handler code at all -- e.g. a write route that doesn't exist because
// -write wasn't passed -- Go's own plain-text 404/405 body, which is
// itself the diagnostic: seeing that instead of this server's usual JSON
// error shape is a direct sign the request never reached application
// code in the first place). Seeing the request alongside it answers the
// natural next question once the response explains *that* something was
// rejected: *what* did the client actually send that triggered it.
//
// Deliberately two separate log lines at two separate levels, not one
// line with the bodies as always-present attributes: slog's level
// filtering is per-call, not per-attribute, so a body attribute on the
// Info line would always render regardless of the configured level. A
// separate Debug call is the only way to make the extra detail actually
// optional.
//
// Reading and re-wrapping r.Body to capture it has a real, if small,
// cost -- not worth paying on every single request when nothing will
// ever look at the result. Gated on whether Debug is actually enabled,
// checked once up front, rather than unconditionally: this way a server
// run at the default -log-level=info pays nothing extra for a capability
// nobody asked it to use this session.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		debugEnabled := slog.Default().Enabled(r.Context(), slog.LevelDebug)
		traceEnabled := slog.Default().Enabled(r.Context(), loglevel.Trace)
		var reqBody []byte
		if debugEnabled && r.Body != nil {
			reqBody, _ = io.ReadAll(r.Body)
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(reqBody))
		}

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK, captureBody: debugEnabled}
		next.ServeHTTP(rec, r)
		duration := time.Since(start)
		slog.Info("request", "method", r.Method, "path", r.URL.RequestURI(), "status", rec.status, "duration", duration)
		if debugEnabled && (traceEnabled || rec.status >= 400) {
			slog.Debug("request details", "method", r.Method, "path", r.URL.RequestURI(), "status", rec.status,
				"requestBody", string(reqBody), "responseBody", rec.body.String())
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	captureBody bool
	body        bytes.Buffer
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Write captures a copy of the response body when debug logging is enabled,
// for withLogging's own debug-level line above. It is rendered for failed
// requests at debug level and every request at trace level.
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.captureBody {
		r.body.Write(b)
	}
	return r.ResponseWriter.Write(b)
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
		slog.Error("error encoding response", "error", err)
	}
}

// decodeStrictJSON decodes a write request's body, rejecting any field
// the target type doesn't recognize rather than silently ignoring it.
// This matters more than it might look: a client that mistypes a field
// name (e.g. "value" instead of "text" on a Note) produces a request
// that's still valid JSON and still decodes without error by default --
// the mistyped field is just dropped, and whatever it would have set
// stays at its zero value. If that zero value happens to make every
// field on the update empty, the request looks like a legitimate no-op
// and returns a misleading success, when what actually happened is the
// client's intended write never took effect. Rejecting unknown fields
// turns that into an immediate, clear 400 instead. See SCOPE.md's "Write
// support" section for the real case this was found from.
func decodeStrictJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
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

// ensureBackupForWrite calls DB.EnsureBackup and, if it fails, writes a
// 500 response and returns a non-nil error so the caller can bail out
// without attempting the write. Every write handler calls this first,
// unconditionally, before doing anything else -- see SCOPE.md's "Write
// support" section for why a backup safety net exists at all. If we can't
// guarantee a backup exists, the write should not be attempted.
func (s *Server) ensureBackupForWrite(w http.ResponseWriter) error {
	if _, err := s.db.EnsureBackup(); err != nil {
		writeError(w, http.StatusInternalServerError,
			"couldn't create a safety backup before writing, so the write was not attempted: "+err.Error())
		return err
	}
	return nil
}

// requireWriteAllowed wraps a write handler with cfg.WriteGuard, checked
// in addition to (not instead of) db.ReadOnly() -- see WriteGuard's own
// doc comment for why db.ReadOnly() alone, decided once at server
// startup, isn't enough for a server meant to run for a long time: it
// can't notice RootsMagic being opened after this server already started
// in write mode. A tripped guard returns 405 with the same Allow header a
// genuinely read-only server would send for the identical request --
// deliberate, not incidental: from a client's point of view, "this server
// started read-only" and "this server was writable but RootsMagic showed
// up" should look the same and need the same handling, not a second error
// shape to build separate logic for. cfg.WriteGuard being nil (every
// caller except cmd/server/main.go, including every test in this
// project) means no additional gating -- only main.go's real,
// process-checking guard can ever say no.
func (s *Server) requireWriteAllowed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.WriteGuard != nil {
			if ok, reason := s.cfg.WriteGuard.Allow(); !ok {
				w.Header().Set("Allow", "GET, HEAD")
				writeError(w, http.StatusMethodNotAllowed, reason)
				return
			}
		}
		next(w, r)
	}
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
