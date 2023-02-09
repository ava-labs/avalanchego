// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stats

import (
	warpStats "github.com/ava-labs/subnet-evm/handlers/warp/stats"
	syncStats "github.com/ava-labs/subnet-evm/sync/handlers/stats"
)

var (
	_ HandlerStats = &handlerStats{}
	_ HandlerStats = &MockHandlerStats{}
)

// HandlerStats reports prometheus metrics for the network handlers
type HandlerStats interface {
	warpStats.SignatureRequestHandlerStats
	syncStats.HandlerStats
}

type handlerStats struct {
	// State sync handler metrics
	syncStats.HandlerStats

	// Warp handler metrics
	warpStats.SignatureRequestHandlerStats
}

func NewHandlerStats(enabled bool) HandlerStats {
	if !enabled {
		return &MockHandlerStats{}
	}
	return &handlerStats{
		HandlerStats:                 syncStats.NewHandlerStats(enabled),
		SignatureRequestHandlerStats: warpStats.NewStats(enabled),
	}
}
