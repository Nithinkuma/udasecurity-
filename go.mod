// opsmate - AI-powered SRE agent using locally-running LLMs via Ollama.
//
// Why "opsmate"? Operations + Mate (teammate). Short, memorable, and describes
// the tool's purpose: a teammate that helps with operations work.
//
// Why a Go module at all? Go produces statically-linked binaries with no runtime
// dependencies, making opsmate trivially distributable - just copy the binary.
// No need for Python virtualenvs or Node.js installations on the target machine.
module opsmate

go 1.22

require github.com/spf13/cobra v1.8.0

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
)
