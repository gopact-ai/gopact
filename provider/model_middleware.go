package provider

import "github.com/gopact-ai/gopact"

// ModelHandler aliases the root model middleware contract for provider users.
type ModelHandler = gopact.ModelHandler

// ModelContext aliases the root model middleware context for provider users.
type ModelContext = gopact.ModelContext

// ModelContextOptions aliases the root model context options.
type ModelContextOptions = gopact.ModelContextOptions

var (
	// NewModelContext aliases the root constructor.
	NewModelContext = gopact.NewModelContext
	// ComposeModelHandler aliases the root middleware composer.
	ComposeModelHandler = gopact.ComposeModelHandler
)
