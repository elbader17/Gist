// Command gist is the Gist MCP server entry point.
//
// It speaks JSON-RPC 2.0 over stdio and exposes four tools for LLM context
// optimization: view_file_slim, enforce_budget, align_context_cache, and
// fetch_diff_context.
//
// Usage:
//
//	gist              Run as MCP server (stdio JSON-RPC)
//	gist --version    Print version
//	gist --help       Print this help
//	gist config       Print the resolved config file path
//	gist init         Write default config to disk
package main

import (
	"fmt"
	"os"

	"github.com/elbader17/gist/pkg/budget"
	"github.com/elbader17/gist/pkg/config"
	"github.com/elbader17/gist/pkg/mcp"
)

func main() {
	if err := run(os.Args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "gist:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin, stdout, stderr *os.File) error {
	if len(args) > 1 {
		switch args[1] {
		case "--version", "-v":
			fmt.Fprintln(stdout, "gist", mcp.ServerVersion)
			return nil
		case "--help", "-h":
			printHelp(stdout)
			return nil
		case "config":
			path, err := config.ConfigPath()
			if err != nil {
				return err
			}
			fmt.Fprintln(stdout, path)
			return nil
		case "init":
			cfg := config.Default()
			if err := cfg.Save(); err != nil {
				return err
			}
			path, err := config.ConfigPath()
			if err != nil {
				return err
			}
			fmt.Fprintln(stdout, "config written to", path)
			return nil
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store, err := budget.NewStore()
	if err != nil {
		return fmt.Errorf("open sessions store: %w", err)
	}

	b := budget.NewBudget(budget.Options{
		LoopThreshold:         cfg.LoopDetectionThreshold,
		MaxCostUSD:            cfg.MaxSessionCostUSD,
		MaxTokens:             cfg.MaxSessionTokens,
		PromptPricePerMillion: cfg.Pricing.PromptPerMillion,
		CostFn:                cfg.CostForTokens,
		Store:                 store,
	})

	dispatcher := &mcp.Dispatcher{Cfg: cfg, Budget: b}
	server := mcp.NewServer(stdin, stdout, mcp.DefaultTools(), dispatcher.Handle)
	return server.Run()
}

func printHelp(w *os.File) {
	fmt.Fprintln(w, "gist - Gist MCP server for context optimization")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gist              Run as MCP server (stdio JSON-RPC)")
	fmt.Fprintln(w, "  gist --version    Print version")
	fmt.Fprintln(w, "  gist --help       Print this help")
	fmt.Fprintln(w, "  gist config       Print the resolved config file path")
	fmt.Fprintln(w, "  gist init         Write default config to disk")
}