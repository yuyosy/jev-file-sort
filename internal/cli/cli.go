package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/goccy/go-yaml"

	"jev-file-sort/internal/config"
)

const usage = `jev-sort sorts files with deterministic rules or Jev classification.

Usage:
  jev-sort config check [--config PATH] [--json]
  jev-sort help
  jev-sort version
`

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	switch args[0] {
	case "version", "--version":
		version := "dev"
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
			version = info.Main.Version
		}
		fmt.Fprintln(stdout, version)
		return 0
	case "config":
		if len(args) < 2 || args[1] != "check" {
			fmt.Fprintln(stderr, "usage: jev-sort config check [--config PATH] [--json]")
			return 2
		}
		return runConfigCheck(args[2:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func runConfigCheck(args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("config check", flag.ContinueOnError)
	set.SetOutput(stderr)
	path := set.String("config", "", "explicit YAML configuration file")
	jsonOutput := set.Bool("json", false, "write JSON")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if set.NArg() != 0 {
		fmt.Fprintln(stderr, "config check does not accept positional arguments")
		return 2
	}
	cfg, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	diagnostics := config.Validate(cfg)
	result := struct {
		Valid       bool                `json:"valid" yaml:"valid"`
		Config      config.Config       `json:"config" yaml:"config"`
		Diagnostics []config.Diagnostic `json:"diagnostics" yaml:"diagnostics"`
	}{Valid: !config.HasErrors(diagnostics), Config: cfg, Diagnostics: diagnostics}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(result)
	} else {
		var data []byte
		data, err = yaml.Marshal(result)
		if err == nil {
			_, err = stdout.Write(data)
		}
	}
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !result.Valid {
		return 2
	}
	return 0
}
