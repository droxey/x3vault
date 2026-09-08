package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/droxey/x3vault/internal/build"
	"github.com/droxey/x3vault/internal/config"
	"github.com/droxey/x3vault/internal/contract"
	"github.com/droxey/x3vault/internal/sync"
	"github.com/droxey/x3vault/internal/vault"
)

const (
	exitOK       = 0
	exitUsage    = 2
	exitBuild    = 3
	exitDevice   = 4
	exitSync     = 5
	exitInternal = 1
)

// Run executes the CLI and returns a process exit code. os.Exit should only be called from main.
func Run(ctx context.Context, args []string) int {
	if len(args) < 2 {
		printUsage(os.Stderr)
		return exitUsage
	}

	cmd := args[1]
	cmdArgs := args[2:]

	switch cmd {
	case "init":
		return runInit(cmdArgs)
	case "build":
		return runBuild(ctx, cmdArgs, os.Stderr)
	case "device":
		if len(cmdArgs) > 0 && cmdArgs[0] == "init" {
			return runDeviceInit(ctx, cmdArgs[1:], os.Stderr)
		}
		fmt.Fprintln(os.Stderr, "usage: x3vault device init [--vault PATH]")
		return exitUsage
	case "sync":
		return runSync(ctx, cmdArgs, os.Stderr)
	case "doctor":
		return runDoctor(ctx, cmdArgs, os.Stderr)
	case "status":
		return runDoctor(ctx, cmdArgs, os.Stderr)
	case "config":
		return runConfig(cmdArgs)
	case "version", "-version", "--version":
		return runVersion(os.Stdout)
	case "help", "-h", "--help":
		printUsage(os.Stderr)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage(os.Stderr)
		return exitUsage
	}
}

func runVersion(out io.Writer) int {
	fmt.Fprintf(out, "x3vault %s (%s)\n", Version, Commit)
	return exitOK
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `x3vault — slim v0: build Obsidian wiki for XTE e-readers, sync to XTEINK

Usage:
  x3vault init --vault PATH
  x3vault build [--vault PATH]
  x3vault device init [--vault PATH]
  x3vault sync [--vault PATH] [--dry-run]
  x3vault doctor [--vault PATH]
  x3vault status [--vault PATH]
  x3vault version
  x3vault config show [--vault PATH]
  x3vault config restore [--vault PATH]
  x3vault config dirs [--vault PATH]
  x3vault config dirs restore [--vault PATH]
  x3vault config dirs ignore DIR... [--vault PATH]
  x3vault config dirs unignore DIR... [--vault PATH]
  x3vault config dirs allow DIR... [--vault PATH]   # switches to whitelist mode
  x3vault config dirs unallow DIR... [--vault PATH]
  --vault PATH   Vault root (default: current directory or config)
  --dry-run      Print plan without mutating the device
  --json         Machine-readable output on stdout

Notes:
  Device must be on its File Transfer / Wi-Fi screen before device init or sync.
`)
}

func flagVault(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--vault" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func wantJSON(args []string) bool {
	for _, a := range args {
		if a == "--json" {
			return true
		}
	}
	return false
}

func wantDryRun(args []string) bool {
	for _, a := range args {
		if a == "--dry-run" {
			return true
		}
	}
	return false
}

func configPath(vault string) string {
	if vault == "" {
		cwd, _ := os.Getwd()
		vault = cwd
	}
	return config.ResolveConfigPath(vault)
}

func runInit(args []string) int {
	vaultPath := flagVault(args)
	if vaultPath == "" {
		fmt.Fprintln(os.Stderr, "init requires --vault PATH")
		return exitUsage
	}
	abs, err := filepath.Abs(vaultPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	cfgPath := config.ConfigPath(abs)
	existing := config.ResolveConfigPath(abs)
	if _, err := os.Stat(existing); err == nil {
		fmt.Fprintf(os.Stderr, "config already exists: %s\n", existing)
		return exitOK
	}

	def := config.Default()
	sourceDir := filepath.Join(abs, def.SourceRoot)
	if st, err := os.Stat(sourceDir); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "%s/ not found under %s\n", def.SourceRoot, abs)
		return exitUsage
	}

	if err := config.WriteDefault(cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", cfgPath)
	fmt.Fprintf(os.Stderr, "build output (default): %s/current/\n", filepath.Join(filepath.Dir(abs), config.EreaderDirName, "build"))
	fmt.Fprintf(os.Stderr, "source: %s\n", sourceDir)
	fmt.Fprintf(os.Stderr, "excluded vault paths: %s\n", strings.Join(def.Sync.ExcludeVaultPaths, ", "))
	for _, d := range config.MissingStandardDirs(sourceDir, def.Wiki.StandardDirs) {
		fmt.Fprintf(os.Stderr, "warning: %s/%s/ not found (standard LLM Wiki folder)\n", def.SourceRoot, d)
	}
	fmt.Fprint(os.Stderr, config.FormatWikiDirsSummary(config.DefaultWikiDirs()))
	return exitOK
}

func runConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: x3vault config show|restore|dirs ...")
		return exitUsage
	}
	switch args[0] {
	case "show":
		return runConfigShow(args[1:])
	case "restore":
		return runConfigRestore(args[1:])
	case "dirs":
		return runConfigDirs(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown config command: %s\n", args[0])
		return exitUsage
	}
}

func runConfigShow(args []string) int {
	cfg, err := loadConfig(flagVault(args))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitUsage
	}
	fmt.Fprint(os.Stdout, config.FormatConfigYAML(cfg))
	return exitOK
}

func runConfigRestore(args []string) int {
	vaultPath := flagVault(args)
	cfgPath := configPath(vaultPath)
	_, err := config.UpdateConfig(cfgPath, func(cfg *config.Config) error {
		cfg.RestoreDefaultsPreservingVault()
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	fmt.Fprintln(os.Stderr, "restored default config (vault_root preserved)")
	cfg, err := config.LoadFromPath(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	fmt.Fprint(os.Stdout, config.FormatConfigYAML(cfg))
	return exitOK
}

func runConfigDirs(args []string) int {
	vaultPath := flagVault(args)
	cfgPath := configPath(vaultPath)

	var tokens []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--vault" {
			i++
			continue
		}
		tokens = append(tokens, args[i])
	}

	if len(tokens) == 0 {
		cfg, err := loadConfig(vaultPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitUsage
		}
		fmt.Fprint(os.Stdout, config.FormatWikiDirsSummary(cfg.Wiki))
		return exitOK
	}

	sub := tokens[0]
	dirArgs := tokens[1:]

	switch sub {
	case "restore":
		_, err := config.UpdateWikiDirs(cfgPath, func(w *config.WikiDirs) error {
			w.RestoreDefaults()
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		fmt.Fprintln(os.Stderr, "restored LLM Wiki default directory rules")
		cfg, err := config.LoadFromPath(cfgPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		fmt.Fprint(os.Stdout, config.FormatWikiDirsSummary(cfg.Wiki))
		return exitOK
	case "allow":
		if len(dirArgs) == 0 {
			fmt.Fprintln(os.Stderr, "usage: x3vault config dirs allow DIR...")
			return exitUsage
		}
		_, err := config.UpdateWikiDirs(cfgPath, func(w *config.WikiDirs) error {
			w.AddAllowed(dirArgs...)
			return w.Validate()
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		fmt.Fprintf(os.Stderr, "switched to whitelist mode; added allowed dirs: %s\n", strings.Join(dirArgs, ", "))
		return exitOK
	case "unallow":
		if len(dirArgs) == 0 {
			fmt.Fprintln(os.Stderr, "usage: x3vault config dirs unallow DIR...")
			return exitUsage
		}
		_, err := config.UpdateWikiDirs(cfgPath, func(w *config.WikiDirs) error {
			w.RemoveAllowed(dirArgs...)
			return w.Validate()
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		fmt.Fprintf(os.Stderr, "removed allowed dirs: %s\n", strings.Join(dirArgs, ", "))
		return exitOK
	case "ignore":
		if len(dirArgs) == 0 {
			fmt.Fprintln(os.Stderr, "usage: x3vault config dirs ignore DIR...")
			return exitUsage
		}
		_, err := config.UpdateWikiDirs(cfgPath, func(w *config.WikiDirs) error {
			w.AddIgnored(dirArgs...)
			return w.Validate()
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		fmt.Fprintf(os.Stderr, "added ignored dirs: %s\n", strings.Join(dirArgs, ", "))
		return exitOK
	case "unignore":
		if len(dirArgs) == 0 {
			fmt.Fprintln(os.Stderr, "usage: x3vault config dirs unignore DIR...")
			return exitUsage
		}
		_, err := config.UpdateWikiDirs(cfgPath, func(w *config.WikiDirs) error {
			w.RemoveIgnored(dirArgs...)
			return w.Validate()
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		fmt.Fprintf(os.Stderr, "removed ignored dirs: %s\n", strings.Join(dirArgs, ", "))
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown config dirs command: %s\n", sub)
		return exitUsage
	}
}

func runBuild(ctx context.Context, args []string, progress io.Writer) int {
	res := contract.NewResult("build")
	jsonOut := wantJSON(args)

	cfg, err := loadConfig(flagVault(args))
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		return exitUsage
	}

	disc, err := vault.Discover(cfg.VaultRoot, cfg.SourceRoot, cfg.Wiki)
	if err != nil {
		res.AddError(err.Error(), cfg.SourceDir())
		emit(res, jsonOut)
		return exitBuild
	}

	fmt.Fprintf(progress, "discovered %d notes under %s\n", len(disc.Notes), disc.SourceRoot)

	br, err := build.Run(cfg, disc, build.RunOptions{Progress: progress})
	if err != nil {
		res.AddError(err.Error(), cfg.BuildRoot)
		emit(res, jsonOut)
		return exitBuild
	}

	res.Summary.Notes = br.Notes
	res.Summary.Assets = br.Assets
	res.Generation = br.Generation
	for _, w := range br.Warnings {
		res.AddWarning(w, "")
	}
	for _, e := range br.Errors {
		res.AddError(e, "")
	}

	fmt.Fprintf(progress, "built generation %s → %s\n", br.Generation, br.StagingDir)
	fmt.Fprintf(progress, "  notes: %d  assets: %d  warnings: %d\n", br.Notes, br.Assets, len(br.Warnings))

	if !res.OK {
		emit(res, jsonOut)
		return exitBuild
	}
	emit(res, jsonOut)
	return exitOK
}

func runDeviceInit(ctx context.Context, args []string, progress io.Writer) int {
	res := contract.NewResult("device init")
	jsonOut := wantJSON(args)

	cfg, err := loadConfig(flagVault(args))
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		return exitUsage
	}

	t := syncTransport(cfg)
	st, err := t.Status(ctx)
	if err != nil {
		printDeviceUnreachable(progress, err)
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		return exitDevice
	}
	fmt.Fprintf(progress, "device: %s  firmware: %s  mode: %s  ip: %s\n", st.Device, st.Version, st.Mode, st.IP)

	opts := syncOpts(cfg, progress)
	if err := sync.DeviceInit(ctx, t, opts.DeviceRoot, opts.OwnershipTool); err != nil {
		res.AddError(err.Error(), opts.DeviceRoot)
		emit(res, jsonOut)
		return exitSync
	}
	fmt.Fprintf(progress, "ownership marker written at %s/_meta/ownership.json\n", opts.DeviceRoot)
	emit(res, jsonOut)
	return exitOK
}

func runSync(ctx context.Context, args []string, progress io.Writer) int {
	res := contract.NewResult("sync")
	jsonOut := wantJSON(args)
	dry := wantDryRun(args)

	cfg, err := loadConfig(flagVault(args))
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		return exitUsage
	}

	current := filepath.Join(cfg.BuildRoot, "current")
	if st, err := os.Stat(current); err != nil || !st.IsDir() {
		res.AddError("no local build; run: x3vault build", current)
		emit(res, jsonOut)
		return exitBuild
	}

	t := syncTransport(cfg)
	st, err := t.Status(ctx)
	if err != nil {
		printDeviceUnreachable(progress, err)
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		return exitDevice
	}
	fmt.Fprintf(progress, "device: %s  firmware: %s  mode: %s\n", st.Device, st.Version, st.Mode)

	opts := syncOpts(cfg, progress)
	plan, err := sync.BuildPlan(ctx, t, opts.DeviceRoot, current, opts)
	if err != nil {
		res.AddError(err.Error(), cfg.Device.Root)
		emit(res, jsonOut)
		return exitSync
	}

	fileDeletes, dirDeletes := 0, 0
	for _, op := range plan.Deletes {
		if op.Type == "directory" {
			dirDeletes++
		} else {
			fileDeletes++
		}
	}
	fmt.Fprintf(progress, "plan: %d uploads, %d file deletes, %d dir deletes\n",
		len(plan.Uploads), fileDeletes, dirDeletes)
	if dry {
		fmt.Fprintln(progress, "dry-run:")
	}

	sr := sync.ApplyPlan(ctx, t, plan, current, dry, opts)
	res.Summary.Notes = sr.Uploaded
	for _, e := range sr.Errors {
		res.AddError(e, "")
	}

	if dry {
		fmt.Fprintf(progress, "dry-run complete (no changes applied)\n")
	} else {
		fmt.Fprintf(progress, "sync complete: uploaded %d, deleted %d\n", sr.Uploaded, sr.Deleted)
	}

	if !res.OK {
		fmt.Fprintln(progress, "sync failed (fail-fast; device may be partially updated)")
		emit(res, jsonOut)
		return exitDevice
	}
	emit(res, jsonOut)
	return exitOK
}

func runDoctor(ctx context.Context, args []string, progress io.Writer) int {
	res := contract.NewResult("doctor")
	jsonOut := wantJSON(args)

	cfg, err := loadConfig(flagVault(args))
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		return exitUsage
	}

	disc, err := vault.Discover(cfg.VaultRoot, cfg.SourceRoot, cfg.Wiki)
	if err != nil {
		res.AddError(err.Error(), cfg.SourceDir())
		emit(res, jsonOut)
		return exitBuild
	}

	res.Summary.Notes = len(disc.Notes)
	fmt.Fprintf(progress, "vault:   %s\n", cfg.VaultRoot)
	fmt.Fprintf(progress, "source:  %s\n", disc.SourceRoot)
	fmt.Fprintf(progress, "notes:   %d\n", len(disc.Notes))
	fmt.Fprintf(progress, "dirs:    allowed=%d ignored=%d\n", len(cfg.Wiki.Allowed), len(cfg.Wiki.Ignored))
	fmt.Fprintf(progress, "build:   %s\n", cfg.BuildRoot)
	printBuildDirStatus(progress, cfg.BuildRoot)
	fmt.Fprintf(progress, "device:  %s  root=%s  timeout=%s\n", cfg.Device.BaseURL, cfg.DeviceRoot(), cfg.DeviceTimeout())
	fmt.Fprintf(progress, "sync:    fail_fast=%v hash_manifest=%v clean_empty_dirs=%v\n",
		cfg.Sync.FailFast, cfg.Sync.HashManifest, cfg.Sync.CleanEmptyDirs)
	fmt.Fprintf(progress, "exclude: %s\n", strings.Join(cfg.Sync.ExcludeVaultPaths, ", "))
	fmt.Fprintf(progress, "cli:     x3vault %s (%s)\n", Version, Commit)

	t := syncTransport(cfg)
	if st, err := t.Status(ctx); err != nil {
		printDeviceUnreachable(progress, err)
	} else {
		fmt.Fprintf(progress, "device:  online %s/%s heap=%d\n", st.Device, st.Version, st.FreeHeap)
		owned, err := sync.HasOwnership(ctx, t, cfg.DeviceRoot())
		if err != nil {
			fmt.Fprintf(progress, "owned:   unknown (%v)\n", err)
		} else {
			fmt.Fprintf(progress, "owned:   %v\n", owned)
		}
	}

	emit(res, jsonOut)
	return exitOK
}

func syncTransport(cfg *config.Config) *sync.Transport {
	return sync.NewTransport(cfg.Device.BaseURL, cfg.DeviceTimeout())
}

func syncOpts(cfg *config.Config, progress io.Writer) sync.Options {
	opts := sync.OptionsFromConfig(cfg)
	opts.Progress = progress
	return opts
}

func printDeviceUnreachable(progress io.Writer, err error) {
	fmt.Fprintf(progress, "device:  unreachable (%s)\n", strings.TrimSpace(err.Error()))
	fmt.Fprintln(progress, "hint: on File Transfer / Wi-Fi, set device.base_url to http://<device-ip> in .xte/config.yaml if crosspoint.local does not resolve")
}

func printBuildDirStatus(progress io.Writer, buildRoot string) {
	current := filepath.Join(buildRoot, "current")
	backup := filepath.Join(buildRoot, "backup")
	if st, err := os.Stat(current); err == nil && st.IsDir() {
		fmt.Fprintf(progress, "current: %s\n", current)
	} else {
		fmt.Fprintln(progress, "current: (none)")
	}
	if st, err := os.Stat(backup); err == nil && st.IsDir() {
		fmt.Fprintf(progress, "backup:  %s\n", backup)
	} else {
		fmt.Fprintln(progress, "backup:  (none)")
	}
}

func loadConfig(vaultFlag string) (*config.Config, error) {
	path := configPath(vaultFlag)
	cfg, err := config.Load(path)
	if err != nil {
		if vaultFlag != "" {
			cfg = config.Default()
			cfg.VaultRoot = vaultFlag
			if err := cfg.Resolve(path); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, err
	}
	if err := cfg.Resolve(path); err != nil {
		return nil, err
	}
	return cfg, nil
}

func emit(res *contract.Result, jsonOut bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintf(os.Stderr, "encode json: %v\n", err)
		}
		return
	}
	for _, d := range res.Diagnostics {
		prefix := d.Level + ": "
		if d.Path != "" {
			fmt.Fprintf(os.Stderr, "%s%s (%s)\n", prefix, d.Message, d.Path)
		} else {
			fmt.Fprintf(os.Stderr, "%s%s\n", prefix, d.Message)
		}
	}
}
