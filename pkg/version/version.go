// Package version is the single source of truth for the qdesk build version.
//
// The release toolchain overrides Version at link time:
//
//	go build -ldflags="-X github.com/jeffwang/qdesk/pkg/version.Version=v0.1.0"
package version

// Version is the semantic version of this build. "dev" for unreleased
// development builds.
var Version = "dev"

// Commit is the short git commit SHA of this build, when known.
var Commit = ""

// String returns a human-readable build identifier.
func String() string {
	if Commit != "" {
		return Version + "+" + Commit
	}
	return Version
}
