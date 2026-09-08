# x3vault (slim v0)

One-way build + exact-mirror sync of an Obsidian LLM Wiki to an XTEINK X3 running Witch Reader.

Canonical repo: [github.com/droxey/x3vault](https://github.com/droxey/x3vault)

## Status

- [x] Configurable vault layout, build, sync, and device settings
- [x] Config + discovery (`all_except_ignored` dir mode)
- [x] Markdown normalize + Obsidian attachment folder
- [x] Alias-aware wikilink index (duplicate keys pick last)
- [x] Deterministic build staging
- [x] Witch HTTP transport + ownership + content-hash sync (fail-fast)
- [x] CLI: device init / sync / dry-run / config

## Quick start

```bash
go build -o bin/x3vault ./cmd/x3vault

./bin/x3vault init --vault /path/to/llmwiki-vault
./bin/x3vault build --vault /path/to/llmwiki-vault

# X3 on File Transfer / Wi-Fi screen:
./bin/x3vault device init --vault /path/to/llmwiki-vault
./bin/x3vault sync --dry-run --vault /path/to/llmwiki-vault
./bin/x3vault sync --vault /path/to/llmwiki-vault
```

## LLM Wiki layout

Standard folders (init warns if missing):

```
wiki/
├── index.md, log.md, overview.md, …   # all root *.md synced
├── sources/
├── entities/
├── concepts/
└── analyses/
```

**Directory policy (default):** `all_except_ignored` — every subdirectory under `source_root` is synced **except** ignored ones. Custom folders (`domains/`, `synthesis/`, etc.) need no extra config.

**Never synced by default:** paths in `sync.exclude_vault_paths` (`raw`, `.obsidian`, `.git`, `.x3vault`), plus ignored dirs (`script/`, `references/`).

```bash
./bin/x3vault config show                            # full config YAML
./bin/x3vault config restore                         # reset defaults (keeps vault_root)
./bin/x3vault config dirs                            # show wiki dir rules
./bin/x3vault config dirs ignore drafts              # exclude a folder
./bin/x3vault config dirs unignore drafts
./bin/x3vault config dirs restore                    # reset wiki dir defaults
```

## Config reference

All settings live in `.x3vault.yaml` at the vault root.

```yaml
schema: 1
vault_root: .
source_root: wiki              # compiled wiki source (relative to vault_root)
build_root: .x3vault/build     # local staging output

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
  assets_root: assets          # output dir for copied images (under build/current/)
  attachment_folder: ""        # override Obsidian attachment path (relative to vault)
  read_obsidian_config: true   # read .obsidian/app.json for attachmentFolderPath

sync:
  fail_fast: true              # stop on first upload/delete error
  hash_manifest: true          # skip unchanged files via _meta/file-hashes.json
  clean_empty_dirs: true       # remove empty remote dirs after file deletes
  exclude_vault_paths:         # never used for asset lookup / discovery escapes
    - raw
    - .obsidian
    - .git
    - .x3vault

device:
  base_url: http://crosspoint.local
  root: /x3vault               # owned mirror root on the X3
  timeout_seconds: 60
  ownership_tool: x3vault      # written to _meta/ownership.json
```

## Sync

Sync compares **SHA-256 content hashes** when `sync.hash_manifest` is enabled. After a successful sync, x3vault writes `{device.root}/_meta/file-hashes.json` on the device. Sync **fails fast** on the first error when `sync.fail_fast` is true. Empty remote directories are removed when `sync.clean_empty_dirs` is true.

## Safety

- One-way only. Vault is never written except `.x3vault.yaml` via `config` commands.
- Deletes only under the configured owned `device.root` after ownership marker is present.
- `_meta/` is never deleted by sync.
