package fmtcmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/format"
	"github.com/gdvalle/bbcue/internal/discovery"
	flag "github.com/spf13/pflag"
)

const usage = `Usage: bbcue fmt [flags] [files...]

Format CUE files. With no arguments, discovers and formats all .cue files in
the current directory (and subdirectories if recurse is enabled), similar to
bbcue gen.

Flags:
`

func Run(args []string) error {
	fs := flag.NewFlagSet("bbcue fmt", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
	}

	recurse := fs.BoolP("recurse", "r", false, "Recurse into subdirectories")
	noRecurse := fs.BoolP("no-recurse", "R", false, "Do not recurse into subdirectories (default)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	doRecurse := *recurse && !*noRecurse
	paths := fs.Args()

	if len(paths) == 0 {
		paths = []string{"."}
	}

	var filesToFormat []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}

		if !info.IsDir() {
			filesToFormat = append(filesToFormat, p)
			continue
		}

		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != p && !doRecurse {
					return filepath.SkipDir
				}
				if path != p && discovery.SkipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), ".cue") {
				filesToFormat = append(filesToFormat, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	for _, file := range filesToFormat {
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}

		formatted, err := format.Source(b, format.Simplify())
		if err != nil {
			return fmt.Errorf("formatting %s: %w", file, err)
		}

		if !bytes.Equal(b, formatted) {
			info, err := os.Stat(file)
			if err != nil {
				return err
			}
			if err := os.WriteFile(file, formatted, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}

	return nil
}
