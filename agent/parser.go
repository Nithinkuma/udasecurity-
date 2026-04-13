package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

// CommandRequest represents a single command the agent wants to execute,
// along with its rationale and optional tool-call linkage.
type CommandRequest struct {
	// Command is the shell command string to pass to sh -c.
	Command string

	// Explanation is the model's one-sentence rationale for running this command.
	// Shown to the user before execution so they understand the agent's intent.
	Explanation string

	// ToolCallID links this request back to the model's tool_call entry.
	// Non-empty only when extracted from structured tool_calls (not text parsing).
	// Required when we send results back as "tool" role messages.
	ToolCallID string
}

// commandArgs is the expected JSON structure of the execute_command tool arguments.
type commandArgs struct {
	Command     string `json:"command"`
	Explanation string `json:"explanation"`
}

// ExtractFromToolCalls extracts CommandRequests from the structured tool_calls
// field returned by models that support function/tool calling (llama3.2, mistral, etc).
//
// Why prefer this over text parsing?
// Tool calls are structured JSON produced to match the schema we defined.
// The model's command is isolated from its prose analysis — we get exactly the
// command string with no risk of accidentally picking up code blocks from
// examples or explanations the model writes.
func ExtractFromToolCalls(toolCalls []ToolCall) []CommandRequest {
	var out []CommandRequest
	for _, tc := range toolCalls {
		if tc.Function.Name != "execute_command" {
			// Ignore unknown tool names defensively.
			// If we add more tools in future, each will be handled here.
			continue
		}

		var args commandArgs
		if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
			// Some models produce slightly malformed JSON (trailing commas,
			// unquoted values). Fall back to regex field extraction.
			args.Command = extractJSONString(string(tc.Function.Arguments), "command")
			args.Explanation = extractJSONString(string(tc.Function.Arguments), "explanation")
		}

		cmd := strings.TrimSpace(args.Command)
		if cmd == "" {
			continue // Skip empty / parse failures
		}

		out = append(out, CommandRequest{
			Command:     cmd,
			Explanation: strings.TrimSpace(args.Explanation),
			ToolCallID:  tc.ID,
		})
	}
	return out
}

// codeBlockRe matches fenced markdown code blocks.
//
// Pattern breakdown:
//   (?s)        — dot matches newlines (multiline mode)
//   ```         — opening fence
//   (?:bash|sh|shell|zsh|console|cmd|)? — optional language label
//   \n?         — optional newline after fence
//   (.*?)       — capture group: the code content (non-greedy)
//   ```         — closing fence
//
// Why markdown code blocks as the fallback?
// LLMs are extensively trained on GitHub markdown and reliably wrap shell
// commands in fenced blocks. Parsing "lines starting with kubectl/docker"
// is too brittle — it breaks on piped commands and multi-line constructs.
var codeBlockRe = regexp.MustCompile("(?s)```(?:bash|sh|shell|zsh|console|cmd|)?\n?(.*?)```")

// ExtractFromText parses a model's plain-text response for shell commands
// enclosed in markdown fenced code blocks.
//
// Used as fallback when the model does not support structured tool calling.
// The trade-offs vs. structured tool calls:
//   + Works with any model that can write markdown
//   - Less reliable: model may wrap example output in code blocks too
//   - No explanation field (we get context from surrounding prose)
//   - Harder to handle multi-command sequences correctly
func ExtractFromText(content string) []CommandRequest {
	var out []CommandRequest
	matches := codeBlockRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		code := strings.TrimSpace(m[1])
		if code == "" {
			continue
		}

		// Filter out blocks that look like config/output examples rather than
		// commands. We don't want to execute YAML manifests or JSON output.
		if looksLikeNonCommand(code) {
			continue
		}

		// Process potentially multi-line command blocks.
		// Each non-comment, non-empty line is a candidate command.
		lines := splitCommandLines(code)
		if len(lines) == 0 {
			continue
		}

		// Strategy for multi-line blocks:
		//  - Single line  → one command
		//  - Multiple lines → join with " && " so they run in sequence
		//    and the agent sees all output together
		//  - Lines ending with "\" → continuation; join with space
		cmd := joinCommandLines(lines)
		if cmd == "" {
			continue
		}

		out = append(out, CommandRequest{
			Command: cmd,
			// No explanation available from text parsing; the surrounding prose
			// provides context and the agent's prior message explains intent.
			Explanation: "",
		})
	}
	return out
}

// splitCommandLines processes a raw code block into individual command strings,
// stripping shell comments and blank lines.
func splitCommandLines(code string) []string {
	var lines []string
	for _, raw := range strings.Split(code, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// joinCommandLines reassembles split lines into a single executable command.
// Lines ending with backslash continuation are joined with a space.
// Otherwise lines are joined with " && " to run sequentially.
func joinCommandLines(lines []string) string {
	if len(lines) == 1 {
		return lines[0]
	}

	// Check for backslash continuations (common in long kubectl commands)
	var reassembled []string
	var current strings.Builder
	for _, line := range lines {
		if strings.HasSuffix(line, "\\") {
			current.WriteString(strings.TrimSuffix(line, "\\"))
			current.WriteString(" ")
		} else {
			current.WriteString(line)
			reassembled = append(reassembled, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		reassembled = append(reassembled, current.String())
	}

	return strings.Join(reassembled, " && ")
}

// commandPrefixes lists known CLI tool names that definitely indicate a command
// (not config or output). Used in looksLikeNonCommand to allow-list blocks.
var commandPrefixes = []string{
	// Kubernetes tooling
	"kubectl", "k ", "helm", "istioctl", "kustomize", "argocd", "flux",
	"kubectx", "kubens", "k9s",
	// Container tooling
	"docker", "podman", "crictl", "nerdctl",
	// Linux diagnostics
	"cat ", "ls ", "ps ", "df ", "du ", "top", "htop", "free ",
	"netstat", "ss ", "lsof", "curl ", "wget ", "dig ", "nslookup",
	"ping ", "traceroute", "mtr ",
	"systemctl", "journalctl", "dmesg", "sysctl",
	"grep ", "awk ", "sed ", "sort ", "uniq ", "wc ", "tail ", "head ",
	"find ", "xargs ",
	// Cloud CLIs
	"aws ", "gcloud ", "az ", "doctl ",
	// Database CLIs
	"psql ", "mysql ", "redis-cli ", "mongosh ", "mongo ",
	// Package managers / build tools
	"apt", "yum", "dnf", "brew", "pip", "npm", "go ", "./",
	// Script invocations
	"bash ", "sh ", "python", "ruby ", "node ",
}

// looksLikeNonCommand returns true when a code block is more likely to be
// configuration, output, or an example than a runnable command.
// We skip these to avoid executing YAML manifests or JSON samples the model
// writes as part of its explanation.
func looksLikeNonCommand(code string) bool {
	trimmed := strings.TrimSpace(code)
	lower := strings.ToLower(trimmed)

	// Allow-list: if it starts with a known command, it's definitely runnable.
	for _, prefix := range commandPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}

	// JSON object or array → output example, not a command
	if (strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]")) {
		return true
	}

	// YAML separator → Kubernetes manifest or Helm values
	if strings.HasPrefix(trimmed, "---") {
		return true
	}

	// Dense "key: value" content → YAML output or config
	colonSpaceCount := strings.Count(trimmed, ": ")
	if colonSpaceCount > 4 {
		return true
	}

	// Looks like structured table output (NAME   READY   STATUS...) → skip
	if strings.Count(trimmed, "   ") > 3 && !strings.Contains(lower, "kubectl") {
		return true
	}

	return false
}

// extractJSONString is a regex-based fallback for extracting a string field
// from potentially malformed JSON. Used when json.Unmarshal fails on model output.
//
// Why not just fix the JSON? We have no control over what the model produces.
// Some models trained on tool-calling data use slightly non-standard formats.
// This regex handles the common cases (trailing commas, single quotes) gracefully.
func extractJSONString(raw, field string) string {
	// Match: "fieldname": "value with possible \" escapes"
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(field) + `"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	m := re.FindStringSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	// Unescape common JSON string escapes
	val := m[1]
	val = strings.ReplaceAll(val, `\"`, `"`)
	val = strings.ReplaceAll(val, `\\`, `\`)
	val = strings.ReplaceAll(val, `\n`, "\n")
	val = strings.ReplaceAll(val, `\t`, "\t")
	return val
}
