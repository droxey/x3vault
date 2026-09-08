# x3vault (slim v0)

Read Obsidian LLM Wiki content and sync it to **XTEINK e-readers** (X3, X4, and compatible devices running Witch Reader) for on-device reading.

Canonical repo: [github.com/droxey/x3vault](https://github.com/droxey/x3vault)

Build output is **Markdown for on-device reading**: wikilinks become relative links, titles become headings, and supported Obsidian formatting is normalized. PNG/JPEG/GIF images remain inline; other image formats are copied and linked with a warning, without conversion.

## Install

**Requirements**

- Go **1.26+** ([`go.mod`](go.mod) requires `1.26.0` and suggests `toolchain go1.26.8`; CI tests `1.26.8` and `1.27.1`)
- An Obsidian vault with a `wiki/` source tree (LLM Wiki layout)
- For device sync: XTE on **File Transfer / Wi-Fi** (Witch Reader transfer screen)

**Install**

```bash
git clone https://github.com/droxey/x3vault.git
cd x3vault
go install ./cmd/x3vault
```

This installs `x3vault` to `$(go env GOBIN)` when configured, otherwise to the first GOPATH entry's `bin/` directory. Ensure that directory is on your `PATH`.

**Build without installing** (local `bin/` only):

```bash
go build -o bin/x3vault ./cmd/x3vault
export PATH="$PWD/bin:$PATH"
```

**First-time vault setup**

```bash
x3vault init --vault /path/to/llmwiki-vault
```

Creates `{vault}/.xte/config.yaml` and prints default paths. Requires `{vault}/wiki/` to exist. Does nothing if config already exists (exit 0).

## Quick start

```bash
export VAULT=/path/to/llmwiki-vault

x3vault build --vault "$VAULT"
x3vault doctor --vault "$VAULT"          # check vault + device

# XTE on File Transfer / Wi-Fi:
x3vault device init --vault "$VAULT"     # once per device root
x3vault sync --dry-run --vault "$VAULT"  # preview plan
x3vault sync --vault "$VAULT"            # upload build/current → device
```

Build normalizes discovered wiki notes and copies resolved local attachments into `../ereader/build/current/`. Sync mirrors the notes and attachments to `/ereader` on the device; the local `build.manifest` is not uploaded. Source notes and attachments remain unchanged. `init` and `config` commands write the program configuration inside the vault.

## Usage

### Global flags

| Flag | Commands | Description |
| ------ | ---------- | ------------- |
| `--vault PATH` | `init`, `build`, `device init`, `sync`, `doctor`, `status`, `config` | Vault/config location. Required for `init`; otherwise defaults to the current directory. `vault_root` in the selected config determines the content root. |
| `--dry-run` | `sync` | Print upload/delete plan; do not change the device. |
| `--json` | `build`, `device init`, `sync`, `doctor`, `status` | JSON result on stdout (schema in `internal/contract/result.go`). |

Place flags after the command or subcommand. Unknown flags, missing flag values, and unexpected arguments fail with exit 2. Invalid existing configuration is reported rather than replaced with defaults.

**Help**

```bash
x3vault help
x3vault --help
```

### Commands

#### `init` — create default config

```bash
x3vault init --vault PATH
```

Writes `{vault}/.xte/config.yaml`. Warns if standard LLM Wiki folders are missing. Exit **2** if `--vault` or `wiki/` is missing.

#### `build` — normalize wiki and copy attachments

```bash
x3vault build [--vault PATH] [--json]
```

- Discovers notes under `source_root` (default `wiki/`)
- Normalizes markdown for XTE e-readers
- Copies resolved attachments to `{build_root}/current/{build.assets_root}/`
- Builds in a separate staging directory while the prior `{build_root}/current/` remains available
- Promotes the completed build and retains the prior `current/` as `backup/`; reports promotion or recovery errors
- Fails without promoting if note normalization or attachment copying fails

Exit **2** config error · **3** discovery/build error

#### `device init` — claim device directory

```bash
x3vault device init [--vault PATH] [--json]
```

Creates `{device.root}/_meta/ownership.json` on the XTE (default root `/ereader`). Run once before first sync. A nonempty unowned directory is refused. Existing ownership must match the marker schema, configured tool, and normalized root. Device must be on File Transfer / Wi-Fi.

Exit **2** config · **4** device unreachable · **5** init failed

#### `sync` — mirror build output to device

```bash
x3vault sync [--vault PATH] [--dry-run] [--json]
```

Uploads `{build_root}/current/` to `{device.root}`. Uses content-hash manifest when enabled. Deletes remote files/dirs not present locally (under owned root only).

Exit **2** config · **3** no local build · **4** device/sync error · **5** plan error

#### `doctor` / `status` — inspect vault and device

```bash
x3vault doctor [--vault PATH] [--json]
x3vault status [--vault PATH] [--json]   # alias for doctor
```

Prints vault paths, note count, build `current/` / `backup/` status, sync settings, CLI version, and device connectivity. An unreachable device or invalid ownership produces a diagnostic warning, including in JSON; successful local inspection still exits 0.

Exit **2** config · **3** discovery error

#### `version` — print CLI version

```bash
x3vault version
```

Prints version and commit (set at link time via `-ldflags`; see **Testing** → smoke script).

Exit **0**

#### `config` — view or edit settings

```bash
x3vault config show [--vault PATH]
x3vault config restore [--vault PATH]     # reset defaults; keeps vault_root
```

**Wiki directory rules** (under `source_root`):

```bash
x3vault config dirs [--vault PATH]                    # show rules
x3vault config dirs restore [--vault PATH]            # reset to defaults
x3vault config dirs ignore DIR... [--vault PATH]      # exclude folders
x3vault config dirs unignore DIR... [--vault PATH]
x3vault config dirs allow DIR... [--vault PATH]       # switch to whitelist mode
x3vault config dirs unallow DIR... [--vault PATH]
```

`config show` and `config restore` print YAML to **stdout**. `config dirs` and `config dirs restore` print directory summaries to stdout. Mutation confirmations and errors go to stderr. Whitelist mode requires at least one allowed directory; use `config dirs restore` to return to the default mode.

### Exit codes

| Code | Meaning |
| ------ | --------- |
| 0 | Success |
| 1 | Unexpected fatal error |
| 2 | Usage / config error |
| 3 | Vault discovery or build error |
| 4 | Device unreachable or sync failed |
| 5 | Device init or sync plan error |

With `--json`, result JSON is written to stdout. Diagnostics also print to stderr, alongside progress output.

### Device connectivity

If `crosspoint.local` does not resolve, set the IP from the XTE File Transfer screen:

```yaml
# {vault}/.xte/config.yaml
device:
  base_url: http://192.168.x.x
```

Then re-run `doctor` before `device init` or `sync`.

## Testing

**Run all tests**

```bash
go test ./... -count=1
```

**Run tests for one package**

```bash
go test ./internal/build/... -v
go test ./internal/markdown/... -v
go test ./internal/config/... -v
go test ./internal/sync/... -v   # includes mock device init + sync
```

**Verify install**

```bash
x3vault help
x3vault version
```

**CI**

GitHub Actions (`.github/workflows/ci.yml`) runs on push/PR to `main`, testing both supported Go release families (`1.26.8` and `1.27.1`):

- `gofmt` check
- `go mod tidy -diff`
- `go vet ./...`
- `staticcheck ./...`
- `govulncheck ./...`
- `go test ./... -race`
- A bounded 10-second `FuzzParseWikilink` run with two workers
- `bash scripts/smoke.sh` — temporary `go install` (with version ldflags) + end-to-end `init` → `build` → `doctor` → `version`, checking JSON and copied content
- `markdownlint-cli2` using `.markdownlint.json`; line length and bold section labels are intentional project style exceptions

**Smoke test** (no device required)

```bash
bash scripts/smoke.sh
```

The smoke script installs into a temporary directory via `go install -ldflags …` using `scripts/version-ldflags.sh`. It verifies JSON results, version metadata, a note, and an attachment, then removes its temporary files. Doctor uses an unreachable loopback address, so no device or Wi-Fi connection is needed.

For a local build with version metadata, source the helper from Bash:

```bash
source scripts/version-ldflags.sh
go build -ldflags "$LDFLAGS" -o bin/x3vault ./cmd/x3vault
```

Or manually:

```bash
TMP=$(mktemp -d)
mkdir -p "$TMP/vault/wiki"
echo '# Index' > "$TMP/vault/wiki/index.md"
x3vault init --vault "$TMP/vault"
x3vault build --vault "$TMP/vault"
ls -R "$TMP/ereader/build/current"
rm -rf "$TMP"
```

## Output layout

For vault at `/path/to/llmwiki-vault/`:

```text
/path/to/
├── llmwiki-vault/              ← Obsidian vault (source content stays unchanged)
│   ├── .xte/config.yaml        ← program config (legacy configs also supported)
│   └── wiki/
└── ereader/build/              ← build output (default build_root)
    ├── current/                ← latest build (sync reads this)
    │   ├── wiki/
    │   ├── assets/<sha256>/    ← attachments grouped by full content hash
    │   └── build.manifest     ← local build summary
    └── backup/                 ← previous current (kept on next build)
```

Each `build` writes a separate staging tree and promotes it only after completion. The previous `current/` remains available during normalization, then becomes `backup/` when the new build is promoted. If promotion fails after moving the old build, restoration is attempted and any recovery error is reported. Run only one build or sync at a time for a given build root.

Builds use `{build_root}/.build.lock` to prevent concurrent builds. After an uncatchable termination, confirm that no build is still running before removing the empty lock directory with `rmdir`, then rerun the build.

Use a dedicated `build_root` **outside** the Obsidian vault. It must not overlap the vault in either direction, including through symbolic links. Its staging, current, and backup directories belong to x3vault.

## LLM Wiki layout

Standard folders (`init` warns if missing):

```text
wiki/
├── index.md, log.md, overview.md, …   # all root *.md synced
├── sources/
├── entities/
├── concepts/
└── analyses/
```

**Directory policy (default):** `all_except_ignored` — subdirectories under `source_root` are discovered except ignored ones and hidden directories. Root-level Markdown notes are included in both directory modes unless excluded by `sync.exclude_vault_paths`. Ignored directories are never written to build output, even when linked or embedded from other notes. Source directories and discovered notes must not be symbolic links.

**Excluded by default:** `sync.exclude_vault_paths` contains `raw`, `.obsidian`, `.git`, `.x3vault`, and `.xte`; ignored wiki directories are `script/` and `references/`. The selected attachment folder permits referenced attachments under a configured `raw/assets/` to be copied. Protected metadata directories (`.git`, `.obsidian`, `.xte`, `.x3vault`) and ignored wiki directories remain blocked.

## Attachments

Resolved local attachments — `![[file.png]]` embeds, `[[file.pdf]]` wikilinks, and inline `[text](file.pdf)` links — are **copied into** `{build_root}/current/{build.assets_root}/<sha256>/` during build. Remote links are preserved without downloading. Missing or excluded targets produce warnings; attachments must resolve inside the vault.

**Where x3vault looks in the vault** (first match wins):

1. Beside the note (`wiki/entities/file.png`)
2. The selected attachment folder, first using the target path, then its filename
3. Relative to `source_root` (`wiki/file.png`)
4. Relative to the vault (`![[attachments/diagram.png]]`)
5. `{vault}/Attachments/<filename>`, then `{vault}/assets/<filename>`
6. `{source_root}/{build.assets_root}/<filename>`

`build.attachment_folder`, when set, selects the attachment folder. Otherwise, `build.read_obsidian_config: true` reads `attachmentFolderPath` from `.obsidian/app.json`, for example:

```json
{
  "attachmentFolderPath": "raw/assets"
}
```

Obsidian's `./attachments` resolves relative to each note's directory; `.` and `./` select the note's directory itself. These per-note forms apply to Obsidian's setting; explicit `build.attachment_folder` is vault-relative.

**Not copied:** files outside the vault, in protected metadata directories, under ignored wiki directories (`script/`, `references/`), or under excluded vault paths unless they are inside the selected attachment folder. Failed resolution prints warnings on stderr during `build`, also included in JSON diagnostics. Attachment read, copy, write, or close errors fail the build.

## Config reference

All settings live in `.xte/config.yaml` inside the vault. If it is absent, legacy `.ereader.yaml`, `.xte.yaml`, and `.x3vault.yaml` at the vault root are checked in that order. Config edits update the selected file.

Configuration must contain one YAML document with known keys. `source_root`, `build.assets_root`, `build.attachment_folder`, and directory rules use relative `/`-separated segments, without `.` or `..` segments. Source and asset output directories must not overlap or use reserved metadata paths. `device.base_url` requires HTTP or HTTPS and a host, without credentials, a query, or a fragment; `timeout_seconds` must be positive. Config files and their `.xte/` directory must not be symbolic links.

```yaml
schema: 1
vault_root: .
source_root: wiki              # compiled wiki source (relative to vault_root)
build_root: ../ereader/build   # default: sibling ereader/ folder; must be outside vault

wiki:
  mode: all_except_ignored     # or whitelist
  # allowed_dirs: [sources, entities]  # required when mode is whitelist
  ignored_dirs:
    - script
    - references
  standard_dirs:               # init warnings only
    - sources
    - entities
    - concepts
    - analyses

build:
  assets_root: assets          # resolved attachments copied to current/assets/<sha256>/
  attachment_folder: ""        # overrides Obsidian's folder when nonempty
  read_obsidian_config: true   # read .obsidian/app.json for attachmentFolderPath

sync:
  fail_fast: true              # stop on first upload/delete error
  hash_manifest: true          # skip unchanged files via _meta/file-hashes.json
  clean_empty_dirs: true       # remove empty remote dirs after file deletes
  exclude_vault_paths:         # selected attachment folder is an explicit exception
    - raw
    - .obsidian
    - .git
    - .x3vault
    - .xte

device:
  base_url: http://crosspoint.local
  root: /ereader               # owned mirror root on the XTE device
  timeout_seconds: 60
  ownership_tool: ereader      # written to _meta/ownership.json
```

## Sync behavior

Sync compares **SHA-256 content hashes**. With `sync.hash_manifest: true`, it uses `{device.root}/_meta/file-hashes.json` as a cache of previously uploaded content and updates it after a successful sync. Remote presence and size are checked before trusting cached hashes. To detect external changes that preserve file size, set `sync.hash_manifest: false` to read remote content. Sync **fails fast** on the first error when `sync.fail_fast` is true. Empty remote directories are removed when `sync.clean_empty_dirs` is true.

An invalid hash manifest stops planning while caching is enabled. To recover, disable `sync.hash_manifest`, sync successfully to remove the stale cache, then enable it and sync again to create a fresh cache. File/directory type conflicts stop planning; move the conflicting device entry or choose another device root before retrying.

Sync changes files incrementally, so a failed sync may leave a partial update. Fix the reported error and rerun sync. Preview the upload/delete plan with `--dry-run` before applying it, especially after changing directory rules or `device.root`.

## Safety

- **Source content is read-only.** x3vault never modifies `wiki/`, attachments, or `.obsidian/`. Build output lives in `build_root/` (default `../ereader/build/current/`), outside the vault.
- One-way only. `init` / `config` commands write `.xte/config.yaml` or the selected legacy configuration file inside the vault.
- Deletes only under the configured owned `device.root` after validating the ownership marker's schema, tool, and root.
- `_meta/` is never deleted by sync.
