package tools

import "github.com/gopact-ai/gopact"

// ToolHandler aliases the root tool middleware contract for registry users.
type ToolHandler = gopact.ToolHandler

// ToolContext aliases the root tool middleware context for registry users.
type ToolContext = gopact.ToolContext

// ToolContextOptions aliases the root tool context options.
type ToolContextOptions = gopact.ToolContextOptions

var (
	// NewToolContext aliases the root constructor.
	NewToolContext = gopact.NewToolContext
	// ComposeToolHandler aliases the root middleware composer.
	ComposeToolHandler = gopact.ComposeToolHandler
)
