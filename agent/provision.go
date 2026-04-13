package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Provision is the single entry-point for "make sure everything needed to run
// opsmate is present and healthy". It runs three sequential checks:
//
//  1. Is the `ollama` binary installed?  → offer to install it
//  2. Is the Ollama server reachable?    → offer to start `ollama serve`
//  3. Is the requested model pulled?     → offer to `ollama pull <model>`
//
// Each check prompts the user interactively if the condition is not met.
// On Linux, steps 1–3 can all be resolved automatically.
// On macOS/Windows, installation requires the user to act; steps 2–3 are automatic.
//
// Why embed provisioning in the tool rather than just printing "go install Ollama"?
// Zero-friction onboarding matters. The target user is an SRE who just got paged —
// they shouldn't have to open a browser, read docs, and run 3 separate commands
// before they can even ask the agent their first question.
func Provision(endpoint, model string) error {
	if err := ensureOllamaInstalled(); err != nil {
		return err
	}
	if err := ensureOllamaServing(endpoint); err != nil {
		return err
	}
	if err := ensureModelAvailable(endpoint, model); err != nil {
		return err
	}
	return nil
}

// ── Step 1: binary installed? ─────────────────────────────────────────────────

// ensureOllamaInstalled checks whether the `ollama` CLI is on $PATH.
// If not, it offers to install it (Linux: official install script) or
// prints platform-specific instructions (macOS, Windows).
func ensureOllamaInstalled() error {
	if _, err := exec.LookPath("ollama"); err == nil {
		return nil // Already installed — nothing to do.
	}

	fmt.Println()
	printWarning("Ollama is not installed on this machine.")
	fmt.Println()

	switch runtime.GOOS {
	case "linux":
		fmt.Printf("%sInstall options for Linux:%s\n", colorBold, colorReset)
		fmt.Printf("  %s• Automatic: %sopsmate will run the official install script%s\n",
			colorCyan, colorDim, colorReset)
		fmt.Printf("  %s• Manual:    %scurl -fsSL https://ollama.com/install.sh | sh%s\n\n",
			colorCyan, colorDim, colorReset)

		if promptYesNo("Install Ollama automatically now?") {
			return installOllamaLinux()
		}
		return fmt.Errorf("ollama is required — install it with: curl -fsSL https://ollama.com/install.sh | sh")

	case "darwin":
		fmt.Printf("%sInstall Ollama on macOS:%s\n", colorBold, colorReset)
		fmt.Printf("  %s• Homebrew:  brew install ollama%s\n", colorCyan, colorReset)
		fmt.Printf("  %s• Download:  https://ollama.com/download/mac%s\n\n", colorCyan, colorReset)
		return fmt.Errorf("ollama is required — install it with: brew install ollama")

	default: // Windows and anything else
		fmt.Printf("%sDownload and install Ollama:%s\n", colorBold, colorReset)
		fmt.Printf("  %shttps://ollama.com/download%s\n\n", colorCyan, colorReset)
		return fmt.Errorf("ollama is required — download it from https://ollama.com/download")
	}
}

// installOllamaLinux runs the official Ollama install script.
// The script is fetched over HTTPS from ollama.com and piped to sh.
// The user is shown what will run and has already confirmed via promptYesNo.
func installOllamaLinux() error {
	printInfo("Running: curl -fsSL https://ollama.com/install.sh | sh")
	fmt.Println()

	// Connect stdout/stderr so the user sees install progress in real time.
	cmd := exec.Command("sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ollama installation failed: %w\n"+
			"Try manually: curl -fsSL https://ollama.com/install.sh | sh", err)
	}

	fmt.Println()
	printSuccess("Ollama installed successfully!")
	return nil
}

// ── Step 2: server running? ───────────────────────────────────────────────────

// ensureOllamaServing checks whether the Ollama HTTP server is accepting requests.
// If not reachable, it offers to start `ollama serve` as a background process
// and waits up to 10 seconds for it to become ready.
func ensureOllamaServing(endpoint string) error {
	if ollamaReachable(endpoint) {
		return nil // Server is already up.
	}

	fmt.Println()
	printWarning("Ollama server is not running (cannot reach " + endpoint + ").")
	fmt.Println()

	if !promptYesNo("Start Ollama server now? (runs: ollama serve)") {
		return fmt.Errorf("ollama server is required — start it with: ollama serve")
	}

	return startOllamaServe(endpoint)
}

// ollamaReachable does a quick HTTP GET to /api/tags with a short timeout.
// Used to test liveness without the full model-list parsing.
func ollamaReachable(endpoint string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(endpoint + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// startOllamaServe launches `ollama serve` as a detached background process
// and polls until the server responds or the timeout expires.
//
// Why detach (cmd.Start instead of cmd.Run)?
// `ollama serve` is a long-running daemon. We fire it and move on.
// The process will outlive opsmate, which is correct — the user probably
// wants Ollama to stay running after the investigation finishes.
func startOllamaServe(endpoint string) error {
	printInfo("Starting Ollama server in background...")

	cmd := exec.Command("ollama", "serve")
	// Do NOT attach stdout/stderr — the daemon's log output would pollute
	// opsmate's terminal output and confuse the user.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ollama serve: %w\n"+
			"Try manually in another terminal: ollama serve", err)
	}

	// Poll up to 10 seconds (5 × 2s intervals) for the server to come up.
	// Local startup is typically < 2 seconds; this gives ample margin.
	for attempt := 1; attempt <= 5; attempt++ {
		time.Sleep(2 * time.Second)
		if ollamaReachable(endpoint) {
			printSuccess("Ollama server is ready.")
			fmt.Println()
			return nil
		}
		printThinking(fmt.Sprintf("Waiting for Ollama to start... (%d/5)", attempt))
	}

	return fmt.Errorf(
		"ollama serve started but didn't become ready within 10 seconds.\n" +
			"Check for errors with: journalctl -u ollama  (or) ollama serve",
	)
}

// ── Step 3: model available? ──────────────────────────────────────────────────

// ensureModelAvailable checks whether the requested model is in Ollama's local
// model store. If not, it offers to pull (download) it.
//
// Pulling can take minutes for large models (deepseek-r1:32b is ~20GB).
// We pipe `ollama pull` stdout/stderr directly so the user sees progress bars.
func ensureModelAvailable(endpoint, model string) error {
	available, pulled, err := listPulledModels(endpoint)
	if err != nil {
		// Server is up (passed step 2) but we can't list models — proceed anyway.
		// The subsequent LLM call will surface a clearer error if the model is missing.
		printWarning(fmt.Sprintf("Could not verify model list: %v — proceeding anyway.", err))
		return nil
	}

	if modelIn(model, pulled) {
		return nil // Model is already pulled and ready.
	}

	fmt.Println()
	printWarning(fmt.Sprintf("Model %q is not pulled in Ollama.", model))

	// Show what models ARE available so the user can choose to use one instead.
	if len(pulled) > 0 {
		fmt.Printf("%sCurrently available models:%s\n", colorDim, colorReset)
		for _, m := range pulled {
			fmt.Printf("  %s• %s%s\n", colorCyan, m, colorReset)
		}
		fmt.Println()
	} else {
		fmt.Println("  (no models pulled yet)")
		fmt.Println()
	}

	// Show estimated download size so the user can make an informed decision.
	sizehint := modelSizeHint(model)
	question := fmt.Sprintf("Pull model %q now?%s", model, sizehint)

	if !promptYesNo(question) {
		// User declined — show a helpful alternative if other models exist.
		if len(pulled) > 0 {
			return fmt.Errorf(
				"model %q not pulled.\n"+
					"Use an already-available model with: opsmate --model %s \"...\"",
				model, pulled[0],
			)
		}
		return fmt.Errorf(
			"model %q not pulled.\n"+
				"Pull it with: ollama pull %s",
			model, model,
		)
	}

	return pullModel(model, available)
}

// pullModel runs `ollama pull <model>` and streams its progress output to the
// terminal. Ollama's pull command shows a progress bar for each layer being
// downloaded, which gives the user accurate feedback during long downloads.
func pullModel(model string, _ []string) error {
	printInfo(fmt.Sprintf("Pulling model: ollama pull %s", model))
	fmt.Println()

	cmd := exec.Command("ollama", "pull", model)
	// Stream pull output directly: the user should see download progress.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull model %q: %w\n"+
			"Try manually: ollama pull %s", model, err, model)
	}

	fmt.Println()
	printSuccess(fmt.Sprintf("Model %q is ready.", model))
	fmt.Println()
	return nil
}

// listPulledModels fetches the full model list from Ollama's /api/tags endpoint.
// Returns: (all model name strings, base names only for matching, error).
func listPulledModels(endpoint string) ([]string, []string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(endpoint + "/api/tags")
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, nil, err
	}

	names := make([]string, len(tagsResp.Models))
	for i, m := range tagsResp.Models {
		names[i] = m.Name
	}
	return names, names, nil
}

// modelIn checks whether `model` matches any entry in the pulled list.
// Matches with or without the ":latest" suffix:
//
//	"gemma4" matches "gemma4:latest"
//	"deepseek-r1:8b" matches "deepseek-r1:8b" exactly
func modelIn(model string, pulled []string) bool {
	base := strings.SplitN(model, ":", 2)[0]
	for _, p := range pulled {
		pBase := strings.SplitN(p, ":", 2)[0]
		if p == model || pBase == base || p == model+":latest" {
			return true
		}
	}
	return false
}

// modelSizeHint returns a human-readable parenthetical size estimate for the
// given model so the user knows what they're about to download.
// Sizes are approximate and based on typical quantization levels.
func modelSizeHint(model string) string {
	hints := map[string]string{
		"gemma4":          " (~5 GB download)",
		"gemma3":          " (~5 GB download)",
		"llama3.2":        " (~2 GB download)",
		"mistral":         " (~4 GB download)",
		"qwen2.5:7b":      " (~4 GB download)",
		"deepseek-r1:8b":  " (~5 GB download)",
		"deepseek-r1:14b": " (~9 GB download)",
		"deepseek-r1:32b": " (~20 GB download)",
		"deepseek-r1:70b": " (~40 GB download)",
	}
	base := strings.SplitN(model, ":", 2)[0]
	if hint, ok := hints[model]; ok {
		return hint
	}
	if hint, ok := hints[base]; ok {
		return hint
	}
	return "" // Unknown model — no size hint, still ask
}

// ── Shared helper ─────────────────────────────────────────────────────────────

// promptYesNo prints a [y/N] question and reads a line from stdin.
// Returns true only for an explicit "y" or "yes" (case-insensitive).
// Default (empty / Enter) is "no" — safe default for potentially long operations.
func promptYesNo(question string) bool {
	fmt.Printf("%s%s [y/N]: %s", colorBold+colorYellow, question, colorReset)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false // EOF (piped input ended)
	}
	ans := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return ans == "y" || ans == "yes"
}
