package httpengine

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"freeman/internal/domain"
)

var client = &http.Client{Timeout: 30 * time.Second}

// Execute builds an HTTP request from item — substituting {{var}} in the
// URL, enabled query params, enabled headers, and a raw body against vars
// — and runs it, capturing the response.
func Execute(ctx context.Context, item domain.Item, vars map[string]string) (*Response, error) {
	reqURL, err := buildURL(item, vars)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if item.Body != nil && item.Body.Mode == domain.BodyModeRaw && item.Body.Raw != "" {
		bodyReader = bytes.NewBufferString(Substitute(item.Body.Raw, vars))
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
	if item.Body != nil && item.Body.Mode == domain.BodyModeRaw && item.Body.RawContentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", item.Body.RawContentType)
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
