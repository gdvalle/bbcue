package discovery

import "strings"

// SkipDir reports whether a directory should be skipped during file discovery.
func SkipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules" || name == "cue.mod"
}
