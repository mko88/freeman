package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// This file is the API's only defence against a web page driving Freeman
// behind the user's back. Binding to loopback keeps other machines out,
// but it is not a boundary against a *browser*: any site the user visits
// can send a cross-origin "simple request" to 127.0.0.1 with no CORS
// preflight, and while it can't read the reply, the write still lands.
// That is enough to matter here — an attacker can point a saved request
// at their own server, put {{apiToken}} in its body, and send it, and
// Freeman substitutes the environment's secret values itself.
//
// The two checks below make every browser-originated cross-origin
// request fail while leaving scripts, curl and the control-API test
// harness (which send neither Origin nor Sec-Fetch-*) untouched.

var (
	errCrossOrigin = errors.New("cross-origin requests are not allowed")
	errNotJSON     = errors.New("request body must be Content-Type: application/json")
)

// GuardSameOrigin wraps next so that:
//
//   - A request carrying an Origin or Sec-Fetch-Site that says it came
//     from another site is refused. Browsers always attach these on
//     cross-origin requests and can't be talked out of it; non-browser
//     callers send neither, so they're unaffected.
//   - A request with a body must declare Content-Type: application/json.
//     That type is not CORS-safelisted, so a cross-origin attempt to use
//     it forces a preflight — which fails, because this API answers no
//     preflight and sends no CORS headers. It closes the text/plain hole
//     that makes the simple-request path work in the first place.
//
// Both binaries wrap their outermost handler with this (see NewHandler
// and cmd/freeman's startControlAPI, which mounts its own /api/ui/*
// routes above this package's).
func GuardSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !originAllowed(r) {
			reject(w, r, http.StatusForbidden, errCrossOrigin)
			return
		}
		if r.ContentLength != 0 && !isJSONContentType(r.Header.Get("Content-Type")) {
			reject(w, r, http.StatusUnsupportedMediaType, errNotJSON)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// maxDrainOnReject bounds how much of a refused request's body is read
// before the response goes out. Generous next to anything this API
// takes, and small enough that refusing a request can't be turned into
// a way to make the server read an unbounded amount.
const maxDrainOnReject = 1 << 20

// reject answers with an error status, first discarding the body the
// client was in the middle of sending. Replying without reading it
// leaves the connection in a state the client can see as a reset
// instead of the status we meant to give it — which showed up as an
// intermittent ConnectionResetError in the control-API suite rather
// than the 415 it was asserting on.
func reject(w http.ResponseWriter, r *http.Request, status int, err error) {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxDrainOnReject))
	}
	writeError(w, status, err)
}

// originAllowed reports whether r's browser-set provenance headers, if
// any, say it came from this same server. A request with neither header
// (curl, a Python script, an agent) is allowed — those aren't subject to
// the ambient-credential problem CSRF exploits in the first place.
func originAllowed(r *http.Request) bool {
	// Sec-Fetch-Site is the more precise of the two: "same-site" still
	// means a different origin (localhost:3000 and localhost:8090 are
	// same-site but not same-origin), so only "same-origin" and "none"
	// (a user-typed URL or bookmark) pass.
	switch r.Header.Get("Sec-Fetch-Site") {
	case "":
		// Not a browser, or one too old to send it — fall through to Origin.
	case "same-origin", "none":
	default:
		return false
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// Compared on host:port, not the full origin string: the scheme can
	// legitimately differ from this server's own when a TLS-terminating
	// proxy sits in front of freeman-server.
	return strings.EqualFold(u.Host, r.Host)
}

// isJSONContentType accepts application/json with or without parameters
// (e.g. "; charset=utf-8"), which is what every JSON client sends.
func isJSONContentType(header string) bool {
	if header == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}
