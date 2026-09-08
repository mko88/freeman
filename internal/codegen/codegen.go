// Package codegen renders a saved domain.Item as a small runnable
// script: bash around curl, PowerShell around Invoke-RestMethod, Python
// around requests, or JavaScript around fetch. Each pulls the URL,
// headers and body out as variables, so a body stays readable and every
// part is editable on its own. It mirrors what
// internal/httpengine.Execute does (same {{var}} substitution, same
// query-param/header/auth/body handling) so the generated command sends
// the same request the app's own "Send" would.
//
// The scripts are code and nothing else — no explanatory comments. Where
// a language can't express an option at all (fetch has no redirect cap
// and no per-request TLS), it's simply absent, and the Options tab is
// where that's written down instead.
package codegen

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"freeman/internal/domain"
	"freeman/internal/httpengine"
)

// Format selects the output flavour.
type Format string

const (
	FormatBash       Format = "bash"
	FormatPowerShell Format = "powershell"
	FormatPython     Format = "python"
	FormatJavaScript Format = "javascript"
)

// Generate renders item, with vars substituted, in the given format.
func Generate(item domain.Item, vars map[string]string, format Format) (string, error) {
	r, err := build(item, vars)
	if err != nil {
		return "", err
	}
	switch format {
	case FormatBash:
		return renderBash(r), nil
	case FormatPowerShell:
		return renderPowerShell(r), nil
	case FormatPython:
		return renderPython(r), nil
	case FormatJavaScript:
		return renderJavaScript(r), nil
	default:
		return "", fmt.Errorf("unknown code format %q", format)
	}
}

// --- neutral intermediate form -------------------------------------------------

type kv struct{ name, value string }

type bodyKind int

const (
	bodyNone bodyKind = iota
	bodyRaw
	bodyURLEncoded
	bodyForm
	bodyBinary
)

type reqBody struct {
	kind        bodyKind
	raw         string // bodyRaw
	pairs       []kv   // bodyURLEncoded (all), bodyForm (text fields)
	files       []kv   // bodyForm file fields: name -> local path
	filePath    string // bodyBinary
	contentType string // implied Content-Type, if the body mode has one
}

type request struct {
	method  string
	url     string
	headers []kv
	body    reqBody
	// These mirror the request's Options, because the defaults differ
	// from Freeman's: curl doesn't follow redirects at all unless told,
	// Invoke-RestMethod follows up to five, and neither imposes a
	// timeout the way Freeman's 30 seconds does. Left implicit and the
	// generated script would quietly do something else than Send.
	follow bool
	// maxRedirects is the effective cap, resolved from the request's
	// Options: the app default when it says nothing, and 0 for "no cap",
	// which each language spells differently.
	maxRedirects int
	// timeout is the effective one — the request's own, or the app-wide
	// default it inherits — rather than only an override, since there is
	// nothing for it to fall back to in a script.
	timeout time.Duration
	// skipTLSVerify and the cert pair carry the transport options
	// through. The pair is set only when both halves are, matching
	// domain.Options.TLS and what Execute itself loads.
	skipTLSVerify bool
	certFile      string
	certKeyFile   string
	// oauth is set when the request authenticates with the
	// client-credentials grant. A token can't be baked in — it expires,
	// and the one Freeman holds is its own — so the script fetches its
	// own before sending, rather than going out unauthenticated.
	oauth *oauthGrant
}

type oauthGrant struct {
	tokenURL, clientID, clientSecret, scope string
}

func (r *request) header(name string) string {
	for _, h := range r.headers {
		if strings.EqualFold(h.name, name) {
			return h.value
		}
	}
	return ""
}

// setHeader replaces the first case-insensitive match in place, or appends.
func (r *request) setHeader(name, value string) {
	for i, h := range r.headers {
		if strings.EqualFold(h.name, name) {
			r.headers[i] = kv{name, value}
			return
		}
	}
	r.headers = append(r.headers, kv{name, value})
}

func build(item domain.Item, vars map[string]string) (request, error) {
	method := strings.ToUpper(strings.TrimSpace(item.Method))
	if method == "" {
		method = "GET"
	}

	u, err := url.Parse(httpengine.Substitute(item.URL, vars))
	if err != nil {
		return request{}, err
	}
	if len(item.Params) > 0 {
		q := u.Query()
		for _, p := range item.Params {
			if p.Enabled {
				q.Add(httpengine.Substitute(p.Key, vars), httpengine.Substitute(p.Value, vars))
			}
		}
		u.RawQuery = q.Encode()
	}

	opts := domain.Options{FollowRedirects: true, StoreCookies: true}
	if item.Options != nil {
		opts = *item.Options
	}
	r := request{
		method:        method,
		url:           u.String(),
		follow:        opts.FollowRedirects,
		maxRedirects:  effectiveMaxRedirects(opts),
		timeout:       effectiveTimeout(opts),
		skipTLSVerify: opts.SkipTLSVerify,
	}
	if opts.ClientCertFile != "" && opts.ClientCertKeyFile != "" {
		r.certFile = httpengine.Substitute(opts.ClientCertFile, vars)
		r.certKeyFile = httpengine.Substitute(opts.ClientCertKeyFile, vars)
	}
	for _, h := range item.Headers {
		if h.Enabled {
			r.headers = append(r.headers, kv{
				httpengine.Substitute(h.Key, vars),
				httpengine.Substitute(h.Value, vars),
			})
		}
	}
	// A configured Auth wins over a hand-written Authorization row, the
	// same precedence httpengine.applyAuth uses.
	if name, value, ok := authHeader(item.Auth, vars); ok {
		r.setHeader(name, value)
	}
	if item.Auth != nil && item.Auth.Type == domain.AuthTypeOAuth2 {
		r.oauth = &oauthGrant{
			tokenURL:     httpengine.Substitute(item.Auth.TokenURL, vars),
			clientID:     httpengine.Substitute(item.Auth.ClientID, vars),
			clientSecret: httpengine.Substitute(item.Auth.ClientSecret, vars),
			scope:        httpengine.Substitute(item.Auth.Scope, vars),
		}
	}

	// Whatever the shared jar would put on this request, written out as
	// a literal Cookie header. Last, so it appends after any auth header
	// and after a hand-written Cookie row it has to merge with.
	if opts.StoreCookies {
		addJarCookies(&r, u)
	}

	r.body = buildBody(item.Body, vars)
	// A raw body carries its Content-Type as a normal header (or not at
	// all); the form/urlencoded/binary modes imply one, which we add only
	// if no header already set it. form-data is the exception — its
	// multipart boundary means the tool has to set the header itself.
	if r.body.contentType != "" && r.body.kind != bodyForm && r.header("Content-Type") == "" {
		r.headers = append(r.headers, kv{"Content-Type", r.body.contentType})
	}
	return r, nil
}

// addJarCookies writes the cookies Freeman would send with this request
// into a plain Cookie header — the ones the shared jar holds for this
// URL's host, path and scheme, since it is asked the same question
// http.Client asks it in Execute.
//
// A header rather than each language's own jar mechanism: the header is
// something every one of the four can send, including fetch, which has
// no jar at all and so used to send no cookies whatever the option said.
// It also means the script needs no file beside it, no session variable
// and no import — you can paste it anywhere and it sends what the app
// sends.
//
// The trade is that a script is a snapshot: it carries the session the
// jar held when the code was generated, and does not keep what the
// response sets. Regenerate after signing in again. A cookie is also
// plainly readable in the script, which is worth knowing before pasting
// one into a ticket.
//
// An existing Cookie header is appended to rather than replaced,
// matching http.Request.AddCookie, which is how the jar's cookies join a
// hand-written one in Execute.
func addJarCookies(r *request, u *url.URL) {
	cookies := httpengine.CookieJar().Cookies(u)
	if len(cookies) == 0 {
		return
	}
	pairs := make([]string, 0, len(cookies))
	for _, c := range cookies {
		pairs = append(pairs, c.Name+"="+c.Value)
	}
	joined := strings.Join(pairs, "; ")
	if existing := r.header("Cookie"); existing != "" {
		joined = existing + "; " + joined
	}
	r.setHeader("Cookie", joined)
}

// The nearest thing to "no cap" that two of the four languages can
// express: PowerShell's -MaximumRedirection is an int with no unlimited
// value, and requests has none either (its own default is 30).
const (
	psMaxRedirection = 2147483647
	pyMaxRedirects   = 1000
)

// effectiveMaxRedirects resolves what Execute would apply: the app
// default when the request says nothing, otherwise its own value, where
// 0 means no cap at all.
func effectiveMaxRedirects(opts domain.Options) int {
	if opts.MaxRedirects == nil {
		return httpengine.DefaultMaxRedirects
	}
	return *opts.MaxRedirects
}

// effectiveTimeout resolves what Execute would actually apply: the
// request's own override, else the app-wide default. Zero means no
// deadline at all, and neither renderer then emits one.
func effectiveTimeout(opts domain.Options) time.Duration {
	if opts.TimeoutMs > 0 {
		return time.Duration(opts.TimeoutMs) * time.Millisecond
	}
	return httpengine.RequestTimeout
}

// binaryContentType mirrors the first half of
// httpengine.detectContentType: the file's extension. Execute sniffs the
// first bytes when the extension says nothing; this package never opens
// files, so an unknown extension falls back to what Execute's own last
// resort is.
//
// Emitting *something* matters: `curl --data-binary` with no
// Content-Type defaults to application/x-www-form-urlencoded, and a
// server then parses the file as a form instead of a body — which is
// what it did, until running the generated script against httpbin
// showed the uploaded file arriving as a form field name.
func binaryContentType(path string) string {
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func authHeader(a *domain.Auth, vars map[string]string) (name, value string, ok bool) {
	if a == nil {
		return "", "", false
	}
	switch a.Type {
	case domain.AuthTypeBearer:
		if t := httpengine.Substitute(a.Token, vars); t != "" {
			return "Authorization", "Bearer " + t, true
		}
	case domain.AuthTypeBasic:
		user := httpengine.Substitute(a.Username, vars)
		pass := httpengine.Substitute(a.Password, vars)
		return "Authorization", "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass)), true
	case domain.AuthTypeAPIKey:
		if k := httpengine.Substitute(a.Key, vars); k != "" {
			return k, httpengine.Substitute(a.Value, vars), true
		}
	}
	return "", "", false
}

func buildBody(b *domain.Body, vars map[string]string) reqBody {
	if b == nil {
		return reqBody{kind: bodyNone}
	}
	switch b.Mode {
	case domain.BodyModeRaw:
		if b.Raw == "" {
			return reqBody{kind: bodyNone}
		}
		return reqBody{kind: bodyRaw, raw: httpengine.Substitute(b.Raw, vars)}

	case domain.BodyModeURLEncoded:
		var pairs []kv
		for _, f := range b.FormFields {
			if f.Enabled {
				pairs = append(pairs, kv{httpengine.Substitute(f.Key, vars), httpengine.Substitute(f.Value, vars)})
			}
		}
		if len(pairs) == 0 {
			return reqBody{kind: bodyNone}
		}
		return reqBody{kind: bodyURLEncoded, pairs: pairs, contentType: "application/x-www-form-urlencoded"}

	case domain.BodyModeForm:
		var pairs, files []kv
		for _, f := range b.FormFields {
			if !f.Enabled {
				continue
			}
			key := httpengine.Substitute(f.Key, vars)
			if f.Type == domain.FormFieldTypeFile {
				files = append(files, kv{key, httpengine.Substitute(f.FilePath, vars)})
			} else {
				pairs = append(pairs, kv{key, httpengine.Substitute(f.Value, vars)})
			}
		}
		if len(pairs) == 0 && len(files) == 0 {
			return reqBody{kind: bodyNone}
		}
		return reqBody{kind: bodyForm, pairs: pairs, files: files, contentType: "multipart/form-data"}

	case domain.BodyModeBinary:
		path := httpengine.Substitute(b.BinaryFilePath, vars)
		if path == "" {
			return reqBody{kind: bodyNone}
		}
		return reqBody{kind: bodyBinary, filePath: path, contentType: binaryContentType(path)}

	default:
		return reqBody{kind: bodyNone}
	}
}

// --- curl --------------------------------------------------------------------

// curlHeaderArgs is every -H the command needs, including the one that
// removes a header rather than adding one — see suppressContentType.
func curlHeaderArgs(r request) [][]string {
	groups := make([][]string, 0, len(r.headers)+1)
	for _, h := range r.headers {
		groups = append(groups, []string{"-H", shQuote(h.name + ": " + h.value)})
	}
	if r.oauth != nil {
		// Double-quoted, uniquely among the headers, because this one has
		// to expand $token from the fetch above it.
		groups = append(groups, []string{"-H", `"Authorization: Bearer $token"`})
	}
	if suppressContentType(r) {
		groups = append(groups, []string{"-H", shQuote("Content-Type:")})
	}
	return groups
}

// suppressContentType reports whether curl needs telling *not* to send
// one. A raw body with no Content-Type row is sent by Execute with no
// Content-Type at all, but curl fills in
// application/x-www-form-urlencoded for --data-raw — so the server
// parses the body as a form and the generated command stops matching
// what Send does. `-H 'Header:'` with nothing after the colon is curl's
// documented way to drop a header it would otherwise add.
//
// The other modes don't need this: form-data is curl's own multipart
// header, and url-encoded and binary both carry a Content-Type of their
// own by the time this runs.
func suppressContentType(r request) bool {
	return r.body.kind == bodyRaw && r.header("Content-Type") == ""
}

// renderBash writes the request as something you'd keep in a file: the
// URL, the headers and the body come out as variables first, so each is
// editable on its own instead of buried in one long command.
//
// A raw body goes in a quoted heredoc, so it needs no escaping at all —
// it appears exactly as typed, however many quotes it contains. That's
// most of the point: a JSON document is meant to stay readable here.
//
// bash rather than POSIX sh: the header and field lists are arrays.
func renderBash(r request) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n\n")
	b.WriteString("url=" + shQuote(r.url) + "\n")
	if r.certFile != "" {
		b.WriteString("cert=" + shQuote(r.certFile) + "\n")
		b.WriteString("key=" + shQuote(r.certKeyFile) + "\n")
	}

	if g := r.oauth; g != nil {
		// sed rather than jq, so the script needs nothing installed.
		b.WriteString("\ntoken=$(curl -sS -X POST " + shQuote(g.tokenURL) + " \\\n")
		b.WriteString("  --data-urlencode 'grant_type=client_credentials' \\\n")
		for _, f := range oauthFields(g) {
			b.WriteString("  --data-urlencode " + shQuote(f) + " \\\n")
		}
		b.WriteString(`  | sed -n 's/.*"access_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')` + "\n")
	}

	headerArgs := curlHeaderArgs(r)
	if len(headerArgs) > 0 {
		b.WriteString("\nheaders=(\n")
		for _, g := range headerArgs {
			b.WriteString("  " + strings.Join(g, " ") + "\n")
		}
		b.WriteString(")\n")
	}

	// These reference the variables declared above rather than inlining
	// values, so the command at the bottom stays a list of names.
	var bodyArgs []string
	switch r.body.kind {
	case bodyRaw:
		delim := heredocDelimiter(r.body.raw)
		b.WriteString("\nbody=$(cat <<'" + delim + "'\n" + r.body.raw + "\n" + delim + "\n)\n")
		bodyArgs = []string{`--data-raw "$body"`}
	case bodyURLEncoded:
		b.WriteString("\nfields=(\n")
		for _, p := range r.body.pairs {
			b.WriteString("  --data-urlencode " + shQuote(p.name+"="+p.value) + "\n")
		}
		b.WriteString(")\n")
		bodyArgs = []string{`"${fields[@]}"`}
	case bodyForm:
		b.WriteString("\nform=(\n")
		for _, p := range r.body.pairs {
			b.WriteString("  -F " + shQuote(p.name+"="+p.value) + "\n")
		}
		for _, f := range r.body.files {
			b.WriteString("  -F " + shQuote(f.name+"=@"+f.value) + "\n")
		}
		b.WriteString(")\n")
		bodyArgs = []string{`"${form[@]}"`}
	case bodyBinary:
		b.WriteString("\nfile=" + shQuote(r.body.filePath) + "\n")
		bodyArgs = []string{`--data-binary "@$file"`}
	}

	// curl doesn't follow redirects unless asked, so following has to be
	// spelled out to match what Send does.
	head := "curl -X " + r.method
	if r.follow {
		head += " -L"
		// curl's own default is 50 and -1 is its unlimited, so both ends
		// of the range have to be said out loud.
		if r.maxRedirects > 0 {
			head += fmt.Sprintf(" --max-redirs %d", r.maxRedirects)
		} else {
			head += " --max-redirs -1"
		}
	}
	lines := []string{head + ` "$url"`}
	if t := curlTransportArgs(r); t != "" {
		lines = append(lines, t)
	}
	if len(headerArgs) > 0 {
		// The reference is omitted along with the array: expanding an
		// empty one under `set -u` is an error on bash before 4.4.
		lines = append(lines, `"${headers[@]}"`)
	}
	lines = append(lines, bodyArgs...)
	b.WriteString("\n" + strings.Join(lines, " \\\n  ") + "\n")
	return b.String()
}

// curlTransportArgs is the timeout and TLS flags as one continuation
// line, or "" when the request leaves all of them at their defaults.
// curl has no timeout of its own, so Freeman's is always spelled out.
func curlTransportArgs(r request) string {
	var args []string
	if r.timeout > 0 {
		args = append(args, "--max-time "+secondsArg(r.timeout))
	}
	if r.skipTLSVerify {
		args = append(args, "--insecure")
	}
	if r.certFile != "" {
		args = append(args, `--cert "$cert" --key "$key"`)
	}
	return strings.Join(args, " ")
}

// secondsArg formats a duration the way curl's --max-time takes it:
// seconds, fractional only when it has to be.
func secondsArg(d time.Duration) string {
	if d%time.Second == 0 {
		return strconv.Itoa(int(d / time.Second))
	}
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}

// heredocDelimiter returns a word that doesn't appear on a line of its
// own in body, so the body can't terminate its own heredoc.
func heredocDelimiter(body string) string {
	delim := "BODY"
	for containsLine(body, delim) {
		delim += "_"
	}
	return delim
}

func containsLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimRight(line, "\r") == want {
			return true
		}
	}
	return false
}

// shQuote wraps s in single quotes for POSIX sh, ending the quote to
// insert an escaped literal quote for every ' in s.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// --- PowerShell ------------------------------------------------------------

// psRequest is what the PowerShell renderer works from: Content-Type is
// pulled out of the headers because Invoke-RestMethod rejects it
// appearing both there and as -ContentType.
type psRequest struct {
	headers     []kv
	contentType string
}

func splitPowerShell(r request) psRequest {
	out := psRequest{}
	for _, h := range r.headers {
		if strings.EqualFold(h.name, "Content-Type") {
			out.contentType = h.value
			continue
		}
		out.headers = append(out.headers, h)
	}
	if out.contentType == "" && r.body.kind == bodyURLEncoded {
		out.contentType = r.body.contentType
	}
	return out
}

func urlEncodedBody(pairs []kv) string {
	values := url.Values{}
	for _, p := range pairs {
		values.Add(p.name, p.value)
	}
	return values.Encode()
}

// renderPowerShell pulls every part out as a variable — $uri, $headers,
// and $body/$form/$file — so the Invoke-RestMethod call at the bottom
// reads as a list of names. A raw body becomes a single-quoted
// here-string, which is literal: no doubling of every ' the way an
// inline PowerShell string needs.
func renderPowerShell(r request) string {
	ps := splitPowerShell(r)
	var b strings.Builder

	b.WriteString("$uri = " + psQuote(r.url) + "\n")

	if r.certFile != "" {
		// CreateFromPemFile gives the certificate an ephemeral key, which
		// Windows' TLS stack won't use; a round trip through PKCS#12
		// persists it, and is what makes -Certificate work there.
		const x509 = "[System.Security.Cryptography.X509Certificates.X509Certificate2]"
		b.WriteString("\n$cert = " + x509 + "::CreateFromPemFile(" + psQuote(r.certFile) + ", " + psQuote(r.certKeyFile) + ")\n")
		b.WriteString("$cert = " + x509 + "::new($cert.Export('Pkcs12'))\n")
	}

	if g := r.oauth; g != nil {
		b.WriteString("\n$token = (Invoke-RestMethod -Method POST -Uri " + psQuote(g.tokenURL) + " -Body @{\n")
		b.WriteString("    grant_type = 'client_credentials'\n")
		for _, f := range oauthFields(g) {
			name, value, _ := strings.Cut(f, "=")
			b.WriteString("    " + name + " = " + psQuote(value) + "\n")
		}
		b.WriteString("}).access_token\n")
	}

	if len(ps.headers) > 0 || r.oauth != nil {
		b.WriteString("\n$headers = @{\n")
		for _, h := range ps.headers {
			b.WriteString("    " + psQuote(h.name) + " = " + psQuote(h.value) + "\n")
		}
		if r.oauth != nil {
			// Double quotes, so $token expands.
			b.WriteString("    'Authorization' = \"Bearer $token\"\n")
		}
		b.WriteString("}\n")
	}

	var bodyParams []string
	switch r.body.kind {
	case bodyRaw:
		b.WriteString("\n$body = " + psBodyLiteral(r.body.raw) + "\n")
		bodyParams = []string{"-Body $body"}
	case bodyURLEncoded:
		b.WriteString("\n$body = " + psQuote(urlEncodedBody(r.body.pairs)) + "\n")
		bodyParams = []string{"-Body $body"}
	case bodyForm:
		b.WriteString("\n$form = @{\n")
		for _, p := range r.body.pairs {
			b.WriteString("    " + psQuote(p.name) + " = " + psQuote(p.value) + "\n")
		}
		for _, f := range r.body.files {
			b.WriteString("    " + psQuote(f.name) + " = Get-Item " + psQuote(f.value) + "\n")
		}
		b.WriteString("}\n")
		bodyParams = []string{"-Form $form"}
	case bodyBinary:
		b.WriteString("\n$file = " + psQuote(r.body.filePath) + "\n")
		bodyParams = []string{"-InFile $file"}
	}

	params := []string{"-Method " + r.method, "-Uri $uri"}
	// Invoke-RestMethod follows up to five redirects on its own, so only
	// a different answer needs saying. 0 stops it following — it raises
	// on the 3xx rather than handing it back, which is as close as this
	// cmdlet gets to curl's default.
	if !r.follow {
		params = append(params, "-MaximumRedirection 0")
	} else if r.maxRedirects > 0 {
		params = append(params, fmt.Sprintf("-MaximumRedirection %d", r.maxRedirects))
	} else {
		params = append(params, fmt.Sprintf("-MaximumRedirection %d", psMaxRedirection))
	}
	// -TimeoutSec is whole seconds and 0 means "wait forever", so a
	// sub-second timeout rounds up to 1 rather than becoming no timeout
	// at all.
	if r.timeout > 0 {
		secs := int((r.timeout + time.Second - 1) / time.Second)
		params = append(params, fmt.Sprintf("-TimeoutSec %d", secs))
	}
	if r.skipTLSVerify {
		params = append(params, "-SkipCertificateCheck")
	}
	if r.certFile != "" {
		params = append(params, "-Certificate $cert")
	}
	if len(ps.headers) > 0 || r.oauth != nil {
		params = append(params, "-Headers $headers")
	}
	params = append(params, bodyParams...)
	if ps.contentType != "" {
		params = append(params, "-ContentType "+psQuote(ps.contentType))
	}

	b.WriteString("\nInvoke-RestMethod `\n    " + strings.Join(params, " `\n    ") + "\n")
	return b.String()
}

// psBodyLiteral prefers a single-quoted here-string — literal, so a body
// full of quotes stays exactly as typed. A here-string ends at a line
// beginning with '@, so a body containing one falls back to an inline
// quoted string rather than producing something that won't parse.
func psBodyLiteral(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "'@") {
			return psQuote(body)
		}
	}
	return "@'\n" + body + "\n'@"
}

// oauthFields lists the grant's optional form fields as name=value, in
// the order both renderers write them. grant_type is always present and
// so is written by each renderer itself.
func oauthFields(g *oauthGrant) []string {
	var out []string
	for _, f := range []struct{ name, value string }{
		{"client_id", g.clientID},
		{"client_secret", g.clientSecret},
		{"scope", g.scope},
	} {
		if f.value != "" {
			out = append(out, f.name+"="+f.value)
		}
	}
	return out
}

// psQuote wraps s in a single-quoted PowerShell string, doubling any '.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// --- Python ------------------------------------------------------------------

// renderPython writes the request against `requests`, which is what
// anyone reading generated Python expects. Everything comes out as a
// variable first, as in the other renderers, so the call at the bottom
// reads as a list of names.
func renderPython(r request) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env python3\n")
	b.WriteString("import requests\n")
	b.WriteString("\nurl = " + pyQuote(r.url) + "\n")

	b.WriteString("\nsession = requests.Session()\n")
	if r.follow {
		cap := r.maxRedirects
		if cap <= 0 {
			cap = pyMaxRedirects
		}
		b.WriteString(fmt.Sprintf("session.max_redirects = %d\n", cap))
	}

	if g := r.oauth; g != nil {
		b.WriteString("\ntoken = session.post(\n")
		b.WriteString("    " + pyQuote(g.tokenURL) + ",\n")
		b.WriteString("    data={\n        'grant_type': 'client_credentials',\n")
		for _, f := range oauthFields(g) {
			name, value, _ := strings.Cut(f, "=")
			b.WriteString("        " + pyQuote(name) + ": " + pyQuote(value) + ",\n")
		}
		b.WriteString("    },\n).json()['access_token']\n")
	}

	headers := r.headers
	if len(headers) > 0 || r.oauth != nil {
		b.WriteString("\nheaders = {\n")
		for _, h := range headers {
			b.WriteString("    " + pyQuote(h.name) + ": " + pyQuote(h.value) + ",\n")
		}
		if r.oauth != nil {
			b.WriteString("    'Authorization': f'Bearer {token}',\n")
		}
		b.WriteString("}\n")
	}

	// requests picks the Content-Type for data=/files=; a raw body is
	// bytes and keeps whatever Content-Type header the request carries.
	var callArgs []string
	var openFiles []string
	switch r.body.kind {
	case bodyRaw:
		b.WriteString("\nbody = " + pyTripleQuote(r.body.raw) + "\n")
		callArgs = append(callArgs, "data=body.encode()")
	case bodyURLEncoded:
		b.WriteString("\nform = {\n")
		for _, p := range r.body.pairs {
			b.WriteString("    " + pyQuote(p.name) + ": " + pyQuote(p.value) + ",\n")
		}
		b.WriteString("}\n")
		callArgs = append(callArgs, "data=form")
	case bodyForm:
		if len(r.body.pairs) > 0 {
			b.WriteString("\nfields = {\n")
			for _, p := range r.body.pairs {
				b.WriteString("    " + pyQuote(p.name) + ": " + pyQuote(p.value) + ",\n")
			}
			b.WriteString("}\n")
			callArgs = append(callArgs, "data=fields")
		}
		b.WriteString("\nfiles = {\n")
		for _, f := range r.body.files {
			b.WriteString("    " + pyQuote(f.name) + ": open(" + pyQuote(f.value) + ", 'rb'),\n")
			openFiles = append(openFiles, f.name)
		}
		b.WriteString("}\n")
		callArgs = append(callArgs, "files=files")
	case bodyBinary:
		b.WriteString("\nwith open(" + pyQuote(r.body.filePath) + ", 'rb') as f:\n    body = f.read()\n")
		callArgs = append(callArgs, "data=body")
	}

	args := []string{pyQuote(r.method), "url"}
	if len(headers) > 0 || r.oauth != nil {
		args = append(args, "headers=headers")
	}
	args = append(args, callArgs...)
	// allow_redirects is always spelled out: requests follows by default
	// for everything but HEAD, so leaving it implicit would make a HEAD
	// behave differently from the same request in the app.
	args = append(args, fmt.Sprintf("allow_redirects=%s", pyBool(r.follow)))
	if r.timeout > 0 {
		args = append(args, "timeout="+secondsArg(r.timeout))
	}
	if r.skipTLSVerify {
		args = append(args, "verify=False")
	}
	if r.certFile != "" {
		args = append(args, "cert=("+pyQuote(r.certFile)+", "+pyQuote(r.certKeyFile)+")")
	}

	b.WriteString("\nresponse = session.request(\n")
	for _, a := range args {
		b.WriteString("    " + a + ",\n")
	}
	b.WriteString(")\n")

	for _, name := range openFiles {
		b.WriteString("files[" + pyQuote(name) + "].close()\n")
	}
	b.WriteString("\nprint(response.status_code)\nprint(response.text)\n")
	return b.String()
}

// pyQuote wraps s in a single-quoted Python string. Backslashes go
// first, or the escapes added after it would be escaped in turn.
func pyQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return "'" + s + "'"
}

// pyTripleQuote keeps a multi-line body readable, the way the heredoc
// and here-string do in the other two. A body that would close the
// quote early, or end in one, falls back to a normal escaped string.
func pyTripleQuote(body string) string {
	if strings.Contains(body, `'''`) || strings.Contains(body, `\`) || strings.HasSuffix(body, "'") {
		return pyQuote(body)
	}
	return "'''\\\n" + body + "'''"
}

func pyBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

// --- JavaScript --------------------------------------------------------------

// renderJavaScript writes the request against fetch, which needs no
// dependency on Node 18+ and is what a browser reader expects too. It's
// the weakest of the four on the transport options: the redirect cap
// and the client certificate have no equivalent and don't appear, and
// the certificate check is skipped process-wide because fetch has no
// per-request setting for it.
func renderJavaScript(r request) string {
	var b strings.Builder

	if r.skipTLSVerify {
		// Process-wide and read when the connection is made, so it has
		// to be set before the first fetch, not passed to it.
		b.WriteString("process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0'\n\n")
	}

	b.WriteString("const url = " + jsQuote(r.url) + "\n")

	if g := r.oauth; g != nil {
		b.WriteString("\nconst tokenResponse = await fetch(" + jsQuote(g.tokenURL) + ", {\n")
		b.WriteString("  method: 'POST',\n")
		b.WriteString("  headers: { 'Content-Type': 'application/x-www-form-urlencoded' },\n")
		b.WriteString("  body: new URLSearchParams({\n    grant_type: 'client_credentials',\n")
		for _, f := range oauthFields(g) {
			name, value, _ := strings.Cut(f, "=")
			b.WriteString("    " + name + ": " + jsQuote(value) + ",\n")
		}
		b.WriteString("  }),\n})\n")
		b.WriteString("const { access_token: token } = await tokenResponse.json()\n")
	}

	if len(r.headers) > 0 || r.oauth != nil {
		b.WriteString("\nconst headers = {\n")
		for _, h := range r.headers {
			b.WriteString("  " + jsKey(h.name) + ": " + jsQuote(h.value) + ",\n")
		}
		if r.oauth != nil {
			b.WriteString("  Authorization: `Bearer ${token}`,\n")
		}
		b.WriteString("}\n")
	}

	var bodyInit string
	switch r.body.kind {
	case bodyRaw:
		b.WriteString("\nconst body = " + jsTemplate(r.body.raw) + "\n")
		bodyInit = "body"
	case bodyURLEncoded:
		b.WriteString("\nconst body = new URLSearchParams({\n")
		for _, p := range r.body.pairs {
			b.WriteString("  " + jsKey(p.name) + ": " + jsQuote(p.value) + ",\n")
		}
		b.WriteString("})\n")
		bodyInit = "body"
	case bodyForm:
		b.WriteString("\nconst body = new FormData()\n")
		for _, p := range r.body.pairs {
			b.WriteString("body.append(" + jsQuote(p.name) + ", " + jsQuote(p.value) + ")\n")
		}
		for _, f := range r.body.files {
			// openAsBlob is Node 20+; a browser would use a File from an
			// <input> instead. Either way FormData sets the multipart
			// boundary itself, so no Content-Type header is added.
			b.WriteString("body.append(" + jsQuote(f.name) + ", await openAsBlob(" + jsQuote(f.value) + "))\n")
		}
		bodyInit = "body"
	case bodyBinary:
		b.WriteString("\nconst body = await readFile(" + jsQuote(r.body.filePath) + ")\n")
		bodyInit = "body"
	}

	if r.body.kind == bodyForm && len(r.body.files) > 0 {
		b.WriteString("\n")
	}

	b.WriteString("\nconst response = await fetch(url, {\n")
	b.WriteString("  method: " + jsQuote(r.method) + ",\n")
	if len(r.headers) > 0 || r.oauth != nil {
		b.WriteString("  headers,\n")
	}
	if bodyInit != "" {
		b.WriteString("  body,\n")
	}
	// 'follow' is fetch's default, but saying it keeps the script honest
	// next to the 'manual' case rather than leaving the reader to know.
	if r.follow {
		b.WriteString("  redirect: 'follow',\n")
	} else {
		b.WriteString("  redirect: 'manual',\n")
	}
	if r.timeout > 0 {
		b.WriteString(fmt.Sprintf("  signal: AbortSignal.timeout(%d),\n", r.timeout.Milliseconds()))
	}
	b.WriteString("})\n")

	b.WriteString("\nconsole.log(response.status)\nconsole.log(await response.text())\n")

	// The imports the body forms above need, prepended once their use is
	// known rather than guessed at the top.
	var imports []string
	if r.body.kind == bodyForm && len(r.body.files) > 0 {
		imports = append(imports, "import { openAsBlob } from 'node:fs'")
	}
	if r.body.kind == bodyBinary {
		imports = append(imports, "import { readFile } from 'node:fs/promises'")
	}
	if len(imports) == 0 {
		return b.String()
	}
	return strings.Join(imports, "\n") + "\n\n" + b.String()
}

// jsQuote wraps s in a single-quoted JavaScript string.
func jsQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return "'" + s + "'"
}

// jsIdentifier matches a header or field name that can be an object key
// as written, so the common ones don't all end up quoted.
var jsIdentifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func jsKey(name string) string {
	if jsIdentifier.MatchString(name) {
		return name
	}
	return jsQuote(name)
}

// jsTemplate keeps a multi-line body readable in a template literal.
// A body containing a backtick or a ${ would change what it means, so
// that falls back to a quoted string.
func jsTemplate(body string) string {
	if strings.ContainsAny(body, "`\\") || strings.Contains(body, "${") {
		return jsQuote(body)
	}
	return "`" + body + "`"
}
