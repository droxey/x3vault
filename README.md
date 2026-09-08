# x3vault (slim v0)

Read Obsidian LLM Wiki content and sync it to **XTEINK e-readers** (X3, X4, and compatible devices running Witch Reader) for on-device reading.

Canonical repo: [github.com/droxey/x3vault](https://github.com/droxey/x3vault)

All build output is **markdown for on-device reading**: wikilinks become relative links, titles render as `#` headings, Obsidian-only syntax is stripped, and images use XTE-friendly formats (PNG/JPEG/GIF).

## Status

- [x] Configurable vault layout, build, sync, and device settings
- [x] Config + discovery (`all_except_ignored` dir mode)
- [x] Markdown normalize for XTE e-readers + Obsidian attachment folder
- [x] Alias-aware wikilink index (duplicate keys pick last)
- [x] Deterministic build staging under `../ereader/build` (alongside vault)
- [x] Witch HTTP transport + ownership + content-hash sync (fail-fast)
- [x] CLI: device init / sync / dry-run / config

## Quick start

Build copies every discovered wiki note (normalized for XTE e-reader viewing) and **every referenced attachment** into `../ereader/build/current/assets/`. Sync mirrors the build tree to the device at `/ereader`. The Obsidian vault is never modified.

### Output layout

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

`build_root` may be set to any path **outside** the Obsidian vault.

```bash
go build -o bin/x3vault ./cmd/x3vault

./bin/x3vault init --vault /path/to/llmwiki-vault
./bin/x3vault build --vault /path/to/llmwiki-vault

# XTE on File Transfer / Wi-Fi screen:
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

**Directory policy (default):** `all_except_ignored` — every subdirectory under `source_root` is synced **except** ignored ones. Ignored directories are never written to build output, even when linked or embedded from other notes.

**Never synced by default:** paths in `sync.exclude_vault_paths` (`raw`, `.obsidian`, `.git`, `.xte`), plus ignored wiki dirs (`script/`, `references/`).

## Attachments

Every referenced attachment — `![[file]]` embeds, `[[file.pdf]]` wikilinks, and inline `[text](file.pdf)` links — is **copied into** `ereader/build/current/assets/` during build. Notes in `ereader/build/current/wiki/` link to those copied files.

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

**Override in config:**

```yaml
build:
  assets_root: assets              # output folder under build/current/
  attachment_folder: attachments   # optional vault source override
  read_obsidian_config: true
```

**Not copied:** files under ignored wiki dirs (`script/`, `references/`) or other excluded vault paths, even when linked from a note.

If attachment resolution fails, `build` prints warnings and leaves broken links — check stderr after `x3vault build`.

```bash
./bin/x3vault config show                            # full config YAML
./bin/x3vault config restore                         # reset defaults (keeps vault_root)
./bin/x3vault config dirs                            # show wiki dir rules
./bin/x3vault config dirs ignore drafts              # exclude a folder
./bin/x3vault config dirs unignore drafts
./bin/x3vault config dirs restore                    # reset wiki dir defaults
```

## Config reference

All settings live in `.xte/config.yaml` inside the vault (legacy `.ereader.yaml`, `.xte.yaml`, and `.x3vault.yaml` at vault root are still read if present).

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

## Sync

Sync compares **SHA-256 content hashes** when `sync.hash_manifest` is enabled. After a successful sync, x3vault writes `{device.root}/_meta/file-hashes.json` on the device. Sync **fails fast** on the first error when `sync.fail_fast` is true. Empty remote directories are removed when `sync.clean_empty_dirs` is true.

### Device workflow

1. Put the XTE on **File Transfer / Wi-Fi** (Witch Reader transfer screen).
2. Check connectivity: `./bin/x3vault doctor --vault PATH`
3. First sync setup: `./bin/x3vault device init --vault PATH`
4. Preview changes: `./bin/x3vault sync --dry-run --vault PATH`
5. Sync build output: `./bin/x3vault sync --vault PATH`

If `crosspoint.local` does not resolve on your network, set the IP shown on the device screen:

```yaml
device:
  base_url: http://192.168.x.x
```

## Safety

- **Obsidian vault is read-only.** x3vault never modifies `wiki/`, attachments, or `.obsidian/`. Build output lives in `build_root/` (default `../ereader/build/current/`), outside the vault.
- One-way only. The only program write inside the vault is `.xte/config.yaml` (via `init` / `config` commands).
- Deletes only under the configured owned `device.root` after ownership marker is present.
- `_meta/` is never deleted by sync.
