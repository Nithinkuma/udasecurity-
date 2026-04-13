// opsmate — AI-powered SRE agent for Kubernetes, Docker, and Linux.
//
// opsmate connects a locally-running LLM (via Ollama) to a real shell,
// creating an agent that investigates infrastructure problems by running
// kubectl, docker, and linux commands iteratively until it reaches a diagnosis.
//
// Architecture:
//
//	main.go            — binary entry point; delegates everything to cmd/
//	cmd/root.go        — cobra CLI: flags, sub-commands, help text
//	config/config.go   — flat configuration struct + defaults
//	agent/agent.go     — the agentic loop (LLM ↔ executor orchestration)
//	agent/ollama.go    — HTTP client for the Ollama /api/chat endpoint
//	agent/parser.go    — extracts commands from tool_calls or markdown text
//	agent/executor.go  — runs sh -c <cmd> with timeout, confirmation, truncation
//	agent/ui.go        — ANSI-colored terminal output helpers
//
// The design deliberately keeps each concern in its own file so that adding
// a new backend (e.g. llama.cpp HTTP, OpenAI-compatible API) means writing
// a new ollama.go equivalent and wiring it into agent.go — no other files change.
package main

import "opsmate/cmd"

func main() {
	cmd.Execute()
}
