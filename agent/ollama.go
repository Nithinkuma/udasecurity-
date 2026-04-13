package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaClient handles all HTTP communication with the Ollama inference server.
//
// Why Ollama and not direct GGUF loading or llama.cpp?
// Ollama provides:
//   - Model management (pull, list, delete) via CLI
//   - Hardware acceleration (Metal on Mac, CUDA on NVIDIA, ROCm on AMD) out of the box
//   - A stable REST API that works the same regardless of backend
//   - OpenAI-compatible endpoint (/v1/chat/completions) for future compatibility
//   - Concurrent request handling when multiple opsmate sessions are active
//
// We use /api/chat rather than /v1/chat/completions because the native Ollama
// API returns richer metadata (model info, token counts) and is more stable.
type OllamaClient struct {
	endpoint   string
	model      string
	httpClient *http.Client
}

// --- Conversation message types ---

// Message is a single turn in the conversation history sent to the model.
//
// Role values:
//   - "system"    : Instructions given once at the start (the SRE prompt)
//   - "user"      : Human turn (problem description or command results in fallback mode)
//   - "assistant" : Model's response (analysis text + optional tool_calls)
//   - "tool"      : Result of a tool call, sent back after execution
//
// Why maintain full history? The model needs context of what it already checked
// to avoid re-running the same commands and to reason about causality between findings.
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is a structured function invocation returned by the model.
// When the model "calls a tool" it means: "please run this function and give me the result."
// In our case, the only tool is execute_command.
type ToolCall struct {
	// ID uniquely identifies this call within a response.
	// Required when sending back tool results so multi-tool responses stay aligned.
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction holds the name and raw JSON arguments of a tool call.
// We keep Arguments as json.RawMessage to defer parsing — the model sometimes
// produces slightly non-standard JSON (e.g. trailing commas) that we handle in parser.go.
type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// --- Tool definition types (sent to model to teach it available tools) ---

// Tool describes a function the model is allowed to call.
// We follow the OpenAI function-calling schema which Ollama also implements.
// Sending a clear, descriptive definition significantly improves model accuracy.
type Tool struct {
	Type     string       `json:"type"`     // always "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction is the function descriptor within a Tool.
type ToolFunction struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  ToolParams `json:"parameters"`
}

// ToolParams uses JSON Schema format to describe accepted arguments.
type ToolParams struct {
	Type       string              `json:"type"` // always "object"
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required"`
}

// Property describes a single argument using JSON Schema.
type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// --- API request/response types ---

// ChatRequest is the full payload for POST /api/chat.
type ChatRequest struct {
	Model    string                 `json:"model"`
	Messages []Message              `json:"messages"`
	Tools    []Tool                 `json:"tools,omitempty"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

// ChatResponse is the response body from POST /api/chat (non-streaming).
type ChatResponse struct {
	Model   string  `json:"model"`
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

// executeCommandTool is the single tool definition we expose to the model.
//
// Design decision: ONE general tool vs. separate kubectl/docker/linux tools.
//
// One tool wins because:
//  1. SRE commands frequently chain: kubectl get pod | grep -v Running | awk '{print $1}'
//     A single tool handles this naturally; separate tools would require the model
//     to "assemble" the pipeline across multiple calls.
//  2. Fewer tools = simpler model decision-making = fewer "wrong tool" errors.
//  3. Future extensibility is free: any new CLI tool (helm, istioctl, awscli)
//     works immediately without changing the tool definition.
//  4. The description gives the model enough context to use appropriate commands.
var executeCommandTool = Tool{
	Type: "function",
	Function: ToolFunction{
		Name: "execute_command",
		Description: `Execute a shell command to investigate or diagnose infrastructure issues.
Commands run in a real shell (sh -c) with your current environment (kubectl context,
Docker socket, AWS credentials, etc.) fully available.

Use this for: kubectl, docker, helm, istioctl, linux (ps, df, netstat, journalctl,
lsof, ss, curl, dig), database CLIs (psql, mysql, redis-cli), and any other tool
in your PATH. Pipes, redirects, and command chaining are fully supported.

Diagnostic-only: prefer read-only commands. Do not make changes unless the user
explicitly requests remediation.`,
		Parameters: ToolParams{
			Type: "object",
			Properties: map[string]Property{
				"command": {
					Type: "string",
					Description: "The exact shell command to run. " +
						"Supports pipes (|), redirects (>), and chaining (&&, ;). " +
						"Example: kubectl get pods -n myapp --sort-by=.status.startTime | tail -20",
				},
				"explanation": {
					Type: "string",
					Description: "One sentence: what you expect this command to reveal " +
						"and why it helps diagnose the issue.",
				},
			},
			Required: []string{"command", "explanation"},
		},
	},
}

// NewOllamaClient creates an Ollama API client for the given endpoint and model.
func NewOllamaClient(endpoint, model string) *OllamaClient {
	return &OllamaClient{
		endpoint: endpoint,
		model:    model,
		// Long timeout: local LLM inference on CPU/GPU can take 10-120 seconds
		// depending on model size and hardware. 10 minutes is generous enough for
		// even a 70B model on CPU while avoiding indefinite hangs.
		httpClient: &http.Client{Timeout: 10 * time.Minute},
	}
}

// Chat sends the conversation history to the Ollama model and returns its response.
//
// When useTools=true, we include the execute_command tool definition so the model
// can return structured tool_calls. When false, the model responds with plain text
// and we parse markdown code blocks in parser.go.
func (c *OllamaClient) Chat(messages []Message, useTools bool) (*ChatResponse, error) {
	req := ChatRequest{
		Model:    c.model,
		Messages: messages,
		// Non-streaming: we wait for the full response before processing.
		// Streaming would print tokens as they arrive (better perceived latency)
		// but requires parsing chunked NDJSON and complicates tool-call detection.
		// Streaming is a worthwhile future improvement once the core loop is stable.
		Stream: false,
		Options: map[string]interface{}{
			// Low temperature = deterministic, focused reasoning.
			// SRE diagnosis benefits from precision (run the right command) not
			// creativity (run an interesting command). 0.1 balances reliability
			// with enough variation to not get stuck in loops.
			"temperature": 0.1,
			// Context window: 16K tokens fits most investigations including
			// kubectl describe output and pod logs. Reduce to 8192 on machines
			// with limited VRAM.
			"num_ctx": 16384,
		},
	}
	if useTools {
		req.Tools = []Tool{executeCommandTool}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	resp, err := c.httpClient.Post(
		c.endpoint+"/api/chat",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("POST /api/chat: %w\n"+
			"Is Ollama running? Start it with: ollama serve", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama returned HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decode /api/chat response: %w", err)
	}

	return &chatResp, nil
}

// CheckHealth is intentionally removed.
// All installation, server-start, and model-pull logic now lives in provision.go
// via the Provision() function, which is called at the start of every investigation.
// This keeps the OllamaClient focused solely on making API calls.
