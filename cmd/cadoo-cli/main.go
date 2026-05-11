// cadoo-cli is the local command-line tool. Phase 0 ships version and
// config-validate; Phase 6 will add a CLI-driven local review.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version.Version)
	case "config":
		configCmd(os.Args[2:])
	case "review":
		reviewCmd(os.Args[2:])
	case "ci":
		ciCmd(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `cadoo — local CLI

Usage:
  cadoo <command> [flags]

Commands:
  version            Show build version
  config validate    Validate a .cadoo.yaml file
  review <pr-url>    Review a PR (Phase 1)
  ci --mr <url>      One-shot CI-mode review (GitLab; runs describe/review/improve by default)
  help               Show this help
`)
}

func configCmd(args []string) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	path := fs.String("file", ".cadoo.yaml", "path to .cadoo.yaml")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	switch fs.Arg(0) {
	case "validate":
		cfg, err := config.LoadFile(*path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s OK (model=%s)\n", *path, cfg.Model)
	default:
		fmt.Fprintln(os.Stderr, "usage: cadoo config validate [--file PATH]")
		os.Exit(2)
	}
}

func reviewCmd(_ []string) {
	fmt.Fprintln(os.Stderr, "review: not implemented yet (phase 1)")
	os.Exit(64)
}
