package httpengine

import (
	"bytes"
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
	duration := time.Since(start)

	return &Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    resp.Header,
		Body:       string(bodyBytes),
		Duration:   duration,
		SizeBytes:  len(bodyBytes),
	}, nil
}

// buildBody returns the request body for body's mode, plus the
// Content-Type it implies ("" if the mode doesn't have one — e.g. no body
// at all, or raw with no explicit RawContentType). Execute decides
// whether that Content-Type actually gets applied (form-data always
// wins; the others only fill in a header the request doesn't already
// have — see Execute). A returned io.Reader may also be an io.Closer
// (an open file, for a form-data file field or BodyModeBinary) — Execute
// takes care of closing it.
func buildBody(body *domain.Body, vars map[string]string) (io.Reader, string, error) {
	if body == nil {
		return nil, "", nil
	}
	switch body.Mode {
	case domain.BodyModeRaw:
		if body.Raw == "" {
			return nil, "", nil
		}
		return bytes.NewBufferString(Substitute(body.Raw, vars)), body.RawContentType, nil

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
