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
		"curl -X GET \"$url\" \\\n  \"${headers[@]}\"",
	} {
		if !strings.Contains(bash, want) {
			t.Fatalf("bash script missing %q:\n%s", want, bash)
		}
	}
	if strings.Contains(bash, "skip") {
		t.Fatalf("disabled param leaked into the bash script:\n%s", bash)
	}

	ps := mustGen(t, item, vars, FormatPowerShell)
	want := "$uri = 'https://api.example.com/users?active=true'\n\n" +
		"$headers = @{\n    'Accept' = 'application/json'\n}\n\n" +
		"Invoke-RestMethod `\n    -Method GET `\n    -Uri $uri `\n    -Headers $headers\n"
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

func TestGenerateUnknownFormat(t *testing.T) {
	if _, err := Generate(domain.Item{URL: "https://x"}, nil, Format("java")); err == nil {
		t.Fatal("expected an error for an unknown format")
	}
}
