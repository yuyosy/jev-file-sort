# jev-file-sort

`jev-file-sort` is a cross-platform file sorting CLI and Bubble Tea UI. It can classify with deterministic extension and path rules or with TypeSafe Jev.

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

The UI shows operation details beside the plan on wide terminals. Use `m` to switch between Simple and Jev modes, `/` to filter, `c` to choose a category, `space` to skip an operation, `h` for history, and `?` for the full key guide. Apply, undo, and redo use confirmation dialogs.

`run` builds its plan in memory and does not create a plan file. It asks for confirmation in a terminal; use `--no-confirm` for explicit non-interactive execution and `--dry-run` to inspect the in-memory plan without moving anything. Use the separate `plan` and `apply` commands when the plan must be reviewed, retained, or applied later.

Running `jev-sort` without a command opens the UI only when stdin and stdout are terminals. Planning never moves source files; `apply` revalidates source fingerprints and destinations before moving anything.

The default user configuration is `${UserConfigDir}/jev-file-sort/config.yaml`. An explicit configuration can be supplied with `--config`.

```yaml
version: 1
mode: simple
scan:
  recursive: true
output:
  root: Sorted
  collision: skip
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
```

Rules can target `file`, `folder`, or both. A matching folder rule moves the folder as one unit and does not classify its children individually. Override a built-in rule by using its rule ID, such as `preset-text`, and setting `enabled: false`.

## Jev

Store a TypeSafe API key in the OS credential store, or set `TYPESAFE_API_KEY` for CI:

```sh
jev-sort auth login
jev-sort plan ~/Downloads --mode jev --out plan.json
```

File content is never sent unless content is enabled in the configuration, the file matches an allow pattern, and `plan` is invoked with `--allow-content`. Jev folder evaluation sends folder metadata and a bounded summary of direct children; it can either categorize the whole folder or continue into its children.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
