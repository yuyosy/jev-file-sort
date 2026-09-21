# jev-file-sort

`jev-file-sort` is a cross-platform file sorting CLI and Bubble Tea UI. It can classify with deterministic extension and path rules or with TypeSafe Jev.

> [!NOTE]
> This project is under development.  
> Features and behavior may change as development progresses.  
> **Features related to Jev are not yet optimized, so their accuracy may be limited.**  
> 

## Build

```sh
go build -o jev-sort ./cmd/jev-sort
```

## Usage

```sh
jev-sort config check
jev-sort run ~/Downloads --dry-run
jev-sort run ~/Downloads
jev-sort run ~/Downloads --no-confirm
jev-sort plan ~/Downloads --recursive --out plan.json
jev-sort apply plan.json
jev-sort history
jev-sort undo <run-id>
jev-sort redo <run-id>
jev-sort ui ~/Downloads
```

The UI shows operation details beside the plan on wide terminals. Use `→` or `c` to choose a category (including for entries skipped by default), `←` to go back, `m` to switch between Simple and Jev modes, and `f` to switch between descendant and whole-folder classification. Whole-folder mode evaluates folder rules in Simple mode and adds Jev folder classification in Jev mode. `/` filters, `space` skips an operation, `h` opens history, and `?` shows the full key guide. Apply, undo, and redo use confirmation dialogs.

`run` builds its plan in memory and does not create a plan file. It asks for confirmation in a terminal; use `--no-confirm` for explicit non-interactive execution and `--dry-run` to inspect the in-memory plan without moving anything. Use the separate `plan` and `apply` commands when the plan must be reviewed, retained, or applied later.

Running `jev-sort` without a command opens the UI only when stdin and stdout are terminals. Planning never moves source files; `apply` revalidates source fingerprints and destinations before moving anything.

The default user configuration is `${UserConfigDir}/jev-file-sort/config.yaml`. An explicit configuration can be supplied with `--config`.

```yaml
version: 1
mode: simple
scan:
  recursive: true
folders:
  rules_enabled: true
output:
  root: Sorted
  collision: skip
  layout: preserve
rules:
  - id: project-folders
    enabled: true
    kinds: [folder]
    match:
      patterns: ["project-*"]
      depth: { min: 0, max: 1 }
    category: documents
  - id: preset-text
    enabled: false
    kinds: [file]
    category: text
jev:
  concurrency: 4
  batch_size: 100
  folder_evaluation:
    enabled: false
    max_entries: 100
  content:
    enabled: false
    allow_patterns: []
    max_bytes: 32768
```

Rules can target `file`, `folder`, or both. A matching folder rule moves the folder as one unit and does not classify its children individually. Override a built-in rule by using its rule ID, such as `preset-text`, and setting `enabled: false`.

`output.layout: preserve` keeps the source-relative directories below each category, so files such as `internal/config/types.go` and `internal/execute/types.go` remain distinct. Set it to `flatten` to place every classified file directly below its category.

## Jev

Store a TypeSafe API key in the OS credential store, or set `TYPESAFE_API_KEY` for CI:

```sh
jev-sort auth login
jev-sort plan ~/Downloads --mode jev --out plan.json
```

File content is never sent unless content is enabled in the configuration, the file matches an allow pattern, and `plan` is invoked with `--allow-content`. Jev folder evaluation sends folder metadata and a bounded summary of direct children; it can either categorize the whole folder or continue into its children.

Jev mode applies deterministic rules first and only sends unresolved entries to the API. Unresolved siblings are submitted as multiple questions in a shared request, split by `jev.batch_size`, with up to `jev.concurrency` batches in flight. Requests rejected for token size are divided automatically. This keeps common extensions local while retaining semantic classification for ambiguous names and content.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
