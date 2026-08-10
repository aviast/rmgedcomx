// Command rmgedcomx serves a GEDCOM X RS API backed by one or more
// RootsMagic SQLite databases, each exposed as its own Collection.
// Read-only by default; see the -write flag and SCOPE.md's "Write
// support" section. See README.md and SCOPE.md.
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
	path     string
	db       *rmdb.DB
	dir      string
	id       string
	title    string
	uniqueID string
}

// extractBypassOSCheckFlag looks for -bypass-os-check in os.Args and
// removes it before flag.Parse() ever sees it -- deliberately not
// registered via the flag package, so it never appears in -h/--help
// output or flag.PrintDefaults(). This is a development/testing aid, not
// a supported way to run write mode in production: it forces
// discoverMediaFolder's macOS-style discovery path regardless of the
// actual platform, which is only meaningful because os.UserHomeDir()
// returns a real, usable directory everywhere -- see
// mediafolder_discovery.go's own doc comment for the full reasoning.
// Nothing else about write mode is affected by it (the RootsMagic.exe
// check and the backup mechanism are both untouched).
func extractBypassOSCheckFlag() bool {
	const flagName = "-bypass-os-check"
	found := false
	filtered := os.Args[:1]
	for _, arg := range os.Args[1:] {
		if arg == flagName {
			found = true
			continue
		}
		filtered = append(filtered, arg)
	}
	os.Args = filtered
	return found
}

func main() {
	bypassOSCheck := extractBypassOSCheckFlag()

	var dbPaths dbFlag
	flag.Var(&dbPaths, "db", "path to a RootsMagic .rmtree/.rmgc SQLite file; repeat -db to serve multiple databases, each as its own Collection (required, at least one)")
	var (
		addr               = flag.String("addr", ":8080", "address to listen on")
		baseURL            = flag.String("base-url", "http://localhost:8080", "base URL used to build absolute links in responses")
		mediaFolder        = flag.String("media-folder", "", "RootsMagic's configured Media Folder, for resolving multimedia paths that use the '?' symbol (see SCOPE.md's \"Multimedia\" section); shared by all databases, since it's a RootsMagic-installation-wide setting, not a per-database one")
		write              = flag.Bool("write", false, "enable write support (POST/PUT/PATCH/DELETE); default is read-only. See SCOPE.md's \"Write support\" section for what's actually implemented at any given stage")
		defaultGenerations = flag.Int("default-generations", 4, "default number of generations for ancestry/descendancy queries")
		maxPageSize        = flag.Int("max-page-size", 200, "maximum number of entries returned by a single paged request")
	)
	flag.Parse()

	if len(dbPaths) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one -db is required")
		flag.Usage()
		os.Exit(2)
	}

	if *write {
		if err := checkRootsMagicNotRunning(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
	}

	// -write and -media-folder are mutually exclusive: write mode reads
	// the Media Folder itself, straight from RootsMagic's own
	// configuration (RootsMagicUser.xml) -- the one source of truth that
	// guarantees a path this server writes will still resolve correctly
	// when RootsMagic itself later reads the file. A manually supplied
	// -media-folder can't offer that guarantee, and silently trusting one
	// that happens to disagree with RootsMagic's own configuration would
	// mean writing something that looks correct here but is actually
	// broken from RootsMagic's point of view. Someone passing both is
	// almost certainly confused about which one is in effect, not
	// deliberately overriding anything -- so this refuses to start rather
	// than silently pick one. See SCOPE.md's "Write support" section.
	effectiveMediaFolder := *mediaFolder
	if *write {
		if *mediaFolder != "" {
			fmt.Fprintln(os.Stderr, "error: -write and -media-folder cannot be used together -- write mode determines the Media Folder itself, from RootsMagic's own configuration, since that's the only source of truth that guarantees a path this server writes will still resolve correctly in RootsMagic later")
			os.Exit(2)
		}
		discovered, err := discoverMediaFolder(bypassOSCheck)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		log.Printf("using Media Folder %q (from RootsMagic's own configuration)", discovered)
		effectiveMediaFolder = discovered
	}

	router, cleanup := SetupRouter(dbPaths, *baseURL, effectiveMediaFolder, *write, *defaultGenerations, *maxPageSize)
	defer cleanup()
	log.Printf("listening on %s", *addr)
	if err := http.ListenAndServe(*addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// SetupRouter builds and returns your HTTP handler
func SetupRouter(dbPaths []string, baseURL string, mediaFolder string, write bool, defaultGenerations int, maxPageSize int) (http.Handler, func()) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("warning: couldn't determine home directory (%v); multimedia paths using '~' won't resolve", err)
	}

	entries := make([]openedDB, 0, len(dbPaths))

	for _, path := range dbPaths {
		db, err := rmdb.Open(path, !write)
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

		uniqueID, err := db.UniqueID()
		if err != nil {
			log.Printf("warning: couldn't determine the RootsMagic UniqueID for %s (%v)", path, err)
			uniqueID = ""
		}

		entries = append(entries, openedDB{path: path, db: db, dir: dir, id: id, title: title, uniqueID: uniqueID})
	}
	cleanup := func() {
		for _, e := range entries {
			e.db.Close()
		}
	}

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

	// One shared guard, not one per collection -- RootsMagic.exe running
	// is a whole-machine condition, not specific to any one database, so
	// every collection this server serves needs to see the same tripped
	// state at the same time. See WriteGuard's own doc comment in
	// internal/api/server.go, and writeguard.go, for the full reasoning.
	var guard *writeGuard
	if write {
		guard = newWriteGuard()
	}

	for _, e := range entries {
		cfg := api.Config{
			ID:                 e.id,
			BaseURL:            baseURL,
			Title:              e.title,
			DefaultGenerations: defaultGenerations,
			MaxPageSize:        maxPageSize,
			Media: rmdb.MediaFolderConfig{
				DatabaseDir: e.dir,
				HomeDir:     homeDir,
				MediaFolder: mediaFolder,
			},
		}
		// Deliberately not "WriteGuard: guard" directly above: guard is a
		// typed *writeGuard, nil when write is false, and assigning a
		// nil *concrete* pointer to an interface field produces a
		// non-nil interface value wrapping that nil pointer -- not a nil
		// interface. requireWriteAllowed's own `!= nil` check would then
		// pass, calling Allow() on a nil receiver and panicking on
		// g.mu.Lock(). Only assigning when guard is genuinely non-nil
		// keeps cfg.WriteGuard a true nil interface in read-only mode,
		// which is what requireWriteAllowed actually checks for.
		if guard != nil {
			cfg.WriteGuard = guard
		}
		srv, err := api.NewServer(e.db, cfg)
		if err != nil {
			log.Fatalf("initializing server for %q: %v", e.path, err)
		}
		collectionEntries = append(collectionEntries, api.CollectionEntry{ID: e.id, Server: srv})
	}

	printCollectionTable(entries, write)

	return api.NewMultiCollectionHandler(collectionEntries), cleanup
}

// printCollectionTable prints the collection id -> title -> database file
// -> UniqueID mapping the person running this server needs to connect a
// client to the right Collection -- this server makes no promise that a
// given database gets the same Collection id across restarts (see
// SCOPE.md), so this table is the intended way a human confirms which is
// which for the session that's about to start. UniqueID, unlike the
// Collection id, IS stable for a given database (RootsMagic assigns it
// once, at file creation -- see SCOPE.md's "Multiple databases /
// Collections" section), so it's included as a way to positively confirm
// "is this actually the same database as last time," if that ever matters
// to you, separately from the human-recognizable but unstable id.
//
// writeMode is called out prominently and separately from the table,
// since it applies to the whole server, not any one collection -- see
// SCOPE.md's "Write support" section.
func printCollectionTable(entries []openedDB, writeMode bool) {
	if writeMode {
		fmt.Fprintln(os.Stdout, "\n*** WRITE MODE ENABLED -- changes made through this API will be written directly to the database files. ***")
	} else {
		fmt.Fprintln(os.Stdout, "\nRead-only (pass -write to enable write support).")
	}

	fmt.Fprintln(os.Stdout, "\nCollections available this session:")
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "COLLECTION ID\tTITLE\tDATABASE FILE\tUNIQUE ID")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.id, e.title, e.path, e.uniqueID)
	}
	tw.Flush()
	fmt.Fprintln(os.Stdout)
}
