package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/goccy/go-yaml"

	"jev-file-sort/internal/classify"
	"jev-file-sort/internal/config"
	"jev-file-sort/internal/execute"
	"jev-file-sort/internal/plan"
)

const usage = `jev-sort sorts files with deterministic rules or Jev classification.

Usage:
  jev-sort config check [--config PATH] [--json]
  jev-sort plan [PATH] --out PLAN.json [options]
  jev-sort apply PLAN.json [--json]
  jev-sort history [--json] [--history-dir PATH]
  jev-sort undo RUN_ID [--json] [--history-dir PATH]
  jev-sort redo RUN_ID [--json] [--history-dir PATH]
  jev-sort recover RUN_ID [--json] [--history-dir PATH]
  jev-sort help
  jev-sort version

Plan options:
  --config PATH       Load an explicit YAML configuration
  --mode MODE         Classification mode: simple or jev
  --output PATH       Override the output root
  --recursive         Explore subdirectories
  --max-depth N       Explore through depth N
  --include PATTERN   Include a pattern (repeatable)
  --exclude PATTERN   Exclude a pattern (repeatable)
  --collision POLICY  Collision policy: skip or number
  --json              Write the command result as JSON
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
	case "plan":
		return runPlan(args[1:], stdout, stderr)
	case "apply":
		return runApply(args[1:], stdout, stderr)
	case "history":
		return runHistory(args[1:], stdout, stderr)
	case "undo":
		return runMutation("undo", args[1:], stdout, stderr)
	case "redo":
		return runMutation("redo", args[1:], stdout, stderr)
	case "recover":
		return runMutation("recover", args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func runApply(args []string, stdout, stderr io.Writer) int {
	flagArgs, positional, err := normalizeOnePositional(args, map[string]bool{})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	set := flag.NewFlagSet("apply", flag.ContinueOnError)
	set.SetOutput(stderr)
	jsonOutput := set.Bool("json", false, "write JSON result")
	if err := set.Parse(flagArgs); err != nil {
		return 2
	}
	if positional == "" {
		fmt.Fprintln(stderr, "apply requires a plan path")
		return 2
	}
	value, err := plan.Read(positional)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	result, err := execute.Apply(context.Background(), value)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	writeResult(stdout, result, *jsonOutput)
	if result.Status != "completed" {
		return 1
	}
	return 0
}

func runHistory(args []string, stdout, stderr io.Writer) int {
	set := flag.NewFlagSet("history", flag.ContinueOnError)
	set.SetOutput(stderr)
	jsonOutput := set.Bool("json", false, "write JSON result")
	directory := set.String("history-dir", "", "history directory")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if set.NArg() != 0 {
		fmt.Fprintln(stderr, "history does not accept positional arguments")
		return 2
	}
	store, err := execute.NewStore(*directory)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	runs, err := store.List()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(runs); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	for _, run := range runs {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", run.ID, run.Status, run.CreatedAt.Format("2006-01-02 15:04:05Z07:00"), run.Plan.Root)
	}
	return 0
}

func runMutation(command string, args []string, stdout, stderr io.Writer) int {
	flagArgs, id, err := normalizeOnePositional(args, map[string]bool{"--history-dir": true})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	set := flag.NewFlagSet(command, flag.ContinueOnError)
	set.SetOutput(stderr)
	jsonOutput := set.Bool("json", false, "write JSON result")
	directory := set.String("history-dir", "", "history directory")
	if err := set.Parse(flagArgs); err != nil {
		return 2
	}
	if id == "" {
		fmt.Fprintf(stderr, "%s requires a run ID\n", command)
		return 2
	}
	var result execute.Result
	if command == "undo" {
		result, err = execute.Undo(context.Background(), id, *directory)
	} else if command == "redo" {
		result, err = execute.Redo(context.Background(), id, *directory)
	} else {
		result, err = execute.Recover(id, *directory)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	writeResult(stdout, result, *jsonOutput)
	if result.Status != "completed" && result.Status != "undone" {
		return 1
	}
	return 0
}

func writeResult(writer io.Writer, result execute.Result, jsonOutput bool) {
	if jsonOutput {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(result)
		return
	}
	fmt.Fprintf(writer, "Run %s: %s\n", result.RunID, result.Status)
	for _, state := range []string{"completed", "undone", "failed"} {
		if count := result.Counts[state]; count > 0 {
			fmt.Fprintf(writer, "%s: %d\n", state, count)
		}
	}
}

func normalizeOnePositional(args []string, valueFlags map[string]bool) ([]string, string, error) {
	var positional string
	result := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if strings.HasPrefix(argument, "-") {
			result = append(result, argument)
			name := argument
			if equals := strings.IndexByte(argument, '='); equals >= 0 {
				name = argument[:equals]
			}
			if valueFlags[name] && !strings.Contains(argument, "=") {
				if index+1 >= len(args) {
					return nil, "", fmt.Errorf("flag %s requires a value", argument)
				}
				index++
				result = append(result, args[index])
			}
			continue
		}
		if positional != "" {
			return nil, "", fmt.Errorf("accepts only one positional argument")
		}
		positional = argument
	}
	return result, positional, nil
}

type stringList []string

func (s *stringList) String() string         { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error { *s = append(*s, value); return nil }

func runPlan(args []string, stdout, stderr io.Writer) int {
	flagArgs, target, argumentErr := normalizePlanArgs(args)
	if argumentErr != nil {
		fmt.Fprintln(stderr, argumentErr)
		return 2
	}
	set := flag.NewFlagSet("plan", flag.ContinueOnError)
	set.SetOutput(stderr)
	configPath := set.String("config", "", "explicit YAML configuration file")
	mode := set.String("mode", "", "classification mode")
	output := set.String("output", "", "output root")
	recursive := set.Bool("recursive", false, "explore subdirectories")
	maxDepth := set.Int("max-depth", -1, "maximum exploration depth")
	collision := set.String("collision", "", "collision policy")
	out := set.String("out", "", "plan output path")
	jsonOutput := set.Bool("json", false, "write JSON result")
	var includes, excludes stringList
	set.Var(&includes, "include", "include pattern")
	set.Var(&excludes, "exclude", "exclude pattern")
	if err := set.Parse(flagArgs); err != nil {
		return 2
	}
	if *out == "" {
		fmt.Fprintln(stderr, "plan requires --out")
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *mode != "" {
		cfg.Mode = *mode
	}
	if *output != "" {
		cfg.Output.Root = *output
	}
	if *recursive {
		cfg.Scan.Recursive = config.Bool(true)
	}
	if *maxDepth >= 0 {
		cfg.Scan.MaxDepth = config.Int(*maxDepth)
		cfg.Scan.Recursive = config.Bool(true)
	}
	if *collision != "" {
		cfg.Output.Collision = *collision
	}
	if includes != nil {
		cfg.Selection.Include = includes
	}
	if excludes != nil {
		cfg.Selection.Exclude = excludes
	}
	diagnostics := config.Validate(cfg)
	if config.HasErrors(diagnostics) {
		writeDiagnostics(stderr, diagnostics)
		return 2
	}
	if cfg.Mode != "simple" {
		fmt.Fprintln(stderr, "jev mode is not available in this build yet")
		return 2
	}
	builder := plan.Builder{Config: cfg, Classifier: classify.Simple{Config: cfg}}
	value, err := builder.Build(context.Background(), target)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	value.Diagnostics = append(value.Diagnostics, diagnostics...)
	planPath, err := filepath.Abs(*out)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := plan.Write(planPath, value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	summary := plan.Summary{PlanID: value.ID, OutputPath: planPath, Counts: map[string]int{}}
	for _, operation := range value.Operations {
		summary.Counts[operation.Status]++
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(summary); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "Plan %s written to %s\n", summary.PlanID, summary.OutputPath)
		for _, status := range []string{"planned", "unchanged", "skipped", "excluded", "error"} {
			if count := summary.Counts[status]; count > 0 {
				fmt.Fprintf(stdout, "%s: %d\n", status, count)
			}
		}
	}
	if summary.Counts["error"] > 0 {
		return 1
	}
	return 0
}

func normalizePlanArgs(args []string) ([]string, string, error) {
	target := "."
	foundTarget := false
	valueFlags := map[string]bool{"--config": true, "--mode": true, "--output": true, "--max-depth": true, "--collision": true, "--out": true, "--include": true, "--exclude": true}
	result := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if strings.HasPrefix(argument, "-") {
			result = append(result, argument)
			name := argument
			if equals := strings.IndexByte(argument, '='); equals >= 0 {
				name = argument[:equals]
			}
			if valueFlags[name] && !strings.Contains(argument, "=") {
				if index+1 >= len(args) {
					return nil, "", fmt.Errorf("flag %s requires a value", argument)
				}
				index++
				result = append(result, args[index])
			}
			continue
		}
		if foundTarget {
			return nil, "", fmt.Errorf("plan accepts at most one target path")
		}
		target, foundTarget = argument, true
	}
	return result, target, nil
}

func writeDiagnostics(writer io.Writer, diagnostics []config.Diagnostic) {
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(writer, "%s: %s: %s\n", diagnostic.Level, diagnostic.Subject, diagnostic.Message)
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
