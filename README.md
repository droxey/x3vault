# x3vault (slim v0)

One-way build + exact-mirror sync of an Obsidian LLM Wiki (`wiki/`) to an XTEINK X3 running Witch Reader.

## Status

- [x] Config + discovery
- [x] LLM Wiki directory rules (allowed/ignored)
- [x] Markdown normalize + assets
- [x] Deterministic build staging
- [x] Witch HTTP transport + ownership + exact mirror
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

## LLM Wiki layout (default)

Defaults follow the [Karpathy LLM Wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f) pattern used by [microsoft/llmwiki](https://github.com/microsoft/llmwiki):

```
wiki/
├── index.md
├── log.md
├── overview.md          # optional hub pages (always synced)
├── conventions.md
├── sources/
├── entities/
├── concepts/
└── analyses/
```

Root-level `*.md` files are always included. Subdirectories are filtered by `wiki.allowed_dirs` and `wiki.ignored_dirs` in `.x3vault.yaml`.

Default **allowed**: `sources`, `entities`, `concepts`, `analyses`  
Default **ignored**: `script`, `references` (tooling/meta, not reader content)

Add custom folders (e.g. `domains`, `synthesis`):

```bash
./bin/x3vault config dirs allow domains synthesis --vault /path/to/vault
./bin/x3vault config dirs                    # show current rules
./bin/x3vault config dirs restore            # reset to LLM Wiki defaults
```

## Commands

| Command | Behavior |
|---------|----------|
| `init --vault PATH` | Write `.x3vault.yaml`, verify `wiki/` |
| `build` | Discover → normalize → emit staging |
| `device init` | Create `/x3vault` + ownership marker |
| `sync [--dry-run]` | Exact-mirror local build → device |
| `doctor` | Paths, note count, device reachability |
| `config dirs` | Show allowed/ignored directory rules |
| `config dirs restore` | Restore LLM Wiki default dirs |
| `config dirs allow DIR...` | Add allowed subdirectory |
| `config dirs ignore DIR...` | Add ignored subdirectory |

## Config example

```yaml
schema: 1
vault_root: .
source_root: wiki
build_root: .x3vault/build
wiki:
  allowed_dirs:
    - sources
    - entities
    - concepts
    - analyses
    - domains          # custom extension
  ignored_dirs:
    - script
    - references
device:
  base_url: http://crosspoint.local
  root: /x3vault
```

## Safety

- Source is always `wiki/` (case-sensitive).
- One-way only. Vault is never written (except `.x3vault.yaml` via `config dirs`).
- Deletes only under owned `/x3vault/` after ownership marker is present.
- `_meta/` is never deleted by sync.
