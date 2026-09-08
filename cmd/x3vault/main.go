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
	fmt.Fprint(os.Stderr, `x3vault — slim v0: build + exact-mirror sync of Obsidian wiki/ to XTEINK X3

Usage:
  x3vault init --vault PATH
  x3vault build [--vault PATH]
  x3vault device init [--vault PATH]
  x3vault sync [--vault PATH] [--dry-run]
  x3vault doctor [--vault PATH]
  x3vault status [--vault PATH]
  x3vault config dirs [--vault PATH]
  x3vault config dirs restore [--vault PATH]
  x3vault config dirs allow DIR... [--vault PATH]
  x3vault config dirs unallow DIR... [--vault PATH]
  x3vault config dirs ignore DIR... [--vault PATH]
  x3vault config dirs unignore DIR... [--vault PATH]

Options:
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
	return filepath.Join(vault, ".x3vault.yaml")
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
	cfgPath := filepath.Join(abs, ".x3vault.yaml")
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Fprintf(os.Stderr, "config already exists: %s\n", cfgPath)
		os.Exit(0)
	}

	wiki := filepath.Join(abs, "wiki")
	if st, err := os.Stat(wiki); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "wiki/ not found under %s\n", abs)
		os.Exit(2)
	}

	if err := config.WriteDefault(cfgPath); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", cfgPath)
	fmt.Fprintf(os.Stderr, "source: %s\n", wiki)
	fmt.Fprint(os.Stderr, config.FormatWikiDirsSummary(config.DefaultWikiDirs()))
}

func runConfig(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: x3vault config dirs ...")
		os.Exit(2)
	}
	if args[0] != "dirs" {
		fmt.Fprintf(os.Stderr, "unknown config command: %s\n", args[0])
		os.Exit(2)
	}
	runConfigDirs(args[1:])
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
		fmt.Fprintf(os.Stderr, "added allowed dirs: %s\n", strings.Join(dirArgs, ", "))
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

	br, err := build.Run(cfg.VaultRoot, cfg.SourceRoot, cfg.BuildRoot, disc)
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

	t := sync.NewTransport(cfg.Device.BaseURL)
	st, err := t.Status()
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		os.Exit(4)
	}
	fmt.Fprintf(os.Stderr, "device: %s  firmware: %s  mode: %s  ip: %s\n", st.Device, st.Version, st.Mode, st.IP)

	if err := sync.DeviceInit(t, cfg.Device.Root); err != nil {
		res.AddError(err.Error(), cfg.Device.Root)
		emit(res, jsonOut)
		os.Exit(5)
	}
	fmt.Fprintf(os.Stderr, "ownership marker written at %s/_meta/ownership.json\n", cfg.Device.Root)
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

	t := sync.NewTransport(cfg.Device.BaseURL)
	st, err := t.Status()
	if err != nil {
		res.AddError(err.Error(), "")
		emit(res, jsonOut)
		os.Exit(4)
	}
	fmt.Fprintf(os.Stderr, "device: %s  firmware: %s  mode: %s\n", st.Device, st.Version, st.Mode)

	plan, err := sync.BuildPlan(t, cfg.Device.Root, current)
	if err != nil {
		res.AddError(err.Error(), cfg.Device.Root)
		emit(res, jsonOut)
		os.Exit(5)
	}

	fmt.Fprintf(os.Stderr, "plan: %d uploads, %d deletes\n", len(plan.Uploads), len(plan.Deletes))
	if dry {
		fmt.Fprintln(os.Stderr, "dry-run:")
	}

	sr := sync.ApplyPlan(t, plan, current, dry)
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
	fmt.Fprintf(os.Stderr, "device:  %s%s\n", cfg.Device.BaseURL, cfg.Device.Root)

	t := sync.NewTransport(cfg.Device.BaseURL)
	if st, err := t.Status(); err != nil {
		fmt.Fprintf(os.Stderr, "device:  unreachable (%s)\n", strings.TrimSpace(err.Error()))
	} else {
		fmt.Fprintf(os.Stderr, "device:  online %s/%s heap=%d\n", st.Device, st.Version, st.FreeHeap)
		owned, _ := sync.HasOwnership(t, cfg.Device.Root)
		fmt.Fprintf(os.Stderr, "owned:   %v\n", owned)
	}

	emit(res, jsonOut)
}

func runStatus(args []string) {
	runDoctor(args)
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
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
