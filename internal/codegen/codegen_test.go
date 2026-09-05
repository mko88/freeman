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

	curl := mustGen(t, item, vars, FormatCurl)
	if !strings.HasPrefix(curl, "curl -X GET 'https://api.example.com/users?active=true'") {
		t.Fatalf("curl one-liner wrong:\n%s", curl)
	}
	if strings.Contains(curl, "skip") {
		t.Fatalf("disabled param leaked into curl:\n%s", curl)
	}
	if !strings.Contains(curl, "-H 'Accept: application/json'") {
		t.Fatalf("header missing from curl:\n%s", curl)
	}
	if strings.Contains(curl, "\n") {
		t.Fatalf("curl one-liner should be a single line:\n%s", curl)
	}

	shell := mustGen(t, item, vars, FormatShell)
	if !strings.HasPrefix(shell, "#!/usr/bin/env bash\ncurl -X GET ") {
		t.Fatalf("shell script wrong:\n%s", shell)
	}
	if !strings.Contains(shell, " \\\n  -H 'Accept: application/json'") {
		t.Fatalf("shell script should put the header on its own continued line:\n%s", shell)
	}

	ps := mustGen(t, item, vars, FormatPowerShell)
	if !strings.HasPrefix(ps, "Invoke-RestMethod -Method GET -Uri 'https://api.example.com/users?active=true'") {
		t.Fatalf("powershell one-liner wrong:\n%s", ps)
	}
	if !strings.Contains(ps, "-Headers @{ 'Accept' = 'application/json' }") {
		t.Fatalf("powershell headers wrong:\n%s", ps)
	}

	psScript := mustGen(t, item, vars, FormatPowerShellScript)
	if !strings.HasPrefix(psScript, "$headers = @{\n    'Accept' = 'application/json'\n}\n\nInvoke-RestMethod `\n    -Method GET `\n") {
		t.Fatalf("powershell script wrong:\n%s", psScript)
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

	curl := mustGen(t, item, vars, FormatCurl)
	if !strings.Contains(curl, "-H 'Authorization: Bearer secret123'") {
		t.Fatalf("bearer token not applied:\n%s", curl)
	}
	// The single quote in the JSON must be escaped for sh.
	if !strings.Contains(curl, `--data-raw '{"name":"Ada'\''s toy"}'`) {
		t.Fatalf("raw body not sh-quoted:\n%s", curl)
	}

	ps := mustGen(t, item, vars, FormatPowerShell)
	// Content-Type moves to -ContentType, not the -Headers hashtable.
	if strings.Contains(ps, "-Headers @{ 'Content-Type'") {
		t.Fatalf("Content-Type should not be in the PS -Headers hashtable:\n%s", ps)
	}
	if !strings.Contains(ps, "-ContentType 'application/json'") {
		t.Fatalf("Content-Type should be a -ContentType arg:\n%s", ps)
	}
	if !strings.Contains(ps, "-Body '{\"name\":\"Ada''s toy\"}'") {
		t.Fatalf("raw body not PS-quoted:\n%s", ps)
	}
	if !strings.Contains(ps, "-Headers @{ 'Authorization' = 'Bearer secret123' }") {
		t.Fatalf("bearer header missing from PS:\n%s", ps)
	}
}

func TestGenerateBasicAuthEncodes(t *testing.T) {
	item := domain.Item{
		Method: "GET",
		URL:    "https://api.example.com",
		Auth:   &domain.Auth{Type: domain.AuthTypeBasic, Username: "alice", Password: "hunter2"},
	}
	curl := mustGen(t, item, nil, FormatCurl)
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
	curl := mustGen(t, item, nil, FormatCurl)
	if !strings.Contains(curl, "--data-urlencode 'a=1'") || !strings.Contains(curl, "--data-urlencode 'b=two words'") {
		t.Fatalf("urlencoded fields wrong:\n%s", curl)
	}
	if !strings.Contains(curl, "-H 'Content-Type: application/x-www-form-urlencoded'") {
		t.Fatalf("urlencoded Content-Type missing:\n%s", curl)
	}

	ps := mustGen(t, item, nil, FormatPowerShell)
	if !strings.Contains(ps, "-Body 'a=1&b=two+words'") {
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
	curl := mustGen(t, form, nil, FormatCurl)
	if !strings.Contains(curl, "-F 'caption=hi'") || !strings.Contains(curl, "-F 'photo=@/tmp/a.png'") {
		t.Fatalf("form-data curl wrong:\n%s", curl)
	}
	if strings.Contains(curl, "Content-Type: multipart") {
		t.Fatalf("curl must set the multipart Content-Type itself, not us:\n%s", curl)
	}
	psScript := mustGen(t, form, nil, FormatPowerShellScript)
	if !strings.Contains(psScript, "$form = @{\n    'caption' = 'hi'\n    'photo' = Get-Item '/tmp/a.png'\n}") {
		t.Fatalf("PS -Form block wrong:\n%s", psScript)
	}

	bin := domain.Item{Method: "PUT", URL: "https://api.example.com/blob", Body: &domain.Body{Mode: domain.BodyModeBinary, BinaryFilePath: "/tmp/x.bin"}}
	if got := mustGen(t, bin, nil, FormatCurl); !strings.Contains(got, "--data-binary '@/tmp/x.bin'") {
		t.Fatalf("binary curl wrong:\n%s", got)
	}
	if got := mustGen(t, bin, nil, FormatPowerShell); !strings.Contains(got, "-InFile '/tmp/x.bin'") {
		t.Fatalf("binary PS wrong:\n%s", got)
	}
}

func TestGenerateUnknownFormat(t *testing.T) {
	if _, err := Generate(domain.Item{URL: "https://x"}, nil, Format("java")); err == nil {
		t.Fatal("expected an error for an unknown format")
	}
}
