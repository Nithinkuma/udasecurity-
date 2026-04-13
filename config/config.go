// Package config holds the runtime configuration for opsmate.
//
// Design rationale: A flat struct rather than deeply nested TOML/YAML config.
// Reasoning: The option count is small and all values come from CLI flags or
// environment variables. Nesting would add complexity (config.LLM.Model vs
// cfg.Model) with no benefit at this scale. If we add sub-commands with very
// different option sets in future, we can split then.
package config

// Config holds all runtime settings passed through from CLI flags or env vars.
type Config struct {
	// OllamaEndpoint is the base URL of the Ollama inference server.
	// Ollama binds to port 11434 by default. Point to a remote machine
	// if you have a beefy GPU server: "http://gpu-box:11434"
	OllamaEndpoint string

	// Model is the Ollama model identifier to use for the SRE agent.
	// The model must already be pulled: `ollama pull <model>`
	//
	// Recommended models (best → fastest):
	//   deepseek-r1:8b   - Strong reasoning, excellent for root-cause analysis
	//   gemma3           - Good balance; native tool-calling on some builds
	//   llama3.2         - Fast, reliable tool-calling support
	//   mistral          - Great at following structured formats
	//   qwen2.5:7b       - Excellent instruction following
	Model string

	// AutoExecute bypasses the per-command confirmation prompt.
	// Default false (safe: always ask). Set true for scripted/CI scenarios
	// where you trust the model and want unattended investigation.
	AutoExecute bool

	// DryRun shows commands the agent plans to run without executing them.
	// Useful for auditing: "what would opsmate do for this problem?"
	DryRun bool

	// MaxIterations caps the agent loop to prevent runaway investigations.
	// Each iteration = one LLM call + N command executions. Typical investigations
	// take 3-8 iterations. 20 is a generous safety ceiling.
	MaxIterations int

	// MaxOutputBytes limits command output sent back to the LLM per command.
	// kubectl describe and log outputs can be 100KB+. We truncate aggressively
	// because: (1) it protects the model's context window, (2) the first N bytes
	// of a log are usually the most relevant for diagnosis, (3) smaller context
	// = faster LLM responses on local hardware.
	MaxOutputBytes int

	// Timeout is the per-command execution deadline in seconds.
	// Some kubectl commands (e.g. port-forward, exec) can hang indefinitely.
	// This ensures the agent loop never blocks on a stuck command.
	Timeout int

	// UseToolCalling controls whether we use Ollama's structured tool/function-
	// calling API or fall back to parsing markdown code blocks in text responses.
	//
	// Why both modes?
	// - Tool calling: Reliable, structured. Model returns JSON with exact command.
	//   Works on: llama3.2, mistral-nemo, qwen2.5, some gemma3 builds.
	// - Text parsing: Fallback for models that don't support tool calling.
	//   Works on: any model that can write markdown code blocks.
	//
	// Default true. Disable with --tool-calling=false for older/simpler models.
	UseToolCalling bool

	// Verbose enables debug output: API request/response sizes, iteration timing,
	// extraction method used. Helpful when a model isn't behaving as expected.
	Verbose bool
}

// DefaultConfig returns safe, locally-focused defaults.
// All values can be overridden via CLI flags or OPSMATE_* env vars.
func DefaultConfig() Config {
	return Config{
		OllamaEndpoint: "http://localhost:11434",
		Model:          "gemma4",
		AutoExecute:    false,
		DryRun:         false,
		MaxIterations:  20,
		MaxOutputBytes: 8000,
		Timeout:        60,
		UseToolCalling: true,
		Verbose:        false,
	}
}
