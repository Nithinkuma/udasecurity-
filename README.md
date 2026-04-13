# opsmate

**AI-powered SRE agent for Kubernetes, Docker, and Linux — runs entirely on your machine.**

opsmate connects a locally-running LLM (via [Ollama](https://ollama.com)) to a real shell,
creating an agent that investigates infrastructure problems by iteratively running
`kubectl`, `docker`, and Linux diagnostic commands until it reaches a diagnosis.

```
$ opsmate "why are pods in namespace payments crashing?"

◆  opsmate — SRE Investigation
  ℹ  Problem:  why are pods in namespace payments crashing?
  ℹ  Model:    gemma3

  ⟳  Analyzing... (round 1/20)

🤖 Agent:
   Let me start by getting an overview of the pods in the payments namespace.

┌─ Command to run
│  Why: Check overall pod status and identify which pods are failing
│  $ kubectl get pods -n payments -o wide --show-labels
└

  Execute this command? [y/N/q=quit]: y
✓  Output:
   NAME                       READY   STATUS             RESTARTS   AGE
   payment-api-5d8f9b-xk2p9   0/1     CrashLoopBackOff   8          23m

  ⟳  Analyzing... (round 2/20)

🤖 Agent:
   The payment-api pod is in CrashLoopBackOff with 8 restarts. Let me check the logs.

┌─ Command to run
│  Why: Get logs from the previous (crashed) container to see the error
│  $ kubectl logs payment-api-5d8f9b-xk2p9 -n payments --previous --tail=50
└

  Execute this command? [y/N/q=quit]: y

  ... (investigation continues) ...

## Root Cause
The payment-api container is OOMKilled. The memory limit is set to 128Mi but the
application requires ~300Mi at startup due to JVM heap initialization.

## Evidence
  • 8 CrashLoopBackOff restarts in 23 minutes
  • OOMKilled in container events (exit code 137)
  • JVM heap flag -Xmx256m exceeds the 128Mi container limit

## Remediation
  1. kubectl edit deployment payment-api -n payments
  2. Set resources.limits.memory to "512Mi"
  3. Set resources.requests.memory to "256Mi"

## Prevention
  • Add JVM_OPTS=-Xmx$(expr $LIMIT_MB - 64)m to auto-calculate heap from limit
  • Set Kubernetes memory limit ≥ JVM -Xmx + 128Mi overhead
  • Add a PodDisruptionBudget and HPA to handle memory pressure gracefully
```

---

## Why opsmate?

| Problem | How opsmate helps |
|---------|-----------------|
| You know *something* is wrong but don't know which 10 of 200 commands to run | The LLM narrows the search space systematically |
| Tribal knowledge lives in people's heads | The system prompt encodes SRE best practices for every investigation |
| On-call at 3am, brain is slow | Describe the symptom; let the agent do the methodical checking |
| New to Kubernetes or a new cluster | The agent explains what each command reveals and why |
| Runbooks are out of date | The agent adapts to whatever it actually finds |

**Why locally-running LLMs?**
- No data leaves your machine — pod names, secrets, logs stay local
- No API rate limits or cost per investigation
- Works on air-gapped clusters and corporate networks
- You control which model to use and when to upgrade

---

## Prerequisites

1. **Install Ollama**: https://ollama.com/download
2. **Start Ollama**:
   ```bash
   ollama serve
   ```
3. **Pull a model**:
   ```bash
   ollama pull gemma3          # default, good balance
   ollama pull deepseek-r1:8b  # best reasoning
   ollama pull llama3.2        # fastest, native tool-calling
   ```
4. **kubectl configured** (for Kubernetes investigations):
   ```bash
   kubectl cluster-info  # should work before running opsmate
   ```

---

## Installation

### Build from source
```bash
git clone <this-repo>
cd opsmate
go build -o opsmate .
sudo mv opsmate /usr/local/bin/   # optional: put in PATH
```

### Quick start (no install)
```bash
go run . "why are my pods crashing in namespace myapp"
```

---

## Usage

```
opsmate [flags] <problem description...>
```

### Basic examples

```bash
# Kubernetes
opsmate "why are pods in namespace payments failing to start?"
opsmate "the frontend deployment has 0/3 pods ready in production"
opsmate "node worker-3 is NotReady, investigate"
opsmate "service checkout cannot reach service inventory"

# Docker
opsmate "my nginx container keeps restarting, what's wrong?"
opsmate "docker build is failing for the api image"

# Linux
opsmate "disk is 95% full on the web server, find what's consuming space"
opsmate "CPU usage spiked to 100% at 14:30, check what happened"
opsmate "the application port 8080 is not listening, investigate"

# Databases (read-only diagnosis)
opsmate "PostgreSQL is rejecting connections, check why"
opsmate "Redis memory usage is high in the cache namespace"
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--model` | `-m` | `gemma3` | Ollama model to use |
| `--endpoint` | `-e` | `http://localhost:11434` | Ollama server URL |
| `--yes` | `-y` | `false` | Auto-execute commands (no confirmation) |
| `--dry-run` | | `false` | Show commands without executing |
| `--max-iter` | | `20` | Max investigation rounds |
| `--max-output` | | `8000` | Max bytes of command output sent to LLM |
| `--timeout` | | `60` | Per-command timeout in seconds |
| `--tool-calling` | | `true` | Use structured tool-calling API |
| `--verbose` | `-v` | `false` | Debug output |

### Environment variables

```bash
export OPSMATE_MODEL=deepseek-r1:8b          # avoid typing --model every time
export OPSMATE_ENDPOINT=http://gpu-box:11434 # use a remote GPU server
```

### Sub-commands

```bash
opsmate models   # list recommended models with pull commands
opsmate help     # full help text
```

---

## Recommended Models

| Model | VRAM | Best for |
|-------|------|---------|
| `deepseek-r1:8b` | ~5GB | Complex root-cause analysis, multi-step reasoning |
| `gemma3` | ~5GB | General SRE work, good balance (default) |
| `llama3.2` | ~2GB | Fast checks, native tool-calling |
| `mistral` | ~4GB | Structured output, following formats precisely |
| `qwen2.5:7b` | ~4GB | Limited hardware, strong instruction following |
| `deepseek-r1:32b` | ~20GB | Most thorough analysis for critical incidents |

**Rule of thumb**: Use `deepseek-r1:8b` if you have the VRAM. Use `llama3.2` when speed matters. Use `gemma3` as a reliable default on Apple Silicon.

### Tool calling support

Some models support Ollama's structured tool-calling API (they return JSON-structured
`tool_calls` rather than markdown text). This is more reliable and precise.

Models with **good** tool-calling: `llama3.2`, `mistral-nemo`, `qwen2.5`
Models with **partial/none**: `gemma3`, `deepseek-r1` (use text parsing fallback)

For models without tool-calling support, opsmate automatically falls back to parsing
markdown code blocks (` ```bash ... ``` `) from the response. This works well in practice.

Disable tool-calling explicitly if a model behaves oddly:
```bash
opsmate --tool-calling=false --model gemma3 "..."
```

---

## How it works

### The agentic loop

```
User: "why are pods in namespace payments crashing?"
        │
        ▼
┌──────────────────────────────────────────────────────────┐
│  1. LLM call with full conversation history              │
│     → Model decides next command (tool_call or text)     │
│                                                          │
│  2. opsmate shows command + reason                       │
│     → User approves (or auto-executes with -y)           │
│                                                          │
│  3. sh -c <command> runs in local shell                  │
│     → Output captured, truncated, structured             │
│                                                          │
│  4. Output appended to conversation history              │
│     → Loop back to step 1                                │
│                                                          │
│  5. Model returns response with no commands              │
│     → Final diagnosis printed, loop exits                │
└──────────────────────────────────────────────────────────┘
```

### Typical investigation flow

A good investigation follows the **Orient → Focus → Diagnose → Conclude** pattern:

1. **Orient**: `kubectl get pods -n <ns>` — what's the overall state?
2. **Focus**: `kubectl describe pod <failing-pod>` — which specific pod, what events?
3. **Diagnose**: `kubectl logs <pod> --previous` — what does the crash say?
4. **Cross-check**: check related resources (service, configmap, node pressure)
5. **Conclude**: root cause + specific remediation steps

### Done detection

The loop terminates naturally when the model stops calling the `execute_command` tool.
This happens when:
- It has enough information for a confident diagnosis
- It has exhausted productive diagnostic avenues
- It determines the issue is outside the scope of available commands

There's no explicit "DONE" keyword — the absence of commands is the done signal.
This is more robust than keyword detection because models vary in how they phrase completion.

---

## Architecture

```
opsmate/
├── main.go                 Entry point — calls cmd.Execute()
├── go.mod                  Module: opsmate (Go 1.22+)
│
├── cmd/
│   └── root.go            cobra CLI: flags, sub-commands, help text
│
├── config/
│   └── config.go          Flat Config struct + defaults
│                          Why flat: simple, no nesting needed at this scale
│
└── agent/
    ├── agent.go           Core agentic loop controller
    │                      Owns: conversation history, loop, done-detection
    │
    ├── ollama.go          Ollama /api/chat HTTP client
    │                      Owns: API types, tool definition, health check
    │
    ├── parser.go          Command extraction from LLM responses
    │                      Two modes: structured tool_calls + markdown fallback
    │
    ├── executor.go        Shell command execution
    │                      Owns: sh -c, timeout, truncation, confirmation
    │
    └── ui.go              ANSI terminal output helpers
                           Why inline not library: 10 codes, zero dep overhead
```

### Key design decisions and justifications

**1. Ollama for local LLM inference**
Ollama was chosen over llama.cpp direct, LM Studio, or Jan because:
- Single stable REST API across all model backends (CPU/Metal/CUDA/ROCm)
- `ollama pull` handles quantization, downloading, and caching
- The API is partially OpenAI-compatible (easy to swap backends later)
- It handles concurrent requests gracefully

**2. Single `execute_command` tool instead of separate kubectl/docker/linux tools**
One general tool beats three specific ones because:
- SRE commands frequently combine tools: `kubectl get pod | grep Error | awk '{print $1}'`
- Fewer tools = simpler model decision-making = fewer mistakes
- Any new tool (helm, istioctl, awscli) works immediately without changing the schema
- The rich description teaches the model what's available

**3. Dual extraction: tool_calls + text parsing fallback**
Not all models support structured tool calling. Rather than requiring a specific
model, opsmate tries tool_calls first (reliable) then falls back to parsing
markdown code blocks (works with any model). This makes the tool usable with the
full Ollama model ecosystem.

**4. `sh -c` for command execution**
kubectl commands routinely use shell features: pipes, process substitution, variable
expansion. Parsing commands into `exec.Command(cmd, args...)` would break these.
`sh -c` gives the model full shell expressiveness. The safety trade-off is acceptable
because: (a) commands come from a trusted local model, (b) every command is shown
to the user before execution, (c) the default is to ask for confirmation.

**5. Full conversation history on every LLM call**
Ollama models are stateless. Sending full history ensures the model can reason about
causality across findings ("the OOMKill I saw in step 3 explains the restart pattern
from step 1"). The downside is growing token count, mitigated by `--max-output` to
keep individual results small.

**6. `--max-output` truncation**
`kubectl describe` and log outputs can be 100KB+. Truncating to 8KB:
- Protects the model's context window (smaller = faster inference)
- Forces the model to use targeted commands (`--tail=50`, `-o name`) rather than
  dumping everything at once
- Still captures the most relevant content (errors appear near the top of logs)

**7. Go as the implementation language**
- Single statically-linked binary: `scp opsmate server: ` and it runs, no deps
- Native concurrency if we add parallel command execution later
- Strong typing catches API shape mismatches at compile time
- Same language as kubectl/helm — familiar to the Kubernetes ecosystem

---

## Safety model

opsmate is designed to be safe to use in production environments:

| Scenario | How it's handled |
|----------|----------------|
| Accidental `kubectl delete` | Confirmation prompt shown before every command |
| Runaway investigation | `--max-iter` cap (default: 20 rounds) |
| Hung command | Per-command timeout (default: 60s, `--timeout`) |
| Huge log output filling context | Output truncated to `--max-output` bytes |
| Sensitive secret values | System prompt instructs model to check existence, not print values |
| Automated scripting | `-y` flag opts in explicitly; default is always confirm |

**The model is advisory, not autonomous.** Every command is shown to you with an
explanation before it runs. Type `q` at any prompt to quit immediately.

---

## Extending opsmate

### Add a new domain (e.g. AWS, Helm)

The system prompt in `agent/agent.go` is the main place to add domain knowledge.
Add a new section with relevant commands and patterns. No code changes required —
the model learns from the prompt.

### Add a new tool

1. Define the tool in `agent/ollama.go` alongside `executeCommandTool`
2. Add extraction logic in `agent/parser.go`
3. Add execution handling in `agent/agent.go`'s command dispatch

### Use a different LLM backend (OpenAI API, llama.cpp server)

Replace `agent/ollama.go` with a new client that:
- Implements the same `Chat(messages []Message, useTools bool) (*ChatResponse, error)` interface
- Maps your backend's response format to the `Message` / `ToolCall` types
- Implements `CheckHealth() error`

No other files need to change.

### Add streaming output

Currently `agent/ollama.go` uses `"stream": false`. To stream:
1. Set `"stream": true` in the request
2. Parse NDJSON chunks as they arrive in a goroutine
3. Print token-by-token to the terminal
4. Accumulate the full response for tool-call detection

---

## Troubleshooting

**`cannot reach Ollama at http://localhost:11434`**
```bash
ollama serve        # start the server
ollama list         # verify models are pulled
```

**`model "gemma3" not found`**
```bash
ollama pull gemma3
```

**Model doesn't use the execute_command tool (just writes text)**

Try disabling tool-calling so opsmate parses markdown blocks instead:
```bash
opsmate --tool-calling=false --model gemma3 "..."
```

Or switch to a model with better tool-calling support:
```bash
opsmate --model llama3.2 "..."
```

**Investigation produces too many iterations without conclusion**

Refine the problem statement to be more specific:
```bash
# Too vague:
opsmate "something is wrong with kubernetes"

# Better:
opsmate "pods in namespace checkout are CrashLoopBackOff since 14:30 UTC"
```

Or increase temperature slightly for more exploratory reasoning (edit `agent/ollama.go`).

**Output is cut off / LLM missing context**

Increase the output limit:
```bash
opsmate --max-output 20000 "..."
```

---

## License

MIT
