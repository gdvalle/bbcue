package importcmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	flag "github.com/spf13/pflag"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	cuejson "cuelang.org/go/encoding/json"
	cuetoml "cuelang.org/go/encoding/toml"
	cueyaml "cuelang.org/go/encoding/yaml"
)

const bbcueFile = "bb.cue"

const usage = `Usage: bbcue import [flags] <files...>

Import data files (JSON, YAML, TOML) as CUE values into bb.cue in the current
directory. Each file becomes a field under the "bbcue" struct, keyed by filename.

Multi-document YAML files are not supported.

If bb.cue already contains an entry for a filename, that file is skipped and
a warning is printed. Other non-conflicting files are still imported.
`

// Run executes the import subcommand with the given arguments.
func Run(args []string) error {
	fs := flag.NewFlagSet("bbcue import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	files := fs.Args()
	if len(files) == 0 {
		fs.Usage()
		return fmt.Errorf("no files specified")
	}

	// Load existing bb.cue to detect conflicts.
	var bbcueVal cue.Value
	bbcueExists := false
	bbcueData, err := os.ReadFile(bbcueFile)
	if err == nil {
		bbcueExists = true
		cctx := cuecontext.New()
		val := cctx.CompileBytes(bbcueData, cue.Filename(bbcueFile))
		if val.Err() == nil {
			bbcueVal = val
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", bbcueFile, err)
	}

	var hasErrors bool
	var imported int
	importedNames := make(map[string]bool)
	for _, file := range files {
		filename := filepath.Base(file)

		// Validate extension.
		format, err := inferImportFormat(filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %s\n", file, err)
			hasErrors = true
			continue
		}

		// Read file.
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %s\n", file, err)
			hasErrors = true
			continue
		}

		// Check for multi-doc YAML.
		if format == "yaml" && isMultiDocYAML(data) {
			fmt.Fprintf(os.Stderr, "skipping %s: multi-document YAML is not supported\n", file)
			hasErrors = true
			continue
		}

		// Check for conflict with existing bb.cue content or earlier imports.
		if hasKey(bbcueVal, filename) || importedNames[filename] {
			fmt.Fprintf(os.Stderr, "skipping %s: %q already exists in %s\n", file, filename, bbcueFile)
			hasErrors = true
			continue
		}

		// Convert to CUE AST expression.
		dataExpr, err := parseData(filename, data, format)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %s\n", file, err)
			hasErrors = true
			continue
		}

		// Build the full CUE entry as an AST and format it.
		entry, err := formatEntry(filename, dataExpr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %s\n", file, err)
			hasErrors = true
			continue
		}

		// Append to bb.cue.
		if err := appendToBBCue(entry, bbcueExists); err != nil {
			return fmt.Errorf("writing %s: %w", bbcueFile, err)
		}
		bbcueExists = true
		importedNames[filename] = true
		imported++

		fmt.Fprintf(os.Stderr, "imported %s\n", file)
	}

	if imported == 0 && hasErrors {
		return fmt.Errorf("no files were imported")
	}

	if hasErrors {
		return fmt.Errorf("some files were skipped due to errors")
	}
	return nil
}

// hasKey checks if a filename already exists as a key under the "bbcue" struct
// in the compiled CUE value. Uses LookupPath with cue.Str for safe path lookup
// regardless of dots, quotes, or special characters in filenames.
func hasKey(val cue.Value, filename string) bool {
	if !val.Exists() {
		return false
	}
	return val.LookupPath(cue.MakePath(cue.Str("bbcue"), cue.Str(filename))).Exists()
}

// inferImportFormat returns the format string for a data file to import.
func inferImportFormat(filename string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".json":
		return "json", nil
	case ".yaml", ".yml":
		return "yaml", nil
	case ".toml":
		return "toml", nil
	default:
		return "", fmt.Errorf("unsupported file extension %q", ext)
	}
}

// isMultiDocYAML checks if YAML data contains multiple documents.
func isMultiDocYAML(data []byte) bool {
	// A document separator is "---" on its own line (after the first document).
	// The first "---" at the start of the file is optional and indicates
	// the start of the first document, not a second document.
	trimmed := data
	// Skip leading "---\n" if present (start-of-document marker).
	if bytes.HasPrefix(trimmed, []byte("---\n")) {
		trimmed = trimmed[4:]
	} else if bytes.HasPrefix(trimmed, []byte("---\r\n")) {
		trimmed = trimmed[5:]
	}
	// Look for another document separator.
	return bytes.Contains(trimmed, []byte("\n---\n")) ||
		bytes.Contains(trimmed, []byte("\n---\r\n")) ||
		bytes.Equal(trimmed, []byte("---\n")) ||
		bytes.Equal(trimmed, []byte("---\r\n"))
}

// parseData converts data bytes into a CUE AST expression.
func parseData(filename string, data []byte, dataFormat string) (ast.Expr, error) {
	switch dataFormat {
	case "json":
		expr, err := cuejson.Extract(filename, data)
		if err != nil {
			return nil, fmt.Errorf("parsing JSON: %w", err)
		}
		return expr, nil

	case "yaml":
		file, err := cueyaml.Extract(filename, data)
		if err != nil {
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
		return fileToExpr(file), nil

	case "toml":
		dec := cuetoml.NewDecoder(filename, bytes.NewReader(data))
		expr, err := dec.Decode()
		if err != nil {
			return nil, fmt.Errorf("parsing TOML: %w", err)
		}
		return expr, nil

	default:
		return nil, fmt.Errorf("unsupported format: %q", dataFormat)
	}
}

// fileToExpr converts an *ast.File (from yaml.Extract) back to an ast.Expr.
func fileToExpr(f *ast.File) ast.Expr {
	if len(f.Decls) == 0 {
		return &ast.StructLit{}
	}
	// If there's a single EmbedDecl, return its expression (handles scalars, lists).
	if len(f.Decls) == 1 {
		if embed, ok := f.Decls[0].(*ast.EmbedDecl); ok {
			return embed.Expr
		}
	}
	// Otherwise, it's struct fields — wrap in a StructLit.
	elts := make([]ast.Decl, len(f.Decls))
	copy(elts, f.Decls)
	return &ast.StructLit{Elts: elts}
}

// setNewlines ensures struct fields are formatted on separate lines by setting
// the first field's relative position to Newline. This makes the CUE formatter
// produce multi-line output with braces on their own lines.
func setNewlines(s *ast.StructLit) {
	for i, elt := range s.Elts {
		if f, ok := elt.(*ast.Field); ok {
			if i == 0 {
				ast.SetRelPos(f, token.Newline)
			}
			if inner, ok := f.Value.(*ast.StructLit); ok {
				setNewlines(inner)
			}
		}
	}
}

// formatEntry builds a complete CUE source snippet for a bbcue entry by
// constructing the AST and formatting it with the CUE formatter.
func formatEntry(filename string, dataExpr ast.Expr) (string, error) {
	if s, ok := dataExpr.(*ast.StructLit); ok {
		setNewlines(s)
	}

	file := &ast.File{
		Decls: []ast.Decl{
			&ast.Field{
				Label: ast.NewIdent("bbcue"),
				Value: ast.NewStruct(
					ast.NewStringLabel(filename), ast.NewStruct(
						ast.NewIdent("content"), dataExpr,
					),
				),
			},
		},
	}

	b, err := format.Node(file, format.Simplify())
	if err != nil {
		return "", fmt.Errorf("formatting CUE: %w", err)
	}
	return string(b), nil
}

// appendToBBCue appends a CUE entry to bb.cue. Creates the file if it doesn't exist.
func appendToBBCue(entry string, exists bool) error {
	if !exists {
		return os.WriteFile(bbcueFile, []byte(entry), 0o644)
	}

	// Read existing content to check if it ends with a newline.
	existing, err := os.ReadFile(bbcueFile)
	if err != nil {
		return err
	}
	prefix := ""
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		prefix = "\n"
	}

	f, err := os.OpenFile(bbcueFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(prefix + entry)
	return err
}
