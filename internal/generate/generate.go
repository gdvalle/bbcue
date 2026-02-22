package generate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"

	"github.com/gdvalle/bbcue/internal/generate/flow"
)

// bbcueSchema is a CUE definition injected into every loaded directory to
// validate the shape of the bbcue output struct. It ensures each output entry
// has a required "content" field and an optional "format" enum, rejecting
// unknown fields early at CUE evaluation time.
const bbcueSchema = `
#bbcueOutput: {
	content: _
	format?: "json" | "yaml" | "toml" | "text"
}
bbcue?: [string]: #bbcueOutput
`

// bbcueSchemaFile is the virtual filename used for the injected schema overlay.
const bbcueSchemaFile = "bbcue_schema_gen.cue"

// outputEntry represents a single output to be written.
type outputEntry struct {
	// resolvedPath is the absolute path where the file will be written.
	resolvedPath string
	// sourceBBCue is the path to the bb.cue file that produced this entry (for error messages).
	sourceBBCue string
	// data is the marshaled content ready to write.
	data []byte
}

// bbDir describes a directory containing a bb.cue file.
type bbDir string

// Options configures the generate run.
type Options struct {
	// Paths are the files or directories to process. If empty, defaults to ".".
	Paths []string
	// NoRecurse disables recursive directory walking.
	NoRecurse bool
}

// Run executes the generate command: discovers bb.cue files, loads and processes
// each one, detects conflicts, and writes output files.
func Run(opts Options) error {
	start := time.Now()

	dirs, err := resolveDirs(opts)
	if err != nil {
		return err
	}

	if len(dirs) == 0 {
		fmt.Fprintf(os.Stderr, "no bb.cue files found\n")
		return nil
	}

	outputs, err := collectOutputs(dirs)
	if err != nil {
		return err
	}

	if err := writeOutputs(outputs); err != nil {
		return err
	}

	elapsed := time.Since(start)
	fmt.Fprintf(os.Stderr, "wrote %d file(s) from %d source(s) in %s\n", len(outputs), len(dirs), elapsed.Round(time.Millisecond))

	return nil
}

func printOutputDetail(entry outputEntry) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	relSource, err := filepath.Rel(cwd, entry.sourceBBCue)
	if err != nil {
		relSource = entry.sourceBBCue
	}
	relSrcDir := filepath.Dir(relSource)
	relDestFile, err := filepath.Rel(cwd, entry.resolvedPath)
	if err != nil {
		relDestFile = entry.resolvedPath
	}

	fmt.Fprintf(os.Stderr, "%s -> %s\n", relSrcDir, relDestFile)
}

// resolveDirs determines which bb.cue directories to process based on options.
func resolveDirs(opts Options) ([]bbDir, error) {
	paths := opts.Paths
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var dirs []bbDir
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("resolving path %q: %w", p, err)
		}

		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", p, err)
		}

		if !info.IsDir() {
			// It's a file; use its parent directory.
			abs = filepath.Dir(abs)
		}

		if opts.NoRecurse {
			// Only check this specific directory.
			if _, err := os.Stat(filepath.Join(abs, "bb.cue")); err == nil {
				dirs = append(dirs, bbDir(abs))
			}
		} else {
			found, err := findBBDirs(abs)
			if err != nil {
				return nil, fmt.Errorf("discovering bb.cue files in %q: %w", p, err)
			}
			dirs = append(dirs, found...)
		}
	}

	return dirs, nil
}

// collectOutputs loads all bb.cue directories and returns the resolved output entries,
// checking for cross-file conflicts.
func collectOutputs(dirs []bbDir) ([]outputEntry, error) {
	cctx := cuecontext.New()
	// seen maps resolved absolute output path → source bb.cue file path.
	seen := make(map[string]string)

	var allErrors []error
	var outputs []outputEntry

	for _, d := range dirs {
		entries, errs := processDir(cctx, d, seen)
		allErrors = append(allErrors, errs...)
		outputs = append(outputs, entries...)
	}

	return outputs, errors.Join(allErrors...)
}

// writeOutputs writes all resolved output entries to disk.
func writeOutputs(outputs []outputEntry) error {
	var errs []error
	for _, entry := range outputs {
		if err := writeEntry(entry); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// writeEntry writes a single output entry to disk, creating directories as needed.
func writeEntry(entry outputEntry) error {
	dir := filepath.Dir(entry.resolvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("%s: creating directory %s: %w", entry.sourceBBCue, dir, err)
	}
	if err := os.WriteFile(entry.resolvedPath, entry.data, 0644); err != nil {
		return fmt.Errorf("%s: writing %s: %w", entry.sourceBBCue, entry.resolvedPath, err)
	}
	printOutputDetail(entry)
	return nil
}

// skipDir reports whether a directory should be skipped during bb.cue discovery.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules" || name == "cue.mod"
}

// findBBDirs walks the directory tree from root and returns all directories
// containing a bb.cue file.
func findBBDirs(root string) ([]bbDir, error) {
	var dirs []bbDir
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() != filepath.Base(root) && skipDir(d.Name()) {
			return filepath.SkipDir
		}
		if _, err := os.Stat(filepath.Join(path, "bb.cue")); err == nil {
			dirs = append(dirs, bbDir(path))
		}
		return nil
	})
	return dirs, err
}

// cueFilesInDir returns the names of all .cue files in dir.
func cueFilesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".cue") {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

// loadCueValue loads all CUE files in a directory and returns the built instance value.
// All .cue files in the directory are loaded together, enabling unification across files.
// Tools mode is always enabled to allow tool imports (tool/file, tool/exec, etc.).
func loadCueValue(cctx *cue.Context, d bbDir) (cue.Value, error) {
	files, err := cueFilesInDir(string(d))
	if err != nil {
		return cue.Value{}, fmt.Errorf("%s: listing .cue files: %w", string(d), err)
	}
	if len(files) == 0 {
		return cue.Value{}, nil
	}

	schemaPath := filepath.Join(string(d), bbcueSchemaFile)
	cfg := &load.Config{
		Dir:   string(d),
		Tools: true,
		Overlay: map[string]load.Source{
			schemaPath: load.FromString(bbcueSchema),
		},
	}
	files = append(files, bbcueSchemaFile)
	insts := load.Instances(files, cfg)
	if len(insts) == 0 {
		return cue.Value{}, fmt.Errorf("%s: no instances loaded", string(d))
	}
	inst := insts[0]
	if inst.Err != nil {
		return cue.Value{}, fmt.Errorf("%s: load error: %w", string(d), inst.Err)
	}

	val := cctx.BuildInstance(inst)
	if val.Err() != nil {
		return cue.Value{}, fmt.Errorf("%s: build error: %w", string(d), val.Err())
	}

	return val, nil
}

// processDir loads CUE files from a directory, runs the flow controller to
// execute tool tasks, extracts bbcue entries, resolves formats, validates
// path safety, checks for conflicts, and returns output entries to write.
func processDir(cctx *cue.Context, d bbDir, seen map[string]string) ([]outputEntry, []error) {
	val, err := loadCueValue(cctx, d)
	if err != nil {
		return nil, []error{err}
	}
	if !val.Exists() {
		return nil, nil
	}

	// Run the flow controller to execute any tool tasks.
	val, err = flow.Run(context.Background(), val, string(d))
	if err != nil {
		return nil, []error{fmt.Errorf("%s: %w", string(d), err)}
	}

	sourceFile := filepath.Join(string(d), "bb.cue")

	outputVal := val.LookupPath(cue.ParsePath("bbcue"))
	if !outputVal.Exists() {
		return nil, nil
	}
	if outputVal.Err() != nil {
		return nil, []error{fmt.Errorf("%s: bbcue error: %w", sourceFile, outputVal.Err())}
	}

	iter, err := outputVal.Fields()
	if err != nil {
		return nil, []error{fmt.Errorf("%s: iterating bbcue: %w", sourceFile, err)}
	}

	var entries []outputEntry
	var errs []error

	for iter.Next() {
		entry, err := processOutputField(sourceFile, string(d), iter, seen)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		entries = append(entries, entry)
	}

	return entries, errs
}

// processOutputField processes a single Output struct field, returning an outputEntry
// ready to write or an error.
func processOutputField(bbCuePath, dir string, iter *cue.Iterator, seen map[string]string) (outputEntry, error) {
	filename := iter.Selector().Unquoted()
	entryVal := iter.Value()

	if err := validateOutputPath(filename, dir); err != nil {
		return outputEntry{}, fmt.Errorf("%s: output %q: %w", bbCuePath, filename, err)
	}

	format, err := resolveFormat(entryVal, filename)
	if err != nil {
		return outputEntry{}, fmt.Errorf("%s: output %q: %w", bbCuePath, filename, err)
	}

	contentVal := entryVal.LookupPath(cue.ParsePath("content"))
	if !contentVal.Exists() {
		return outputEntry{}, fmt.Errorf("%s: output %q: missing content field", bbCuePath, filename)
	}

	data, err := marshalContent(contentVal, format)
	if err != nil {
		return outputEntry{}, fmt.Errorf("%s: output %q: %w", bbCuePath, filename, err)
	}

	resolvedPath := filepath.Clean(filepath.Join(dir, filepath.FromSlash(filename)))

	if existingSource, ok := seen[resolvedPath]; ok {
		return outputEntry{}, fmt.Errorf("%s: output %q: conflict: already written by %s", bbCuePath, filename, existingSource)
	}
	seen[resolvedPath] = bbCuePath

	return outputEntry{
		resolvedPath: resolvedPath,
		sourceBBCue:  bbCuePath,
		data:         data,
	}, nil
}

// resolveFormat determines the output format. If an explicit format field is
// set in the entry value, it is used. Otherwise, the format is inferred from
// the filename extension.
func resolveFormat(entryVal cue.Value, filename string) (string, error) {
	formatVal := entryVal.LookupPath(cue.ParsePath("format"))
	if formatVal.Exists() && formatVal.Err() == nil {
		f, err := formatVal.String()
		if err != nil {
			return "", fmt.Errorf("invalid format value: %w", err)
		}
		return f, nil
	}
	return inferFormat(filename)
}

// validateOutputPath ensures the output path does not escape the bb.cue directory.
func validateOutputPath(filename string, baseDir string) error {
	if filepath.IsAbs(filename) {
		return fmt.Errorf("path %q escapes bb.cue directory: absolute paths are not allowed", filename)
	}

	resolved := filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(filename)))

	rel, err := filepath.Rel(baseDir, resolved)
	if err != nil {
		return fmt.Errorf("path %q escapes bb.cue directory", filename)
	}
	if strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path %q escapes bb.cue directory", filename)
	}

	return nil
}
