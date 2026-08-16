# x3vault (slim v0)

One-way build + exact-mirror sync of an Obsidian wiki/ folder to an XTEINK X3 running Witch Reader.

## Status

- [x] Config + discovery
- [x] Markdown normalize + assets
- [x] Deterministic build staging
- [x] Witch HTTP transport + ownership + exact mirror
- [x] CLI: device init / sync / dry-run

## Quick start

```bash
go build -o bin/x3vault ./cmd/x3vault

./bin/x3vault init --vault /Users/droxey/dev/obsidian/brain
./bin/x3vault build --vault /Users/droxey/dev/obsidian/brain

# X3 on File Transfer / Wi-Fi screen:
./bin/x3vault device init --vault /Users/droxey/dev/obsidian/brain
./bin/x3vault sync --dry-run --vault /Users/droxey/dev/obsidian/brain
./bin/x3vault sync --vault /Users/droxey/dev/obsidian/brain
```

## Commands

| Command | Behavior |
|---------|----------|
| `init --vault PATH` | Write `.x3vault.yaml`, verify `wiki/` |
| `build` | Discover → normalize → emit staging |
| `device init` | Create `/x3vault` + ownership marker |
| `sync [--dry-run]` | Exact-mirror local build → device |
| `doctor` | Paths, note count, device reachability |

## Safety

- Source is always `wiki/` (case-sensitive).
- One-way only. Vault is never written.
- Deletes only under owned `/x3vault/` after ownership marker is present.
- `_meta/` is never deleted by sync.
