package main

import (
	"embed"
	"encoding/json"
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
// POST /api/ui/action is layered on top of internal/httpapi's handler
// (which only knows about *core.App's data operations) since driving the
// GUI itself needs the Wails-bound app.DispatchUIAction — see
// internal/wailsapp/app.go.
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
	mux.Handle("/", httpapi.NewHandler(app.App, nil))

	log.Printf("freeman: control API on http://%s (set FREEMAN_CONTROL_LISTEN to change)", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("freeman: control API not started: %v", err)
	}
}
