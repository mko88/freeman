// Package httpapi exposes internal/core's application logic as a JSON
// HTTP API. It has two callers: cmd/freeman-server serves it (with a
// static frontend mounted at "/") as the headless counterpart to the
// desktop GUI; cmd/freeman also starts it loopback-only, alongside the
// GUI, as a "control API" external scripts/tools/AI agents can drive the
// running desktop app through (see internal/wailsapp.GetTheme for the one
// desktop-only endpoint this doesn't cover). Both wrap the same *core.App
// rather than duplicating its logic.
package httpapi

import (
	"errors"
	"io/fs"
	"net/http"

	"freeman/internal/core"
	"freeman/internal/domain"
	"freeman/internal/headercatalog"
	"freeman/internal/theme"
)

// executeRequest is POST /api/execute's body shape.
type executeRequest struct {
	CollectionID  string `json:"collectionId"`
	ItemID        string `json:"itemId"`
	EnvironmentID string `json:"environmentId"`
}

// namedRequest is the body for the routes whose whole payload is a
// display name — creating a collection, renaming one.
type namedRequest struct {
	Name string `json:"name"`
}

// codegenRequest is POST /api/codegen's body shape: the request to render
// (passed inline so unsaved editor edits can be previewed), the
// environment to resolve {{var}} against, and the output format (see
// internal/codegen: bash or powershell).
type codegenRequest struct {
	Item          domain.Item `json:"item"`
	EnvironmentID string      `json:"environmentId"`
	Format        string      `json:"format"`
}

// NewHandler builds the HTTP API, plus a static frontend handler mounted
// at "/" if static is non-nil (cmd/freeman-server passes the built web
// frontend; cmd/freeman's control API passes nil since the GUI itself is
// the frontend — "/" then just 404s, only /api/* is served). The result
// is wrapped in GuardSameOrigin so this is safe by construction for any
// caller — cmd/freeman wraps its own outer mux too, for the /api/ui/*
// routes it mounts above this one.
func NewHandler(app *core.App, static fs.FS) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /api/workspace", func(w http.ResponseWriter, r *http.Request) {
		info, err := app.CurrentWorkspace()
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	})

	mux.HandleFunc("GET /api/collections", func(w http.ResponseWriter, r *http.Request) {
		list, err := app.ListCollections()
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	})

	mux.HandleFunc("GET /api/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		c, err := app.GetCollection(r.PathValue("id"))
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	})

	mux.HandleFunc("POST /api/collections", func(w http.ResponseWriter, r *http.Request) {
		var body namedRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		c, err := app.CreateCollection(body.Name)
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	})

	mux.HandleFunc("PATCH /api/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body namedRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		c, err := app.RenameCollection(r.PathValue("id"), body.Name)
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	})

	mux.HandleFunc("DELETE /api/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := app.DeleteCollection(r.PathValue("id")); err != nil {
			writeCoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /api/collections/{id}/requests", func(w http.ResponseWriter, r *http.Request) {
		var item domain.Item
		if err := decodeJSON(r, &item); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, err := app.SaveRequest(r.PathValue("id"), item)
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
	})

	mux.HandleFunc("DELETE /api/collections/{id}/requests/{itemId}", func(w http.ResponseWriter, r *http.Request) {
		if err := app.DeleteRequest(r.PathValue("id"), r.PathValue("itemId")); err != nil {
			writeCoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/environments", func(w http.ResponseWriter, r *http.Request) {
		list, err := app.ListEnvironments()
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	})

	mux.HandleFunc("GET /api/environments/{id}", func(w http.ResponseWriter, r *http.Request) {
		e, err := app.GetEnvironment(r.PathValue("id"))
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, e)
	})

	mux.HandleFunc("POST /api/environments", func(w http.ResponseWriter, r *http.Request) {
		var env domain.Environment
		if err := decodeJSON(r, &env); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, err := app.SaveEnvironment(env)
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
	})

	mux.HandleFunc("DELETE /api/environments/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := app.DeleteEnvironment(r.PathValue("id")); err != nil {
			writeCoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /api/execute", func(w http.ResponseWriter, r *http.Request) {
		var req executeRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		resp, err := app.ExecuteRequest(r.Context(), req.CollectionID, req.ItemID, req.EnvironmentID)
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("POST /api/codegen", func(w http.ResponseWriter, r *http.Request) {
		var req codegenRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		code, err := app.GenerateRequestCode(req.Item, req.EnvironmentID, req.Format)
		if err != nil {
			writeCoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"code": code})
	})

	// Resolved from theme.yaml inside the workspace directory (the one
	// thing guaranteed to persist via a Docker volume mount), not
	// appdata.Dir() — a per-user OS config dir has no durable meaning
	// inside a container. See internal/wailsapp.GetTheme for desktop's
	// appdata-based equivalent.
	mux.HandleFunc("GET /api/theme", func(w http.ResponseWriter, r *http.Request) {
		palette := theme.Resolve(theme.Load(app.WorkspaceRoot()))
		writeJSON(w, http.StatusOK, palette)
	})

	// Common request-header names/values for editor autocomplete, resolved
	// from headers.yaml in the workspace directory (durable across a
	// container restart via the volume mount) — see
	// internal/wailsapp.GetHeaderCatalog for desktop's appdata-based
	// equivalent, and internal/headercatalog for the format.
	mux.HandleFunc("GET /api/headers", func(w http.ResponseWriter, r *http.Request) {
		catalog := headercatalog.Resolve(headercatalog.Load(app.WorkspaceRoot()))
		writeJSON(w, http.StatusOK, catalog)
	})

	if static != nil {
		mux.Handle("/", spaHandler(static))
	}

	return GuardSameOrigin(mux)
}

// writeCoreError maps a core.App error to a status code: 409 for "no
// workspace is open yet" (a normal, expected state right after launch —
// not a server fault), 500 for everything else. internal/domain/store
// don't have a richer error taxonomy (e.g. not-found vs I/O failure), so
// this is deliberately coarse for v1.
func writeCoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, core.ErrNoWorkspace) {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}
