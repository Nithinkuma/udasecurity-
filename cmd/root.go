// Package cmd wires together the CLI interface using cobra.
//
// Why cobra over the stdlib "flag" package?
//   - Auto-generates --help with usage, flag descriptions, and examples
//   - Supports persistent flags (inherited by sub-commands like "models")
//   - Handles flag validation and type conversion with good error messages
//   - Used by kubectl, helm, docker, and most cloud-native Go CLIs — users
//     expect this UX (--flag=value, -f value, positional args, --help)
//   - Sub-command support is built-in, making future expansion (opsmate fix,
//     opsmate explain, opsmate watch) straightforward
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"opsmate/agent"
	"opsmate/config"
)

// cfg is populated from CLI flags and env vars before RunE executes.
var cfg config.Config

// rootCmd is the top-level "opsmate" command.
// Positional arguments form the problem statement so the user can type:
//
//	opsmate why are my pods crashing in namespace frontend
//
// without quoting, which is friendlier in shell scripts and muscle memory.
var rootCmd = &cobra.Command{
	Use:   "opsmate [flags] <problem description...>",
	Short: "AI-powered SRE agent for Kubernetes, Docker, and Linux",
	Long: colorBold + `
  ██████╗ ██████╗ ███████╗███╗   ███╗ █████╗ ████████╗███████╗
 ██╔═══██╗██╔══██╗██╔════╝████╗ ████║██╔══██╗╚══██╔══╝██╔════╝
 ██║   ██║██████╔╝███████╗██╔████╔██║███████║   ██║   █████╗
 ██║   ██║██╔═══╝ ╚════██║██║╚██╔╝██║██╔══██║   ██║   ██╔══╝
 ╚██████╔╝██║     ███████║██║ ╚═╝ ██║██║  ██║   ██║   ███████╗
  ╚═════╝ ╚═╝     ╚══════╝╚═╝     ╚═╝╚═╝  ╚═╝   ╚═╝   ╚══════╝
` + colorReset + `
opsmate is an SRE agent that uses locally-running LLMs (via Ollama) to
diagnose infrastructure issues through an iterative command-execute-analyze loop.

You describe a problem in plain English. opsmate generates diagnostic commands
(kubectl, docker, linux), executes them, feeds the results back to the model,
and repeats until it has a root-cause analysis and remediation plan.

` + colorBold + `WORKFLOW:` + colorReset + `
  User describes problem
       ↓
  LLM decides which command to run
       ↓
  opsmate shows command + asks confirmation (unless -y)
       ↓
  Command executes, output sent back to LLM
       ↓
  LLM analyzes output, runs next command or concludes
       ↓
  Final diagnosis: root cause, evidence, remediation, prevention

` + colorBold + `PREREQUISITES:` + colorReset + `
  1. Install Ollama:  https://ollama.com  (or just run opsmate — it will guide you)
  2. Pull a model:    ollama pull gemma4  (opsmate will offer to do this too)
  3. Run opsmate:     opsmate "why are my pods crashing in namespace myapp"

` + colorBold + `EXAMPLES:` + colorReset + `
  # Kubernetes pod investigation
  opsmate "why are pods in namespace payments failing to start?"
  opsmate -n production "the checkout deployment has 0/3 pods ready"
  opsmate --model deepseek-r1:8b "investigate OOMKilled pods in namespace api"

  # Docker investigation
  opsmate "my nginx container keeps restarting, check what's wrong"

  # Linux system investigation
  opsmate "disk usage is at 95% on the web server, find what's consuming space"

  # Skip confirmation for scripted use
  opsmate -y "check status of all deployments in namespace staging"

  # Audit mode: see what would run without executing
  opsmate --dry-run "diagnose high CPU usage in the worker nodes"

  # Use a remote Ollama instance (GPU server)
  opsmate --endpoint http://gpu-server:11434 --model deepseek-r1:32b "..."`,

	// MinimumNArgs(1): require at least one word of problem description.
	// Error message is printed by cobra automatically on violation.
	Args: cobra.MinimumNArgs(1),

	// SilenceUsage prevents cobra from printing the full usage block on every
	// RunE error — it would bury the actual error message.
	SilenceUsage: true,

	RunE: func(cmd *cobra.Command, args []string) error {
		// Join all positional args as the problem description.
		// This lets the user avoid quoting:
		//   opsmate why is pod frontend crashing
		// vs the less ergonomic:
		//   opsmate "why is pod frontend crashing"
		problem := strings.Join(args, " ")

		a := agent.NewAgent(cfg)
		return a.Investigate(problem)
	},
}

// modelsCmd is a convenience sub-command that lists recommended models and
// how to pull them. Avoids users having to look up the Ollama documentation.
var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Show recommended Ollama models for SRE work",
	Long:  "List recommended LLM models for opsmate and how to pull them via Ollama.",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(colorBold + "\nRecommended models for opsmate SRE investigations:" + colorReset)
		fmt.Println()

		models := []struct {
			name, pull, notes string
		}{
			{
				"gemma4",
				"ollama pull gemma4",
				"Default model. Latest Gemma generation — great balance of speed and quality. ~5GB VRAM.",
			},
			{
				"deepseek-r1:8b",
				"ollama pull deepseek-r1:8b",
				"Best reasoning. Excellent root-cause analysis. ~5GB VRAM.",
			},
			{
				"llama3.2",
				"ollama pull llama3.2",
				"Fast. Native tool-calling. Good for quick checks. ~2GB VRAM.",
			},
			{
				"mistral",
				"ollama pull mistral",
				"Reliable at following structured output format. ~4GB VRAM.",
			},
			{
				"qwen2.5:7b",
				"ollama pull qwen2.5:7b",
				"Strong instruction following. Good on limited hardware. ~4GB VRAM.",
			},
			{
				"deepseek-r1:32b",
				"ollama pull deepseek-r1:32b",
				"Best quality for complex multi-service issues. Needs 20GB+ VRAM.",
			},
		}

		for _, m := range models {
			fmt.Printf("  %s%-22s%s  %s\n", colorBold+colorCyan, m.name, colorReset, m.notes)
			fmt.Printf("  %s  $ %s%s\n\n", colorDim, m.pull, colorReset)
		}

		fmt.Printf("Set model via flag:   %sopsmate --model deepseek-r1:8b \"...\" %s\n", colorYellow, colorReset)
		fmt.Printf("Set model via env:    %sexport OPSMATE_MODEL=deepseek-r1:8b%s\n\n", colorYellow, colorReset)
		return nil
	},
}

// ANSI codes for the long description formatting.
// These are defined here locally (not from agent package) to avoid
// package-level import cycles. The values are identical.
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
)

// Execute is called from main() to kick off the cobra command tree.
// It handles flag parsing, sub-command routing, and error display.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		// cobra already printed the error; exit with code 1.
		os.Exit(1)
	}
}

func init() {
	defaults := config.DefaultConfig()

	// ── Persistent flags (apply to all sub-commands) ──────────────────────

	// Endpoint and model are the most commonly overridden settings.
	// We support both env vars (for shell profiles) and flags (for one-off use).
	rootCmd.PersistentFlags().StringVarP(
		&cfg.OllamaEndpoint, "endpoint", "e",
		envOrDefault("OPSMATE_ENDPOINT", defaults.OllamaEndpoint),
		"Ollama API server URL (env: OPSMATE_ENDPOINT)",
	)
	rootCmd.PersistentFlags().StringVarP(
		&cfg.Model, "model", "m",
		envOrDefault("OPSMATE_MODEL", defaults.Model),
		"Ollama model to use — must be pulled first (env: OPSMATE_MODEL)",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&cfg.Verbose, "verbose", "v",
		false,
		"Enable debug output (API payloads, extraction method, timing)",
	)

	// ── Investigate flags (root command only) ─────────────────────────────

	rootCmd.Flags().BoolVarP(
		&cfg.AutoExecute, "yes", "y",
		false,
		"Auto-execute all commands without confirmation prompts",
	)
	rootCmd.Flags().BoolVar(
		&cfg.DryRun, "dry-run",
		false,
		"Show commands that would run without executing them",
	)
	rootCmd.Flags().IntVar(
		&cfg.MaxIterations, "max-iter",
		defaults.MaxIterations,
		"Maximum investigation rounds before stopping",
	)
	rootCmd.Flags().IntVar(
		&cfg.MaxOutputBytes, "max-output",
		defaults.MaxOutputBytes,
		"Maximum command output bytes to send to LLM (protects context window)",
	)
	rootCmd.Flags().IntVar(
		&cfg.Timeout, "timeout",
		defaults.Timeout,
		"Per-command execution timeout in seconds",
	)
	rootCmd.Flags().BoolVar(
		&cfg.UseToolCalling, "tool-calling",
		defaults.UseToolCalling,
		"Use structured tool-calling API (disable for models without tool support)",
	)

	// Register sub-commands
	rootCmd.AddCommand(modelsCmd)
}

// envOrDefault returns the environment variable value if set, otherwise the default.
// This enables: export OPSMATE_MODEL=deepseek-r1:8b in ~/.bashrc and never
// need to type --model again.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
