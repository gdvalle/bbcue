package flow

import (
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	cuflow "cuelang.org/go/tools/flow"
)

// runFileRead implements tool/file.Read.
// Mirrors cuelang.org/go/pkg/tool/file.cmdRead.Run.
func runFileRead(t *cuflow.Task, dir string) error {
	v := t.Value()

	filename, err := v.LookupPath(cue.ParsePath("filename")).String()
	if err != nil {
		return fmt.Errorf("file.Read: getting filename: %w", err)
	}

	path := filename
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("file.Read: %w", err)
	}

	// Match CUE's behavior: return string if contents is constrained to string,
	// otherwise return bytes.
	var contents interface{} = b
	if v.LookupPath(cue.ParsePath("contents")).IncompleteKind() == cue.StringKind {
		contents = string(b)
	}

	return t.Fill(map[string]interface{}{"contents": contents})
}

// runFileAppend implements tool/file.Append.
// Mirrors cuelang.org/go/pkg/tool/file.cmdAppend.Run.
func runFileAppend(t *cuflow.Task, dir string) error {
	v := t.Value()

	filename, err := v.LookupPath(cue.ParsePath("filename")).String()
	if err != nil {
		return fmt.Errorf("file.Append: getting filename: %w", err)
	}

	if !filepath.IsAbs(filename) {
		filename = filepath.Join(dir, filename)
	}

	contents, err := fileContentsBytes(v)
	if err != nil {
		return fmt.Errorf("file.Append: getting contents: %w", err)
	}

	mode := filePermissions(v, 0o666)

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("file.Append: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(contents); err != nil {
		return fmt.Errorf("file.Append: %w", err)
	}

	return nil
}

// runFileCreate implements tool/file.Create.
// Mirrors cuelang.org/go/pkg/tool/file.cmdCreate.Run.
func runFileCreate(t *cuflow.Task, dir string) error {
	v := t.Value()

	filename, err := v.LookupPath(cue.ParsePath("filename")).String()
	if err != nil {
		return fmt.Errorf("file.Create: getting filename: %w", err)
	}

	if !filepath.IsAbs(filename) {
		filename = filepath.Join(dir, filename)
	}

	contents, err := fileContentsBytes(v)
	if err != nil {
		return fmt.Errorf("file.Create: getting contents: %w", err)
	}

	mode := filePermissions(v, 0o666)

	if err := os.WriteFile(filename, contents, mode); err != nil {
		return fmt.Errorf("file.Create: %w", err)
	}

	return nil
}

// runFileGlob implements tool/file.Glob.
// Mirrors cuelang.org/go/pkg/tool/file.cmdGlob.Run.
func runFileGlob(t *cuflow.Task, dir string) error {
	v := t.Value()

	glob, err := v.LookupPath(cue.ParsePath("glob")).String()
	if err != nil {
		return fmt.Errorf("file.Glob: getting glob: %w", err)
	}

	if !filepath.IsAbs(glob) {
		glob = filepath.Join(dir, glob)
	}

	matches, err := filepath.Glob(glob)
	if err != nil {
		return fmt.Errorf("file.Glob: %w", err)
	}

	// Convert to forward-slash paths relative to dir, matching CUE's behavior.
	files := make([]string, len(matches))
	for i, m := range matches {
		rel, err := filepath.Rel(dir, m)
		if err != nil {
			rel = m
		}
		files[i] = filepath.ToSlash(rel)
	}

	return t.Fill(map[string]interface{}{"files": files})
}

// runFileMkdir implements tool/file.Mkdir (and tool/file.MkdirAll).
// Mirrors cuelang.org/go/pkg/tool/file.cmdMkdir.Run.
func runFileMkdir(t *cuflow.Task, dir string) error {
	v := t.Value()

	p, err := v.LookupPath(cue.ParsePath("path")).String()
	if err != nil {
		return fmt.Errorf("file.Mkdir: getting path: %w", err)
	}

	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}

	mode := filePermissions(v, 0o777)

	createParents := false
	if cp := v.LookupPath(cue.ParsePath("createParents")); cp.Exists() {
		if b, err := cp.Bool(); err == nil {
			createParents = b
		}
	}

	if createParents {
		if err := os.MkdirAll(p, mode); err != nil {
			return fmt.Errorf("file.Mkdir: %w", err)
		}
	} else {
		// If the directory already exists, it's a no-op.
		if _, err := os.Stat(p); err == nil {
			return nil
		}
		if err := os.Mkdir(p, mode); err != nil {
			return fmt.Errorf("file.Mkdir: %w", err)
		}
	}

	return nil
}

// runFileMkdirTemp implements tool/file.MkdirTemp.
// Mirrors cuelang.org/go/pkg/tool/file.cmdMkdirTemp.Run.
func runFileMkdirTemp(t *cuflow.Task) error {
	v := t.Value()

	dir := ""
	if d := v.LookupPath(cue.ParsePath("dir")); d.Exists() {
		if s, err := d.String(); err == nil {
			dir = s
		}
	}

	pattern := ""
	if p := v.LookupPath(cue.ParsePath("pattern")); p.Exists() {
		if s, err := p.String(); err == nil {
			pattern = s
		}
	}

	path, err := os.MkdirTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("file.MkdirTemp: %w", err)
	}

	return t.Fill(map[string]interface{}{"path": path})
}

// runFileRemoveAll implements tool/file.RemoveAll.
// Mirrors cuelang.org/go/pkg/tool/file.cmdRemoveAll.Run.
func runFileRemoveAll(t *cuflow.Task, dir string) error {
	v := t.Value()

	p, err := v.LookupPath(cue.ParsePath("path")).String()
	if err != nil {
		return fmt.Errorf("file.RemoveAll: getting path: %w", err)
	}

	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}

	if _, err := os.Stat(p); err != nil {
		return t.Fill(map[string]interface{}{"success": false})
	}

	if err := os.RemoveAll(p); err != nil {
		return fmt.Errorf("file.RemoveAll: %w", err)
	}

	return t.Fill(map[string]interface{}{"success": true})
}

// fileContentsBytes reads the "contents" field from a CUE value as []byte.
func fileContentsBytes(v cue.Value) ([]byte, error) {
	cv := v.LookupPath(cue.ParsePath("contents"))
	switch cv.IncompleteKind() {
	case cue.StringKind:
		s, err := cv.String()
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	case cue.BytesKind:
		return cv.Bytes()
	default:
		return cv.Bytes()
	}
}

// filePermissions reads the "permissions" field from a CUE value, falling back
// to the given default.
func filePermissions(v cue.Value, defaultMode os.FileMode) os.FileMode {
	pv := v.LookupPath(cue.ParsePath("permissions"))
	if !pv.Exists() {
		return defaultMode
	}
	n, err := pv.Int64()
	if err != nil {
		return defaultMode
	}
	return os.FileMode(n)
}
