// Package tools exposes selected xray-core CLI generators under
// "XrayR tools …" so the node binary can produce REALITY / ML-DSA /
// VLESS Encryption material without a separate xray binary.
package tools

import (
	"fmt"
	"os"
	"strings"

	// Register protocol modules and xray-core CLI commands (x25519, mldsa65, vlessenc, …).
	_ "github.com/HenZenKuriRIP/XrayR4u/main/distro/all"
	"github.com/xtls/xray-core/main/commands/base"
)

// toolNames are the generators we surface. Other xray subcommands (api, run, …)
// stay hidden to avoid confusing this panel-backend binary with full xray.
var toolNames = map[string]bool{
	"x25519":   true,
	"mldsa65":  true,
	"mlkem768": true,
	"vlessenc": true,
	"uuid":     true,
	"help":     true,
}

// IsToolsInvocation reports whether os.Args should enter tools mode
// instead of starting the panel service.
func IsToolsInvocation(args []string) bool {
	if len(args) < 2 {
		return false
	}
	a := args[1]
	if a == "tools" || a == "tool" {
		return true
	}
	// Allow: XrayR x25519 | XrayR vlessenc | …
	return toolNames[a]
}

// Run dispatches generator commands. args is the full os.Args slice.
// Supported forms:
//
//	XrayR tools
//	XrayR tools x25519
//	XrayR tools mldsa65
//	XrayR tools vlessenc
//	XrayR tools mlkem768
//	XrayR tools uuid
//	XrayR x25519 | mldsa65 | vlessenc | …   (short form)
func Run(args []string) {
	base.CommandEnv.Exec = "XrayR tools"

	// Strip program name.
	rest := args[1:]
	if len(rest) > 0 && (rest[0] == "tools" || rest[0] == "tool") {
		rest = rest[1:]
	}

	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		printHelp()
		return
	}

	name := rest[0]
	if !toolNames[name] {
		fmt.Fprintf(os.Stderr, "unknown tools command %q\n\n", name)
		printHelp()
		os.Exit(2)
	}

	cmd := findCommand(name)
	if cmd == nil || !cmd.Runnable() {
		fmt.Fprintf(os.Stderr, "command %q is not available in this build\n", name)
		os.Exit(2)
	}

	// Parse command-local flags (e.g. mldsa65 -i seed).
	cmd.Flag.Usage = func() { cmd.Usage() }
	if err := cmd.Flag.Parse(rest[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "flag error: %v\n", err)
		os.Exit(2)
	}
	cmd.Run(cmd, cmd.Flag.Args())
}

func findCommand(name string) *base.Command {
	for _, cmd := range base.RootCommand.Commands {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

func printHelp() {
	exec := base.CommandEnv.Exec
	fmt.Printf(`%s — generate keys / PQ material (same logic as xray-core CLI)

Usage:
  XrayR tools <command> [flags]
  XrayR <command> [flags]          # short form

Commands:
  x25519     REALITY / VLESS Encryption X25519 key pair
  mldsa65    REALITY ML-DSA-65 Seed + Verify (post-quantum signature)
  mlkem768   ML-KEM-768 seed / client material
  vlessenc   VLESS Encryption decryption + encryption pair (recommended for PQ payload)
  uuid       Generate UUID
  help       Show this help

Examples:
  XrayR tools x25519
  XrayR tools mldsa65
  XrayR tools vlessenc
  XrayR tools mldsa65 -i "<seed>"

Notes:
  • Use the same XrayR / xray-core version on node and for generation.
  • Panel may also generate REALITY keys; paste Seed into server, Verify into subscription.
  • For vlessenc: put "decryption" on server (config / panel), "encryption" on client.
`, exec)

	// List registered tool commands with short descriptions when available.
	fmt.Println("Details:")
	for _, cmd := range base.RootCommand.Commands {
		n := cmd.Name()
		if !toolNames[n] || n == "help" {
			continue
		}
		short := strings.TrimSpace(cmd.Short)
		if short == "" {
			short = n
		}
		fmt.Printf("  %-10s %s\n", n, short)
	}
}
