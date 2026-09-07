package codegen

import (
	"strings"
	"testing"

	"freeman/internal/domain"
)

func mustGen(t *testing.T, item domain.Item, vars map[string]string, f Format) string {
	t.Helper()
	out, err := Generate(item, vars, f)
	if err != nil {
		t.Fatalf("Generate(%s): %v", f, err)
	}
	return out
}

func TestGenerateGETWithParamsHeadersAndVars(t *testing.T) {
	item := domain.Item{
		Method:  "GET",
		URL:     "{{base}}/users",
		Params:  []domain.QueryParam{{Key: "active", Value: "true", Enabled: true}, {Key: "skip", Value: "0", Enabled: false}},
		Headers: []domain.Header{{Key: "Accept", Value: "application/json", Enabled: true}},
	}
	vars := map[string]string{"base": "https://api.example.com"}

	bash := mustGen(t, item, vars, FormatBash)
	for _, want := range []string{
		"#!/usr/bin/env bash\nset -euo pipefail\n",
		"url='https://api.example.com/users?active=true'",
		"headers=(\n  -H 'Accept: application/json'\n)",
		// -L because Freeman follows redirects and curl doesn't unless
		// told, --max-time because Freeman gives up after 30s and curl
		// never does, and the jar flags because the request is on the
		// shared cookie jar — see TestGenerateMatchesTransportOptions
		// and TestGenerateMatchesCookieOption.
		"curl -X GET -L \"$url\" \\\n  -b 'cookies.txt' -c 'cookies.txt' --max-time 30 \\\n  \"${headers[@]}\"",
	} {
		if !strings.Contains(bash, want) {
			t.Fatalf("bash script missing %q:\n%s", want, bash)
		}
	}
	if strings.Contains(bash, "skip") {
		t.Fatalf("disabled param leaked into the bash script:\n%s", bash)
	}

	// Compared whole, not by substring: this is the one test that pins
	// the shape of a script end to end. A generated script is code and
	// nothing else, so this is all of it.
	ps := mustGen(t, item, vars, FormatPowerShell)
	want := "$uri = 'https://api.example.com/users?active=true'\n\n" +
		"$headers = @{\n    'Accept' = 'application/json'\n}\n\n" +
		"Invoke-RestMethod `\n    -Method GET `\n    -Uri $uri `\n    -TimeoutSec 30 `\n" +
		"    -SessionVariable session `\n    -Headers $headers\n"
	if ps != want {
		t.Fatalf("powershell script wrong:\ngot:\n%s\nwant:\n%s", ps, want)
	}
}

// TestGenerateScriptsExtractBodyVerbatim is the point of both formats: a
// raw body becomes a heredoc (bash) or a here-string (PowerShell), both
// of which are literal — so a document full of apostrophes reads exactly
// as typed. Inline quoting would render each one as
//
//	'\''    (bash)
//	''      (PowerShell)
//
// and a JSON document full of those is neither readable nor editable.
//
// The examples are indented so gofmt reads them as a code block: a bare
// pair of apostrophes in doc-comment prose gets rewritten to a closing
// quotation mark.
func TestGenerateScriptsExtractBodyVerbatim(t *testing.T) {
	body := "{\n  \"note\": \"Ada's order\",\n  \"tag\": \"it's fine\"\n}"
	item := domain.Item{
		Method:  "POST",
		URL:     "https://api.example.com/orders",
		Headers: []domain.Header{{Key: "Content-Type", Value: "application/json", Enabled: true}},
		Body:    &domain.Body{Mode: domain.BodyModeRaw, Raw: body},
	}

	bash := mustGen(t, item, nil, FormatBash)
	if !strings.Contains(bash, "body=$(cat <<'BODY'\n"+body+"\nBODY\n)") {
		t.Fatalf("bash script should hold the body in a quoted heredoc:\n%s", bash)
	}
	if !strings.Contains(bash, `--data-raw "$body"`) {
		t.Fatalf("bash script should pass the body by variable:\n%s", bash)
	}
	if strings.Contains(bash, `'\''`) {
		t.Fatalf("a heredoc body needs no sh escaping:\n%s", bash)
	}

	ps := mustGen(t, item, nil, FormatPowerShell)
	if !strings.Contains(ps, "$body = @'\n"+body+"\n'@") {
		t.Fatalf("powershell script should hold the body in a here-string:\n%s", ps)
	}
	if !strings.Contains(ps, "-Body $body") {
		t.Fatalf("powershell script should pass the body by variable:\n%s", ps)
	}
	if strings.Contains(ps, "Ada''s") {
		t.Fatalf("a here-string body needs no PowerShell escaping:\n%s", ps)
	}
}

// TestGenerateBinaryCarriesContentType covers a fidelity bug found by
// running the generated script for real: `curl --data-binary` with no
// Content-Type defaults to application/x-www-form-urlencoded, so
// httpbin parsed the uploaded file as a *form field name* instead of a
// body. The extension-derived type mirrors what httpengine sends.
func TestGenerateBinaryCarriesContentType(t *testing.T) {
	known := domain.Item{
		Method: "PUT", URL: "https://api.example.com/blob",
		Body: &domain.Body{Mode: domain.BodyModeBinary, BinaryFilePath: "/tmp/upload.txt"},
	}
	if got := mustGen(t, known, nil, FormatBash); !strings.Contains(got, "-H 'Content-Type: text/plain") {
		t.Fatalf("bash should send the extension's content type:\n%s", got)
	}
	if got := mustGen(t, known, nil, FormatPowerShell); !strings.Contains(got, "-ContentType 'text/plain") {
		t.Fatalf("PS script should send the extension's content type:\n%s", got)
	}

	// Execute sniffs the file when the extension says nothing; this
	// package never opens files, so it falls back to Execute's own last
	// resort instead.
	unknown := domain.Item{
		Method: "PUT", URL: "https://api.example.com/blob",
		Body: &domain.Body{Mode: domain.BodyModeBinary, BinaryFilePath: "/tmp/blob.weirdext"},
	}
	if got := mustGen(t, unknown, nil, FormatBash); !strings.Contains(got, "-H 'Content-Type: application/octet-stream'") {
		t.Fatalf("an unknown extension should fall back to octet-stream:\n%s", got)
	}
}

// TestGenerateRawWithoutContentTypeSuppressesCurlDefault is the other
// half of the same bug: Execute sends a raw body with no Content-Type
// when no header row sets one, but curl invents
// application/x-www-form-urlencoded for --data-raw. `-H 'Header:'` is
// curl's way of dropping a header it would otherwise add.
func TestGenerateRawWithoutContentTypeSuppressesCurlDefault(t *testing.T) {
	bare := domain.Item{
		Method: "POST", URL: "https://api.example.com/x",
		Body: &domain.Body{Mode: domain.BodyModeRaw, Raw: "hello plain text"},
	}
	if got := mustGen(t, bare, nil, FormatBash); !strings.Contains(got, "-H 'Content-Type:'") {
		t.Fatalf("bash should suppress curl's default content type:\n%s", got)
	}

	// With a real Content-Type row there is nothing to suppress.
	typed := domain.Item{
		Method: "POST", URL: "https://api.example.com/x",
		Headers: []domain.Header{{Key: "Content-Type", Value: "text/plain", Enabled: true}},
		Body:    &domain.Body{Mode: domain.BodyModeRaw, Raw: "hello"},
	}
	if got := mustGen(t, typed, nil, FormatBash); strings.Contains(got, "-H 'Content-Type:'") {
		t.Fatalf("nothing to suppress when a content type is set:\n%s", got)
	}

	// Nor is there for the modes that carry their own.
	form := domain.Item{
		Method: "POST", URL: "https://api.example.com/x",
		Body: &domain.Body{Mode: domain.BodyModeForm, FormFields: []domain.FormField{
			{Key: "a", Value: "1", Enabled: true},
		}},
	}
	if got := mustGen(t, form, nil, FormatBash); strings.Contains(got, "Content-Type") {
		t.Fatalf("curl sets multipart's own content type; we must not touch it:\n%s", got)
	}
}

// TestGenerateHeredocDelimiterAvoidsBody guards the one way a heredoc
// can be terminated early: a body containing the delimiter on a line of
// its own.
func TestGenerateHeredocDelimiterAvoidsBody(t *testing.T) {
	item := domain.Item{
		Method: "POST",
		URL:    "https://api.example.com/x",
		Body:   &domain.Body{Mode: domain.BodyModeRaw, Raw: "first\nBODY\nlast"},
	}
	shell := mustGen(t, item, nil, FormatBash)
	if !strings.Contains(shell, "<<'BODY_'\nfirst\nBODY\nlast\nBODY_\n") {
		t.Fatalf("delimiter should have moved out of the body's way:\n%s", shell)
	}
}

// TestGenerateScriptBodyVariants covers the non-raw body modes in the
// script forms: each gets its own named variable rather than being
// spliced into the command.
func TestGenerateScriptBodyVariants(t *testing.T) {
	urlenc := domain.Item{
		Method: "POST", URL: "https://api.example.com/form",
		Body: &domain.Body{Mode: domain.BodyModeURLEncoded, FormFields: []domain.FormField{
			{Key: "a", Value: "1", Enabled: true},
			{Key: "b", Value: "two words", Enabled: true},
		}},
	}
	shell := mustGen(t, urlenc, nil, FormatBash)
	if !strings.Contains(shell, "fields=(\n  --data-urlencode 'a=1'\n  --data-urlencode 'b=two words'\n)") {
		t.Fatalf("urlencoded fields should be a bash array:\n%s", shell)
	}
	if !strings.Contains(shell, `"${fields[@]}"`) {
		t.Fatalf("urlencoded curl call should expand the array:\n%s", shell)
	}
	if ps := mustGen(t, urlenc, nil, FormatPowerShell); !strings.Contains(ps, "$body = 'a=1&b=two+words'\n") ||
		!strings.Contains(ps, "-ContentType 'application/x-www-form-urlencoded'") {
		t.Fatalf("urlencoded PS script wrong:\n%s", ps)
	}

	form := domain.Item{
		Method: "POST", URL: "https://api.example.com/upload",
		Body: &domain.Body{Mode: domain.BodyModeForm, FormFields: []domain.FormField{
			{Key: "caption", Value: "hi", Enabled: true},
			{Key: "photo", Type: domain.FormFieldTypeFile, FilePath: "/tmp/a.png", Enabled: true},
		}},
	}
	if got := mustGen(t, form, nil, FormatBash); !strings.Contains(got, "form=(\n  -F 'caption=hi'\n  -F 'photo=@/tmp/a.png'\n)") ||
		!strings.Contains(got, `"${form[@]}"`) {
		t.Fatalf("form-data shell script wrong:\n%s", got)
	}

	bin := domain.Item{
		Method: "PUT", URL: "https://api.example.com/blob",
		Body: &domain.Body{Mode: domain.BodyModeBinary, BinaryFilePath: "/tmp/x.bin"},
	}
	if got := mustGen(t, bin, nil, FormatBash); !strings.Contains(got, "file='/tmp/x.bin'") ||
		!strings.Contains(got, `--data-binary "@$file"`) {
		t.Fatalf("binary shell script wrong:\n%s", got)
	}
	if got := mustGen(t, bin, nil, FormatPowerShell); !strings.Contains(got, "$file = '/tmp/x.bin'") ||
		!strings.Contains(got, "-InFile $file") {
		t.Fatalf("binary PS script wrong:\n%s", got)
	}
}

func TestGenerateRawBodyAndBearerAuth(t *testing.T) {
	item := domain.Item{
		Method:  "POST",
		URL:     "https://api.example.com/things",
		Headers: []domain.Header{{Key: "Content-Type", Value: "application/json", Enabled: true}},
		Auth:    &domain.Auth{Type: domain.AuthTypeBearer, Token: "{{tok}}"},
		Body:    &domain.Body{Mode: domain.BodyModeRaw, Raw: `{"name":"Ada's toy"}`},
	}
	vars := map[string]string{"tok": "secret123"}

	bash := mustGen(t, item, vars, FormatBash)
	if !strings.Contains(bash, "-H 'Authorization: Bearer secret123'") {
		t.Fatalf("bearer token not applied:\n%s", bash)
	}
	// The heredoc is literal, so the apostrophe survives unescaped.
	if !strings.Contains(bash, "body=$(cat <<'BODY'\n"+`{"name":"Ada's toy"}`+"\nBODY\n)") {
		t.Fatalf("raw body should be a verbatim heredoc:\n%s", bash)
	}

	ps := mustGen(t, item, vars, FormatPowerShell)
	// Content-Type moves to -ContentType, not the $headers hashtable.
	if strings.Contains(ps, "'Content-Type' =") {
		t.Fatalf("Content-Type should not be in the PS $headers hashtable:\n%s", ps)
	}
	if !strings.Contains(ps, "-ContentType 'application/json'") {
		t.Fatalf("Content-Type should be a -ContentType arg:\n%s", ps)
	}
	if !strings.Contains(ps, "$body = @'\n"+`{"name":"Ada's toy"}`+"\n'@") {
		t.Fatalf("raw body should be a verbatim here-string:\n%s", ps)
	}
	if !strings.Contains(ps, "$headers = @{\n    'Authorization' = 'Bearer secret123'\n}") {
		t.Fatalf("bearer header missing from PS:\n%s", ps)
	}
}

func TestGenerateBasicAuthEncodes(t *testing.T) {
	item := domain.Item{
		Method: "GET",
		URL:    "https://api.example.com",
		Auth:   &domain.Auth{Type: domain.AuthTypeBasic, Username: "alice", Password: "hunter2"},
	}
	curl := mustGen(t, item, nil, FormatBash)
	if !strings.Contains(curl, "-H 'Authorization: Basic YWxpY2U6aHVudGVyMg=='") {
		t.Fatalf("basic auth not base64-encoded:\n%s", curl)
	}
}

func TestGenerateURLEncodedBody(t *testing.T) {
	item := domain.Item{
		Method: "POST",
		URL:    "https://api.example.com/form",
		Body: &domain.Body{Mode: domain.BodyModeURLEncoded, FormFields: []domain.FormField{
			{Key: "a", Value: "1", Enabled: true},
			{Key: "b", Value: "two words", Enabled: true},
		}},
	}
	curl := mustGen(t, item, nil, FormatBash)
	if !strings.Contains(curl, "--data-urlencode 'a=1'") || !strings.Contains(curl, "--data-urlencode 'b=two words'") {
		t.Fatalf("urlencoded fields wrong:\n%s", curl)
	}
	if !strings.Contains(curl, "-H 'Content-Type: application/x-www-form-urlencoded'") {
		t.Fatalf("urlencoded Content-Type missing:\n%s", curl)
	}

	ps := mustGen(t, item, nil, FormatPowerShell)
	if !strings.Contains(ps, "$body = 'a=1&b=two+words'") || !strings.Contains(ps, "-Body $body") {
		t.Fatalf("PS urlencoded body wrong:\n%s", ps)
	}
}

func TestGenerateFormDataAndBinary(t *testing.T) {
	form := domain.Item{
		Method: "POST", URL: "https://api.example.com/upload",
		Body: &domain.Body{Mode: domain.BodyModeForm, FormFields: []domain.FormField{
			{Key: "caption", Value: "hi", Enabled: true},
			{Key: "photo", Type: domain.FormFieldTypeFile, FilePath: "/tmp/a.png", Enabled: true},
		}},
	}
	curl := mustGen(t, form, nil, FormatBash)
	if !strings.Contains(curl, "-F 'caption=hi'") || !strings.Contains(curl, "-F 'photo=@/tmp/a.png'") {
		t.Fatalf("form-data curl wrong:\n%s", curl)
	}
	if strings.Contains(curl, "Content-Type: multipart") {
		t.Fatalf("curl must set the multipart Content-Type itself, not us:\n%s", curl)
	}
	psScript := mustGen(t, form, nil, FormatPowerShell)
	if !strings.Contains(psScript, "$form = @{\n    'caption' = 'hi'\n    'photo' = Get-Item '/tmp/a.png'\n}") {
		t.Fatalf("PS -Form block wrong:\n%s", psScript)
	}

	bin := domain.Item{Method: "PUT", URL: "https://api.example.com/blob", Body: &domain.Body{Mode: domain.BodyModeBinary, BinaryFilePath: "/tmp/x.bin"}}
	if got := mustGen(t, bin, nil, FormatBash); !strings.Contains(got, "file='/tmp/x.bin'") ||
		!strings.Contains(got, `--data-binary "@$file"`) {
		t.Fatalf("binary bash wrong:\n%s", got)
	}
	if got := mustGen(t, bin, nil, FormatPowerShell); !strings.Contains(got, "$file = '/tmp/x.bin'") ||
		!strings.Contains(got, "-InFile $file") {
		t.Fatalf("binary PS wrong:\n%s", got)
	}
}

// The generated command has to send what Send sends, and both tools
// disagree with Freeman's defaults in opposite directions: curl doesn't
// follow redirects at all, Invoke-RestMethod follows up to five.
func TestGenerateMatchesRedirectOptions(t *testing.T) {
	item := domain.Item{Method: "GET", URL: "https://api.example.com/thing"}

	// Unset options mean the defaults, which follow.
	if got := mustGen(t, item, nil, FormatBash); !strings.Contains(got, "curl -X GET -L ") {
		t.Fatalf("a request that follows should generate -L:\n%s", got)
	}

	item.Options = &domain.Options{FollowRedirects: false, StoreCookies: true}
	if got := mustGen(t, item, nil, FormatBash); strings.Contains(got, " -L ") {
		t.Fatalf("a request that doesn't follow should not generate -L:\n%s", got)
	}
	if got := mustGen(t, item, nil, FormatPowerShell); !strings.Contains(got, "-MaximumRedirection 0") {
		t.Fatalf("PowerShell follows by default, so not following has to be spelled out:\n%s", got)
	}

	item.Options = &domain.Options{FollowRedirects: true, MaxRedirects: 3, StoreCookies: true}
	if got := mustGen(t, item, nil, FormatBash); !strings.Contains(got, "--max-redirs 3") {
		t.Fatalf("a redirect cap should carry into curl:\n%s", got)
	}
	if got := mustGen(t, item, nil, FormatPowerShell); !strings.Contains(got, "-MaximumRedirection 3") {
		t.Fatalf("a redirect cap should carry into PowerShell:\n%s", got)
	}
}

// Timeout and the TLS pair are the rest of what Options can say about
// how a request is sent. Each has a direct equivalent in both tools, so
// leaving any of them out would generate a script that reaches a
// different server, or waits forever where Send would give up.
func TestGenerateMatchesTransportOptions(t *testing.T) {
	item := domain.Item{Method: "GET", URL: "https://api.example.com/thing"}

	// Freeman's own 30s applies even with nothing set, and neither tool
	// would impose it, so it's always spelled out.
	if got := mustGen(t, item, nil, FormatBash); !strings.Contains(got, "--max-time 30") {
		t.Fatalf("the app-wide timeout should carry into curl:\n%s", got)
	}
	if got := mustGen(t, item, nil, FormatPowerShell); !strings.Contains(got, "-TimeoutSec 30") {
		t.Fatalf("the app-wide timeout should carry into PowerShell:\n%s", got)
	}

	item.Options = &domain.Options{FollowRedirects: true, StoreCookies: true, TimeoutMs: 2500}
	if got := mustGen(t, item, nil, FormatBash); !strings.Contains(got, "--max-time 2.5") {
		t.Fatalf("curl takes fractional seconds, so 2500ms should stay 2.5:\n%s", got)
	}
	// -TimeoutSec is whole seconds and 0 means forever, so it rounds up.
	if got := mustGen(t, item, nil, FormatPowerShell); !strings.Contains(got, "-TimeoutSec 3") {
		t.Fatalf("a sub-second-precision timeout should round up, not vanish:\n%s", got)
	}
	item.Options.TimeoutMs = 200
	if got := mustGen(t, item, nil, FormatPowerShell); !strings.Contains(got, "-TimeoutSec 1") {
		t.Fatalf("a sub-second timeout must not round down to no timeout:\n%s", got)
	}

	item.Options = &domain.Options{FollowRedirects: true, StoreCookies: true, SkipTLSVerify: true}
	if got := mustGen(t, item, nil, FormatBash); !strings.Contains(got, "--insecure") {
		t.Fatalf("skipping the certificate check should carry into curl:\n%s", got)
	}
	if got := mustGen(t, item, nil, FormatPowerShell); !strings.Contains(got, "-SkipCertificateCheck") {
		t.Fatalf("skipping the certificate check should carry into PowerShell:\n%s", got)
	}

	item.Options = &domain.Options{
		FollowRedirects:   true,
		StoreCookies:      true,
		ClientCertFile:    "/certs/client.pem",
		ClientCertKeyFile: "/certs/client.key",
	}
	got := mustGen(t, item, nil, FormatBash)
	for _, want := range []string{"cert='/certs/client.pem'", "key='/certs/client.key'", `--cert "$cert" --key "$key"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("bash client cert missing %q:\n%s", want, got)
		}
	}
	got = mustGen(t, item, nil, FormatPowerShell)
	for _, want := range []string{
		"::CreateFromPemFile('/certs/client.pem', '/certs/client.key')",
		"$cert.Export('Pkcs12')",
		"-Certificate $cert",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("PowerShell client cert missing %q:\n%s", want, got)
		}
	}

	// Half a pair is no pair — the same rule Execute applies before it
	// loads them, so a script can't present a certificate Send wouldn't.
	item.Options.ClientCertKeyFile = ""
	if got := mustGen(t, item, nil, FormatBash); strings.Contains(got, "--cert") {
		t.Fatalf("a certificate without its key should be ignored:\n%s", got)
	}
	if got := mustGen(t, item, nil, FormatPowerShell); strings.Contains(got, "-Certificate") {
		t.Fatalf("a certificate without its key should be ignored:\n%s", got)
	}
}

// The cookie jar is the option that used to be dropped in silence: two
// requests differing only in it generated identical scripts, and the
// one meant to be on the jar quietly sent nothing. Every format now
// either honours it or says in a comment that it can't.
func TestGenerateMatchesCookieOption(t *testing.T) {
	item := domain.Item{Method: "GET", URL: "https://api.example.com/thing"}

	// fetch is absent: it has no cookie jar of any kind, so nothing is
	// emitted for it either way.
	on := map[Format][]string{
		FormatBash:       {"-b 'cookies.txt' -c 'cookies.txt'"},
		FormatPowerShell: {"-SessionVariable session"},
		FormatPython:     {"MozillaCookieJar('cookies-python.txt')", "session.cookies.save("},
	}
	for format, wants := range on {
		got := mustGen(t, item, nil, format)
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s on the jar is missing %q:\n%s", format, want, got)
			}
		}
	}

	// Python and bash deliberately use different files: curl stores a
	// session cookie's expiry as 0 and Python as empty, and Python reads
	// a 0 as expired — so one shared file would have Python drop curl's
	// cookies and save the emptied jar back over them.
	if py := mustGen(t, item, nil, FormatPython); strings.Contains(py, "MozillaCookieJar('cookies.txt')") {
		t.Errorf("Python must not share curl's jar file:\n%s", py)
	}

	item.Options = &domain.Options{FollowRedirects: true, StoreCookies: false}
	off := map[Format][]string{
		FormatBash:       {"cookies.txt"},
		FormatPowerShell: {"-SessionVariable"},
		FormatPython:     {"MozillaCookieJar"},
	}
	for format, unwanted := range off {
		got := mustGen(t, item, nil, format)
		for _, bad := range unwanted {
			if strings.Contains(got, bad) {
				t.Errorf("%s off the jar should not mention %q:\n%s", format, bad, got)
			}
		}
	}

	// The point of all of the above: for the three formats that have a
	// jar, the two requests must not produce the same script.
	for _, format := range []Format{FormatBash, FormatPowerShell, FormatPython} {
		withJar := mustGen(t, domain.Item{Method: "GET", URL: "https://api.example.com/thing"}, nil, format)
		withoutJar := mustGen(t, item, nil, format)
		if withJar == withoutJar {
			t.Errorf("%s generates the same script whether or not the request is on the cookie jar", format)
		}
	}
}

// Python goes through requests, which can express every option Freeman
// has — the only one of the four besides curl that can.
func TestGeneratePython(t *testing.T) {
	item := domain.Item{
		Method:  "POST",
		URL:     "https://api.example.com/orders",
		Headers: []domain.Header{{Key: "X-Trace", Value: "abc", Enabled: true}},
		Auth:    &domain.Auth{Type: domain.AuthTypeBearer, Token: "t0ken"},
		Body:    &domain.Body{Mode: domain.BodyModeRaw, Raw: "{\n  \"sku\": \"A-1\"\n}"},
		Options: &domain.Options{
			FollowRedirects:   true,
			MaxRedirects:      3,
			TimeoutMs:         2500,
			SkipTLSVerify:     true,
			ClientCertFile:    "/certs/client.pem",
			ClientCertKeyFile: "/certs/client.key",
		},
	}
	got := mustGen(t, item, nil, FormatPython)
	for _, want := range []string{
		"import requests",
		"url = 'https://api.example.com/orders'",
		"session = requests.Session()",
		"session.max_redirects = 3",
		"'X-Trace': 'abc',",
		"'Authorization': 'Bearer t0ken',",
		// The heredoc's counterpart: a body stays as typed rather than
		// being escaped into one line.
		"body = '''\\\n{\n  \"sku\": \"A-1\"\n}'''",
		"'POST',",
		"data=body.encode()",
		"allow_redirects=True",
		"timeout=2.5",
		"verify=False",
		"cert=('/certs/client.pem', '/certs/client.key')",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("python script missing %q:\n%s", want, got)
		}
	}
}

// fetch is the weakest of the four on transport options: the redirect
// cap, the cookie jar and the client certificate simply have no
// equivalent. What it *can* do still has to be right.
func TestGenerateJavaScriptTransportOptions(t *testing.T) {
	item := domain.Item{
		Method: "GET",
		URL:    "https://api.example.com/thing",
		Options: &domain.Options{
			FollowRedirects:   true,
			MaxRedirects:      3,
			StoreCookies:      true,
			SkipTLSVerify:     true,
			ClientCertFile:    "/certs/client.pem",
			ClientCertKeyFile: "/certs/client.key",
			TimeoutMs:         2500,
		},
	}
	got := mustGen(t, item, nil, FormatJavaScript)
	for _, want := range []string{
		// The certificate check is the one TLS option fetch can honour,
		// and only process-wide — set before the first request, not
		// passed to it.
		"process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0'",
		"AbortSignal.timeout(2500)",
		"redirect: 'follow'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("javascript script missing %q:\n%s", want, got)
		}
	}

	item.Options.FollowRedirects = false
	if got := mustGen(t, item, nil, FormatJavaScript); !strings.Contains(got, "redirect: 'manual'") {
		t.Errorf("not following should generate redirect: 'manual':\n%s", got)
	}
}

// Both new formats have to render every body mode without producing
// something that won't parse — the file paths differ per platform, so
// this asserts on the call rather than running it.
func TestGeneratePythonAndJavaScriptBodyModes(t *testing.T) {
	cases := []struct {
		name       string
		body       domain.Body
		python, js string
	}{
		{
			name:   "urlencoded",
			body:   domain.Body{Mode: domain.BodyModeURLEncoded, FormFields: []domain.FormField{{Key: "a", Value: "1", Enabled: true}}},
			python: "data=form",
			js:     "new URLSearchParams({",
		},
		{
			name: "form-data with a file",
			body: domain.Body{Mode: domain.BodyModeForm, FormFields: []domain.FormField{
				{Key: "caption", Value: "hi", Enabled: true},
				{Key: "blob", Type: domain.FormFieldTypeFile, FilePath: "/tmp/x.bin", Enabled: true},
			}},
			python: "'blob': open('/tmp/x.bin', 'rb'),",
			js:     "body.append('blob', await openAsBlob('/tmp/x.bin'))",
		},
		{
			name:   "binary",
			body:   domain.Body{Mode: domain.BodyModeBinary, BinaryFilePath: "/tmp/x.bin"},
			python: "with open('/tmp/x.bin', 'rb') as f:",
			js:     "await readFile('/tmp/x.bin')",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := domain.Item{Method: "POST", URL: "https://api.example.com/x", Body: &tc.body}
			if got := mustGen(t, item, nil, FormatPython); !strings.Contains(got, tc.python) {
				t.Errorf("python missing %q:\n%s", tc.python, got)
			}
			if got := mustGen(t, item, nil, FormatJavaScript); !strings.Contains(got, tc.js) {
				t.Errorf("javascript missing %q:\n%s", tc.js, got)
			}
		})
	}
}

// A body that would end its own quoting has to fall back rather than
// generate something that won't parse — the same rule the heredoc
// delimiter and the here-string already follow.
func TestGeneratePythonAndJavaScriptEscapeAwkwardBodies(t *testing.T) {
	for _, raw := range []string{"has ''' inside", `back\slash`, "ends with '", "a ${template} and a ` tick"} {
		item := domain.Item{
			Method: "POST",
			URL:    "https://api.example.com/x",
			Body:   &domain.Body{Mode: domain.BodyModeRaw, Raw: raw},
		}
		py := mustGen(t, item, nil, FormatPython)
		if strings.Contains(py, "'''"+raw) {
			t.Errorf("python inlined a body that would close its own quote: %q\n%s", raw, py)
		}
		js := mustGen(t, item, nil, FormatJavaScript)
		if strings.Contains(js, "`"+raw+"`") && strings.ContainsAny(raw, "`\\") {
			t.Errorf("javascript inlined a body that would break its template: %q\n%s", raw, js)
		}
	}
}

// A generated script is code you run, not documentation: whatever a
// language can't express is left out rather than explained. Checked
// against the busiest request in this file, so every renderer's
// optional branches are on at once.
func TestGenerateScriptsCarryNoComments(t *testing.T) {
	item := domain.Item{
		Method:  "POST",
		URL:     "https://api.example.com/orders",
		Headers: []domain.Header{{Key: "X-Trace", Value: "abc", Enabled: true}},
		Auth: &domain.Auth{
			Type:     domain.AuthTypeOAuth2,
			TokenURL: "https://auth.example.com/token",
			ClientID: "id",
		},
		Body: &domain.Body{Mode: domain.BodyModeRaw, Raw: `{"sku": "A-1"}`},
		Options: &domain.Options{
			FollowRedirects:   true,
			MaxRedirects:      3,
			StoreCookies:      true,
			TimeoutMs:         2500,
			SkipTLSVerify:     true,
			ClientCertFile:    "/certs/client.pem",
			ClientCertKeyFile: "/certs/client.key",
		},
	}
	// The shebang is not a comment — it's what makes the file runnable.
	prefixes := map[Format][]string{
		FormatBash:       {"#"},
		FormatPowerShell: {"#"},
		FormatPython:     {"#"},
		FormatJavaScript: {"//", "/*"},
	}
	for format, marks := range prefixes {
		got := mustGen(t, item, nil, format)
		for i, line := range strings.Split(got, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#!") {
				continue
			}
			for _, mark := range marks {
				if strings.HasPrefix(trimmed, mark) {
					t.Errorf("%s line %d is a comment: %q\n%s", format, i+1, trimmed, got)
				}
			}
		}
	}
}

func TestGenerateUnknownFormat(t *testing.T) {
	if _, err := Generate(domain.Item{URL: "https://x"}, nil, Format("java")); err == nil {
		t.Fatal("expected an error for an unknown format")
	}
}

// A token can't be baked into a script — it expires, and the one Freeman
// holds is its own — so the script fetches its own rather than going out
// unauthenticated.
func TestGenerateOAuth2FetchesItsOwnToken(t *testing.T) {
	item := domain.Item{
		Method: "GET", URL: "https://api.example.com/thing",
		Auth: &domain.Auth{
			Type:         domain.AuthTypeOAuth2,
			TokenURL:     "https://auth.example.com/token",
			ClientID:     "abc",
			ClientSecret: "s3cret",
			Scope:        "read:things",
		},
	}

	bash := mustGen(t, item, nil, FormatBash)
	for _, want := range []string{
		"token=$(curl -sS -X POST 'https://auth.example.com/token'",
		"--data-urlencode 'grant_type=client_credentials'",
		"--data-urlencode 'client_id=abc'",
		"--data-urlencode 'scope=read:things'",
		`-H "Authorization: Bearer $token"`,
	} {
		if !strings.Contains(bash, want) {
			t.Fatalf("bash script missing %q:\n%s", want, bash)
		}
	}
	// The bearer header is the one that must expand, so it can't be
	// single-quoted like the rest.
	if strings.Contains(bash, `'Authorization: Bearer $token'`) {
		t.Fatalf("the bearer header must be double-quoted so $token expands:\n%s", bash)
	}

	ps := mustGen(t, item, nil, FormatPowerShell)
	for _, want := range []string{
		"$token = (Invoke-RestMethod -Method POST -Uri 'https://auth.example.com/token' -Body @{",
		"grant_type = 'client_credentials'",
		"client_id = 'abc'",
		`'Authorization' = "Bearer $token"`,
		"-Headers $headers",
	} {
		if !strings.Contains(ps, want) {
			t.Fatalf("powershell script missing %q:\n%s", want, ps)
		}
	}
}
