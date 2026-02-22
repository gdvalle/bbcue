package main

import (
	"fmt"
	"os"

	"github.com/gdvalle/bbcue/internal/gencmd"
	"github.com/gdvalle/bbcue/internal/importcmd"
	"github.com/gdvalle/bbcue/internal/version"
)

const usage = `Usage: bbcue <command> [flags] [args...]

Commands:
  gen       Discover bb.cue files and write configured outputs (default)
  import    Import data files into bb.cue

Global flags:
  --version, -v    Print version and build information
  --help, -h       Show this help message

Run "bbcue <command> --help" for more information on a command.
`

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		// Default to gen with no arguments.
		if err := gencmd.Run(nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// Check for top-level flags before subcommand dispatch.
	switch args[0] {
	case "--version", "-v":
		if err := version.Print(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case "--help", "-h":
		fmt.Fprint(os.Stderr, usage)
		return
	}

	// Subcommand dispatch.
	var err error
	switch args[0] {
	case "gen":
		err = gencmd.Run(args[1:])
	case "import":
		err = importcmd.Run(args[1:])
	default:
		// If the first arg is not a known subcommand, treat everything
		// as arguments to the default "gen" command.
		err = gencmd.Run(args)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
