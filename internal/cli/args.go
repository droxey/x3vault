package cli

import (
	"fmt"
	"strings"
)

// validateArgs checks the whole invocation before a command can touch a vault or
// device. In particular, misspelled dry-run flags must never start a live sync.
func validateArgs(args []string) ([]string, bool, error) {
	if len(args) < 2 {
		return args, false, fmt.Errorf("a command is required")
	}
	cmd := args[1]
	i := 2
	jsonAllowed, dryAllowed, vaultAllowed := false, false, true
	dirsRequired := false
	switch cmd {
	case "init":
	case "build", "doctor", "status":
		jsonAllowed = true
	case "sync":
		jsonAllowed, dryAllowed = true, true
	case "device":
		if i >= len(args) || args[i] != "init" {
			if i < len(args) && isHelp(args[i]) {
				return args, true, nil
			}
			return nil, false, fmt.Errorf("usage: x3vault device init [--vault PATH] [--json]")
		}
		i++
		jsonAllowed = true
	case "config":
		if i >= len(args) {
			return nil, false, fmt.Errorf("usage: x3vault config show|restore|dirs")
		}
		switch args[i] {
		case "show", "restore":
			i++
		case "dirs":
			i++
			if i < len(args) && !strings.HasPrefix(args[i], "-") {
				switch args[i] {
				case "restore":
				case "allow", "unallow", "ignore", "unignore":
					dirsRequired = true
				default:
					return nil, false, fmt.Errorf("unknown config dirs command: %s", args[i])
				}
				i++
			}
		default:
			if isHelp(args[i]) {
				return args, true, nil
			}
			return nil, false, fmt.Errorf("unknown config command: %s", args[i])
		}
	case "version", "-version", "--version", "help", "-h", "--help":
		vaultAllowed = false
	default:
		return nil, false, fmt.Errorf("unknown command: %s", cmd)
	}
	validated := append([]string(nil), args[:i]...)
	seen := map[string]bool{}
	dirCount := 0
	help := false
	for ; i < len(args); i++ {
		a := args[i]
		if isHelp(a) {
			help = true
			continue
		}
		if strings.HasPrefix(a, "--vault=") {
			value := strings.TrimPrefix(a, "--vault=")
			if !vaultAllowed || seen["--vault"] || value == "" || strings.HasPrefix(value, "-") {
				return nil, false, fmt.Errorf("invalid or repeated --vault")
			}
			seen["--vault"] = true
			validated = append(validated, "--vault", strings.TrimPrefix(a, "--vault="))
			continue
		}
		switch a {
		case "--vault":
			if !vaultAllowed || seen[a] || i+1 == len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
				return nil, false, fmt.Errorf("--vault requires one PATH and may be specified once")
			}
			seen[a] = true
			i++
			validated = append(validated, a, args[i])
		case "--json", "--dry-run":
			if seen[a] || (a == "--json" && !jsonAllowed) || (a == "--dry-run" && !dryAllowed) {
				return nil, false, fmt.Errorf("unsupported or repeated flag: %s", a)
			}
			seen[a] = true
			validated = append(validated, a)
		default:
			if !dirsRequired || strings.HasPrefix(a, "-") || strings.TrimSpace(a) == "" {
				return nil, false, fmt.Errorf("unexpected argument: %s", a)
			}
			dirCount++
			validated = append(validated, a)
		}
	}
	if dirsRequired && dirCount == 0 && !help {
		return nil, false, fmt.Errorf("directory command requires at least one directory")
	}
	return validated, help, nil
}

func isHelp(arg string) bool { return arg == "--help" || arg == "-h" }
