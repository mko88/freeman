package httpengine

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"

	"freeman/internal/domain"
)

var client = &http.Client{Timeout: 30 * time.Second}

// Execute builds an HTTP request from item — substituting {{var}} in the
// URL, enabled query params, enabled headers, and the body (raw text,
// form-data, or x-www-form-urlencoded) against vars — and runs it,
// capturing the response.
func Execute(ctx context.Context, item domain.Item, vars map[string]string) (*Response, error) {
	reqURL, err := buildURL(item, vars)
	if err != nil {
		return nil, err
	}

	bodyReader, bodyContentType, err := buildBody(item.Body, vars)
	if err != nil {
		return nil, err
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
// have — see Execute).
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
			if err := w.WriteField(Substitute(f.Key, vars), Substitute(f.Value, vars)); err != nil {
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

	default:
		return nil, "", nil
	}
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
