// Package settings holds the app-wide defaults: every setting on a
// request's Options tab, which a new request starts from, plus how big a
// response may get before it stops being shown inline or read at all.
//
// Persisted as settings.yaml in the workspace rather than the app-data
// directory: they describe how this collection of requests should be
// sent, so a workspace shared through git carries them with it. Unlike
// theme.yaml and headers.yaml, these are edited in the app — the file
// is just where the answer lives.
package settings

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const filename = "settings.yaml"

// Settings is settings.yaml's shape. Every field has a floor and a
// ceiling in Clamp, because a request that can never finish and a
// response cap of zero are both ways to make the app look broken.
type Settings struct {
	// RequestTimeoutMs is how long a request may take when it doesn't
	// set its own. 0 means no deadline at all.
	RequestTimeoutMs int `yaml:"requestTimeoutMs" json:"requestTimeoutMs"`
	// MaxRedirects is the cap a request inherits when it doesn't set its
	// own. 0 means no cap.
	MaxRedirects int `yaml:"maxRedirects" json:"maxRedirects"`
	// InlineResponseBytes is the size above which a response body is
	// written to the cache and offered as a file instead of shown.
	InlineResponseBytes int64 `yaml:"inlineResponseBytes" json:"inlineResponseBytes"`
	// MaxResponseBytes is the size above which a response body stops
	// being read at all, and comes back truncated.
	MaxResponseBytes int64 `yaml:"maxResponseBytes" json:"maxResponseBytes"`

	// The rest of the Options tab, as a new request starts it. Unlike the
	// two above — which the engine also falls back to for a request that
	// sets nothing — these are only a starting point: a saved request
	// carries its own answer, so changing one here doesn't reach back and
	// change requests already written.
	//
	// Named as the options are (see domain.Options) so the two read as
	// the same list rather than two vocabularies for one thing.
	FollowRedirects bool `yaml:"followRedirects" json:"followRedirects"`
	StoreCookies    bool `yaml:"storeCookies" json:"storeCookies"`
	SkipTLSVerify   bool `yaml:"skipTlsVerify" json:"skipTlsVerify"`
	// The certificate paths take {{variables}}, like the request's own —
	// which CA to verify against usually belongs to the environment, so
	// naming one here is naming the same variable for every new request.
	CACertFile        string `yaml:"caCertFile" json:"caCertFile"`
	UseCustomCA       bool   `yaml:"useCustomCA" json:"useCustomCA"`
	ClientCertFile    string `yaml:"clientCertFile" json:"clientCertFile"`
	ClientCertKeyFile string `yaml:"clientCertKeyFile" json:"clientCertKeyFile"`
}

// Defaults are what Freeman shipped with as constants.
func Defaults() Settings {
	return Settings{
		RequestTimeoutMs:    30_000,
		MaxRedirects:        10,
		InlineResponseBytes: 1 << 20,  // 1 MiB
		MaxResponseBytes:    64 << 20, // 64 MiB
		// The two that are on unless you say otherwise, matching every
		// other HTTP client. The rest are off/empty, which is their zero
		// value — nothing to state.
		FollowRedirects: true,
		StoreCookies:    true,
	}
}

// Clamp brings a value back into a range the app can work in, so a
// hand-edited settings.yaml or a bad control-API call can't wedge it.
// A negative timeout or cap means the same as zero — no limit — rather
// than an error nobody would see.
func Clamp(s Settings) Settings {
	d := Defaults()
	if s.RequestTimeoutMs < 0 {
		s.RequestTimeoutMs = 0
	}
	if s.MaxRedirects < 0 {
		s.MaxRedirects = 0
	}
	if s.InlineResponseBytes <= 0 {
		s.InlineResponseBytes = d.InlineResponseBytes
	}
	if s.MaxResponseBytes <= 0 {
		s.MaxResponseBytes = d.MaxResponseBytes
	}
	// Reading less than is shown inline would truncate a body the pane
	// was about to render whole.
	if s.MaxResponseBytes < s.InlineResponseBytes {
		s.MaxResponseBytes = s.InlineResponseBytes
	}
	return s
}

// Load returns the workspace's settings, falling back to Defaults when
// the file is missing or unreadable — a broken settings.yaml should not
// stop a workspace from opening.
func Load(dir string) Settings {
	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		return Defaults()
	}
	// Start from the defaults so a file listing one key keeps the rest.
	s := Defaults()
	if err := yaml.Unmarshal(data, &s); err != nil {
		return Defaults()
	}
	return Clamp(s)
}

// Save persists s to dir as YAML.
func Save(dir string, s Settings) error {
	data, err := yaml.Marshal(Clamp(s))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filename), data, 0o644)
}
