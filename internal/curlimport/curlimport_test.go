package curlimport

import (
	"testing"

	"freeman/internal/domain"
)

func mustParse(t *testing.T, text string) domain.Item {
	t.Helper()
	item, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return item
}

// The shape a browser's "Copy as cURL" produces: continuations, single
// quotes, a JSON body, and no -X because the body implies POST.
func TestParseBrowserCopyAsCurl(t *testing.T) {
	item := mustParse(t, `curl 'https://api.example.com/v1/orders?page=2&sort=asc' \
  -H 'Accept: application/json' \
  -H 'Authorization: Bearer t0ken' \
  --data-raw '{"sku":"A-1"}'`)

	if item.Method != "POST" {
		t.Errorf("a body with no -X should be POST, got %q", item.Method)
	}
	if item.URL != "https://api.example.com/v1/orders" {
		t.Errorf("query should be split off the URL, got %q", item.URL)
	}
	if len(item.Params) != 2 {
		t.Errorf("expected page and sort as rows, got %+v", item.Params)
	}
	if len(item.Headers) != 2 {
		t.Fatalf("expected two headers, got %+v", item.Headers)
	}
	if item.Headers[0].Key != "Accept" || item.Headers[0].Value != "application/json" {
		t.Errorf("header not split at the colon: %+v", item.Headers[0])
	}
	if item.Body == nil || item.Body.Mode != domain.BodyModeRaw || item.Body.Raw != `{"sku":"A-1"}` {
		t.Errorf("raw body wrong: %+v", item.Body)
	}
	if item.Name != "orders" {
		t.Errorf("name should come from the last path segment, got %q", item.Name)
	}
}

// The options curl and Freeman both have. -L and -k are booleans; the
// numbers carry units this has to convert.
func TestParseOptions(t *testing.T) {
	item := mustParse(t, `curl -L --max-redirs 3 --max-time 2.5 -k --cert /c.pem --key /k.pem https://x.test/thing`)
	if item.Options == nil {
		t.Fatal("expected options")
	}
	o := item.Options
	if !o.FollowRedirects || o.MaxRedirects == nil || *o.MaxRedirects != 3 {
		t.Errorf("redirects wrong: follow=%v max=%v", o.FollowRedirects, o.MaxRedirects)
	}
	if o.TimeoutMs != 2500 {
		t.Errorf("--max-time is seconds, TimeoutMs is milliseconds: got %d", o.TimeoutMs)
	}
	if !o.SkipTLSVerify || o.ClientCertFile != "/c.pem" || o.ClientCertKeyFile != "/k.pem" {
		t.Errorf("TLS options wrong: %+v", o)
	}
}

// curl's -1 means unlimited, and so does Freeman's 0.
func TestParseUnlimitedRedirects(t *testing.T) {
	item := mustParse(t, `curl -L --max-redirs -1 https://x.test/`)
	if item.Options == nil || item.Options.MaxRedirects == nil || *item.Options.MaxRedirects != 0 {
		t.Fatalf("expected an unlimited cap as 0, got %+v", item.Options)
	}
}

// A request with no options at all must not grow an options block, or
// every import would look customised.
func TestParseLeavesOptionsUnsetWhenNoneAreGiven(t *testing.T) {
	if item := mustParse(t, `curl https://x.test/thing`); item.Options != nil {
		t.Fatalf("expected no options, got %+v", item.Options)
	}
}

func TestParseBodyModes(t *testing.T) {
	form := mustParse(t, `curl -F caption=hi -F blob=@/tmp/x.bin https://x.test/upload`)
	if form.Body == nil || form.Body.Mode != domain.BodyModeForm || len(form.Body.FormFields) != 2 {
		t.Fatalf("-F should be form-data: %+v", form.Body)
	}
	if f := form.Body.FormFields[1]; f.Type != domain.FormFieldTypeFile || f.FilePath != "/tmp/x.bin" {
		t.Errorf("@path should be a file field: %+v", f)
	}

	enc := mustParse(t, `curl --data-urlencode a=1 --data-urlencode b=two https://x.test/f`)
	if enc.Body == nil || enc.Body.Mode != domain.BodyModeURLEncoded || len(enc.Body.FormFields) != 2 {
		t.Fatalf("--data-urlencode should be urlencoded: %+v", enc.Body)
	}
}

func TestParseBasicAuth(t *testing.T) {
	item := mustParse(t, `curl -u alice:s3cret https://x.test/private`)
	if item.Auth == nil || item.Auth.Type != domain.AuthTypeBasic || item.Auth.Username != "alice" || item.Auth.Password != "s3cret" {
		t.Fatalf("-u should become basic auth: %+v", item.Auth)
	}
}

// What people actually paste, rather than what a shell would accept.
func TestParseForgivesPastedText(t *testing.T) {
	cases := map[string]string{
		"a copied prompt":     "$ curl https://x.test/thing",
		"smart quotes":        "curl \u2018https://x.test/thing\u2019",
		"cmd continuation":    "curl ^\n  https://x.test/thing",
		"powershell backtick": "curl `\n  https://x.test/thing",
		"wrapped, no slashes": "curl\n  https://x.test/thing",
		"attached short flag": `curl -H'X-A: 1' https://x.test/thing`,
		"long flag with =":    `curl --header=X-A:1 https://x.test/thing`,
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			item := mustParse(t, text)
			if item.URL != "https://x.test/thing" {
				t.Fatalf("got %q", item.URL)
			}
		})
	}
}

func TestParseRejectsWhatItCannotUse(t *testing.T) {
	for name, text := range map[string]string{
		"not curl":       `wget https://x.test/`,
		"no url":         `curl -X POST -H 'A: b'`,
		"unclosed quote": `curl 'https://x.test/thing`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(text); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
