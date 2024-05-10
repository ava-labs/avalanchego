// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package antithesis

import (
	"context"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/api/health"
)

// Waits for the nodes at the provided URIs to report healthy.
func AwaitHealthyNodes(ctx context.Context, uris []string) {
	for _, uri := range uris {
		awaitHealthyNode(ctx, uri)
	}
	log.Println("all nodes reported healthy")
}

func awaitHealthyNode(ctx context.Context, uri string) {
	client := health.NewClient(uri)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	log.Printf("awaiting node health at %s", uri)
	for {
		res, err := client.Health(ctx, nil)
		switch {
		case err != nil:
			log.Printf("node couldn't be reached at %s", uri)
		case res.Healthy:
			log.Printf("node reported healthy at %s", uri)
			return
		default:
			log.Printf("node reported unhealthy at %s", uri)
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			log.Fatalf("node health check cancelled at %s", uri)
		}
	}
}
