package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/plugin"
	"github.com/avifenesh/eigen/internal/skill"
)

// runMarketplaceCmd implements `eigen marketplace <add|list|remove|delete|enable|disable|update>`.
func runMarketplaceCmd(args []string) {
	reg, err := plugin.NewRegistry()
	if err != nil {
		fail(fmt.Errorf("marketplace: %w", err))
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: eigen marketplace <add|list|remove|delete|enable|disable|update> …")
		os.Exit(2)
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: eigen marketplace add <owner/repo[/subdir][@ref] | url | local-dir | marketplace.json-url>")
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		mkt, rec, err := reg.AddMarketplace(ctx, args[1], nil)
		if err != nil {
			fail(fmt.Errorf("marketplace add: %w", err))
		}
		fmt.Printf("✓ added marketplace %q (%d plugin(s)) from %s\n", rec.Name, len(mkt.Plugins), rec.Source)
		for _, p := range mkt.Plugins {
			fmt.Printf("  %-24s %s\n", p.Name, p.Description)
		}
		fmt.Printf("install one with: eigen plugin install <name>\n")
	case "list":
		markets, err := reg.Markets()
		if err != nil {
			fail(err)
		}
		if len(markets) == 0 {
			fmt.Println("no marketplaces added (eigen marketplace add <owner/repo|url|local-dir>)")
			return
		}
		for _, m := range markets {
			when := m.Added.Format("2006-01-02")
			state := "enabled"
			if m.Disabled {
				state = "disabled"
			}
			fmt.Printf("%-20s %-8s %s  (added %s)\n", m.Name, state, m.Source, when)
		}
	case "remove", "rm", "delete", "del":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: eigen marketplace remove/delete <name>")
			os.Exit(2)
		}
		ok, err := reg.RemoveMarket(args[1])
		if err != nil {
			fail(err)
		}
		if !ok {
			fmt.Printf("no marketplace %q\n", args[1])
			return
		}
		fmt.Printf("deleted marketplace %q (installed plugins are unaffected)\n", args[1])
	case "enable", "disable":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "usage: eigen marketplace %s <name>\n", args[0])
			os.Exit(2)
		}
		enable := args[0] == "enable"
		ok, err := reg.SetMarketEnabled(args[1], enable)
		if err != nil {
			fail(err)
		}
		if !ok {
			fmt.Printf("no marketplace %q\n", args[1])
			return
		}
		state := "disabled"
		if enable {
			state = "enabled"
		}
		fmt.Printf("%s marketplace %q\n", state, args[1])
	case "update", "refresh":
		// Re-fetch each catalog to surface new plugins/versions. We don't cache
		// the catalog, so "update" just re-validates it's reachable + reports
		// counts (installs always fetch fresh).
		markets, err := reg.Markets()
		if err != nil {
			fail(err)
		}
		if len(markets) == 0 {
			fmt.Println("no marketplaces to update")
			return
		}
		for _, m := range markets {
			if m.Disabled {
				fmt.Printf("- %-20s disabled\n", m.Name)
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			mkt, _, aerr := reg.AddMarketplace(ctx, m.Source, nil)
			cancel()
			if aerr != nil {
				fmt.Printf("✗ %-20s %v\n", m.Name, aerr)
				continue
			}
			fmt.Printf("✓ %-20s %d plugin(s)\n", m.Name, len(mkt.Plugins))
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown marketplace subcommand %q (want: add | list | remove/delete | enable | disable | update)\n", args[0])
		os.Exit(2)
	}
}

// runPluginCmd implements `eigen plugin <install|list|remove|delete|enable|disable>`.
func runPluginCmd(args []string, provider, model string) {
	reg, err := plugin.NewRegistry()
	if err != nil {
		fail(fmt.Errorf("plugin: %w", err))
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: eigen plugin <install|list|remove|delete|enable|disable> …")
		os.Exit(2)
	}
	switch args[0] {
	case "install", "add":
		src, rest := splitSource(args[1:])
		fs := flag.NewFlagSet("plugin install", flag.ExitOnError)
		mkt := fs.String("marketplace", "", "which marketplace to install from (default: any that lists it)")
		force := fs.Bool("force", false, "install even if the security scan flags it")
		overwrite := fs.Bool("overwrite", false, "replace an already-installed plugin")
		noScan := fs.Bool("no-scan", false, "skip the vulnerability scan (not recommended)")
		_ = fs.Parse(rest)
		if src == "" {
			fmt.Fprintln(os.Stderr, "usage: eigen plugin install <name> [--marketplace M] [--force] [--overwrite] [--no-scan]")
			os.Exit(2)
		}
		opts := plugin.InstallOptions{Force: *force, Overwrite: *overwrite}
		if !*noScan {
			prov, err := llm.New(provider, model)
			if err != nil {
				fail(fmt.Errorf("plugin install: %w", err))
			}
			opts.Scanner = skill.ProviderScanner{P: smallProvider(prov)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		res, err := reg.InstallPlugin(ctx, src, *mkt, opts)
		if err != nil {
			fail(fmt.Errorf("plugin install: %w", err))
		}
		printInstallResult(res, opts.Scanner != nil)
	case "list":
		installed, err := reg.Installed()
		if err != nil {
			fail(err)
		}
		if len(installed) == 0 {
			fmt.Println("no plugins installed (eigen plugin install <name>)")
			return
		}
		for _, p := range installed {
			fmt.Printf("%-20s %s\n", p.Name, p.Description)
			fmt.Printf("    %d skill(s), %d agent(s), %d command(s), %d mcp server(s), %d hook(s)", len(p.Skills), len(p.Agents), len(p.Commands), len(p.MCPServers), p.Hooks)
			if p.Marketplace != "" {
				fmt.Printf("  · from %s", p.Marketplace)
			}
			fmt.Println()
		}
	case "remove", "rm", "uninstall", "delete", "del":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: eigen plugin remove/delete <name>")
			os.Exit(2)
		}
		ok, err := reg.Uninstall(args[1])
		if err != nil {
			fail(err)
		}
		if !ok {
			fmt.Printf("no plugin %q installed\n", args[1])
			return
		}
		fmt.Printf("deleted plugin %q (skills, agents, commands, mcp servers, hooks, and bundle)\n", args[1])
	case "enable", "disable":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "usage: eigen plugin %s <name>\n", args[0])
			os.Exit(2)
		}
		enable := args[0] == "enable"
		ok, err := reg.SetEnabled(args[1], enable)
		if err != nil {
			fail(err)
		}
		if !ok {
			fmt.Printf("no plugin %q installed\n", args[1])
			return
		}
		state := "disabled"
		if enable {
			state = "enabled"
		}
		fmt.Printf("%s plugin %q (applies to NEW sessions)\n", state, args[1])
	default:
		fmt.Fprintf(os.Stderr, "unknown plugin subcommand %q (want: install | list | remove/delete | enable | disable)\n", args[0])
		os.Exit(2)
	}
}

func printInstallResult(res *plugin.InstallResult, scanned bool) {
	p := res.Plugin
	if len(res.Scans) > 0 {
		fmt.Printf("⚠ installed %q despite scan flags:\n", p.Name)
		for _, s := range res.Scans {
			fmt.Printf("  [%s]\n", s.Component)
			for _, r := range s.Reasons {
				fmt.Println("    - " + r)
			}
		}
	} else if scanned {
		fmt.Printf("✓ scan clean — installed %q\n", p.Name)
	} else {
		fmt.Printf("installed %q (scan skipped)\n", p.Name)
	}
	fmt.Printf("  %d skill(s), %d agent(s), %d command(s), %d mcp server(s), %d hook(s)\n", len(p.Skills), len(p.Agents), len(p.Commands), len(p.MCPServers), p.Hooks)
	for _, w := range res.Warnings {
		fmt.Println("  note: " + w)
	}
	if len(p.MCPServers) > 0 {
		fmt.Println("  MCP servers are niche (gated) — the agent unlocks them via search_tools.")
	}
}
