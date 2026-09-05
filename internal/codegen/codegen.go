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
	"net/url"
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
		return renderCurl(r, false), nil
	case FormatShell:
		return renderCurl(r, true), nil
	case FormatPowerShell:
		return renderPowerShell(r, false), nil
	case FormatPowerShellScript:
		return renderPowerShell(r, true), nil
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
		return reqBody{kind: bodyBinary, filePath: path}

	default:
		return reqBody{kind: bodyNone}
	}
}

// --- curl --------------------------------------------------------------------

func renderCurl(r request, multiline bool) string {
	// Each element is one logical argument group, kept together on a line
	// in the multi-line form.
	groups := [][]string{{"curl", "-X", r.method, shQuote(r.url)}}
	for _, h := range r.headers {
		groups = append(groups, []string{"-H", shQuote(h.name + ": " + h.value)})
	}
	switch r.body.kind {
	case bodyRaw:
		groups = append(groups, []string{"--data-raw", shQuote(r.body.raw)})
	case bodyURLEncoded:
		for _, p := range r.body.pairs {
			groups = append(groups, []string{"--data-urlencode", shQuote(p.name + "=" + p.value)})
		}
	case bodyForm:
		for _, p := range r.body.pairs {
			groups = append(groups, []string{"-F", shQuote(p.name + "=" + p.value)})
		}
		for _, f := range r.body.files {
			groups = append(groups, []string{"-F", shQuote(f.name + "=@" + f.value)})
		}
	case bodyBinary:
		groups = append(groups, []string{"--data-binary", shQuote("@" + r.body.filePath)})
	}

	lines := make([]string, len(groups))
	for i, g := range groups {
		lines[i] = strings.Join(g, " ")
	}
	if !multiline {
		return strings.Join(lines, " ")
	}
	return "#!/usr/bin/env bash\n" + strings.Join(lines, " \\\n  ")
}

// shQuote wraps s in single quotes for POSIX sh, ending the quote to
// insert an escaped literal quote for every ' in s.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// --- PowerShell ------------------------------------------------------------

func renderPowerShell(r request, script bool) string {
	// Content-Type is passed as -ContentType, never in the -Headers
	// hashtable — Invoke-RestMethod rejects it appearing in both.
	var contentType string
	var headers []kv
	for _, h := range r.headers {
		if strings.EqualFold(h.name, "Content-Type") {
			contentType = h.value
			continue
		}
		headers = append(headers, h)
	}

	params := []string{"-Method " + r.method, "-Uri " + psQuote(r.url)}

	if len(headers) > 0 {
		if script {
			params = append(params, "-Headers $headers")
		} else {
			params = append(params, "-Headers "+psHashtable(headers, " "))
		}
	}

	switch r.body.kind {
	case bodyRaw:
		params = append(params, "-Body "+psQuote(r.body.raw))
	case bodyURLEncoded:
		values := url.Values{}
		for _, p := range r.body.pairs {
			values.Add(p.name, p.value)
		}
		params = append(params, "-Body "+psQuote(values.Encode()))
		if contentType == "" {
			contentType = r.body.contentType
		}
	case bodyForm:
		fields := make([]kv, 0, len(r.body.pairs)+len(r.body.files))
		fields = append(fields, r.body.pairs...)
		var fileExprs []string
		for _, f := range r.body.files {
			fileExprs = append(fileExprs, psQuote(f.name)+" = Get-Item "+psQuote(f.value))
		}
		if script {
			params = append(params, "-Form $form")
		} else {
			params = append(params, "-Form "+psFormHashtable(fields, fileExprs, " "))
		}
	case bodyBinary:
		params = append(params, "-InFile "+psQuote(r.body.filePath))
	}

	if contentType != "" {
		params = append(params, "-ContentType "+psQuote(contentType))
	}

	if !script {
		return "Invoke-RestMethod " + strings.Join(params, " ")
	}

	var b strings.Builder
	if len(headers) > 0 {
		b.WriteString("$headers = @{\n")
		for _, h := range headers {
			b.WriteString("    " + psQuote(h.name) + " = " + psQuote(h.value) + "\n")
		}
		b.WriteString("}\n\n")
	}
	if r.body.kind == bodyForm {
		b.WriteString("$form = @{\n")
		for _, p := range r.body.pairs {
			b.WriteString("    " + psQuote(p.name) + " = " + psQuote(p.value) + "\n")
		}
		for _, f := range r.body.files {
			b.WriteString("    " + psQuote(f.name) + " = Get-Item " + psQuote(f.value) + "\n")
		}
		b.WriteString("}\n\n")
	}
	b.WriteString("Invoke-RestMethod `\n    " + strings.Join(params, " `\n    "))
	return b.String()
}

func psHashtable(pairs []kv, sep string) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = psQuote(p.name) + " = " + psQuote(p.value)
	}
	return "@{ " + strings.Join(parts, ";"+sep) + " }"
}

func psFormHashtable(text []kv, fileExprs []string, sep string) string {
	parts := make([]string, 0, len(text)+len(fileExprs))
	for _, p := range text {
		parts = append(parts, psQuote(p.name)+" = "+psQuote(p.value))
	}
	parts = append(parts, fileExprs...)
	return "@{ " + strings.Join(parts, ";"+sep) + " }"
}

// psQuote wraps s in a single-quoted PowerShell string, doubling any '.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
