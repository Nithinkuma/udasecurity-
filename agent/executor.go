package agent

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"opsmate/config"
)

// CommandResult holds everything about a completed (or skipped) command execution.
type CommandResult struct {
	Command  string        // The command that was run (or offered to run)
	Stdout   string        // Standard output (possibly truncated)
	Stderr   string        // Standard error (possibly truncated)
	ExitCode int           // Shell exit code; 0 = success
	Duration time.Duration // Wall-clock time for the execution
	Skipped  bool          // True if the user declined via confirmation prompt
	DryRun   bool          // True if --dry-run mode was active
	Truncated bool         // True if output was truncated to MaxOutputBytes
}

// Executor runs shell commands with safety controls: confirmation prompts,
// dry-run mode, per-command timeouts, and output size limits.
//
// Why "sh -c" instead of exec.Command with parsed args?
// SRE commands routinely use shell features that require a real shell interpreter:
//   kubectl get pods -n app | grep CrashLoop | awk '{print $1}'
//   for pod in $(kubectl get pods -o name); do kubectl logs $pod --tail=5; done
//   kubectl exec -it $(kubectl get pod -l app=web -o name | head -1) -- bash
//
// exec.Command with explicit args cannot handle any of these. Using "sh -c"
// gives the model full shell expressiveness at the cost of slightly weaker
// argument isolation — an acceptable trade-off since commands come from a
// trusted local model and are always shown before execution.
type Executor struct {
	cfg config.Config
}

// NewExecutor creates a command executor with the given runtime configuration.
func NewExecutor(cfg config.Config) *Executor {
	return &Executor{cfg: cfg}
}

// Run is the top-level method that handles the full lifecycle of a command:
// display → confirm → execute → display result.
//
// It respects DryRun and AutoExecute flags, and always shows the command
// and explanation to the user before doing anything.
func (e *Executor) Run(req CommandRequest) CommandResult {
	// Always show the command and rationale so the user knows what's happening.
	printCommand(req.Command, req.Explanation)

	// --- Dry-run mode ---
	// Show everything but don't actually execute.
	// Useful for: "what would opsmate do for this problem?"
	if e.cfg.DryRun {
		printWarning("DRY RUN — command not executed")
		return CommandResult{
			Command: req.Command,
			Stdout:  "[dry-run mode: command not executed]",
			DryRun:  true,
		}
	}

	// --- Confirmation gate ---
	// By default (AutoExecute=false) we ask the user before each command.
	// This is the primary safety mechanism — the user stays in control.
	// Auto-execute (--yes / -y) is provided for trusted automated workflows.
	if !e.cfg.AutoExecute {
		if !e.askConfirmation() {
			printSkipped(req.Command)
			return CommandResult{
				Command: req.Command,
				Stdout:  "[command skipped by user]",
				Skipped: true,
			}
		}
	}

	// --- Execute ---
	result := e.runShell(req.Command)

	// Display the result in the terminal so the user can follow along.
	// We show the stdout+stderr combined for readability.
	combined := combinedOutput(result)
	printCommandResult(combined, result.ExitCode, result.Truncated)

	return result
}

// runShell executes the command string through sh -c with timeout enforcement.
func (e *Executor) runShell(command string) CommandResult {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(e.cfg.Timeout)*time.Second,
	)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	// Inherit the parent process environment so kubectl config, kubeconfig,
	// DOCKER_HOST, AWS_PROFILE, KUBECONFIG, and other critical env vars are
	// available to the commands the agent runs.
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			// Command timed out — report this clearly in stderr so the model
			// knows the command didn't fail, it just ran too long.
			exitCode = -1
			_, _ = fmt.Fprintf(&stderr, "\n[TIMED OUT after %ds]", e.cfg.Timeout)
		}
	}

	// Truncate outputs to protect the model's context window.
	// Very large outputs (full pod logs, wide kubectl describe) often contain
	// repetitive content. Truncating to the first N bytes preserves the most
	// relevant early content (errors, stack traces tend to appear at the start).
	stdoutStr, stdoutTruncated := truncateOutput(stdout.String(), e.cfg.MaxOutputBytes)
	stderrStr, stderrTruncated := truncateOutput(stderr.String(), e.cfg.MaxOutputBytes/2)

	return CommandResult{
		Command:   command,
		Stdout:    stdoutStr,
		Stderr:    stderrStr,
		ExitCode:  exitCode,
		Duration:  duration,
		Truncated: stdoutTruncated || stderrTruncated,
	}
}

// askConfirmation prompts the user interactively and waits for y/n/q.
// 'q' or 'quit' immediately exits the program (clean escape hatch during an
// investigation that's going in an unexpected direction).
func (e *Executor) askConfirmation() bool {
	fmt.Printf("\n%s  Execute this command? [y/N/q=quit]: %s",
		colorYellow+colorBold, colorReset)

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		// EOF (e.g. piped input ended) — treat as "no"
		return false
	}

	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	switch answer {
	case "y", "yes":
		return true
	case "q", "quit", "exit":
		fmt.Println("\nQuitting investigation.")
		os.Exit(0)
	}
	return false
}

// FormatResultForLLM converts a CommandResult into a clearly structured string
// to send back to the model as the "tool result" or "user" message.
//
// Format design choices:
//   - Label stdout vs stderr separately: models handle structured input better
//   - Include exit code: model needs this to detect command failures
//   - Include duration: helps model understand timeouts vs fast empty results
//   - Append truncation note: model should know it has partial output
//   - Explicit "(empty)" for no output: prevents model from guessing
func FormatResultForLLM(result CommandResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Command: %s\n", result.Command)
	fmt.Fprintf(&b, "Exit Code: %d\n", result.ExitCode)
	fmt.Fprintf(&b, "Duration: %s\n", result.Duration.Round(time.Millisecond))

	if result.Skipped {
		b.WriteString("Status: SKIPPED (user declined to run this command)\n")
		return b.String()
	}
	if result.DryRun {
		b.WriteString("Status: DRY RUN (not executed)\n")
		return b.String()
	}

	b.WriteString("\n")

	if result.Stdout != "" {
		b.WriteString("--- stdout ---\n")
		b.WriteString(result.Stdout)
		if !strings.HasSuffix(result.Stdout, "\n") {
			b.WriteString("\n")
		}
	} else {
		b.WriteString("--- stdout ---\n(empty)\n")
	}

	if result.Stderr != "" {
		b.WriteString("--- stderr ---\n")
		b.WriteString(result.Stderr)
		if !strings.HasSuffix(result.Stderr, "\n") {
			b.WriteString("\n")
		}
	}

	if result.Truncated {
		fmt.Fprintf(&b, "\n[NOTE: Output was truncated to %d bytes. "+
			"If you need more, use --tail, -o name, or narrower selectors.]\n",
			// Surface the config value through the string for model awareness
			len(result.Stdout)+len(result.Stderr))
	}

	if result.ExitCode != 0 {
		fmt.Fprintf(&b, "\n[NOTE: Command exited with code %d — this may indicate an error. "+
			"Check stderr above.]\n", result.ExitCode)
	}

	return b.String()
}

// combinedOutput merges stdout and stderr into a single display string.
// If both are non-empty, stderr is appended with a visual separator.
func combinedOutput(r CommandResult) string {
	if r.Stderr == "" {
		return r.Stdout
	}
	if r.Stdout == "" {
		return r.Stderr
	}
	return r.Stdout + "\n[stderr]\n" + r.Stderr
}

// truncateOutput limits a string to maxBytes.
// Truncation happens at the last newline within the limit to avoid cutting
// mid-line, which makes output harder to read and reason about.
// Returns the (possibly truncated) string and a bool indicating truncation occurred.
func truncateOutput(s string, maxBytes int) (string, bool) {
	if len(s) <= maxBytes {
		return s, false
	}

	// Try to cut cleanly at a line boundary in the second half of the limit
	// (avoids cutting near the start when lines are long).
	cutPoint := maxBytes
	if lastNL := strings.LastIndex(s[:maxBytes], "\n"); lastNL > maxBytes/2 {
		cutPoint = lastNL + 1
	}

	truncated := s[:cutPoint]
	truncated += fmt.Sprintf("\n... [truncated: showing %d of %d bytes] ...\n",
		cutPoint, len(s))
	return truncated, true
}
