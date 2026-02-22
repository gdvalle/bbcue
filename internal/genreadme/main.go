// Command genreadme generates README.md from README.md.tmpl, inlining
// code blocks extracted from the txtar integration-test fixtures so the
// README examples are always in sync with the tests.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"golang.org/x/tools/txtar"
)

const (
	txtarDir     = "test/testdata/script"
	templateFile = "README.md.tmpl"
	outputFile   = "README.md"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "genreadme: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Pre-parse every txtar archive so template lookups are fast.
	archives := map[string]*txtar.Archive{}
	entries, err := os.ReadDir(txtarDir)
	if err != nil {
		return fmt.Errorf("reading txtar dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txtar") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".txtar")
		a, err := txtar.ParseFile(filepath.Join(txtarDir, e.Name()))
		if err != nil {
			return fmt.Errorf("parsing %s: %w", e.Name(), err)
		}
		archives[name] = a
	}

	funcMap := template.FuncMap{
		// file returns the contents of a named member inside a txtar archive.
		// Usage in template: {{file "yaml_explicit" "bb.cue"}}
		"file": func(archiveName, memberName string) (string, error) {
			a, ok := archives[archiveName]
			if !ok {
				return "", fmt.Errorf("txtar archive %q not found", archiveName)
			}
			for _, f := range a.Files {
				if f.Name == memberName {
					return strings.TrimRight(string(f.Data), "\n"), nil
				}
			}
			return "", fmt.Errorf("member %q not found in archive %q", memberName, archiveName)
		},
	}

	tmpl, err := template.New(filepath.Base(templateFile)).Funcs(funcMap).ParseFiles(templateFile)
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, nil); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	if err := os.WriteFile(outputFile, []byte(buf.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", outputFile, err)
	}

	fmt.Fprintf(os.Stderr, "wrote %s\n", outputFile)
	return nil
}
