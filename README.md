# x3vault (slim v0)

Read Obsidian LLM Wiki content and sync it to **XTEINK e-readers** (X3, X4, and compatible devices running Witch Reader) for on-device reading.

Canonical repo: [github.com/droxey/x3vault](https://github.com/droxey/x3vault)

Build output is **markdown for on-device reading**: wikilinks become relative links, titles render as `#` headings, Obsidian-only syntax is stripped, and images use XTE-friendly formats (PNG/JPEG/GIF).

## Install

**Requirements**

- Go **1.22+** ([`go.mod`](go.mod) pins `1.22.2`)
- An Obsidian vault with a `wiki/` source tree (LLM Wiki layout)
- For device sync: XTE on **File Transfer / Wi-Fi** (Witch Reader transfer screen)

**Install**

```bash
git clone https://github.com/droxey/x3vault.git
cd x3vault
go install ./cmd/x3vault
```

This installs `x3vault` to `$(go env GOPATH)/bin`. Ensure that directory is on your `PATH`.

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

Build copies every discovered wiki note and **every referenced attachment** into `../ereader/build/current/`. Sync mirrors that tree to `/ereader` on the device. The Obsidian vault is never modified.

## Usage

### Global flags

| Flag | Commands | Description |
|------|----------|-------------|
| `--vault PATH` | all except `init` (required there) | Vault root. Default: current directory or path from config. |
| `--dry-run` | `sync` | Print upload/delete plan; do not change the device. |
| `--json` | `build`, `device init`, `sync`, `doctor`, `status` | JSON result on stdout (schema in `internal/contract/result.go`). |

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
- Copies referenced attachments to `{build_root}/current/assets/`
- Backs up prior `{build_root}/current/` → `{build_root}/backup/` before building; restores backup if the build fails

Exit **2** config error · **3** discovery/build error

#### `device init` — claim device directory

```bash
x3vault device init [--vault PATH] [--json]
```

Creates `{device.root}/_meta/ownership.json` on the XTE (default root `/ereader`). Run once before first sync. Device must be on File Transfer / Wi-Fi.

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

Prints vault paths, note count, build `current/` / `backup/` status, sync settings, and device connectivity.

Exit **2** config · **3** discovery error

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

`config show` and `config restore` print YAML to **stdout**. Other config commands print status to stderr.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Unexpected fatal error |
| 2 | Usage / config error |
| 3 | Vault discovery or build error |
| 4 | Device unreachable or sync failed |
| 5 | Device init or sync plan error |

With `--json`, result JSON is written to stdout. On failure, errors and warnings also print to stderr.

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
```

**CI**

GitHub Actions (`.github/workflows/ci.yml`) runs on push/PR to `main`:

- `go test ./... -count=1`
- `bash scripts/smoke.sh` — `go install` + end-to-end `init` → `build` → `doctor`

**Smoke test** (no device required)

```bash
bash scripts/smoke.sh
```

Or manually:

```bash
TMP=$(mktemp -d)
mkdir -p "$TMP/vault/wiki"
echo '# Index' > "$TMP/vault/wiki/index.md"
x3vault init --vault "$TMP/vault"
x3vault build --vault "$TMP/vault"
x3vault doctor --vault "$TMP/vault"
ls -R "$(dirname "$TMP")/ereader/build/current"
```

## Output layout

For vault at `/path/to/llmwiki-vault/`:

```
/path/to/
├── llmwiki-vault/              ← Obsidian vault (read-only to x3vault)
│   ├── .xte/config.yaml        ← program config (only vault write)
│   └── wiki/
└── ereader/build/              ← build output (default build_root)
    ├── current/                ← latest build (sync reads this)
    │   ├── wiki/
    │   └── assets/
    └── backup/                 ← previous current (kept on next build)
```

Each `build` renames `current/` → `backup/` first, then writes a new `current/`. If the build fails, the previous `current/` is restored from `backup/` automatically.

`build_root` may be set to any path **outside** the Obsidian vault. It must not lie inside the vault or its subfolders (including `wiki/`, `.obsidian/`, etc.).

## LLM Wiki layout

Standard folders (`init` warns if missing):

```
wiki/
├── index.md, log.md, overview.md, …   # all root *.md synced
├── sources/
├── entities/
├── concepts/
└── analyses/
```

**Directory policy (default):** `all_except_ignored` — every subdirectory under `source_root` is synced **except** ignored ones. Ignored directories are never written to build output, even when linked or embedded from other notes.

**Never synced by default:** paths in `sync.exclude_vault_paths` (`raw`, `.obsidian`, `.git`, `.xte`), plus ignored wiki dirs (`script/`, `references/`).

## Attachments

Every referenced attachment — `![[file]]` embeds, `[[file.pdf]]` wikilinks, and inline `[text](file.pdf)` links — is **copied into** `ereader/build/current/assets/` during build.

**Where x3vault looks in the vault** (first match wins):

1. Beside the note (`wiki/entities/file.png`)
2. Obsidian `attachmentFolderPath` from `.obsidian/app.json` when `build.read_obsidian_config: true` (including paths under `raw/`)
3. Explicit `build.attachment_folder` in config
4. Vault-relative paths in the embed (`![[attachments/diagram.png]]`)
5. `{vault}/assets/`, `{vault}/Attachments/`, and other standard candidates

```json
{
  "attachmentFolderPath": "raw/assets"
}
```

**Not copied:** files under ignored wiki dirs (`script/`, `references/`) or excluded vault paths, even when linked from a note. Failed resolution prints warnings on stderr during `build`.

## Config reference

All settings live in `.xte/config.yaml` inside the vault. Legacy `.ereader.yaml`, `.xte.yaml`, and `.x3vault.yaml` at vault root are still read if present.

```yaml
schema: 1
vault_root: .
source_root: wiki              # compiled wiki source (relative to vault_root)
build_root: ../ereader/build   # default: sibling ereader/ folder; must be outside vault

wiki:
  mode: all_except_ignored     # or whitelist
  ignored_dirs:
    - script
    - references
  standard_dirs:               # init warnings only
    - sources
    - entities
    - concepts
    - analyses

build:
  assets_root: assets          # all referenced attachments copied to build/current/assets/
  attachment_folder: ""        # optional vault source override (Obsidian used when empty)
  read_obsidian_config: true   # read .obsidian/app.json for attachmentFolderPath

sync:
  fail_fast: true              # stop on first upload/delete error
  hash_manifest: true          # skip unchanged files via _meta/file-hashes.json
  clean_empty_dirs: true       # remove empty remote dirs after file deletes
  exclude_vault_paths:         # never used for asset lookup / discovery escapes
    - raw
    - .obsidian
    - .git
    - .xte

device:
  base_url: http://crosspoint.local
  root: /ereader               # owned mirror root on the XTE device
  timeout_seconds: 60
  ownership_tool: ereader      # written to _meta/ownership.json
```

## Sync behavior

Sync compares **SHA-256 content hashes** when `sync.hash_manifest` is enabled. After a successful sync, x3vault writes `{device.root}/_meta/file-hashes.json` on the device. Sync **fails fast** on the first error when `sync.fail_fast` is true. Empty remote directories are removed when `sync.clean_empty_dirs` is true.

## Safety

- **Obsidian vault is read-only.** x3vault never modifies `wiki/`, attachments, or `.obsidian/`. Build output lives in `build_root/` (default `../ereader/build/current/`), outside the vault.
- One-way only. The only program write inside the vault is `.xte/config.yaml` (via `init` / `config` commands).
- Deletes only under the configured owned `device.root` after ownership marker is present.
- `_meta/` is never deleted by sync.
