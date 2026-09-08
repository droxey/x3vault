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

Build copies every discovered wiki note (normalized for XTE e-reader viewing) and every referenced attachment from the Obsidian vault into `../ereader/build/current/` (alongside the vault folder). Sync mirrors that tree to the device at `/ereader`. The Obsidian vault is never modified.

### Output layout

For vault at `/path/to/llmwiki-vault/`:

```
/path/to/
├── llmwiki-vault/          ← Obsidian vault (read-only to x3vault)
│   ├── .ereader.yaml
│   └── wiki/
└── ereader/build/current/  ← build output (default build_root)
    ├── wiki/               ← normalized notes
    │   └── entities/note.md
    └── assets/             ← referenced attachments
        └── ab12/paper.pdf
```

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

**Never synced by default:** paths in `sync.exclude_vault_paths` (`raw`, `.obsidian`, `.git`), plus ignored wiki dirs (`script/`, `references/`).

```bash
./bin/x3vault config show                            # full config YAML
./bin/x3vault config restore                         # reset defaults (keeps vault_root)
./bin/x3vault config dirs                            # show wiki dir rules
./bin/x3vault config dirs ignore drafts              # exclude a folder
./bin/x3vault config dirs unignore drafts
./bin/x3vault config dirs restore                    # reset wiki dir defaults
```

## Config reference

All settings live in `.ereader.yaml` at the vault root (legacy `.xte.yaml` and `.x3vault.yaml` are still read if present).

```yaml
schema: 1
vault_root: .
source_root: wiki              # compiled wiki source (relative to vault_root)
build_root: ../ereader/build   # alongside vault; resolves to ../ereader/build/current at sync

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
  assets_root: assets          # copied attachments (images, PDFs, etc.) for device links
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

device:
  base_url: http://crosspoint.local
  root: /ereader               # owned mirror root on the XTE device
  timeout_seconds: 60
  ownership_tool: ereader      # written to _meta/ownership.json
```

## Sync

Sync compares **SHA-256 content hashes** when `sync.hash_manifest` is enabled. After a successful sync, x3vault writes `{device.root}/_meta/file-hashes.json` on the device. Sync **fails fast** on the first error when `sync.fail_fast` is true. Empty remote directories are removed when `sync.clean_empty_dirs` is true.

## Safety

- **Obsidian vault is read-only.** x3vault never modifies `wiki/`, attachments, or `.obsidian/`. Build output lives in `build_root/` (default `../ereader/build/current/`), outside the vault.
- One-way only. The only write inside the vault is `.ereader.yaml` (via `config` commands).
- Deletes only under the configured owned `device.root` after ownership marker is present.
- `_meta/` is never deleted by sync.

## Reviewer notes

**Goal:** Read Obsidian LLM Wiki from the vault and sync normalized output to XTEINK X3/X4 for further reading on-device.

**Merge with `main`:** Conflicts resolved in favor of this branch's `all_except_ignored` directory policy (main's merged PR #1 used whitelist-by-default `allowed_dirs`). Custom wiki folders sync automatically without `config dirs allow`.

**Path rename:** Tooling paths use `.ereader.yaml`, `../ereader/build`, and `/ereader` on device (legacy `.xte.yaml` and `.x3vault.yaml` still loaded).

**Obsidian vault:** Never modified. Build copies all discovered wiki notes and referenced attachments to `../ereader/build/current/`; sync mirrors to the device.

**Output format:** All device markdown is normalized for XTE e-reader screens (visible `#` titles, stripped Obsidian syntax, PNG/JPEG/GIF images; SVG/WebP linked with warnings).
