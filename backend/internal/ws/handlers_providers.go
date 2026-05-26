package ws

import (
	"context"

	"github.com/mdjarv/agentique/backend/internal/providers"
)

func (c *conn) handleProvidersModels(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, _ struct{}) (providers.ListModelsResult, error) {
		return providers.ListModels(ctx), nil
	})
}
