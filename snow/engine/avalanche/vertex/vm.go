// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type LinearizableVMWithEngine interface {
	DAGVM

	// Linearize is called after [Initialize] and after the DAG has been
	// finalized. After Linearize is called:
	//
	// - PendingTxs will never be called again
	// - GetTx will never be called again
	// - ParseTx may still be called
	// - All the block based functions of the [block.ChainVM] must work as
	//   expected.
	//
	// Linearize is part of the VM initialization, and will be called at most
	// once per VM instantiation. This means that Linearize should be called
	// every time the chain restarts after the DAG has finalized.
	Linearize(
		ctx context.Context,
		stopVertexID ids.ID,
	) error
}

type LinearizableVM interface {
	DAGVM

	// Linearize is called after [Initialize] and after the DAG has been
	// finalized. After Linearize is called:
	//
	// - PendingTxs will never be called again
	// - GetTx will never be called again
	// - ParseTx may still be called
	// - All the block based functions of the [block.ChainVM] must work as
	//   expected.
	//
	// Linearize is part of the VM initialization, and will be called at most
	// once per VM instantiation. This means that Linearize should be called
	// every time the chain restarts after the DAG has finalized.
	Linearize(ctx context.Context, stopVertexID ids.ID) error
}

// DAGVM defines the minimum functionality that an avalanche VM must
// implement
type DAGVM interface {
	block.ChainVM

	// Convert a stream of bytes to a transaction or return an error
	ParseTx(ctx context.Context, txBytes []byte) (snowstorm.Tx, error)
}
