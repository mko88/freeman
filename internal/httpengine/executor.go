package httpengine

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
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

var client = &http.Client{Timeout: 30 * time.Second}

// Execute builds an HTTP request from item — substituting {{var}} in the
// URL, enabled query params, enabled headers, and the body (raw text,
// form-data — text and/or file fields, x-www-form-urlencoded, or a whole
// file as binary) against vars — and runs it, capturing the response.
func Execute(ctx context.Context, item domain.Item, vars map[string]string) (*Response, error) {
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

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	bodyBytes = decodeContentEncoding(resp.Header, bodyBytes)
	duration := time.Since(start)

	result := &Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    resp.Header,
		Duration:   duration,
		SizeBytes:  len(bodyBytes),
	}
	if len(bodyBytes) > LargeResponseThreshold {
		if path, ferr := WriteResponseBodyFile(bodyBytes, resp.Header.Get("Content-Type")); ferr == nil {
			result.Truncated = true
			result.BodyFile = path
			return result, nil
		}
		// A failed write (e.g. a full disk) isn't a reason to fail a
		// request that already succeeded — fall through and inline it.
	}
	result.Body = string(bodyBytes)
	return result, nil
}

// decodeContentEncoding transparently decodes a still-compressed
// response body. net/http already does this for gzip when it added the
// Accept-Encoding header itself (and then clears Content-Encoding) — this
// covers what's left: deflate, brotli, and gzip a server sent
// unrequested. On success it strips Content-Encoding/Content-Length from
// h so the returned response reflects the decoded bytes, the same way
// net/http's own gzip handling does. A decode failure returns the body
// untouched rather than failing a request that already came back.
func decodeContentEncoding(h http.Header, body []byte) []byte {
	var reader io.Reader
	switch strings.ToLower(strings.TrimSpace(h.Get("Content-Encoding"))) {
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return body
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
		return body
	}

	decoded, err := io.ReadAll(reader)
	if err != nil {
		return body
	}
	h.Del("Content-Encoding")
	h.Del("Content-Length")
	return decoded
}

// WriteResponseBodyFile writes data to a new temp file, named with an
// extension guessed from contentType (see ExtensionFor) so opening it in
// an external editor — see ReadResponseBodyFile's callers — lands on a
// sensible default application instead of "unknown file type". Returns
// its full path. Exported so wailsapp's response cache (see
// wailsapp.loadResponseCache) can materialize the same kind of file for
// a large *reloaded* response, not just a freshly executed one.
func WriteResponseBodyFile(data []byte, contentType string) (string, error) {
	f, err := os.CreateTemp("", "freeman-response-*"+ExtensionFor(contentType))
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// IsResponseBodyFile reports whether path is exactly the directory and
// naming convention WriteResponseBodyFile uses — the check
// ReadResponseBodyFile applies before reading, exported so a caller that
// only needs to open/serve the file (not read its content into memory,
// e.g. wailsapp.App.OpenResponseExternally) can apply the same guard
// without a wasted read.
func IsResponseBodyFile(path string) bool {
	return filepath.Clean(filepath.Dir(path)) == filepath.Clean(os.TempDir()) &&
		strings.HasPrefix(filepath.Base(path), "freeman-response-")
}

// ReadResponseBodyFile reads back a body previously written by
// WriteResponseBodyFile — the desktop app's "show anyway" and the
// headless server's GET /api/execute/body both go through this rather
// than os.ReadFile directly. It refuses anything outside the exact
// directory and naming convention Execute itself writes to (see
// IsResponseBodyFile), so a path from an HTTP query parameter (the
// headless server's case) can't turn this into a generic
// arbitrary-file-read.
func ReadResponseBodyFile(path string) ([]byte, error) {
	if !IsResponseBodyFile(path) {
		return nil, fmt.Errorf("not a response body file: %q", path)
	}
	return os.ReadFile(path)
}

// ExtensionFor picks a file extension from a Content-Type header so a
// response body written to disk opens in a sensible default application.
// mime.ExtensionsByType returns several candidates in an unspecified
// order (and nothing at all for some of the content types most API
// responses actually use), so the common cases are matched explicitly.
// Exported for the same reason as WriteResponseBodyFile.
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
