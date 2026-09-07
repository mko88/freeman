// Package version reports which build of Freeman is running, for the
// About line in the app and for GET /api/version.
package version

// Set at link time by build.sh (see its ldflags). The defaults are what
// a plain `go build` or `wails build` produces, so a binary built
// without them says "dev" rather than claiming to be a release.
var (
	// Version is `git describe --tags --always --dirty`: "v0.1.0" on a
	// clean tag, "v0.1.0-4-g94b2835" a few commits past one, and
	// "…-dirty" with uncommitted changes.
	Version = "dev"
	// Commit is the short hash. Version usually carries it too, but not
	// when the build sits exactly on a tag — and a bug report wants it
	// either way.
	Commit = ""
	// Date is when the binary was linked, RFC 3339 in UTC.
	Date = ""
)

// Info is what GET /api/version returns and what the frontend renders.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

func Get() Info {
	return Info{Version: Version, Commit: Commit, Date: Date}
}
