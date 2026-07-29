package ws

import (
	"context"

	"github.com/mdjarv/agentique/backend/internal/providers"
)

// defaultCatalog serves tests and any wiring that omits a catalog: base aliases
// plus whatever the provider CLIs advertise on disk, with no learned labels.
var defaultCatalog = providers.New()

func (c *conn) handleProvidersModels(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, _ struct{}) (providers.ListModelsResult, error) {
		catalog := c.catalog
		if catalog == nil {
			catalog = defaultCatalog
		}
		return catalog.ListModels(ctx), nil
	})
}
