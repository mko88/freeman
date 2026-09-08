// Package curlimport turns a curl command into a domain.Item, so a
// request copied out of an API's docs or a browser's "Copy as cURL"
// becomes something you can send and edit.
//
// The mirror of internal/codegen's bash renderer, and deliberately
// forgiving where that one is exact: this reads what people actually
// paste — wrapped across lines with backslashes, `$` prompts, smart
// quotes from a web page — rather than only what curl itself would
// accept. Flags it doesn't recognise are skipped instead of failing,
// because a request with one option missing is more use than an error.
package curlimport

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"freeman/internal/domain"
)

// Parse reads a curl command and returns the request it describes.
func Parse(text string) (domain.Item, error) {
	args, err := tokenize(text)
	if err != nil {
		return domain.Item{}, err
	}
	if len(args) == 0 || !strings.EqualFold(args[0], "curl") {
		return domain.Item{}, errors.New("not a curl command: expected it to start with `curl`")
	}
	return build(args[1:])
}

func build(args []string) (domain.Item, error) {
	item := domain.Item{Type: domain.ItemTypeRequest}
	opts := domain.Options{FollowRedirects: false, StoreCookies: true}
	var (
		rawURL      string
		method      string
		dataParts   []string
		dataIsForm  bool
		formFields  []domain.FormField
		optsTouched bool
	)

	// next returns the value that belongs to a flag, whether written as
	// `-H x`, `-Hx` or `--header=x`.
	next := func(i *int, inline string) string {
		if inline != "" {
			return inline
		}
		if *i+1 < len(args) {
			*i++
			return args[*i]
		}
		return ""
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag, inline := splitFlag(arg)

		switch flag {
		case "-X", "--request":
			method = strings.ToUpper(next(&i, inline))
		case "-H", "--header":
			if k, v, ok := splitHeader(next(&i, inline)); ok {
				item.Headers = append(item.Headers, domain.Header{Key: k, Value: v, Enabled: true})
			}
		case "-d", "--data", "--data-raw", "--data-binary", "--data-ascii":
			dataParts = append(dataParts, next(&i, inline))
		case "--data-urlencode":
			dataIsForm = true
			if k, v, ok := strings.Cut(next(&i, inline), "="); ok {
				formFields = append(formFields, domain.FormField{Key: k, Value: v, Type: domain.FormFieldTypeText, Enabled: true})
			}
		case "-F", "--form":
			if f, ok := formField(next(&i, inline)); ok {
				formFields = append(formFields, f)
				dataIsForm = false
			}
		case "-u", "--user":
			user, pass, _ := strings.Cut(next(&i, inline), ":")
			item.Auth = &domain.Auth{Type: domain.AuthTypeBasic, Username: user, Password: pass}
		case "-L", "--location":
			opts.FollowRedirects, optsTouched = true, true
		case "-k", "--insecure":
			opts.SkipTLSVerify, optsTouched = true, true
		case "--max-redirs":
			if n, err := atoi(next(&i, inline)); err == nil {
				capped := n
				if capped < 0 {
					capped = 0 // curl's -1 is unlimited, and so is Freeman's 0
				}
				opts.MaxRedirects, optsTouched = &capped, true
			}
		case "--max-time", "-m":
			if secs, err := atof(next(&i, inline)); err == nil {
				opts.TimeoutMs, optsTouched = int(secs*1000+0.5), true
			}
		case "--cert", "-E":
			opts.ClientCertFile, optsTouched = next(&i, inline), true
		case "--key":
			opts.ClientCertKeyFile, optsTouched = next(&i, inline), true
		case "--url":
			rawURL = next(&i, inline)
		case "-A", "--user-agent":
			item.Headers = append(item.Headers, domain.Header{Key: "User-Agent", Value: next(&i, inline), Enabled: true})
		case "-b", "--cookie":
			// A cookie header, not the jar: --cookie can name a file, and
			// a file is not something a saved request can carry.
			if v := next(&i, inline); v != "" && !strings.Contains(v, "/") && !strings.Contains(v, `\`) {
				item.Headers = append(item.Headers, domain.Header{Key: "Cookie", Value: v, Enabled: true})
			}
		case "-o", "--output", "-c", "--cookie-jar":
			next(&i, inline) // takes a value, means nothing here
		case "":
			// Not a flag: the URL, unless one was already given.
			if rawURL == "" {
				rawURL = arg
			}
		default:
			// An unrecognised flag. Skipping its value too would need a
			// table of which flags take one; leaving it is safe, because
			// a stray value only becomes the URL if none was found.
		}
	}

	if rawURL == "" {
		return domain.Item{}, errors.New("no URL in that curl command")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return domain.Item{}, fmt.Errorf("unusable URL %q: %w", rawURL, err)
	}
	// Query params become rows, so they're editable rather than buried
	// in the URL — the same split the request editor makes.
	for key, values := range u.Query() {
		for _, v := range values {
			item.Params = append(item.Params, domain.QueryParam{Key: key, Value: v, Enabled: true})
		}
	}
	u.RawQuery = ""
	item.URL = u.String()
	item.Name = defaultName(u)

	item.Body = buildBody(dataParts, formFields, dataIsForm)
	item.Method = resolveMethod(method, item.Body)
	if optsTouched {
		item.Options = &opts
	}
	return item, nil
}

func buildBody(dataParts []string, formFields []domain.FormField, urlencoded bool) *domain.Body {
	switch {
	case len(formFields) > 0:
		mode := domain.BodyModeForm
		if urlencoded {
			mode = domain.BodyModeURLEncoded
		}
		return &domain.Body{Mode: mode, FormFields: formFields}
	case len(dataParts) > 0:
		return &domain.Body{Mode: domain.BodyModeRaw, Raw: strings.Join(dataParts, "&")}
	default:
		return nil
	}
}

// resolveMethod applies curl's own rule: -X wins, otherwise a body
// makes it a POST and its absence a GET.
func resolveMethod(explicit string, body *domain.Body) string {
	if explicit != "" {
		return explicit
	}
	if body != nil {
		return "POST"
	}
	return "GET"
}

// defaultName is the last path segment, or the host when there isn't
// one — enough to tell rows apart in the sidebar without making the
// reader name every import.
func defaultName(u *url.URL) string {
	trimmed := strings.Trim(u.Path, "/")
	if trimmed == "" {
		return u.Host
	}
	parts := strings.Split(trimmed, "/")
	return parts[len(parts)-1]
}

func splitFlag(arg string) (flag, inline string) {
	if !strings.HasPrefix(arg, "-") || arg == "-" {
		return "", ""
	}
	if strings.HasPrefix(arg, "--") {
		if name, value, ok := strings.Cut(arg, "="); ok {
			return name, value
		}
		return arg, ""
	}
	// A short flag, possibly with its value stuck to it: -H'X: 1'.
	if len(arg) > 2 {
		return arg[:2], arg[2:]
	}
	return arg, ""
}

func splitHeader(s string) (key, value string, ok bool) {
	k, v, found := strings.Cut(s, ":")
	if !found {
		return "", "", false
	}
	return strings.TrimSpace(k), strings.TrimSpace(v), true
}

// formField reads -F's `name=value`, `name=@path` (a file) and
// `name=<path` (a file's contents as the value — treated the same, since
// both are "this field is that file").
func formField(s string) (domain.FormField, bool) {
	k, v, ok := strings.Cut(s, "=")
	if !ok {
		return domain.FormField{}, false
	}
	f := domain.FormField{Key: k, Type: domain.FormFieldTypeText, Value: v, Enabled: true}
	if path, isFile := strings.CutPrefix(v, "@"); isFile {
		f.Type, f.FilePath, f.Value = domain.FormFieldTypeFile, stripFormOptions(path), ""
	} else if path, isFile := strings.CutPrefix(v, "<"); isFile {
		f.Type, f.FilePath, f.Value = domain.FormFieldTypeFile, stripFormOptions(path), ""
	}
	return f, true
}

// curl allows `;type=image/png` and friends after a file name; the path
// stops at the first one.
func stripFormOptions(path string) string {
	if i := strings.Index(path, ";"); i >= 0 {
		return path[:i]
	}
	return path
}
