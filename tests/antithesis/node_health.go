// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package antithesis

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// Waits for the nodes at the provided URIs to report healthy.
func awaitHealthyNodes(ctx context.Context, log logging.Logger, uris []string) error {
	for _, uri := range uris {
		if err := awaitHealthyNode(ctx, log, uri); err != nil {
			return err
		}
	}
	log.Info("all nodes reported healthy")
	return nil
}

func awaitHealthyNode(ctx context.Context, log logging.Logger, uri string) error {
	client := health.NewClient(uri)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	log.Info("awaiting node health",
		zap.String("uri", uri),
	)
	for {
		res, err := client.Health(ctx, nil)
		switch {
		case err != nil:
			log.Warn("failed to reach node",
				zap.String("uri", uri),
				zap.Error(err),
			)
		case res.Healthy:
			log.Info("node reported healthy",
				zap.String("uri", uri),
			)
			return nil
		default:
			log.Info("node reported unhealthy",
				zap.String("uri", uri),
			)
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("node health check cancelled at %s: %w", uri, ctx.Err())
		}
	}
}
