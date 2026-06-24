// Command gist is the Gist MCP server entry point.
//
// It speaks JSON-RPC 2.0 over stdio and exposes six tools for LLM context
// optimization: view_file_slim, enforce_budget, align_context_cache,
// fetch_diff_context, squeeze_context, and report_savings.
//
// Usage:
//
//	gist                    Run as MCP server (stdio JSON-RPC)
//	gist --version          Print version
//	gist --help             Print this help
//	gist config             Print the resolved config file path
//	gist init               Write default config to disk
//	gist wrap [opts] -- <cmd> [args...]
//	                        Run <cmd> with transparent I/O capture
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elbader17/gist/pkg/budget"
	"github.com/elbader17/gist/pkg/cache"
	"github.com/elbader17/gist/pkg/config"
	"github.com/elbader17/gist/pkg/mcp"
	"github.com/elbader17/gist/pkg/metrics"
)

func main() {
	if code := run(os.Args, os.Stdin, os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

func run(args []string, stdin, stdout, stderr *os.File) int {
	if len(args) > 1 {
		switch args[1] {
		case "--version", "-v":
			fmt.Fprintln(stdout, "gist", mcp.ServerVersion)
			return 0
		case "--help", "-h":
			printHelp(stdout)
			return 0
		case "config":
			path, err := config.ConfigPath()
			if err != nil {
				fmt.Fprintln(stderr, "gist:", err)
				return 1
			}
			fmt.Fprintln(stdout, path)
			return 0
		case "init":
			cfg := config.Default()
			if err := cfg.Save(); err != nil {
				fmt.Fprintln(stderr, "gist:", err)
				return 1
			}
			path, err := config.ConfigPath()
			if err != nil {
				fmt.Fprintln(stderr, "gist:", err)
				return 1
			}
			fmt.Fprintln(stdout, "config written to", path)
			return 0
		case "wrap":
			return runWrap(args[2:], WrapOptions{
				Stdin:  stdin,
				Stdout: stdout,
				Stderr: stderr,
			})
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "gist: load config:", err)
		return 1
	}

	store, err := budget.NewStore()
	if err != nil {
		fmt.Fprintln(stderr, "gist: open sessions store:", err)
		return 1
	}

	flusher := budget.NewFlusher(store, 2*time.Second)
	defer flusher.Stop()

	b := budget.NewBudget(budget.Options{
		LoopThreshold:         cfg.LoopDetectionThreshold,
		MaxCostUSD:            cfg.MaxSessionCostUSD,
		MaxTokens:             cfg.MaxSessionTokens,
		PromptPricePerMillion: cfg.Pricing.PromptPerMillion,
		CostFn:                cfg.CostForTokens,
		Store:                 store,
		Flusher:               flusher,
	})

	astCache := cache.New(cfg.CacheMaxEntries, cfg.CacheMaxBytes)

	metricsDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintln(stderr, "gist: resolve metrics dir:", err)
		return 1
	}
	metricsPath := filepath.Join(metricsDir, "metrics.json")
	recorder := metrics.NewRecorder(metricsPath)
	defer recorder.Stop()

	dispatcher := &mcp.Dispatcher{
		Cfg:     cfg,
		Budget:  b,
		Cache:   astCache,
		Metrics: recorder,
	}
	server := mcp.NewServer(stdin, stdout, mcp.DefaultTools(), dispatcher.Handle)
	if err := server.Run(); err != nil {
		fmt.Fprintln(stderr, "gist: server:", err)
		return 1
	}
	return 0
}

func runWrap(args []string, opts WrapOptions) int {
	opts.Stdin = nil
	opts.Stdout = nil
	opts.Stderr = nil
	command, cmdArgs, wrapOpts, err := parseWrapArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gist wrap:", err)
		return 1
	}
	wrapOpts.Stdin = os.Stdin
	wrapOpts.Stdout = os.Stdout
	wrapOpts.Stderr = os.Stderr
	return RunWrap(command, cmdArgs, wrapOpts)
}

func parseWrapArgs(args []string) (string, []string, WrapOptions, error) {
	opts := WrapOptions{}
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--":
			args = args[1:]
			goto done
		case "--dir":
			if len(args) < 2 {
				return "", nil, opts, fmt.Errorf("--dir requires a path argument")
			}
			opts.Dir = args[1]
			args = args[2:]
		case "--quiet", "-q":
			opts.Quiet = true
			args = args[1:]
		default:
			return "", nil, opts, fmt.Errorf("unknown flag %q", args[0])
		}
	}
done:
	if len(args) == 0 {
		return "", nil, opts, fmt.Errorf("missing command after `--`")
	}
	return args[0], args[1:], opts, nil
}

func printHelp(w *os.File) {
	fmt.Fprintln(w, "gist - Gist MCP server for context optimization")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gist                      Run as MCP server (stdio JSON-RPC)")
	fmt.Fprintln(w, "  gist --version            Print version")
	fmt.Fprintln(w, "  gist --help               Print this help")
	fmt.Fprintln(w, "  gist config               Print the resolved config file path")
	fmt.Fprintln(w, "  gist init                 Write default config to disk")
	fmt.Fprintln(w, "  gist wrap [opts] -- CMD    Run CMD with transparent I/O capture")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Wrap options:")
	fmt.Fprintln(w, "  --dir <path>    Override capture directory")
	fmt.Fprintln(w, "  --quiet, -q     Suppress informational messages")
}