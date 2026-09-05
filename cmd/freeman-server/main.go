// Command freeman-server is the headless counterpart to cmd/freeman: it
// serves the same collection/environment/request functionality (via
// internal/core, wrapped by internal/httpapi) as a plain HTTP server, so
// it can run in a container hit from a browser instead of as a native
// Wails desktop app. It has no dependency on Wails at all — no CGO, no
// GTK/WebKit, no mingw — so it builds and runs anywhere a Go toolchain
// does.
package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"

	"freeman/internal/core"
	"freeman/internal/httpapi"
)

//go:embed all:web
var webFS embed.FS

func main() {
	workspace := flag.String("workspace", os.Getenv("FREEMAN_WORKSPACE"), "workspace directory (required)")
	// Loopback by default: this server has no authentication, serves the
	// workspace (including the .local.json secret overlays) for reading
	// and writing, and will issue arbitrary outbound HTTP on request via
	// POST /api/execute — so anyone who can reach it gets both a
	// disclosure and an SSRF pivot. Exposing it beyond localhost has to
	// be a deliberate act (FREEMAN_LISTEN / -listen), and should mean
	// putting an authenticating proxy in front. The container image sets
	// FREEMAN_LISTEN=:8080 because a published Docker port can only
	// reach a container process bound to all interfaces; docker-compose
	// then publishes it on the host's loopback only.
	listen := flag.String("listen", envOr("FREEMAN_LISTEN", "127.0.0.1:8080"), "listen address")
	flag.Parse()

	if *workspace == "" {
		log.Fatal("freeman-server: -workspace or FREEMAN_WORKSPACE is required")
	}

	app := core.NewApp()
	if _, err := app.OpenWorkspace(*workspace); err != nil {
		log.Fatalf("freeman-server: opening workspace %q: %v", *workspace, err)
	}

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("freeman-server: %v", err)
	}

	handler := httpapi.NewHandler(app, static)
	log.Printf("freeman-server: workspace %q, listening on %s", *workspace, *listen)
	log.Fatal(http.ListenAndServe(*listen, handler))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
