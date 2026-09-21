package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/goccy/go-yaml"
	"golang.org/x/term"

	"jev-file-sort/internal/auth"
	"jev-file-sort/internal/classify"
	"jev-file-sort/internal/config"
	"jev-file-sort/internal/execute"
	jevclient "jev-file-sort/internal/jev"
	"jev-file-sort/internal/plan"
	"jev-file-sort/internal/ui"
)

const usage = `jev-sort sorts files with deterministic rules or Jev classification.

Usage:
  jev-sort config check [--config PATH] [--json]
  jev-sort ui [PATH] [--config PATH]
  jev-sort plan [PATH] --out PLAN.json [options]
  jev-sort run [PATH] [--dry-run] [--no-confirm] [options]
  jev-sort apply PLAN.json [--json]
  jev-sort history [--json] [--history-dir PATH]
  jev-sort undo RUN_ID [--json] [--history-dir PATH]
  jev-sort redo RUN_ID [--json] [--history-dir PATH]
  jev-sort recover RUN_ID [--json] [--history-dir PATH]
  jev-sort auth login|status|logout
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
  --allow-content     Authorize configured file-content transmission
  --json              Write the command result as JSON

Run options:
  --dry-run           Build and display an in-memory plan without moving files
  --no-confirm        Apply the in-memory plan without an interactive prompt
`

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
			return runUI(nil, stderr)
		}
		fmt.Fprintln(stderr, "a terminal is required for implicit UI mode")
		fmt.Fprint(stderr, usage)
		return 2
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
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
	case "ui":
		return runUI(args[1:], stderr)
	case "plan":
		return runPlan(args[1:], stdout, stderr)
	case "run":
		return runCombined(args[1:], stdout, stderr)
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
	case "auth":
		return runAuth(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func runCombined(args []string, stdout, stderr io.Writer) int {
	planArgs, dryRun, noConfirm, jsonOutput, err := parseRunControls(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if flagValue(planArgs, "--out") != "" {
		fmt.Fprintln(stderr, "run does not write plan files; use plan followed by apply to retain a plan")
		return 2
	}
	value, planCode := buildPlanInMemory(planArgs, stderr)
	if planCode == 2 {
		return 2
	}
	summary := summarizePlan(value, "")
	if dryRun || planCode != 0 {
		writeRunSummary(stdout, summary, value, jsonOutput, true)
		if planCode != 0 {
			return 1
		}
		return 0
	}
	planned := summary.Counts["planned"]
	if planned == 0 {
		writeRunSummary(stdout, summary, value, jsonOutput, false)
		return 0
	}
	if !jsonOutput {
		writePlanPreview(stdout, summary, value)
	}
	if !noConfirm {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintln(stderr, "run requires --no-confirm when stdin is not a terminal")
			return 2
		}
		if jsonOutput {
			writePlanPreview(stderr, summary, value)
		}
		confirmed, err := confirm(stderr)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if !confirmed {
			fmt.Fprintln(stdout, "Cancelled.")
			return 0
		}
	}
	result, err := execute.Apply(context.Background(), value)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if jsonOutput {
		payload := struct {
			Plan   plan.Summary   `json:"plan"`
			Result execute.Result `json:"result"`
		}{Plan: summary, Result: result}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(payload); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		writeResult(stdout, result, false)
	}
	if result.Status != "completed" {
		return 1
	}
	return 0
}

func parseRunControls(args []string) (remaining []string, dryRun, noConfirm, jsonOutput bool, err error) {
	for _, argument := range args {
		name, value, hasValue := argument, "true", false
		if equals := strings.IndexByte(argument, '='); equals >= 0 {
			name, value, hasValue = argument[:equals], argument[equals+1:], true
		}
		switch name {
		case "--dry-run", "--no-confirm", "--json":
			parsed := true
			if hasValue {
				parsed, err = strconv.ParseBool(value)
				if err != nil {
					return nil, false, false, false, fmt.Errorf("invalid value for %s", name)
				}
			}
			switch name {
			case "--dry-run":
				dryRun = parsed
			case "--no-confirm":
				noConfirm = parsed
			case "--json":
				jsonOutput = parsed
			}
		default:
			remaining = append(remaining, argument)
		}
	}
	return remaining, dryRun, noConfirm, jsonOutput, nil
}

func flagValue(args []string, name string) string {
	for index, argument := range args {
		if argument == name && index+1 < len(args) {
			return args[index+1]
		}
		if strings.HasPrefix(argument, name+"=") {
			return strings.TrimPrefix(argument, name+"=")
		}
	}
	return ""
}

func summarizePlan(value plan.Plan, path string) plan.Summary {
	summary := plan.Summary{PlanID: value.ID, OutputPath: path, Counts: map[string]int{}}
	for _, operation := range value.Operations {
		summary.Counts[operation.Status]++
	}
	return summary
}

func writeRunSummary(writer io.Writer, summary plan.Summary, value plan.Plan, jsonOutput, dryRun bool) {
	if jsonOutput {
		payload := struct {
			DryRun bool         `json:"dry_run"`
			Plan   plan.Summary `json:"plan"`
		}{DryRun: dryRun, Plan: summary}
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(payload)
		return
	}
	writePlanPreview(writer, summary, value)
}

func writePlanPreview(writer io.Writer, summary plan.Summary, value plan.Plan) {
	fmt.Fprintf(writer, "Plan %s\n", summary.PlanID)
	for _, operation := range value.Operations {
		if operation.Status == "planned" {
			fmt.Fprintf(writer, "move %s -> %s\n", operation.Source, operation.Destination)
		}
	}
	for _, status := range []string{"planned", "unchanged", "skipped", "excluded", "error"} {
		if count := summary.Counts[status]; count > 0 {
			fmt.Fprintf(writer, "%s: %d\n", status, count)
		}
	}
}

func confirm(writer io.Writer) (bool, error) {
	fmt.Fprint(writer, "Apply this plan? [y/N] ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false, scanner.Err()
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

func runUI(args []string, stderr io.Writer) int {
	flagArgs, target, err := normalizeOnePositional(args, map[string]bool{"--config": true})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if target == "" {
		target = "."
	}
	set := flag.NewFlagSet("ui", flag.ContinueOnError)
	set.SetOutput(stderr)
	configPath := set.String("config", "", "explicit YAML configuration file")
	if err := set.Parse(flagArgs); err != nil {
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	diagnostics := config.Validate(cfg)
	if config.HasErrors(diagnostics) {
		writeDiagnostics(stderr, diagnostics)
		return 2
	}
	classifier, err := classifierForConfig(cfg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	program := tea.NewProgram(ui.New(target, cfg, classifier, classifierForConfig))
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func classifierForConfig(cfg config.Config) (plan.Classifier, error) {
	if cfg.Mode == "simple" {
		return classify.Simple{Config: cfg}, nil
	}
	apiKey, _, err := auth.Resolve()
	if err != nil {
		return nil, err
	}
	return classify.Jev{Config: cfg, Client: jevclient.Client{Config: cfg.Jev, APIKey: apiKey}}, nil
}

func runAuth(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: jev-sort auth login|status|logout")
		return 2
	}
	switch args[0] {
	case "login":
		var value string
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprint(stderr, "TypeSafe API key: ")
			data, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(stderr)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			value = string(data)
		} else {
			scanner := bufio.NewScanner(os.Stdin)
			if !scanner.Scan() {
				if scanner.Err() != nil {
					fmt.Fprintln(stderr, scanner.Err())
				} else {
					fmt.Fprintln(stderr, "no API key received")
				}
				return 1
			}
			value = scanner.Text()
		}
		if err := auth.Set(value); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "API key saved in the OS credential store")
		return 0
	case "status":
		source, configured, err := auth.Status()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if configured {
			fmt.Fprintf(stdout, "configured (%s)\n", source)
		} else {
			fmt.Fprintln(stdout, "not configured")
		}
		return 0
	case "logout":
		if err := auth.Delete(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "stored API key removed")
		return 0
	default:
		fmt.Fprintln(stderr, "usage: jev-sort auth login|status|logout")
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

func buildPlanInMemory(args []string, stderr io.Writer) (plan.Plan, int) {
	flagArgs, target, argumentErr := normalizePlanArgs(args)
	if argumentErr != nil {
		fmt.Fprintln(stderr, argumentErr)
		return plan.Plan{}, 2
	}
	set := flag.NewFlagSet("run", flag.ContinueOnError)
	set.SetOutput(stderr)
	configPath := set.String("config", "", "explicit YAML configuration file")
	mode := set.String("mode", "", "classification mode")
	output := set.String("output", "", "output root")
	recursive := set.Bool("recursive", false, "explore subdirectories")
	maxDepth := set.Int("max-depth", -1, "maximum exploration depth")
	collision := set.String("collision", "", "collision policy")
	allowContent := set.Bool("allow-content", false, "authorize configured file-content transmission")
	var includes, excludes stringList
	set.Var(&includes, "include", "include pattern")
	set.Var(&excludes, "exclude", "exclude pattern")
	if err := set.Parse(flagArgs); err != nil {
		return plan.Plan{}, 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return plan.Plan{}, 2
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
	cfg.Jev.Content.Authorized = *allowContent
	diagnostics := config.Validate(cfg)
	if config.HasErrors(diagnostics) {
		writeDiagnostics(stderr, diagnostics)
		return plan.Plan{}, 2
	}
	classifier, err := classifierForConfig(cfg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return plan.Plan{}, 2
	}
	value, err := (plan.Builder{Config: cfg, Classifier: classifier}).Build(context.Background(), target)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return plan.Plan{}, 1
	}
	value.Diagnostics = append(value.Diagnostics, diagnostics...)
	if countPlanStatus(value, "error") > 0 {
		return value, 1
	}
	return value, 0
}

func countPlanStatus(value plan.Plan, status string) int {
	count := 0
	for _, operation := range value.Operations {
		if operation.Status == status {
			count++
		}
	}
	return count
}

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
	allowContent := set.Bool("allow-content", false, "authorize configured file-content transmission")
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
	cfg.Jev.Content.Authorized = *allowContent
	diagnostics := config.Validate(cfg)
	if config.HasErrors(diagnostics) {
		writeDiagnostics(stderr, diagnostics)
		return 2
	}
	classifier, err := classifierForConfig(cfg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	builder := plan.Builder{Config: cfg, Classifier: classifier}
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
