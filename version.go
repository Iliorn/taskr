package main

import (
	"runtime/debug"
	"strings"
)

// Version resolution.
//
// The release workflow bakes the tag in with
// `-ldflags "-X main.appVersion=v1.31.0"`, and that is always authoritative.
// But it is not the only way taskr gets installed: `go install
// github.com/Iliorn/taskr@latest` (which the README recommends) compiles
// without ldflags, and a binary built that way used to report itself as
// "dev" forever. That is not just cosmetic — the update check compares the
// running version against the latest release tag by string, so a "dev"
// binary reported an update available on every single check, and the
// Settings tab showed a version that told the user nothing about what they
// were actually running.
//
// The Go toolchain already knows the answer in both remaining cases and
// stamps it into the binary: a module-proxy install records the module
// version, and a plain `go build` inside the repo records the VCS revision.
// resolveVersion reads whichever of those is present.

// buildVersion is the ldflags slot. Kept separate from appVersion so the
// resolution below has an unmodified input to test against; main.go's
// -X target stays `main.appVersion` for compatibility with every existing
// build script, and init() reconciles the two.
func init() {
	appVersion = resolveVersion(appVersion, readBuildInfo())
}

// readBuildInfo is a var so tests can substitute a synthetic BuildInfo
// instead of depending on how the test binary itself was compiled.
var readBuildInfo = func() *debug.BuildInfo {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return info
}

// devVersion is what appVersion holds when no ldflags version was injected.
const devVersion = "dev"

// resolveVersion picks the most specific version available, in order:
//
//  1. the ldflags value, when the build actually set one (a release);
//  2. the module version recorded by `go install module@version`;
//  3. the VCS revision recorded by `go build` in a git checkout, as
//     "dev+<short sha>" with a "-dirty" suffix for uncommitted changes;
//  4. plain "dev".
//
// Case 3 deliberately keeps the "dev" prefix: it is not a release, and
// isReleaseVersion below keys off exactly that to avoid offering a
// self-update that would replace a locally built binary with a stale tag.
func resolveVersion(ldflags string, info *debug.BuildInfo) string {
	if v := strings.TrimSpace(ldflags); v != "" && v != devVersion {
		return v
	}
	if info == nil {
		return devVersion
	}
	// A module-proxy install records a real semver here. A build from a
	// local directory records "(devel)", which carries no information.
	if v := strings.TrimSpace(info.Main.Version); v != "" && v != "(devel)" {
		return v
	}
	var revision string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if revision == "" {
		return devVersion
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	v := devVersion + "+" + revision
	if dirty {
		v += "-dirty"
	}
	return v
}

// isReleaseVersion reports whether the running binary identifies itself as a
// published release, i.e. something the latest-release tag can meaningfully
// be compared against. Anything derived from a local checkout is not.
func isReleaseVersion(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && v != devVersion && !strings.HasPrefix(v, devVersion+"+")
}

// sameRelease compares a running version against a release tag tolerantly.
// Tags are published as "v1.31.0" but a version can reach us without the
// leading v (a packager's ldflags, a module version normalised elsewhere),
// and a build-metadata suffix after "+" is by semver definition not part of
// version identity. Comparing raw strings made all of those read as "an
// update is available" on every check.
func sameRelease(running, latest string) bool {
	return normalizeVersion(running) == normalizeVersion(latest)
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	return v
}
