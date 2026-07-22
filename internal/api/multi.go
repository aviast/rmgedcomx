package api

import (
	"net/http"

	"github.com/aviast/rmgedcomx/internal/gedcomx"
)

// CollectionEntry pairs a fully-configured, single-collection Server with
// the id it should be mounted at (Server.cfg.ID, kept here too so the
// multi-collection assembly doesn't need to reach into Server's internals).
type CollectionEntry struct {
	ID     string
	Server *Server
}

// NewMultiCollectionHandler assembles the complete HTTP route table
// spanning every collection this server has open (one per -db flag). It's
// the only thing that needs to know more than one collection exists --
// every *Server, and everything in convert.go/handlers.go, stays scoped to
// exactly one collection, unaware there might be others. See SCOPE.md's
// "Multiple databases / Collections" section for the design this
// implements.
//
// It builds three things:
//
//  1. The Collections/Collection discovery states (RS spec Sections 4.4,
//     4.5), which necessarily span every collection -- these can't live on
//     a single collection's Server, so they're assembled here from all of
//     them. GET / serves the same content as GET /collections (the
//     Collections list) rather than a single Collection: with potentially
//     more than one collection open, there's no longer a single one for
//     the root to unambiguously be.
//  2. A single global 501 stub for OAuth2 (/oauth2/token) -- authentication
//     isn't a per-collection concept.
//  3. Each collection's own resource routes (persons, relationships, ...),
//     mounted under /collections/{id}/ by stripping that prefix and
//     delegating to that collection's own Server.resourceHandler().
func NewMultiCollectionHandler(entries []CollectionEntry) http.Handler {
	mux := http.NewServeMux()

	byID := make(map[string]*Server, len(entries))
	for _, e := range entries {
		byID[e.ID] = e.Server
	}
	// Stable order for the Collections list: the order collections were
	// given on the command line, not map iteration order.
	orderedIDs := make([]string, len(entries))
	for i, e := range entries {
		orderedIDs[i] = e.ID
	}

	listCollections := func(w http.ResponseWriter, r *http.Request) {
		collections := make([]gedcomx.Collection, 0, len(orderedIDs))
		for _, id := range orderedIDs {
			c, err := byID[id].buildCollection()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			collections = append(collections, c)
		}
		writeJSON(w, http.StatusOK, gedcomx.CollectionsDocument{
			Results:     len(collections),
			Collections: collections,
		})
	}
	mux.HandleFunc("GET /{$}", listCollections)
	mux.HandleFunc("GET /collections", listCollections)

	mux.HandleFunc("GET /collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		srv, ok := byID[id]
		if !ok {
			notFound(w, "collection", id)
			return
		}
		c, err := srv.buildCollection()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, gedcomx.CollectionDocument{Collections: []gedcomx.Collection{c}, Links: c.Links})
	})

	collectionsNotImplemented := notImplemented(
		"this server is read-only; create/update/delete transitions on this resource are not implemented")
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		mux.HandleFunc(method+" /collections", collectionsNotImplemented)
		mux.HandleFunc(method+" /collections/{id}", collectionsNotImplemented)
	}

	oauthNotImplemented := notImplemented("OAuth2 is not implemented; this server has no authentication")
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		mux.HandleFunc(method+" /oauth2/token", oauthNotImplemented)
	}

	for _, e := range entries {
		prefix := "/collections/" + e.ID
		mux.Handle(prefix+"/", http.StripPrefix(prefix, e.Server.resourceHandler()))
	}

	return withLogging(withGedcomXContentType(mux))
}
