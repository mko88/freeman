// Package codegen renders a saved domain.Item as a runnable command in an
// external tool — a curl invocation or a PowerShell Invoke-RestMethod
// call, each as a one-liner or a small script. It mirrors what
// internal/httpengine.Execute does (same {{var}} substitution, same
// query-param/header/auth/body handling) so the generated command sends
// the same request the app's own "Send" would.
package codegen

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"path/filepath"
	"strings"

	"freeman/internal/domain"
	"freeman/internal/httpengine"
)

// Format selects the output flavour.
type Format string

const (
	FormatCurl             Format = "curl"
	FormatShell            Format = "shell"
	FormatPowerShell       Format = "powershell"
	FormatPowerShellScript Format = "powershell-script"
)

// Generate renders item, with vars substituted, in the given format.
func Generate(item domain.Item, vars map[string]string, format Format) (string, error) {
	r, err := build(item, vars)
	if err != nil {
		return "", err
	}
	switch format {
	case FormatCurl:
		return renderCurl(r), nil
	case FormatShell:
		return renderShellScript(r), nil
	case FormatPowerShell:
		return renderPowerShell(r), nil
	case FormatPowerShellScript:
		return renderPowerShellScript(r), nil
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

	r := request{method: method, url: u.String()}
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

// renderCurl is the one-liner: everything inline, quoted for sh.
func renderCurl(r request) string {
	groups := [][]string{{"curl", "-X", r.method, shQuote(r.url)}}
	groups = append(groups, curlHeaderArgs(r)...)
	groups = append(groups, curlBodyArgs(r.body)...)

	parts := make([]string, len(groups))
	for i, g := range groups {
		parts[i] = strings.Join(g, " ")
	}
	return strings.Join(parts, " ")
}

// curlBodyArgs renders the body as inline curl argument groups.
func curlBodyArgs(b reqBody) [][]string {
	switch b.kind {
	case bodyRaw:
		return [][]string{{"--data-raw", shQuote(b.raw)}}
	case bodyURLEncoded:
		out := make([][]string, 0, len(b.pairs))
		for _, p := range b.pairs {
			out = append(out, []string{"--data-urlencode", shQuote(p.name + "=" + p.value)})
		}
		return out
	case bodyForm:
		out := make([][]string, 0, len(b.pairs)+len(b.files))
		for _, p := range b.pairs {
			out = append(out, []string{"-F", shQuote(p.name + "=" + p.value)})
		}
		for _, f := range b.files {
			out = append(out, []string{"-F", shQuote(f.name + "=@" + f.value)})
		}
		return out
	case bodyBinary:
		return [][]string{{"--data-binary", shQuote("@" + b.filePath)}}
	}
	return nil
}

// renderShellScript is the same request as something you'd keep in a
// file: the URL, the headers and the body come out as variables first,
// so each is editable on its own instead of buried in one long command.
//
// A raw body goes in a quoted heredoc, which means it needs no escaping
// at all. The one-liner has to turn every apostrophe into '\'' to
// survive sh quoting, and a JSON document full of those is neither
// readable nor editable — which is most of the point of the script form.
func renderShellScript(r request) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n\n")
	b.WriteString("url=" + shQuote(r.url) + "\n")

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

	lines := []string{"curl -X " + r.method + ` "$url"`}
	if len(headerArgs) > 0 {
		// The reference is omitted along with the array: expanding an
		// empty one under `set -u` is an error on bash before 4.4.
		lines = append(lines, `"${headers[@]}"`)
	}
	lines = append(lines, bodyArgs...)
	b.WriteString("\n" + strings.Join(lines, " \\\n  ") + "\n")
	return b.String()
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

// psRequest is what both PowerShell renderers work from: Content-Type is
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

// renderPowerShell is the one-liner: hashtables and body inline.
func renderPowerShell(r request) string {
	ps := splitPowerShell(r)
	params := []string{"-Method " + r.method, "-Uri " + psQuote(r.url)}
	if len(ps.headers) > 0 {
		params = append(params, "-Headers "+psHashtable(ps.headers, nil))
	}
	switch r.body.kind {
	case bodyRaw:
		params = append(params, "-Body "+psQuote(r.body.raw))
	case bodyURLEncoded:
		params = append(params, "-Body "+psQuote(urlEncodedBody(r.body.pairs)))
	case bodyForm:
		params = append(params, "-Form "+psHashtable(r.body.pairs, r.body.files))
	case bodyBinary:
		params = append(params, "-InFile "+psQuote(r.body.filePath))
	}
	if ps.contentType != "" {
		params = append(params, "-ContentType "+psQuote(ps.contentType))
	}
	return "Invoke-RestMethod " + strings.Join(params, " ")
}

// renderPowerShellScript pulls every part out as a variable — $uri,
// $headers, and $body/$form/$file — so the Invoke-RestMethod call at the
// bottom reads as a list of names. A raw body becomes a single-quoted
// here-string, which is literal: no doubling of every ' the way an
// inline PowerShell string needs.
func renderPowerShellScript(r request) string {
	ps := splitPowerShell(r)
	var b strings.Builder

	b.WriteString("$uri = " + psQuote(r.url) + "\n")

	if len(ps.headers) > 0 {
		b.WriteString("\n$headers = @{\n")
		for _, h := range ps.headers {
			b.WriteString("    " + psQuote(h.name) + " = " + psQuote(h.value) + "\n")
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
	if len(ps.headers) > 0 {
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

// psHashtable renders a PowerShell hashtable literal on one line. files,
// when given, are appended as Get-Item entries — the -Form shape for
// file fields.
func psHashtable(pairs []kv, files []kv) string {
	parts := make([]string, 0, len(pairs)+len(files))
	for _, p := range pairs {
		parts = append(parts, psQuote(p.name)+" = "+psQuote(p.value))
	}
	for _, f := range files {
		parts = append(parts, psQuote(f.name)+" = Get-Item "+psQuote(f.value))
	}
	return "@{ " + strings.Join(parts, "; ") + " }"
}

// psQuote wraps s in a single-quoted PowerShell string, doubling any '.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

