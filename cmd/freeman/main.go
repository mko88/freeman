package main

import (
	"embed"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"freeman/internal/httpapi"
	"freeman/internal/wailsapp"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := wailsapp.NewApp()

	controlAddr := os.Getenv("FREEMAN_CONTROL_LISTEN")
	if controlAddr == "" {
		controlAddr = "127.0.0.1:8090"
	}
	app.SetControlAPIAddr(controlAddr)
	go startControlAPI(app, controlAddr)

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "freeman",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.Startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

// startControlAPI runs internal/httpapi's JSON API — loopback-only,
// alongside the GUI, wrapping the exact same *core.App the Wails
// bindings use — so external scripts, test harnesses, or AI agents can
// drive Freeman (list/save requests and environments, execute requests,
// load data) without going through the webview. Never listens beyond
// 127.0.0.1 by default: no auth in front of it, so it must not be
// network-reachable. A bind failure (e.g. a second Freeman instance
// already running) is logged and left there — it must not take the GUI
// down with it.
//
// Loopback keeps other machines out but not the browser the user already
// has open, so the whole mux goes through httpapi.GuardSameOrigin — see
// internal/httpapi/guard.go for what that refuses and why. It's applied
// here, at the outer mux, because the /api/ui/* routes below are
// cmd/freeman's own; httpapi.NewHandler guards the routes it owns itself.
//
// POST /api/ui/action and GET /api/ui/state are layered on top of
// internal/httpapi's handler (which only knows about *core.App's data
// operations) since driving/reading the GUI itself needs the Wails-bound
// app.DispatchUIAction/UIState — see internal/wailsapp/app.go.
func startControlAPI(app *wailsapp.App, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/ui/action", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Action  string         `json:"action"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		app.DispatchUIAction(body.Action, body.Payload)
		w.WriteHeader(http.StatusNoContent)
	})
	// The read-side counterpart to POST /api/ui/action: what's currently
	// on screen (the unsaved draft, the last response, ...) — see
	// wailsapp.App.ReportUIState/UIState — instead of a screenshot.
	mux.HandleFunc("GET /api/ui/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// UIState is already a JSON string (App.svelte encodes it before
		// sending) — write it as-is rather than json.Encode'ing it again,
		// which would wrap it as a quoted JSON string literal instead of
		// serving the object itself.
		io.WriteString(w, app.UIState())
	})
	mux.Handle("/", httpapi.NewHandler(app.App, nil))

	log.Printf("freeman: control API on http://%s (set FREEMAN_CONTROL_LISTEN to change)", addr)
	if err := http.ListenAndServe(addr, httpapi.GuardSameOrigin(mux)); err != nil {
		log.Printf("freeman: control API not started: %v", err)
	}
}
