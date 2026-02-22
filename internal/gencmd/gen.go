package gencmd

import (
	"fmt"
	"os"

	"github.com/gdvalle/bbcue/internal/generate"
	"github.com/gdvalle/bbcue/internal/version"
	flag "github.com/spf13/pflag"
)

const usage = `Usage: bbcue gen [flags] [paths...]

Discover bb.cue files and write configured outputs. With no paths, checks the
current directory. When paths are given, each file's parent directory or
directory is used as a root.

Use -r to recurse into subdirectories.

This is the default command: "bbcue" is equivalent to "bbcue gen".
`

// Run executes the gen subcommand with the given arguments.
func Run(args []string) error {
	fs := flag.NewFlagSet("bbcue gen", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
	}

	recurse := fs.BoolP("recurse", "r", false, "Recurse into subdirectories")
	noRecurse := fs.BoolP("no-recurse", "R", false, "Do not recurse into subdirectories (default)")
	showVersion := fs.BoolP("version", "v", false, "Print version and build information")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		return version.Print(os.Stdout)
	}

	// --no-recurse takes precedence over --recurse if both are given.
	doRecurse := *recurse && !*noRecurse

	opts := generate.Options{
		Paths:     fs.Args(),
		NoRecurse: !doRecurse,
	}

	return generate.Run(opts)
}
