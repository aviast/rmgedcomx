// Command rmgedcomx serves a read-only GEDCOM X RS API backed by one or
// more RootsMagic SQLite databases, each exposed as its own Collection.
// See README.md and SCOPE.md.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/aviast/rmgedcomx/internal/api"
	"github.com/aviast/rmgedcomx/internal/collectionid"
	"github.com/aviast/rmgedcomx/internal/rmdb"
)

// dbFlag collects repeated -db flags into a slice, since flag.String only
// keeps the last occurrence (see SCOPE.md's "Multiple databases /
// Collections" section for why that matters here).
type dbFlag []string

func (d *dbFlag) String() string { return strings.Join(*d, ",") }
func (d *dbFlag) Set(v string) error {
	*d = append(*d, v)
	return nil
}

// openedDB is one -db argument's worth of state, from opening the file
// through deriving its (pre-dedupe) Collection id/title.
type openedDB struct {
	path  string
	db    *rmdb.DB
	dir   string
	id    string
	title string
}

func main() {
	var dbPaths dbFlag
	flag.Var(&dbPaths, "db", "path to a RootsMagic .rmtree/.rmgc SQLite file; repeat -db to serve multiple databases, each as its own Collection (required, at least one)")
	var (
		addr               = flag.String("addr", ":8080", "address to listen on")
		baseURL            = flag.String("base-url", "http://localhost:8080", "base URL used to build absolute links in responses")
		mediaFolder        = flag.String("media-folder", "", "RootsMagic's configured Media Folder, for resolving multimedia paths that use the '?' symbol (see SCOPE.md's \"Multimedia\" section); shared by all databases, since it's a RootsMagic-installation-wide setting, not a per-database one")
		defaultGenerations = flag.Int("default-generations", 4, "default number of generations for ancestry/descendancy queries")
		maxPageSize        = flag.Int("max-page-size", 200, "maximum number of entries returned by a single paged request")
	)
	flag.Parse()

	if len(dbPaths) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one -db is required")
		flag.Usage()
		os.Exit(2)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("warning: couldn't determine home directory (%v); multimedia paths using '~' won't resolve", err)
	}

	entries := make([]openedDB, 0, len(dbPaths))
	for _, path := range dbPaths {
		db, err := rmdb.Open(path)
		if err != nil {
			log.Fatalf("opening RootsMagic database %q: %v", path, err)
		}
		log.Printf("opened %s (%s)", path, db.SchemaHint())

		dir, err := filepath.Abs(filepath.Dir(path))
		if err != nil {
			log.Fatalf("resolving directory of %q: %v", path, err)
		}

		rootName, err := db.RootPersonDisplayName()
		if err != nil {
			log.Printf("warning: couldn't determine the Home Person for %s (%v); its Collection id/title will fall back to the filename", path, err)
			rootName = ""
		}
		id, title := collectionid.Derive(rootName, path)

		entries = append(entries, openedDB{path: path, db: db, dir: dir, id: id, title: title})
	}
	defer func() {
		for _, e := range entries {
			e.db.Close()
		}
	}()

	// Dedupe ids across the whole batch -- see collectionid.Dedupe: this
	// is a last-resort safety net, engaged only if two databases actually
	// produced the same id (e.g. the same file passed twice).
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.id
	}
	ids = collectionid.Dedupe(ids)
	for i := range entries {
		entries[i].id = ids[i]
	}

	collectionEntries := make([]api.CollectionEntry, 0, len(entries))
	for _, e := range entries {
		srv, err := api.NewServer(e.db, api.Config{
			ID:                 e.id,
			BaseURL:            *baseURL,
			Title:              e.title,
			DefaultGenerations: *defaultGenerations,
			MaxPageSize:        *maxPageSize,
			Media: rmdb.MediaFolderConfig{
				DatabaseDir: e.dir,
				HomeDir:     homeDir,
				MediaFolder: *mediaFolder,
			},
		})
		if err != nil {
			log.Fatalf("initializing server for %q: %v", e.path, err)
		}
		collectionEntries = append(collectionEntries, api.CollectionEntry{ID: e.id, Server: srv})
	}

	printCollectionTable(entries)

	log.Printf("listening on %s", *addr)
	if err := http.ListenAndServe(*addr, api.NewMultiCollectionHandler(collectionEntries)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// printCollectionTable prints the collection id -> title -> database file
// mapping the person running this server needs to connect a client to the
// right Collection -- this server makes no promise that a given database
// gets the same Collection id across restarts (see SCOPE.md), so this
// table is the intended way a human confirms which is which for the
// session that's about to start.
func printCollectionTable(entries []openedDB) {
	fmt.Fprintln(os.Stdout, "\nCollections available this session:")
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "COLLECTION ID\tTITLE\tDATABASE FILE")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", e.id, e.title, e.path)
	}
	tw.Flush()
	fmt.Fprintln(os.Stdout)
}
