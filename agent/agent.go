package agent

import (
	"fmt"
	"strings"

	"opsmate/config"
)

// systemPrompt is injected as the "system" role message at the start of every
// conversation. It defines the agent's identity, capabilities, investigation
// methodology, and the done-condition.
//
// Prompt engineering notes:
//
//  1. Role framing ("expert SRE") strongly primes the model to produce
//     terse, accurate, operations-focused responses rather than generic answers.
//
//  2. Explicit tool-use instructions are required even for models that support
//     tool calling — without them many models default to just writing text.
//
//  3. Investigation structure (broad→narrow→deep) prevents the model from
//     immediately jumping to kubectl logs on the wrong pod, or running 20
//     commands when 3 would suffice.
//
//  4. "Read-only preference" prevents accidental `kubectl delete` or
//     `docker rm` commands during a diagnostic investigation.
//
//  5. Explicit done-condition with a structured output format. Without this,
//     models often keep running commands indefinitely or stop without a summary.
const systemPrompt = `You are an expert SRE (Site Reliability Engineer) agent embedded in a CLI tool
called opsmate. You have deep expertise in:

  • Kubernetes (kubectl, helm, kustomize, RBAC, CRDs, operators, networking)
  • Docker and container runtimes (containerd, CRI-O)
  • Linux system administration (systemd, cgroups, networking, filesystems)
  • Cloud infrastructure (AWS, GCP, Azure — accessed through their CLIs)
  • Databases (PostgreSQL, MySQL, Redis, MongoDB — status and diagnostics only)
  • Observability: logs, events, resource metrics, network traces

═══════════════════════════════════════════════
HOW YOU WORK
═══════════════════════════════════════════════

You investigate problems by running diagnostic commands using the execute_command
tool. Follow this pattern:

  1. ORIENT   – Get a broad view. List resources, check statuses, read events.
  2. FOCUS    – Identify the specific failing component(s).
  3. DIAGNOSE – Deep-dive: logs, describe output, resource usage, network checks.
  4. CONCLUDE – Synthesize findings into a clear root cause and remediation plan.

Run commands iteratively. Analyze each result before deciding what to check next.
Do not run all commands at once — the output of one command guides the next.

═══════════════════════════════════════════════
KUBECTL CHEAT SHEET (use these patterns)
═══════════════════════════════════════════════

Pod issues:
  kubectl get pods -n <ns> -o wide --show-labels
  kubectl describe pod <name> -n <ns>           ← events, resource limits, volumes
  kubectl logs <name> -n <ns> --tail=100         ← current container logs
  kubectl logs <name> -n <ns> --previous --tail=100  ← logs from crashed container
  kubectl get events -n <ns> --sort-by=.lastTimestamp | tail -30

Node issues:
  kubectl get nodes -o wide
  kubectl describe node <name>
  kubectl top nodes  (requires metrics-server)
  kubectl top pods -n <ns> --containers

Networking:
  kubectl get svc,endpoints,ingress -n <ns>
  kubectl describe ingress <name> -n <ns>
  kubectl exec -n <ns> <pod> -- curl -sv http://<service>:<port>/health

Deployments / ReplicaSets:
  kubectl rollout status deployment/<name> -n <ns>
  kubectl rollout history deployment/<name> -n <ns>
  kubectl get rs -n <ns> -l app=<name>

Config / Secrets (existence only — do not print secret values):
  kubectl get configmap,secret -n <ns>
  kubectl describe configmap <name> -n <ns>

═══════════════════════════════════════════════
RULES
═══════════════════════════════════════════════

  ✓ Prefer read-only diagnostic commands
  ✓ Do not delete, patch, or restart resources unless the user explicitly asks
  ✓ If a command fails, analyze the error — it is often itself a finding
  ✓ If you need to check a secret's value, describe it to check existence/keys only
  ✗ Do not print base64 secret values
  ✗ Do not run exec into pods unless strictly necessary for diagnosis

═══════════════════════════════════════════════
WHEN YOU ARE DONE
═══════════════════════════════════════════════

Stop calling execute_command when you have enough information for a diagnosis.
End your investigation with this exact structure:

## Root Cause
[One paragraph: what specifically went wrong and why]

## Evidence
[Bullet list: key findings from your commands with specific values, counts, messages]

## Remediation
[Numbered steps the user should take to fix the issue]

## Prevention
[How to avoid this class of problem in future]

If you cannot determine root cause, explain what you found, what you suspect,
and what additional access or information would let you confirm it.`

// Agent is the central coordinator of the opsmate investigation loop.
// It owns the conversation state and orchestrates calls between the LLM and executor.
type Agent struct {
	client   *OllamaClient
	executor *Executor
	cfg      config.Config

	// messages is the full conversation history sent to the LLM on every call.
	// We send the entire history each time because:
	//  (a) Ollama models are stateless — there is no server-side session
	//  (b) The model needs all prior context to reason about causality
	//      (e.g. "pod X is in OOMKilled because I saw high RSS in the metrics")
	// Downside: token count grows with each iteration. For very long
	// investigations, older messages could be summarized to save context space.
	// That optimization is left for a future version.
	messages []Message
}

// NewAgent constructs an Agent with the system prompt pre-loaded.
func NewAgent(cfg config.Config) *Agent {
	return &Agent{
		client:   NewOllamaClient(cfg.OllamaEndpoint, cfg.Model),
		executor: NewExecutor(cfg),
		cfg:      cfg,
		messages: []Message{
			// System message is always first. It persists for the entire investigation.
			{Role: "system", Content: systemPrompt},
		},
	}
}

// Investigate is the main entry point. It runs the full agentic loop for a
// given problem description and returns when the investigation is complete.
//
// ┌─────────────────────────────────────────────────────────────────┐
// │                     AGENTIC LOOP                                │
// │                                                                 │
// │  User problem                                                   │
// │      │                                                          │
// │      ▼                                                          │
// │  ┌──────────┐   tool_calls / code blocks   ┌──────────────┐    │
// │  │  Ollama  │ ─────────────────────────►   │   Executor   │    │
// │  │   LLM    │ ◄─────────────────────────   │  (sh -c cmd) │    │
// │  └──────────┘   command results            └──────────────┘    │
// │      │                                                          │
// │      │  no commands in response                                 │
// │      ▼                                                          │
// │   Final analysis (done)                                         │
// └─────────────────────────────────────────────────────────────────┘
func (a *Agent) Investigate(problem string) error {
	// Provision checks and auto-resolves three things before starting:
	// 1. ollama binary installed?  → offer to install
	// 2. ollama server running?    → offer to start
	// 3. model pulled?             → offer to pull
	if err := Provision(a.cfg.OllamaEndpoint, a.cfg.Model); err != nil {
		return err
	}

	printHeader("opsmate — SRE Investigation")
	printInfo("Problem:  " + problem)
	printInfo("Model:    " + a.cfg.Model)
	printInfo("Endpoint: " + a.cfg.OllamaEndpoint)
	if a.cfg.DryRun {
		printWarning("DRY RUN MODE — commands displayed but not executed")
	}
	if a.cfg.AutoExecute {
		printWarning("AUTO-EXECUTE MODE — commands run without confirmation")
	}
	fmt.Println()

	// Seed the conversation with the user's problem statement.
	a.messages = append(a.messages, Message{
		Role:    "user",
		Content: problem,
	})

	// ── Main investigation loop ────────────────────────────────────────────
	for iteration := 1; iteration <= a.cfg.MaxIterations; iteration++ {
		if a.cfg.Verbose {
			printDebug(fmt.Sprintf("iteration %d/%d | %d messages in history",
				iteration, a.cfg.MaxIterations, len(a.messages)))
		}

		// ── LLM call ──────────────────────────────────────────────────────
		printThinking(fmt.Sprintf("Analyzing... (round %d/%d)",
			iteration, a.cfg.MaxIterations))

		resp, err := a.client.Chat(a.messages, a.cfg.UseToolCalling)
		if err != nil {
			return fmt.Errorf("LLM error at iteration %d: %w", iteration, err)
		}

		assistantMsg := resp.Message

		// Print any prose the model returned alongside or instead of tool calls.
		// This gives the user insight into the model's reasoning at each step.
		if strings.TrimSpace(assistantMsg.Content) != "" {
			printAssistant(assistantMsg.Content)
		}

		// ── Command extraction ─────────────────────────────────────────────
		// Determine which path produced commands: structured tool_calls or text.
		var commands []CommandRequest
		usingToolCalls := a.cfg.UseToolCalling && len(assistantMsg.ToolCalls) > 0

		if usingToolCalls {
			// Structured path — preferred when model supports tool calling.
			commands = ExtractFromToolCalls(assistantMsg.ToolCalls)
			if a.cfg.Verbose {
				printDebug(fmt.Sprintf("extracted %d commands via tool_calls", len(commands)))
			}
		} else {
			// Text-parsing fallback — works with any markdown-capable model.
			commands = ExtractFromText(assistantMsg.Content)
			if a.cfg.Verbose && len(commands) > 0 {
				printDebug(fmt.Sprintf("extracted %d commands via text parsing", len(commands)))
			}
		}

		// ── Done check ────────────────────────────────────────────────────
		// When the model provides no commands to run, the investigation is
		// complete and the assistant's prose above is the final analysis.
		// This is the natural "done" signal — the model simply stops asking
		// for tool results when it has enough information.
		if len(commands) == 0 {
			printSuccess("── Investigation complete ──")
			return nil
		}

		// Add the assistant's message (including tool_calls) to history BEFORE
		// executing commands. This ensures the conversation stays coherent even
		// if the user quits mid-execution.
		a.messages = append(a.messages, assistantMsg)

		// ── Execute commands ──────────────────────────────────────────────
		var resultParts []string
		for i, cmd := range commands {
			printSection(fmt.Sprintf("Command %d / %d", i+1, len(commands)))
			result := a.executor.Run(cmd)
			resultParts = append(resultParts, FormatResultForLLM(result))
		}

		// ── Feed results back to the model ────────────────────────────────
		// Message role selection:
		//
		//  "tool" role  — used when the model explicitly made tool_calls.
		//                 The OpenAI/Ollama spec requires tool results to use
		//                 this role so the model can correlate results back to
		//                 its requests. Models trained on this convention
		//                 (llama3.2, mistral-nemo, qwen2.5) handle it correctly.
		//
		//  "user" role  — used for models that responded with text (not tool_calls).
		//                 We inject results as a user turn with an explicit
		//                 "here are the command results" framing so the model
		//                 understands this is external data, not its own output.
		combinedResults := strings.Join(resultParts, "\n---\n")

		if usingToolCalls {
			a.messages = append(a.messages, Message{
				Role:    "tool",
				Content: combinedResults,
			})
		} else {
			a.messages = append(a.messages, Message{
				Role: "user",
				Content: "Here are the results of the commands you requested:\n\n" +
					combinedResults +
					"\n\nPlease analyze these results. " +
					"Run more commands if needed, or provide your final diagnosis.",
			})
		}
	}
	// ── Loop limit reached ─────────────────────────────────────────────────
	printWarning(fmt.Sprintf(
		"Reached the maximum iteration limit (%d). Investigation stopped early.",
		a.cfg.MaxIterations,
	))
	printInfo("Tip: increase the limit with --max-iter, or refine the problem statement.")
	return nil
}
