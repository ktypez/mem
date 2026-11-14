package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// mem v2 — a wiki-style memory service.
//
// Pages are markdown files on disk (MEM_WIKI_DIR); a SQLite FTS5 index is
// derived from them and can be rebuilt at any time. Every agent can read and
// write: facts land directly in the page for their topic. There are no edges,
// no review inbox, and no external embedding dependency.

const port = "3200"

// defaultListenAddr is where the server binds when MEM_LISTEN_ADDR is unset:
// loopback only.
func defaultListenAddr() string {
	return "127.0.0.1:" + port
}

// splitList splits a comma-separated environment value, dropping blanks.
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// loadServerConfig reads the listen address and credentials from the environment
// and refuses to start in a configuration that would expose the API by accident.
func loadServerConfig() (string, authConfig) {
	listenAddr := strings.TrimSpace(os.Getenv("MEM_LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = defaultListenAddr()
	}
	cfg := authConfig{
		APIToken:       strings.TrimSpace(os.Getenv("MEM_API_TOKEN")),
		StewardToken:   strings.TrimSpace(os.Getenv("MEM_STEWARD_TOKEN")),
		UIUser:         strings.TrimSpace(os.Getenv("MEM_UI_USER")),
		UIPass:         strings.TrimSpace(os.Getenv("MEM_UI_PASSWORD")),
		AllowedOrigins: splitList(os.Getenv("MEM_ALLOWED_ORIGINS")),
	}
	if cfg.UIUser == "" {
		cfg.UIUser = "admin"
	}
	if cfg.APIToken == "" && cfg.StewardToken == "" && !isLoopbackListenAddress(listenAddr) {
		log.Fatal("MEM_API_TOKEN is required when listening on a non-loopback address")
	}
	if !cfg.enabled() {
		// A reverse proxy or socket proxy in front of a loopback listener makes
		// every remote client look local, so running open must be a deliberate choice.
		if os.Getenv("MEM_ALLOW_UNAUTHENTICATED") != "true" {
			log.Fatal("no authentication configured: set MEM_API_TOKEN (and MEM_UI_PASSWORD for the web UI), or set MEM_ALLOW_UNAUTHENTICATED=true for local development only")
		}
		log.Printf("WARNING: running without authentication (MEM_ALLOW_UNAUTHENTICATED=true); never put a proxy in front of this mode")
	}
	return listenAddr, cfg
}

func isLoopbackListenAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// startRescanner keeps the index in sync with hand edits to the markdown files.
func startRescanner(stop <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if n, err := rescanWiki(); err != nil {
				log.Printf("rescan: %v", err)
			} else if n > 0 {
				log.Printf("rescan: %d page(s) synced from disk", n)
			}
		}
	}
}

func runServer() error {
	listenAddr, authCfg := loadServerConfig()
	if err := openIndex(); err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer closeIndex()
	if n, err := rescanWiki(); err != nil {
		log.Printf("startup rescan failed: %v", err)
	} else if n > 0 {
		log.Printf("startup rescan: %d page(s) synced", n)
	}

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health", handleHealth)

	// Wiki API
	mux.HandleFunc("/v1/wiki/pages", handleWikiPages)
	mux.HandleFunc("/v1/wiki/pages/", handleWikiPage)
	mux.HandleFunc("/v1/wiki/reindex", handleWikiReindex)

	// Compat API (memoh-style aliases kept for existing clients)
	mux.HandleFunc("/v1/memory/nodes", handleNodesCompat)
	mux.HandleFunc("/v1/memory/nodes/", handleNodeCompat)
	mux.HandleFunc("/v1/memory/edges", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, 200, map[string]interface{}{"edges": []interface{}{}, "count": 0})
	})
	mux.HandleFunc("/v1/memory/search", handleSearch)
	mux.HandleFunc("/v1/memory/stats", handleStats)
	mux.HandleFunc("/v1/memory/context", handleContext)
	mux.HandleFunc("/v1/memory/extract", handleExtract)
	mux.HandleFunc("/v1/memory/brief", handleBrief)
	mux.HandleFunc("/v1/memory/inbox", handleInbox)
	mux.HandleFunc("/v1/memory/backup", handleBackup)
	mux.HandleFunc("/v1/memory/restore", handleRestore)
	mux.HandleFunc("/api/nodes", handleNodesCompat)
	mux.HandleFunc("/api/nodes/", handleNodeCompat)
	mux.HandleFunc("/api/brief", handleBrief)
	mux.HandleFunc("/api/inbox", handleInbox)

	// UI
	mux.HandleFunc("/", handleUI)

	server := &http.Server{
		Addr: listenAddr, Handler: apiAuthMiddleware(authCfg, mux),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second,
	}

	// Disk rescan keeps the index aligned with hand edits.
	stopScan := make(chan struct{})
	go startRescanner(stopScan, 5*time.Minute)

	// Shut down cleanly so the rescan stops instead of writing mid-exit.
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	go func() {
		<-ctx.Done()
		close(stopScan)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("mem wiki service starting on %s (wiki dir: %s)", listenAddr, wikiDir())
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	log.Printf("mem service stopped")
	return nil
}

// flagsFirst reorders CLI arguments so flags precede positional arguments:
// Go's flag package stops parsing at the first positional, which surprises
// users who write `mem add page "text" -source x`.
func flagsFirst(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") && len(args[i]) > 1 {
			flags = append(flags, args[i])
			switch args[i] {
			case "-gzip", "-h", "-help", "-help=true":
				// boolean flags take no value
			default:
				if i+1 < len(args) {
					flags = append(flags, args[i+1])
					i++
				}
			}
		} else {
			rest = append(rest, args[i])
		}
	}
	return append(flags, rest...)
}

func main() {
	osArgs := os.Args[1:]
	if len(osArgs) == 0 {
		osArgs = []string{"help"}
	}
	cmd := osArgs[0]
	rest := osArgs[1:]
	if cmd == "add" || cmd == "search" {
		rest = flagsFirst(rest)
	}

	var showHelp bool
	var legacyBackupFlag string // -backup, accepted for CLI compatibility
	var legacyRestoreFlag string
	var fileFlag string
	var useGzip bool
	var searchLimit int
	var cliSource string

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.BoolVar(&showHelp, "help", false, "show help")
	fs.BoolVar(&showHelp, "h", false, "show help (shorthand)")
	fs.StringVar(&legacyBackupFlag, "backup", "", "alias for --file (compat)")
	fs.StringVar(&legacyRestoreFlag, "restore", "", "alias for --file (compat)")
	fs.StringVar(&fileFlag, "file", "", "file path for backup/restore/import (use - for stdin/stdout)")
	fs.BoolVar(&useGzip, "gzip", false, "use gzip compression for backup")
	fs.IntVar(&searchLimit, "limit", 10, "max results for search")
	fs.StringVar(&cliSource, "source", "cli", "writer label recorded by mem add")
	if err := fs.Parse(rest); err != nil {
		fmt.Println(err)
		return
	}

	if showHelp || cmd == "help" {
		fmt.Print(`mem - Wiki Memory Service (v2)

Usage:
  mem server                    Start the HTTP server
  mem list                      List wiki pages (title, tags, updated)
  mem show PAGE                 Print a page (title or slug)
  mem search QUERY [-limit N]   Search the wiki
  mem add PAGE TEXT [-source S] Append a fact line to a page
  mem organize                  Nightly housekeeping (dedupe, trash rotation)
  mem backup --file FILE        Backup the wiki (gzipped tar; use - for stdout)
  mem restore --file FILE       Restore the wiki from a backup
  mem reindex                   Rebuild the search index from the page files
  mem import-legacy --file F    Convert an old mem backup (nodes/edges) into pages
  mem help                      Show this help

The wiki is markdown files in MEM_WIKI_DIR (default ~/mem-wiki). The SQLite
search index (.mem-index.db) is derived state and always rebuildable.

Environment variables:
  MEM_WIKI_DIR          Wiki directory (default: ~/mem-wiki)
  MEM_API_TOKEN         Bearer token for agents (read + write)
  MEM_STEWARD_TOKEN     Optional second token, accepted like MEM_API_TOKEN
  MEM_LISTEN_ADDR       Bind address (default: 127.0.0.1:3200)
  MEM_UI_USER           Web UI basic auth username (default: admin)
  MEM_UI_PASSWORD       Web UI basic auth password
  MEM_ALLOWED_ORIGINS   Extra browser origins allowed to write (reverse proxies that rewrite Host)
  MEM_ALLOW_UNAUTHENTICATED=true  Allow running without any credential (local development only)

Examples:
  mem backup --file backup.tar.gz
  mem restore --file backup.tar.gz
  mem import-legacy --file mem-legacy-backup.json.gz
  mem backup | ssh remote mem restore --file -
`)
		return
	}

	switch cmd {
	case "server":
		if err := runServer(); err != nil {
			log.Fatal(err)
		}
		return
	case "backup":
		fn := fileFlag
		if fn == "" {
			fn = legacyBackupFlag
		}
		if err := runCLIBackup(fn, useGzip); err != nil {
			log.Fatal(err)
		}
		return
	case "restore":
		fn := fileFlag
		if fn == "" {
			fn = legacyRestoreFlag
		}
		if err := runCLIRestore(fn); err != nil {
			log.Fatal(err)
		}
		return
	case "reindex":
		if err := openIndex(); err != nil {
			log.Fatal(err)
		}
		defer closeIndex()
		n, err := rescanWiki()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Index rebuilt: %d change(s) synced\n", n)
		return
	case "list":
		pages, err := listPages()
		if err != nil {
			log.Fatal(err)
		}
		for _, p := range pages {
			fmt.Printf("%-40s %-18s %s  [%s]\n", p.Title, p.Slug, strings.TrimSpace(p.UpdatedAt[:10]), strings.Join(p.Tags, ","))
		}
		fmt.Printf("(%d pages)\n", len(pages))
		return
	case "show":
		if len(fs.Args()) < 1 {
			fmt.Println("usage: mem show PAGE")
			return
		}
		name := fs.Args()[0]
		p, err := loadPage(slugify(name))
		if err != nil {
			slug, slugErr := slugForTitle(name)
			if slugErr == nil {
				p, err = loadPage(slug)
			}
		}
		if err != nil {
			fmt.Printf("page not found: %s\n", name)
			os.Exit(1)
		}
		out, _ := os.ReadFile(pageFile(p.Slug))
		os.Stdout.Write(out)
		return
	case "search":
		query := strings.Join(fs.Args(), " ")
		if strings.TrimSpace(query) == "" {
			fmt.Println("usage: mem search QUERY")
			return
		}
		if err := openIndex(); err != nil {
			log.Fatal(err)
		}
		defer closeIndex()
		hits := searchWiki(query, searchLimit)
		for _, h := range hits {
			fmt.Printf("## %s (%s)\n%s\n\n", h.Title, h.Slug, h.Excerpt)
		}
		fmt.Printf("(%d result(s))\n", len(hits))
		return
	case "add":
		args := fs.Args()
		if len(args) < 2 {
			fmt.Println("usage: mem add PAGE TEXT")
			return
		}
		if err := openIndex(); err != nil {
			log.Fatal(err)
		}
		defer closeIndex()
		p, created, err := appendFact(args[0], "", strings.Join(args[1:], " "), cliSource)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("appended to %q (page %s, %s)\n", p.Title, p.Slug, map[bool]string{true: "created", false: "updated"}[created])
		return
	case "organize":
		res, err := runOrganizer()
		if err != nil {
			log.Fatal(err)
		}
		organizeLog(res)
		closeIndex()
		fmt.Println(formatOrganizeResult(res))
		return
	case "import-legacy":
		if err := runImportLegacy(fileFlag); err != nil {
			log.Fatal(err)
		}
		return
	default:
		fmt.Printf("Unknown command: %s\n(mem server | mem backup --file F | mem restore --file F | mem reindex | mem import-legacy --file F)\n", cmd)
	}
}
