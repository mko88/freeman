package httpengine

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andybalholm/brotli"

	"freeman/internal/domain"
)

// DefaultMaxRedirects matches net/http's own limit, so a request that
// says nothing behaves as it always did.
// A var, not a const, so internal/settings can carry the workspace's
// answer — see core.App.applySettings.
var DefaultMaxRedirects = 10

// cookies is the session every request that opts in shares — one jar for
// the app, so logging in on one request authenticates the next. Package
// level alongside RequestTimeout and MaxResponseBytes, which is how this
// package already holds things that outlive a single call; core.App
// clears it when a workspace opens (ResetCookies), since cookies belong
// to whoever you were talking to, not to the app.
var cookies = NewJar()

// ResetCookies empties the shared jar.
func ResetCookies() {
	cookies.Clear()
}

// CookieJar exposes the shared jar so the settings window can list and
// remove what's in it. One jar for the app, so there is nothing to
// address it by.
func CookieJar() *Jar { return cookies }

// optionsOf supplies the defaults for a request that has no Options —
// everything saved before they existed, and anything nobody has touched.
// Following redirects and keeping cookies are both on, which is what
// every other HTTP client does and what the app did before this.
//
// The certificate paths get the same {{var}} substitution as the URL and
// the headers: which certificate to present is a property of the
// environment you're pointed at, so it has to be settable there rather
// than baked into every request.
func optionsOf(item domain.Item, vars map[string]string) domain.Options {
	if item.Options == nil {
		return domain.Options{FollowRedirects: true, StoreCookies: true}
	}
	opts := *item.Options
	opts.ClientCertFile = Substitute(opts.ClientCertFile, vars)
	opts.ClientCertKeyFile = Substitute(opts.ClientCertKeyFile, vars)
	return opts
}

// clientFor builds the client for one request. Transport is left nil so
// every request still shares http.DefaultTransport's connection pool —
// only the per-request policy differs.
//
// No Client.Timeout: Execute applies RequestTimeout as a context
// deadline instead, so the caller's own cancellation still composes with
// it rather than being shadowed.
func clientFor(opts domain.Options) (*http.Client, func(), error) {
	c := &http.Client{}
	release := func() {}

	if opts.TLS() {
		tr, err := transportFor(opts)
		if err != nil {
			return nil, nil, err
		}
		c.Transport = tr
		// This transport serves one request, so its idle connections
		// would otherwise sit open with nothing able to reuse them —
		// only a request with the same TLS settings could, and each one
		// builds its own. Execute has read the body by the time this
		// runs, so there's nothing in flight to cut off.
		release = tr.CloseIdleConnections
	}

	if opts.StoreCookies {
		c.Jar = cookies
	}
	if !opts.FollowRedirects {
		// Hand back the 3xx itself rather than chasing it.
		c.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		return c, release, nil
	}
	max := DefaultMaxRedirects
	if opts.MaxRedirects != nil {
		max = *opts.MaxRedirects
	}
	if max <= 0 {
		// No cap. net/http applies its own only when CheckRedirect is
		// nil, so this has to be an explicit "always follow" — the
		// request's timeout is what ends a redirect loop.
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
		return c, release, nil
	}
	c.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if len(via) >= max {
			return fmt.Errorf("stopped after %d redirects", max)
		}
		return nil
	}
	return c, release, nil
}

// transportFor clones the default transport so a request with its own
// TLS settings still inherits everything else about it — proxy handling
// from the environment, timeouts, HTTP/2.
func transportFor(opts domain.Options) (*http.Transport, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default transport is not an *http.Transport")
	}
	tr := base.Clone()
	cfg := &tls.Config{InsecureSkipVerify: opts.SkipTLSVerify} //nolint:gosec // the point of the option
	if opts.ClientCertFile != "" && opts.ClientCertKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(opts.ClientCertFile, opts.ClientCertKeyFile)
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	tr.TLSClientConfig = cfg
	return tr, nil
}

// Execute builds an HTTP request from item — substituting {{var}} in the
// URL, enabled query params, enabled headers, the Auth helper (bearer/
// basic/api-key → a header), and the body (raw text, form-data — text
// and/or file fields, x-www-form-urlencoded, or a whole file as binary)
// against vars — and runs it, capturing the response.
func Execute(ctx context.Context, item domain.Item, vars map[string]string) (*Response, error) {
	opts := optionsOf(item, vars)

	// A per-request timeout overrides the app-wide default, and 0 still
	// means "no deadline" for either.
	timeout := RequestTimeout
	if opts.TimeoutMs > 0 {
		timeout = time.Duration(opts.TimeoutMs) * time.Millisecond
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	reqURL, err := buildURL(item, vars)
	if err != nil {
		return nil, err
	}

	bodyReader, bodyContentType, err := buildBody(item.Body, vars)
	if err != nil {
		return nil, err
	}
	if closer, ok := bodyReader.(io.Closer); ok {
		// buildBody's binary/file-field paths return an open *os.File.
		// http.Client closes an io.ReadCloser request body itself once
		// the request has actually been sent, but only then — if
		// anything below returns early (NewRequestWithContext failing,
		// say) nothing else ever closes it. This is safe either way:
		// by the time Execute returns, the body has already been fully
		// streamed (or never started), so a second Close is at worst a
		// harmless "already closed" error, which is ignored.
		defer closer.Close()
	}

	method := item.Method
	if method == "" {
		method = http.MethodGet
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}

	for _, h := range item.Headers {
		if h.Enabled {
			req.Header.Set(Substitute(h.Key, vars), Substitute(h.Value, vars))
		}
	}

	if err := applyAuth(req, item.Auth, vars); err != nil {
		return nil, err
	}

	if bodyContentType != "" {
		if item.Body != nil && item.Body.Mode == domain.BodyModeForm {
			// multipart's Content-Type carries a boundary generated for
			// this exact body — an explicit header can never have the
			// right one, so this always wins over it.
			req.Header.Set("Content-Type", bodyContentType)
		} else if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", bodyContentType)
		}
	}

	httpClient, release, err := clientFor(opts)
	if err != nil {
		return nil, err
	}
	defer release()

	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, capped, err := readCapped(resp.Body)
	if err != nil {
		return nil, err
	}
	decoded, decodeCapped := decodeContentEncoding(resp.Header, bodyBytes)
	bodyBytes = decoded
	duration := time.Since(start)

	return &Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    resp.Header,
		Body:       string(bodyBytes),
		Duration:   duration,
		SizeBytes:  len(bodyBytes),
		Capped:     capped || decodeCapped,
	}, nil
}

// readCapped reads at most MaxResponseBytes from r, reporting whether
// there was more to read. It asks for one byte past the ceiling so it
// can tell "exactly at the limit" from "over it" without a second read.
func readCapped(r io.Reader) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxResponseBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > MaxResponseBytes {
		return data[:MaxResponseBytes], true, nil
	}
	return data, false, nil
}

// applyAuth turns item.Auth into a request header, overriding any
// Authorization row that came from item.Headers — the Auth tab is the
// dedicated way to set it. Values go through Substitute so a token or
// password can be an {{environment var}}. A nil Auth or AuthTypeNone is
// a no-op, as is a bearer/apikey with its key field left blank.
// applyAuth can fail, because oauth2 has to go and get a token first —
// and a request whose credentials couldn't be obtained must not be sent
// unauthenticated as if nothing were wrong.
//
// The token call runs on the request's own context, so a per-request
// timeout covers the whole send rather than just the part after it.
func applyAuth(req *http.Request, a *domain.Auth, vars map[string]string) error {
	if a == nil {
		return nil
	}
	switch a.Type {
	case domain.AuthTypeBearer:
		if token := Substitute(a.Token, vars); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	case domain.AuthTypeBasic:
		user := Substitute(a.Username, vars)
		pass := Substitute(a.Password, vars)
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	case domain.AuthTypeAPIKey:
		if name := Substitute(a.Key, vars); name != "" {
			req.Header.Set(name, Substitute(a.Value, vars))
		}
	case domain.AuthTypeOAuth2:
		token, err := oauth2Token(req.Context(), a, vars)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return nil
}

// decodeContentEncoding transparently decodes a still-compressed
// response body. net/http already does this for gzip when it added the
// Accept-Encoding header itself (and then clears Content-Encoding) — this
// covers what's left: deflate, brotli, and gzip a server sent
// unrequested. On success it strips Content-Encoding/Content-Length from
// h so the returned response reflects the decoded bytes, the same way
// net/http's own gzip handling does. A decode failure returns the body
// untouched rather than failing a request that already came back.
func decodeContentEncoding(h http.Header, body []byte) ([]byte, bool) {
	var reader io.Reader
	switch strings.ToLower(strings.TrimSpace(h.Get("Content-Encoding"))) {
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return body, false
		}
		reader = gr
	case "br":
		reader = brotli.NewReader(bytes.NewReader(body))
	case "deflate":
		// Content-Encoding: deflate is meant to be zlib-wrapped, but
		// plenty of servers send raw DEFLATE — try zlib, fall back.
		if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
			reader = zr
		} else {
			reader = flate.NewReader(bytes.NewReader(body))
		}
	default:
		return body, false
	}

	// Capped here and not in Execute's read: this is the side that
	// matters for a compression bomb, where the bytes off the wire were
	// comfortably under the ceiling and only the decoded stream isn't.
	decoded, capped, err := readCapped(reader)
	if err != nil {
		return body, false
	}
	h.Del("Content-Encoding")
	h.Del("Content-Length")
	return decoded, capped
}

// ExtensionFor picks a file extension from a Content-Type header so a
// response body written to disk (see wailsapp's response cache) opens in
// a sensible default application. mime.ExtensionsByType returns several
// candidates in an unspecified order (and nothing at all for some of the
// content types most API responses actually use), so the common cases
// are matched explicitly.
func ExtensionFor(contentType string) string {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch {
	case strings.Contains(mediaType, "json"):
		return ".json"
	case strings.Contains(mediaType, "html"):
		return ".html"
	case strings.Contains(mediaType, "xml"):
		return ".xml"
	case strings.HasPrefix(mediaType, "image/"):
		switch mediaType {
		case "image/jpeg":
			return ".jpg"
		case "image/svg+xml":
			return ".svg"
		case "image/x-icon", "image/vnd.microsoft.icon":
			return ".ico"
		default:
			// image/png -> .png, image/gif -> .gif, image/webp -> .webp
			return "." + strings.TrimPrefix(mediaType, "image/")
		}
	default:
		return ".txt"
	}
}

// buildBody returns the request body for body's mode, plus the
// Content-Type it implies ("" if the mode doesn't have one — e.g. no body
// at all, or raw, which has none: its Content-Type is just a regular
// header, like any other — see Execute). Execute decides whether that
// Content-Type actually gets applied (form-data always wins; the others
// only fill in a header the request doesn't already have). A returned
// io.Reader may also be an io.Closer (an open file, for a form-data file
// field or BodyModeBinary) — Execute takes care of closing it.
func buildBody(body *domain.Body, vars map[string]string) (io.Reader, string, error) {
	if body == nil {
		return nil, "", nil
	}
	switch body.Mode {
	case domain.BodyModeRaw:
		if body.Raw == "" {
			return nil, "", nil
		}
		return bytes.NewBufferString(Substitute(body.Raw, vars)), "", nil

	case domain.BodyModeForm:
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for _, f := range body.FormFields {
			if !f.Enabled {
				continue
			}
			key := Substitute(f.Key, vars)
			if f.Type == domain.FormFieldTypeFile {
				path := Substitute(f.FilePath, vars)
				if path == "" {
					continue
				}
				if err := writeFormFile(w, key, path); err != nil {
					return nil, "", fmt.Errorf("form field %q: %w", f.Key, err)
				}
				continue
			}
			if err := w.WriteField(key, Substitute(f.Value, vars)); err != nil {
				return nil, "", err
			}
		}
		if err := w.Close(); err != nil {
			return nil, "", err
		}
		return &buf, w.FormDataContentType(), nil

	case domain.BodyModeURLEncoded:
		values := url.Values{}
		for _, f := range body.FormFields {
			if f.Enabled {
				values.Add(Substitute(f.Key, vars), Substitute(f.Value, vars))
			}
		}
		if len(values) == 0 {
			return nil, "", nil
		}
		return bytes.NewBufferString(values.Encode()), "application/x-www-form-urlencoded", nil

	case domain.BodyModeBinary:
		path := Substitute(body.BinaryFilePath, vars)
		if path == "" {
			return nil, "", nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, "", err
		}
		contentType, err := detectContentType(f, path)
		if err != nil {
			f.Close()
			return nil, "", err
		}
		return f, contentType, nil

	default:
		return nil, "", nil
	}
}

// writeFormFile streams path into a new multipart file part named
// fieldName, with a Content-Type detected from the file itself (unlike
// multipart.Writer.CreateFormFile, which always hardcodes
// application/octet-stream).
func writeFormFile(w *multipart.Writer, fieldName, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	contentType, err := detectContentType(f, path)
	if err != nil {
		return err
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(
		`form-data; name="%s"; filename="%s"`,
		escapeQuotes(fieldName), escapeQuotes(filepath.Base(path)),
	))
	header.Set("Content-Type", contentType)
	part, err := w.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, f)
	return err
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

// detectContentType prefers the file extension (more reliable for common
// types like .json or .png than content sniffing) and falls back to
// sniffing the first 512 bytes the way net/http itself does, defaulting
// to application/octet-stream if neither yields anything. f is left
// positioned at the start either way, ready to be sent as the body.
func detectContentType(f *os.File, path string) (string, error) {
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct, nil
	}
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return http.DetectContentType(buf[:n]), nil
}

func buildURL(item domain.Item, vars map[string]string) (string, error) {
	u, err := url.Parse(Substitute(item.URL, vars))
	if err != nil {
		return "", err
	}
	if len(item.Params) == 0 {
		return u.String(), nil
	}
	q := u.Query()
	for _, p := range item.Params {
		if p.Enabled {
			q.Add(Substitute(p.Key, vars), Substitute(p.Value, vars))
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
