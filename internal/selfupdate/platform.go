// Package selfupdate implements the bbcue self-update subcommand.
package selfupdate

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// AssetName returns the release asset filename for the given platform.
func AssetName(goos, goarch string) string {
	return fmt.Sprintf("bbcue-%s-%s", goos, goarch)
}

// CurrentAsset returns the asset name for the current platform.
func CurrentAsset() string {
	return AssetName(runtime.GOOS, runtime.GOARCH)
}

// CurrentRevision returns the full VCS revision SHA and dirty state from build
// info. Returns an error if build info or VCS revision is unavailable.
func CurrentRevision() (string, bool, error) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false, fmt.Errorf("no build info available")
	}
	var revision string
	modified := false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return "", false, fmt.Errorf("no VCS revision in build info")
	}
	return revision, modified, nil
}

// ExtractShortSHA extracts the short commit SHA from a release tag name.
// Tags are expected to follow the format: v0.0.0-{YYYYMMDDHHmmss}-{shortsha}
// Returns the last segment of the tag after stripping the leading "v".
func ExtractShortSHA(tagName string) string {
	name := strings.TrimPrefix(tagName, "v")
	parts := strings.Split(name, "-")
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
}

// SameRelease reports whether the current clean VCS revision matches the given
// release tag's commit SHA. Dirty builds are always treated as different from
// a release, even when they share the same underlying revision.
func SameRelease(currentRevision string, currentModified bool, tagName string) bool {
	if currentModified {
		return false
	}
	short := ExtractShortSHA(tagName)
	if short == "" || len(currentRevision) < len(short) {
		return false
	}
	return strings.EqualFold(currentRevision[:len(short)], short)
}
