// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"github.com/ava-labs/avalanchego/api/health"
)

type BootstrapableEngine interface {
	AcceptedFrontierHandler
	AcceptedHandler
	AncestorsHandler
	InternalHandler

	// Start engine operations from given request ID
	Start(ctx context.Context, startReqID uint32) error

	Restart(startReqID uint32, f func(reqID uint32), vm VM)

	// Returns nil if the engine is healthy.
	// Periodically called and reported through the health API
	health.Checker

	// Clear removes all containers to be processed upon bootstrapping
	Clear(ctx context.Context) error
}

type AvalancheBootstrapableEngine interface {
	AncestorsHandler

	InternalHandler

	// Start engine operations from given request ID
	Start(ctx context.Context, startReqID uint32) error

	// Returns nil if the engine is healthy.
	// Periodically called and reported through the health API
	health.Checker

	// Clear removes all containers to be processed upon bootstrapping
	Clear(ctx context.Context) error
}
