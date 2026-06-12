// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

var _ adaptor.SyncableVM[*blocks.Block, *summary] = (*SinceGenesis[hook.Transaction])(nil)

// SinceGenesis is a harness around a [VM], providing an `Initialize` method
// that treats the chain as being asynchronous since genesis.
type SinceGenesis[T hook.Transaction] struct {
	*VM[T] // created by [SinceGenesis.Initialize]
}

// Initialize implements [adaptor.SyncableVM].
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (*SinceGenesis[T]) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	avaDB database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	panic("unimplemented")
}

// Shutdown implements [adaptor.SyncableVM].
func (*SinceGenesis[T]) Shutdown(ctx context.Context) error {
	panic("unimplemented")
}

// StateSyncEnabled implements [adaptor.SyncableVM].
func (*SinceGenesis[T]) StateSyncEnabled(context.Context) (bool, error) {
	panic("unimplemented")
}
