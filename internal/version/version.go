package version

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
)

// cueModule is the module path for the CUE dependency.
const cueModule = "cuelang.org/go"

// Print writes version and build information to w.
// It uses runtime/debug.ReadBuildInfo to extract VCS metadata
// (commit SHA, dirty flag) and build settings (GOOS, GOARCH, etc.)
// that Go embeds automatically when building with module support.
func Print(w io.Writer) error {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return fmt.Errorf("failed to read build info")
	}

	fmt.Fprintf(w, "bbcue version %s\n", mainVersion(bi))
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "%-16s%s\n", "cue library", cueLibVersion(bi))
	fmt.Fprintf(w, "%-16s%s\n", "go", runtime.Version())
	fmt.Fprintf(w, "%-16s%s\n", "GOOS", runtime.GOOS)
	fmt.Fprintf(w, "%-16s%s\n", "GOARCH", runtime.GOARCH)

	// Print VCS and other build settings.
	for _, s := range bi.Settings {
		if s.Value == "" {
			continue
		}
		switch s.Key {
		case "vcs", "vcs.revision", "vcs.time", "vcs.modified":
			fmt.Fprintf(w, "%-16s%s\n", s.Key, s.Value)
		}
	}

	return nil
}

// mainVersion returns the version of the main module (this binary).
// When installed via `go install`, this is the module version.
// When built from a local checkout, it's typically "(devel)".
func mainVersion(bi *debug.BuildInfo) string {
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	// Fall back to VCS info if available.
	var rev, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev != "" {
		v := rev
		if len(v) > 12 {
			v = v[:12]
		}
		if modified == "true" {
			v += "+dirty"
		}
		return v
	}
	return "(devel)"
}

// cueLibVersion returns the version of the cuelang.org/go dependency.
func cueLibVersion(bi *debug.BuildInfo) string {
	for _, dep := range bi.Deps {
		if dep.Path == cueModule {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}
	return "(unknown)"
}
