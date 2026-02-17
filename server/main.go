package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

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

	hub := ws.NewHub()
	go hub.Run()

	mux := http.NewServeMux()

	// API routes
	api.RegisterRoutes(mux, hub)

	// Drive map routes
	driveHandlers := api.NewDriveHandlers(drives.DefaultDataPath, hub)
	api.RegisterDriveRoutes(mux, driveHandlers)

	// WebSocket endpoint
	mux.HandleFunc("/api/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeWs(hub, w, r)
	})

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

	// Auto-resume setup if it was interrupted by a reboot (e.g. partition resize)
	api.AutoResumeSetup(hub)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("SentryUSB server starting on %s", addr)
	if *dev {
		log.Printf("Running in development mode (no static file serving)")
	}

	if err := http.ListenAndServe(addr, mux); err != nil {
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
