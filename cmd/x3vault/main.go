package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/droxey/x3vault/internal/build"
	"github.com/droxey/x3vault/internal/config"
	"github.com/droxey/x3vault/internal/contract"
	"github.com/droxey/x3vault/internal/sync"
	"github.com/droxey/x3vault/internal/vault"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "init":
		runInit(args)
	case "build":
		runBuild(args)
	case "device":
		if len(args) > 0 && args[0] == "init" {
			runDeviceInit(args[1:])
		} else {
			fmt.Fprintln(os.Stderr, "usage: x3vault device init [--vault PATH]")
			os.Exit(2)
		}
	case "sync":
		runSync(args)
	case "doctor":
		runDoctor(args)
	case "status":
		runStatus(args)
	case "config":
		runConfig(args)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `x3vault — slim v0: build Obsidian wiki for XTE e-readers, sync to XTEINK

Usage:
  x3vault init --vault PATH
  x3vault build [--vault PATH]
  x3vault device init [--vault PATH]
  x3vault sync [--vault PATH] [--dry-run]
  x3vault doctor [--vault PATH]
  x3vault status [--vault PATH]
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

func runInit(args []string) {
	vaultPath := flagVault(args)
	if vaultPath == "" {
		fmt.Fprintln(os.Stderr, "init requires --vault PATH")
		os.Exit(2)
	}
	abs, err := filepath.Abs(vaultPath)
	if err != nil {
		fatal(err)
	}
	cfgPath := config.ConfigPath(abs)
	existing := config.ResolveConfigPath(abs)
	if _, err := os.Stat(existing); err == nil {
		fmt.Fprintf(os.Stderr, "config already exists: %s\n", existing)
		os.Exit(0)
	}

	def := config.Default()
	sourceDir := filepath.Join(abs, def.SourceRoot)
	if st, err := os.Stat(sourceDir); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "%s/ not found under %s\n", def.SourceRoot, abs)
		os.Exit(2)
	}

	if err := config.WriteDefault(cfgPath); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", cfgPath)
	fmt.Fprintf(os.Stderr, "build output (default): %s/current/\n", filepath.Join(filepath.Dir(abs), config.EreaderDirName, "build"))
	fmt.Fprintf(os.Stderr, "source: %s\n", sourceDir)
	fmt.Fprintf(os.Stderr, "excluded vault paths: %s\n", strings.Join(def.Sync.ExcludeVaultPaths, ", "))
	for _, d := range config.MissingStandardDirs(sourceDir, def.Wiki.StandardDirs) {
		fmt.Fprintf(os.Stderr, "warning: %s/%s/ not found (standard LLM Wiki folder)\n", def.SourceRoot, d)
	}
	fmt.Fprint(os.Stderr, config.FormatWikiDirsSummary(config.DefaultWikiDirs()))
}

func runConfig(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: x3vault config show|restore|dirs ...")
		os.Exit(2)
	}
	switch args[0] {
	case "show":
		runConfigShow(args[1:])
	case "restore":
		runConfigRestore(args[1:])
	case "dirs":
		runConfigDirs(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown config command: %s\n", args[0])
		os.Exit(2)
	}
}

func runConfigShow(args []string) {
	cfg, err := loadConfig(flagVault(args))
	if err != nil {
		fatal(err)
	}
	fmt.Fprint(os.Stdout, config.FormatConfigYAML(cfg))
}

func runConfigRestore(args []string) {
	vaultPath := flagVault(args)
	cfgPath := configPath(vaultPath)
	_, err := config.UpdateConfig(cfgPath, func(cfg *config.Config) error {
		cfg.RestoreDefaultsPreservingVault()
		return nil
	})
	if err != nil {
		fatal(err)
	}
	fmt.Fprintln(os.Stderr, "restored default config (vault_root preserved)")
	cfg, err := config.LoadFromPath(cfgPath)
	if err != nil {
		fatal(err)
	}
	fmt.Fprint(os.Stdout, config.FormatConfigYAML(cfg))
}

func runConfigDirs(args []string) {
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
			fatal(err)
		}
		fmt.Fprint(os.Stdout, config.FormatWikiDirsSummary(cfg.Wiki))
		return
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
			fatal(err)
		}
		fmt.Fprintln(os.Stderr, "restored LLM Wiki default directory rules")
		cfg, err := config.LoadFromPath(cfgPath)
		if err != nil {
			fatal(err)
		}
		fmt.Fprint(os.Stdout, config.FormatWikiDirsSummary(cfg.Wiki))
	case "allow":
		if len(dirArgs) == 0 {
			fmt.Fprintln(os.Stderr, "usage: x3vault config dirs allow DIR...")
			os.Exit(2)
		}
		_, err := config.UpdateWikiDirs(cfgPath, func(w *config.WikiDirs) error {
			w.AddAllowed(dirArgs...)
			return w.Validate()
		})
		if err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "switched to whitelist mode; added allowed dirs: %s\n", strings.Join(dirArgs, ", "))
	case "unallow":
		if len(dirArgs) == 0 {
			fmt.Fprintln(os.Stderr, "usage: x3vault config dirs unallow DIR...")
			os.Exit(2)
		}
		_, err := config.UpdateWikiDirs(cfgPath, func(w *config.WikiDirs) error {
			w.RemoveAllowed(dirArgs...)
			return w.Validate()
		})
		if err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "removed allowed dirs: %s\n", strings.Join(dirArgs, ", "))
	case "ignore":
		if len(dirArgs) == 0 {
			fmt.Fprintln(os.Stderr, "usage: x3vault config dirs ignore DIR...")
			os.Exit(2)
		}
		_, err := config.UpdateWikiDirs(cfgPath, func(w *config.WikiDirs) error {
			w.AddIgnored(dirArgs...)
			return w.Validate()
		})
		if err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "added ignored dirs: %s\n", strings.Join(dirArgs, ", "))
	case "unignore":
		if len(dirArgs) == 0 {
			fmt.Fprintln(os.Stderr, "usage: x3vault config dirs unignore DIR...")
			os.Exit(2)
		}
		_, err := config.UpdateWikiDirs(cfgPath, func(w *config.WikiDirs) error {
			w.RemoveIgnored(dirArgs...)
			return w.Validate()
		})
		if err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "removed ignored dirs: %s\n", strings.Join(dirArgs, ", "))
	default:
		fmt.Fprintf(os.Stderr, "unknown config dirs command: %s\n", sub)
		os.Exit(2)
	}
}

func runBuild(args []string) {
	res := contract.NewResult("build")
	jsonOut := wantJSON(args)

	cfg, err := loadConfig(flagVault(args))
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		os.Exit(2)
	}

	disc, err := vault.Discover(cfg.VaultRoot, cfg.SourceRoot, cfg.Wiki)
	if err != nil {
		res.AddError(err.Error(), cfg.SourceDir())
		emit(res, jsonOut)
		os.Exit(3)
	}

	fmt.Fprintf(os.Stderr, "discovered %d notes under %s\n", len(disc.Notes), disc.SourceRoot)

	br, err := build.Run(cfg, disc)
	if err != nil {
		res.AddError(err.Error(), cfg.BuildRoot)
		emit(res, jsonOut)
		os.Exit(3)
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

	fmt.Fprintf(os.Stderr, "built generation %s → %s\n", br.Generation, br.StagingDir)
	fmt.Fprintf(os.Stderr, "  notes: %d  assets: %d  warnings: %d\n", br.Notes, br.Assets, len(br.Warnings))

	if !res.OK {
		emit(res, jsonOut)
		os.Exit(3)
	}
	emit(res, jsonOut)
}

func runDeviceInit(args []string) {
	res := contract.NewResult("device init")
	jsonOut := wantJSON(args)

	cfg, err := loadConfig(flagVault(args))
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		os.Exit(2)
	}

	t := syncTransport(cfg)
	st, err := t.Status()
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		os.Exit(4)
	}
	fmt.Fprintf(os.Stderr, "device: %s  firmware: %s  mode: %s  ip: %s\n", st.Device, st.Version, st.Mode, st.IP)

	opts := syncOpts(cfg)
	if err := sync.DeviceInit(t, opts.DeviceRoot, opts.OwnershipTool); err != nil {
		res.AddError(err.Error(), opts.DeviceRoot)
		emit(res, jsonOut)
		os.Exit(5)
	}
	fmt.Fprintf(os.Stderr, "ownership marker written at %s/_meta/ownership.json\n", opts.DeviceRoot)
	emit(res, jsonOut)
}

func runSync(args []string) {
	res := contract.NewResult("sync")
	jsonOut := wantJSON(args)
	dry := wantDryRun(args)

	cfg, err := loadConfig(flagVault(args))
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		os.Exit(2)
	}

	current := filepath.Join(cfg.BuildRoot, "current")
	if st, err := os.Stat(current); err != nil || !st.IsDir() {
		res.AddError("no local build; run: x3vault build", current)
		emit(res, jsonOut)
		os.Exit(3)
	}

	t := syncTransport(cfg)
	st, err := t.Status()
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		os.Exit(4)
	}
	fmt.Fprintf(os.Stderr, "device: %s  firmware: %s  mode: %s\n", st.Device, st.Version, st.Mode)

	opts := syncOpts(cfg)
	plan, err := sync.BuildPlan(t, opts.DeviceRoot, current, opts)
	if err != nil {
		res.AddError(err.Error(), cfg.Device.Root)
		emit(res, jsonOut)
		os.Exit(5)
	}

	fileDeletes, dirDeletes := 0, 0
	for _, op := range plan.Deletes {
		if op.Type == "directory" {
			dirDeletes++
		} else {
			fileDeletes++
		}
	}
	fmt.Fprintf(os.Stderr, "plan: %d uploads, %d file deletes, %d dir deletes\n",
		len(plan.Uploads), fileDeletes, dirDeletes)
	if dry {
		fmt.Fprintln(os.Stderr, "dry-run:")
	}

	sr := sync.ApplyPlan(t, plan, current, dry, opts)
	res.Summary.Notes = sr.Uploaded
	for _, e := range sr.Errors {
		res.AddError(e, "")
	}

	if dry {
		fmt.Fprintf(os.Stderr, "dry-run complete (no changes applied)\n")
	} else {
		fmt.Fprintf(os.Stderr, "sync complete: uploaded %d, deleted %d\n", sr.Uploaded, sr.Deleted)
	}

	if !res.OK {
		fmt.Fprintln(os.Stderr, "sync failed (fail-fast; device may be partially updated)")
		emit(res, jsonOut)
		os.Exit(4)
	}
	emit(res, jsonOut)
}

func runDoctor(args []string) {
	res := contract.NewResult("doctor")
	jsonOut := wantJSON(args)

	cfg, err := loadConfig(flagVault(args))
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		os.Exit(2)
	}

	disc, err := vault.Discover(cfg.VaultRoot, cfg.SourceRoot, cfg.Wiki)
	if err != nil {
		res.AddError(err.Error(), cfg.SourceDir())
		emit(res, jsonOut)
		os.Exit(3)
	}

	res.Summary.Notes = len(disc.Notes)
	fmt.Fprintf(os.Stderr, "vault:   %s\n", cfg.VaultRoot)
	fmt.Fprintf(os.Stderr, "source:  %s\n", disc.SourceRoot)
	fmt.Fprintf(os.Stderr, "notes:   %d\n", len(disc.Notes))
	fmt.Fprintf(os.Stderr, "dirs:    allowed=%d ignored=%d\n", len(cfg.Wiki.Allowed), len(cfg.Wiki.Ignored))
	fmt.Fprintf(os.Stderr, "build:   %s\n", cfg.BuildRoot)
	printBuildDirStatus(cfg.BuildRoot)
	fmt.Fprintf(os.Stderr, "device:  %s  root=%s  timeout=%s\n", cfg.Device.BaseURL, cfg.DeviceRoot(), cfg.DeviceTimeout())
	fmt.Fprintf(os.Stderr, "sync:    fail_fast=%v hash_manifest=%v clean_empty_dirs=%v\n",
		cfg.Sync.FailFast, cfg.Sync.HashManifest, cfg.Sync.CleanEmptyDirs)
	fmt.Fprintf(os.Stderr, "exclude: %s\n", strings.Join(cfg.Sync.ExcludeVaultPaths, ", "))

	t := syncTransport(cfg)
	if st, err := t.Status(); err != nil {
		fmt.Fprintf(os.Stderr, "device:  unreachable (%s)\n", strings.TrimSpace(err.Error()))
	} else {
		fmt.Fprintf(os.Stderr, "device:  online %s/%s heap=%d\n", st.Device, st.Version, st.FreeHeap)
		owned, _ := sync.HasOwnership(t, cfg.DeviceRoot())
		fmt.Fprintf(os.Stderr, "owned:   %v\n", owned)
	}

	emit(res, jsonOut)
}

func runStatus(args []string) {
	runDoctor(args)
}

func syncTransport(cfg *config.Config) *sync.Transport {
	return sync.NewTransport(cfg.Device.BaseURL, cfg.DeviceTimeout())
}

func syncOpts(cfg *config.Config) sync.Options {
	return sync.OptionsFromConfig(cfg)
}

func printBuildDirStatus(buildRoot string) {
	current := filepath.Join(buildRoot, "current")
	backup := filepath.Join(buildRoot, "backup")
	if st, err := os.Stat(current); err == nil && st.IsDir() {
		fmt.Fprintf(os.Stderr, "current: %s\n", current)
	} else {
		fmt.Fprintf(os.Stderr, "current: (none)\n")
	}
	if st, err := os.Stat(backup); err == nil && st.IsDir() {
		fmt.Fprintf(os.Stderr, "backup:  %s\n", backup)
	} else {
		fmt.Fprintf(os.Stderr, "backup:  (none)\n")
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
		_ = enc.Encode(res)
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

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
