// Command rmgedcomx serves a read-only GEDCOM X RS API backed by a
// RootsMagic SQLite database. See README.md and SCOPE.md.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/aviast/rmgedcomx/internal/api"
	"github.com/aviast/rmgedcomx/internal/rmdb"
)

func main() {
	var (
		dbPath             = flag.String("db", "", "path to the RootsMagic .rmtree/.rmgc SQLite file (required)")
		addr               = flag.String("addr", ":8080", "address to listen on")
		baseURL            = flag.String("base-url", "http://localhost:8080", "base URL used to build absolute links in responses")
		title              = flag.String("title", "", "title for the Collection resource (default: the -db file's name, without extension)")
		defaultGenerations = flag.Int("default-generations", 4, "default number of generations for ancestry/descendancy queries")
		maxPageSize        = flag.Int("max-page-size", 200, "maximum number of entries returned by a single paged request")
	)
	flag.Parse()

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "error: -db is required")
		flag.Usage()
		os.Exit(2)
	}

	collectionTitle := *title
	if collectionTitle == "" {
		base := filepath.Base(*dbPath)
		collectionTitle = strings.TrimSuffix(base, filepath.Ext(base))
	}

	db, err := rmdb.Open(*dbPath)
	if err != nil {
		log.Fatalf("opening RootsMagic database: %v", err)
	}
	defer db.Close()
	log.Printf("opened %s (%s)", *dbPath, db.SchemaHint())

	srv, err := api.NewServer(db, api.Config{
		BaseURL:            *baseURL,
		Title:              collectionTitle,
		DefaultGenerations: *defaultGenerations,
		MaxPageSize:        *maxPageSize,
	})
	if err != nil {
		log.Fatalf("initializing server: %v", err)
	}

	log.Printf("listening on %s", *addr)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
