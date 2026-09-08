# Main Branch Review — Before Fixes

Review date: 2026-09-08. Repository: `droxey/x3vault`.
Baseline: `main` at `9b8ebe17029ea0b0f8c53d1001432435ef5b6b58`.
The checkout was clean; no `AGENTS.md` was present.

All 44 tracked files (5,159 lines) were read, including all production code,
tests, README, module files, scripts, CI, and ignore rules. Findings below
describe the unchanged baseline. This is a complete inventory of findings
from this review, not a guarantee that no undiscovered defects remain.

## Baseline Evidence

| Check | Result |
| --- | --- |
| Unit tests, Go 1.26.0 | Passed: `go test ./... -count=1` |
| Race tests, Go 1.26.8 | Passed: `go test ./... -race -count=1` |
| Vet, Go 1.26.8 | Passed: `go vet ./...` |
| Module consistency | Passed: `go mod tidy -diff` |
| Formatting | Five Go files differ from `gofmt` |
| Shell syntax | Both scripts pass `bash -n` |
| Device hardware | Not available; transport review uses HTTP fixtures |

Temporary fixtures reproduced deletion outside build output, writes into a
vault through a build-root symlink, reading an outside note through a symlink,
silent fallback after an invalid config schema, and a doubled relative vault
path. Markdown and sync regression probes found failures absent from existing
tests. Fixtures contained only synthetic data; no actual vault or device was used.

## Findings And Fix Recommendations

P1 means a data safety or destructive-operation boundary defect. P2 means a
functional, integrity, diagnostic, or verification defect. P3 means maintenance
or documentation cleanup. Line references refer to the baseline commit.

| ID | Priority | Evidence and consequence | Recommended change |
| --- | --- | --- | --- |
| R01 | P1 | `config/dirs.go:95-109` accepts embedded parent segments; `build/build.go:218-220` passes them to `RemoveAll`. `safe/../../../../victim` deleted a sibling fixture directory. | Validate actual path segments before normalization; remove redundant destructive pruning and enforce containment before filesystem writes. |
| R02 | P1 | `config/config.go:185-198` and `vault/guard.go:11-28` check lexical paths only. A build-root symlink into the vault passed and created `vault/current`. | Canonicalize existing ancestors, reject source/output overlap and symlink escapes before mutation, and validate build entry points. |
| R03 | P1 | `vault/discover.go:67-88` accepts symlinked Markdown files. An outside fixture note was copied successfully. `markdown/normalize.go:210-303` likewise lacks canonical asset containment. | Restrict discovery and asset reads to regular files inside the canonical vault and recheck exclusion rules after resolution. |
| R04 | P1 | `cli/cli.go:566-583` replaces any config load error with defaults when `--vault` is supplied. Invalid schema 999 produced success; sync could use an unintended root or policy. | Default only when config is absent; propagate parse, validation, and permission errors. |
| R05 | P1 | `sync/sync.go:58-96,131-148` treats a filename as proof of ownership; marker schema, tool, and root are unread. Initialization ignores listing errors. | Decode and validate ownership before planning/applying; fail closed on unreadable or foreign markers and uncertain directory state. |
| R06 | P1 | `sync/sync.go:150-151,174-189,351-454` lacks consistent root, listing-name, and plan validation. A crafted `../_meta/ownership.json` entry scheduled deletion of protected metadata; malformed directories can escape reads or recurse. | Share canonical root rules; reject non-segment entries, reserved metadata operations, invalid plans, and root mismatches before mutation. |
| R07 | P1 | `sync/hashmanifest.go:41-65` reads symlinks and includes local `_meta`; apply lacks a verified immutable snapshot. | Reject non-regular local entries and reserved metadata; bind the plan to verified local hashes and revalidate before applying. |
| R08 | P2 | `sync/sync.go:319-326` trusts cached hashes before checking remote existence/size. Deleted remote files can remain missing. | Require matching existence and size before using the manifest optimization. |
| R09 | P2 | `sync/transport.go:219` limits successful file reads to 64 KiB without signaling truncation. Large manifests become invalid JSON and large files cannot be compared correctly. | Keep diagnostic limits separate from successful downloads; return complete data or an explicit size error. |
| R10 | P2 | `sync/sync.go:215-217` creates only each upload's immediate parent. First sync of deeply nested notes/assets can fail. | Plan all missing ancestors in deterministic parent-first order; test with a strict filesystem-like device fake. |
| R11 | P2 | `sync/sync.go:166` loads/parses the remote manifest even when disabled; `hashmanifest.go:88-94` treats every read failure as absence. | Respect the switch and distinguish missing metadata from transport failures; document cache recovery. |
| R12 | P1 | `sync/sync.go:379-475` rehashes local files after upload, so the manifest can describe bytes never sent. An old manifest survives partial or manifest-disabled updates; a later revert can incorrectly skip changed remote data. Continue-on-error mode can still delete after failed uploads. | Invalidate old receipts before mutation, publish verified plan hashes only after successful transfer, and retain remote files when uploads fail. |
| R13 | P2 | Sync uploads precede deletes and do not account for file/directory type conflicts. | Detect incompatible remote types during planning and fail before changes, with actionable recovery guidance. |
| R14 | P2 | `markdown/device.go:41-45` and `normalize.go:64-69` run global regex replacements through code, links, prose, and comments. Fixtures corrupt code expressions and `[Jump](#MyHeading)`. | Restrict transformations to parsed Markdown contexts; preserve code spans/blocks, link destinations, and ordinary text. |
| R15 | P2 | `markdown/device.go:182` treats any path containing `assets/` as already rewritten. Legitimate `../assets/pic.png` is skipped without warning. | Distinguish generated references from source references, avoiding repeated rewrite passes. |
| R16 | P2 | `markdown/normalize.go:85,133,195` classifies dotted note names as attachments before checking notes; heading-only links and embed headings fail. | Resolve note targets first, relative to the current note; support local and embedded heading references. |
| R17 | P2 | `markdown/normalize.go:330-346` guesses source-prefix state and loses a nested `wiki/` component; `.MD` handling is inconsistent. | Keep note paths explicitly source-relative and asset paths build-relative; normalize extension checks consistently. |
| R18 | P2 | `markdown/normalize.go:382-402` and `index.go:65-90` parse YAML using incompatible regexes. CRLF frontmatter, apostrophes, quoted hashes, block tags, and quoted commas are mishandled. | Parse frontmatter once with the existing YAML dependency and reuse metadata extraction. |
| R19 | P2 | `markdown/device.go:16-17,148-202` cannot reliably parse escaped, percent-encoded, titled, angle-delimited, or parenthesized attachment links. | Parse Markdown link syntax and URI components, preserving titles and fragments. |
| R20 | P2 | `markdown/normalize.go:371-379` removes Unicode from heading fragments; asset fragments such as `page=2` are stripped or changed. | Preserve attachment fragments verbatim and use Unicode-aware heading anchors; escape generated destinations and labels. |
| R21 | P2 | `markdown/normalize.go:289-309` names assets with only four hex hash characters plus a lossy sanitized basename. Distinct same-name files can overwrite each other. | Use full content hashes and copy the exact bytes hashed; cover deterministic collision fixtures. |
| R22 | P2 | `markdown/normalize.go:87-104,135-150` turns attachment copy errors into warnings; `copyFileToDir:325-327` ignores close failures. Incomplete builds can be promoted. | Distinguish missing references from I/O failures and propagate failed reads/writes/closes. |
| R23 | P2 | `build/build.go:42-56` removes `current` before building and ignores rollback errors; shared staging/backup paths permit competing builds. | Keep current intact until promotion, serialize builds, clean failed staging safely, and report rollback failures. |
| R24 | P2 | `build/build.go:241-274` hashes absolute note paths only. Generation changes with checkout location but not asset-only changes and ignores `.MD` files. | Hash deterministic relative paths and all produced content, excluding time-dependent metadata. |
| R25 | P2 | `cli/cli.go:572-573` resolves a relative `--vault vault` twice when no config exists. | Resolve the flag to an absolute path once before config resolution. |
| R26 | P2 | CLI argument scans silently ignore unknown flags, missing values, and unexpected arguments. A misspelled sync dry-run flag could perform real writes. | Validate command-specific arguments before any I/O, including duplicate/missing flags and per-command help. |
| R27 | P2 | `cli/cli.go:586-603` omits stderr diagnostics in JSON mode and cannot fail on output encoding errors; `runBuild` drops detailed errors when a partial result accompanies an error. Doctor does not expose connectivity problems in JSON. | Preserve result details, report diagnostics consistently, and make output failures nonzero; expose doctor warnings. |
| R28 | P2 | `cmd/x3vault/main.go:12-16` bypasses deferred cleanup through `os.Exit`; build receives but never uses the signal context. | Return from a helper before exit and propagate build cancellation through staging and promotion. |
| R29 | P2 | Config YAML ignores unknown keys; path validation uses substring checks or cleans invalid values before checking. Device URL and duration validation are incomplete. | Decode known fields strictly; validate source/assets namespaces, portable relative segments, device URL/root, and bounded positive timeouts. |
| R30 | P2 | `config/config.go:349-361` overwrites config directly; config updates persist resolved absolute paths and duplicate loading/saving logic. `pathutil.ContainedIn` mishandles filesystem roots. | Save atomically, preserve serialized relative paths during edits, consolidate updates, and use `filepath.Rel` containment semantics. |
| R31 | P2 | CI selects Go 1.25; `go.mod` suggests 1.25.13. Current supported families are 1.26/1.27. Staticcheck v0.6.1 predates both. | Test supported Go 1.26.8 and 1.27.1; pin a compatible staticcheck and current verified govulncheck. A low language minimum alone is not a vulnerability. |
| R32 | P2 | CI has no format/Markdown gate; the fuzz target has a no-op assertion and CI executes seeds only. Mock device allows missing parents and weakens sync tests. | Add formatting and declared Markdown lint rules, meaningful bounded fuzzing, and realistic device regressions. |
| R33 | P2 | `scripts/version-ldflags.sh:3-4` uses caller `$0`; sourcing interactively reports `dev (none)` despite a valid checkout. | Resolve the helper using `BASH_SOURCE[0]`; verify source and smoke invocation paths. |
| R34 | P3 | README manual smoke uses the wrong output path; stdout/stderr claims, attachment order/exclusions, image support wording, backup behavior, and vault-write claims do not match code. Legacy config commands write legacy files. | Align all examples and contracts with verified final behavior; state device test limits and unsupported syntax clearly. |
| R35 | P3 | Dead `Transport.EnsureDir`, `IsObsidianManagedPath`, no-op `pathJoin`, unused result fields, redundant prefix helpers, duplicate config command/update branches, and redundant output pruning remain. `.gitignore` duplicates `/bin/` and contains unused template comments. Sync sorts have no tie-breaker; `_meta` checks also protect unrelated `_metadata` directories. | Remove proven unused code and obsolete comments; share behavior where it has one contract, add deterministic ordering and precise reserved-directory checks, retaining useful rationale and compatibility paths. |
| R36 | P3 | Five files are not gofmt-formatted; README has no declared Markdown lint policy. | Apply gofmt and a focused markdownlint configuration; do not present arbitrary heading case or wrapping as universal standards. |

Paths in the table omit the common `internal/` prefix except `cmd/`, scripts,
README, and CI. Additional findings discovered during regression work will be
reported before their fixes and recorded as an addendum.

## Execution Plan

1. Add failing regressions for config, local filesystem, ownership, sync, and
   Markdown failures. Keep all fixtures disposable and device calls mocked.
2. Fix config/path safety and build lifecycle; preserve existing note content,
   legacy config compatibility, and the configured `raw/assets` exception.
3. Fix sync validation, manifest integrity, transport reads, and directory plans.
4. Fix Markdown parsing, assets, links, metadata, CLI arguments and diagnostics.
5. Remove proven dead/duplicate code; update docs, toolchain, scripts, and CI.
6. Run supported-toolchain tests, race tests, vet, staticcheck, govulncheck,
   format, Markdown lint, shell checks, bounded fuzzing, and local smoke tests.
7. Independently review the complete diff and document remaining limitations.

Changes remain uncommitted on the local `main` checkout. No push, PR, remote
device mutation, or repository setting changes are part of this execution.

## Standards Used

Go 1.26.8 and 1.27.1 are current supported patch releases as of this review.
Sources: [Go release history](https://go.dev/doc/devel/release),
[security support](https://go.dev/doc/security/),
[toolchain selection](https://go.dev/doc/toolchain),
[Go review comments](https://go.dev/wiki/CodeReviewComments),
[vet](https://pkg.go.dev/cmd/vet),
[race detector](https://go.dev/doc/articles/race_detector),
[fuzzing](https://go.dev/doc/security/fuzz/), and
[govulncheck limitations](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck).

Markdown syntax and platform behavior were checked against
[CommonMark 0.31.2](https://spec.commonmark.org/0.31.2/),
[GFM](https://github.github.com/gfm/), and
[markdownlint rules](https://github.com/DavidAnson/markdownlint/blob/main/doc/Rules.md).
Lint conventions are configurable style policy, not universal syntax requirements.
Staticcheck compatibility: [2026.2.1 release](https://github.com/dominikh/go-tools/releases/tag/2026.2.1).

## Verification Addendum — Reported Before Follow-up Fixes

| ID | Priority | Finding | Recommendation |
| --- | --- | --- | --- |
| R37 | P2 | The newly introduced Goldmark 1.7.13 parser dependency has advisory GO-2026-5320, fixed in 1.7.17. Govulncheck reports no affected imported package or reachable symbol in x3vault, which does not use its HTML renderer. | Upgrade to the verified fixed version and rerun parser, build, CLI, and vulnerability checks. |
| R38 | P1 | HTTP directory listing accepts JSON `null` as an empty directory and ignores trailing JSON after an array. Initialization can interpret a malformed response as confirmation of emptiness. The old mock also emitted `null` for empty directories. | Require a non-null JSON array with no trailing value before trusting the listing; update the mock and cover rejection without ownership writes. Physical firmware response compatibility remains untested. |

Advisory source: [GO-2026-5320](https://pkg.go.dev/vuln/GO-2026-5320).

## Independent Review Addendum — Reported Before Follow-up Fixes

These findings refer to the reviewed working tree after the initial fixes.

| ID | Priority | Finding | Recommendation |
| --- | --- | --- | --- |
| R39 | P2 | `config/dirs.go` no longer converts native paths to slash-separated paths in `ShouldWalkDir`. On Windows, nested whitelist directories passed by discovery contain backslashes and fail the configured slash-based comparisons. | Normalize native runtime paths with `filepath.ToSlash` at the directory-policy boundary while preserving strict validation of raw config values. |
| R40 | P2 | `config/config.go` accepts `source_root` or `build.assets_root` equal to `build.manifest` or a descendant. Build creates a directory there and necessarily fails when writing its manifest file. | Reserve the build manifest's top-level namespace in config validation and cover both root settings. |
| R41 | P2 | `obsidian/config.go` reads `.obsidian/app.json` without checking its file type or containment. A FIFO can block before cancellation is observed; a symlink can read outside the vault or point at an endless device. | Treat Obsidian metadata as optional only after validating a contained regular file and its directory; reject symlinks and special files before reading. |
| R42 | P1 | `markdown/index.go` and `normalize.go` use absolute-path reads after build validation. A synthetic progress callback replacing a validated note with an outside symlink causes outside content to be read and promoted. Ordinary pre-existing outside symlinks are already rejected. | Share a contained regular-note reader for indexing and normalization, anchored to the configured source, and reject changed unsafe entries at the actual read boundary. Add the deterministic swap regression without claiming a general hostile-filesystem sandbox. |
| R43 | P3 | The original upstream behind `gopkg.in/yaml.v3 v3.0.1` was archived April 1, 2025. The official YAML organization maintains the v3 successor for security fixes. No currently exploitable vulnerability was established. | Migrate imports and the dependency to maintained `go.yaml.in/yaml/v3 v3.0.5` and verify strict config and frontmatter compatibility. |
| R44 | P2 | CI action versions declare Node 20, whose runner removal is scheduled for September 23, 2026. Hosted runners currently default to Node 24, so this does not establish that present CI runs use unsupported Node 20 or fail. | Update checkout, setup-go, and setup-node to verified supported Node 24 action releases. |

Maintenance sources: [archived YAML upstream](https://github.com/go-yaml/yaml),
[successor maintenance policy](https://github.com/yaml/go-yaml#version-intentions),
[maintained YAML v3](https://pkg.go.dev/go.yaml.in/yaml/v3), and
[GitHub Actions Node 20 migration notice](https://github.blog/changelog/2025-09-19-deprecation-of-node-20-on-github-actions-runners/).

### R42 Integration Details — Reported Before Corrections

On resuming draft PR #13, a failing regression confirmed that its new note
reader rejected vault-root aliases: discovery returns canonical note paths,
while the reader retained the lexical config path. Open the validated discovery
source to retain alias compatibility. Indexing also replaced the build context
with `context.Background()`; pass cancellation through the shared reader.
The draft source-replacement test's implicit-label expectation was incorrect;
an explicit fixture label tests original metadata resolution without altering
normalization behavior.
