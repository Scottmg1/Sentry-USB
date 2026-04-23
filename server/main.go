package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime/debug"

	"github.com/Scottmg1/Sentry-USB/server/api"
	"github.com/Scottmg1/Sentry-USB/server/drives"
	"github.com/Scottmg1/Sentry-USB/server/ws"
)

//go:embed all:static
var staticFiles embed.FS

func main() {
	port := flag.Int("port", 8788, "HTTP server port")
	dev := flag.Bool("dev", false, "Development mode (don't serve embedded static files)")
	staticDir := flag.String("static", "", "Path to static files directory (overrides embedded)")
	flag.Parse()

	// Set a soft memory limit to help the GC be more aggressive on
	// memory-constrained Pis (512MB–1GB RAM). GOMEMLIMIT is a soft
	// target — the runtime will try harder to return memory to the OS
	// before reaching this limit, reducing OOM risk.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(200 * 1024 * 1024) // 200MB soft limit
	}

	// pprof debug endpoint — localhost only so it's not exposed on the network.
	// Access via: curl http://localhost:6060/debug/pprof/heap > heap.prof
	go func() {
		log.Printf("pprof listening on localhost:6060")
		if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
			log.Printf("pprof server failed: %v", err)
		}
	}()

	// Self-heal: update peripheral files (scripts, BLE daemon, etc.) if the
	// binary is newer than the last migration.  Runs in the background so the
	// HTTP server starts immediately.  Safe to run repeatedly; never touches
	// setup-wizard configuration.
	go runStartupMigration()

	// Load web auth credentials from config
	api.InitAuth()

	hub := ws.NewHub()
	go hub.Run()

	mux := http.NewServeMux()

	// API routes
	api.RegisterRoutes(mux, hub)

	// Drive map routes. The drive aggregate backfill on a first-boot-
	// after-upgrade can take 10+ minutes on a 512MB Pi; it owns the
	// nudge loop directly (same tier as Archive / Drive Processing) and
	// is reflected in the webui manager's isBusy predicate below so
	// user Stop / timer expiry cannot interrupt it.
	driveHandlers := api.NewDriveHandlers(drives.DefaultDataPath, hub)
	api.RegisterDriveRoutes(mux, driveHandlers)

	// Keep-awake manager (user-controlled from web UI). isBusy returns
	// true whenever a higher-priority system op owns the nudge loop;
	// webui Start queues as pending, and webui Stop / expiry leaves the
	// nudge alone.
	kam := api.NewKeepAwakeManager(func() bool {
		return api.IsArchiving() ||
			driveHandlers.Processor().IsRunning() ||
			driveHandlers.Store().MigrationStatus().Active
	})
	api.RegisterKeepAwakeRoutes(mux, kam)

	// Away Mode manager (user-controlled AP from web UI)
	awm := api.NewAwayModeManager()
	awm.RestoreFromFile()
	api.RegisterAwayModeRoutes(mux, awm)

	// Memory debug page (sentryusb.local/memory)
	mux.HandleFunc("GET /memory", api.MemoryPage)

	// WebSocket endpoint
	mux.HandleFunc("/api/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWs(hub, w, r)
	})

	// Serve TeslaCam video files (replaces nginx's role of serving /var/www/html/TeslaCam/)
	// The FUSE mount at /var/www/html/TeslaCam maps to /mutable/TeslaCam via cttseraser
	mux.Handle("/TeslaCam/", http.StripPrefix("/TeslaCam/", http.FileServer(http.Dir("/var/www/html/TeslaCam"))))

	// Also serve /fs/ for music/lightshow/boombox autofs mounts
	mux.Handle("/fs/", http.StripPrefix("/fs/", http.FileServer(http.Dir("/var/www/html/fs"))))

	// Static file serving
	if !*dev {
		var staticFS http.FileSystem
		if *staticDir != "" {
			staticFS = http.Dir(*staticDir)
		} else {
			sub, err := fs.Sub(staticFiles, "static")
			if err != nil {
				log.Fatal("Failed to access embedded static files:", err)
			}
			staticFS = http.FS(sub)
		}

		// SPA fallback: serve index.html for any non-API, non-file route
		mux.Handle("/", spaHandler(staticFS))
	}

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("SentryUSB server starting on %s", addr)
	if *dev {
		log.Printf("Running in development mode (no static file serving)")
	}

	handler := api.NewAuthMiddleware(mux)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}

// spaHandler serves static files and falls back to index.html for SPA routing
func spaHandler(staticFS http.FileSystem) http.Handler {
	fileServer := http.FileServer(staticFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to open the file
		f, err := staticFS.Open(r.URL.Path)
		if err != nil {
			// File doesn't exist, serve index.html for SPA routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}
