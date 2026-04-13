// Package agent contains the core SRE agent logic: the agentic loop,
// Ollama API client, command executor, and LLM response parser.
package agent

import (
	"fmt"
	"strings"
)

// ANSI terminal escape codes for colored output.
//
// Why hand-roll color codes instead of a library like "fatih/color"?
// These 10 codes cover 100% of our needs. Adding a library for this would:
// (1) increase binary size, (2) add a transitive dependency graph to audit,
// (3) require handling Windows compatibility that the library handles but we
// don't need since kubectl/docker are primarily Linux/macOS tools.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// printHeader prints a prominent section header to mark the start of an investigation.
func printHeader(title string) {
	line := strings.Repeat("─", 62)
	fmt.Printf("\n%s%s%s\n", colorBold+colorBlue, line, colorReset)
	fmt.Printf("%s  ◆  %s%s\n", colorBold+colorBlue, title, colorReset)
	fmt.Printf("%s%s%s\n\n", colorBold+colorBlue, line, colorReset)
}

// printSection marks the start of a command execution block.
func printSection(title string) {
	fmt.Printf("\n%s┌─ %s%s\n", colorCyan+colorBold, title, colorReset)
}

// printInfo prints an informational line (model name, endpoint, etc.).
func printInfo(msg string) {
	fmt.Printf("%s  ℹ  %s%s\n", colorCyan, msg, colorReset)
}

// printThinking shows the "model is working" spinner line.
func printThinking(msg string) {
	fmt.Printf("%s  ⟳  %s%s\n", colorDim+colorYellow, msg, colorReset)
}

// printAssistant prints the model's prose response (analysis, explanations).
// Indented and colored to distinguish from command output.
func printAssistant(msg string) {
	fmt.Printf("\n%s🤖 Agent:%s\n", colorBold+colorGreen, colorReset)
	// Indent each line for visual separation from command blocks
	for _, line := range strings.Split(strings.TrimRight(msg, "\n"), "\n") {
		fmt.Printf("   %s\n", line)
	}
	fmt.Println()
}

// printCommand shows a command about to be executed, with its rationale.
// This is the human-readable "what the agent wants to do" display shown
// before asking for confirmation or auto-executing.
func printCommand(cmd, explanation string) {
	fmt.Printf("\n%s┌─ Command to run%s\n", colorYellow+colorBold, colorReset)
	if explanation != "" {
		fmt.Printf("%s│  Why: %s%s%s\n", colorYellow, colorDim+colorWhite, explanation, colorReset)
	}
	fmt.Printf("%s│  %s$ %s%s%s\n", colorYellow, colorBold, colorWhite, cmd, colorReset)
	fmt.Printf("%s└%s\n", colorYellow, colorReset)
}

// printCommandResult prints the output of an executed command.
// Non-zero exit codes are shown in red to draw attention to failures.
func printCommandResult(output string, exitCode int, truncated bool) {
	statusIcon := colorGreen + "✓" + colorReset
	if exitCode != 0 {
		statusIcon = fmt.Sprintf("%s✗ exit(%d)%s", colorRed, exitCode, colorReset)
	}
	fmt.Printf("   %s Output:\n", statusIcon)

	if output == "" {
		fmt.Printf("   %s(no output)%s\n", colorDim, colorReset)
	} else {
		for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
			fmt.Printf("   %s%s%s\n", colorDim, line, colorReset)
		}
	}
	if truncated {
		fmt.Printf("   %s[output truncated — full text sent to model]%s\n", colorYellow, colorReset)
	}
}

// printSuccess prints a success/completion message.
func printSuccess(msg string) {
	fmt.Printf("%s✓  %s%s\n", colorGreen+colorBold, msg, colorReset)
}

// printWarning prints a warning (dry-run notice, auto-execute notice, etc.).
func printWarning(msg string) {
	fmt.Printf("%s⚠  %s%s\n", colorYellow+colorBold, msg, colorReset)
}

// printError prints an error message.
func printError(msg string) {
	fmt.Printf("%s✗  %s%s\n", colorRed+colorBold, msg, colorReset)
}

// printDebug prints verbose debug info (only shown with --verbose).
func printDebug(msg string) {
	fmt.Printf("%s[debug] %s%s\n", colorDim, msg, colorReset)
}

// printSkipped notes that the user declined to run a command.
func printSkipped(cmd string) {
	fmt.Printf("   %s↷ Skipped: %s%s\n", colorDim, cmd, colorReset)
}
