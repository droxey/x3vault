# x3vault (slim v0)

One-way build + exact-mirror sync of an Obsidian LLM Wiki (`wiki/`) to an XTEINK X3 running Witch Reader.

Canonical repo: [github.com/droxey/x3vault](https://github.com/droxey/x3vault)

## Status

- [x] Config + discovery (`all_except_ignored` dir mode)
- [x] Markdown normalize + Obsidian attachment folder
- [x] Alias-aware wikilink index (duplicate keys pick last)
- [x] Deterministic build staging
- [x] Witch HTTP transport + ownership + content-hash sync (fail-fast)
- [x] CLI: device init / sync / dry-run / config dirs

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

**Directory policy (default):** `all_except_ignored` — every subdirectory under `wiki/` is synced **except** ignored ones. Custom folders (`domains/`, `synthesis/`, etc.) need no extra config.

**Never synced:** `raw/` (outside `wiki/`), ignored dirs (`script/`, `references/` by default).

```bash
./bin/x3vault config dirs                         # show rules
./bin/x3vault config dirs ignore drafts           # exclude a folder
./bin/x3vault config dirs unignore drafts
./bin/x3vault config dirs restore                 # reset defaults
```

## Config example

```yaml
schema: 1
vault_root: .
source_root: wiki
build_root: .x3vault/build
wiki:
  mode: all_except_ignored
  ignored_dirs:
    - script
    - references
device:
  base_url: http://crosspoint.local
  root: /x3vault
```

## Sync

Sync compares **SHA-256 content hashes**. After a successful sync, x3vault writes `/x3vault/_meta/file-hashes.json` on the device. Sync **fails fast** on the first error (device left unchanged if hash manifest update fails).

## Safety

- Source is always `wiki/` only; `raw/` never uploaded.
- One-way only. Vault is never written except `.x3vault.yaml` via `config dirs`.
- Deletes only under owned `/x3vault/` after ownership marker is present.
- `_meta/` is never deleted by sync.
