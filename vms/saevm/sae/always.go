// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

var _ adaptor.SyncVM[*blocks.Block, *stateSummary] = (*SinceGenesis[hook.Transaction])(nil)

// SinceGenesis is a harness around a [VM], providing an `Initialize` method
// that treats the chain as being asynchronous since genesis.
type SinceGenesis[T hook.Transaction] struct {
	*VM // created by [SinceGenesis.Initialize]

	hooks  hook.PointsG[T]
	config Config
}

// NewSinceGenesis constructs a new [SinceGenesis].
func NewSinceGenesis[T hook.Transaction](hooks hook.PointsG[T], c Config) *SinceGenesis[T] {
	return &SinceGenesis[T]{
		hooks:  hooks,
		config: c,
	}
}

// Initialize initializes the VM.
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (vm *SinceGenesis[_]) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	db := newEthDB(avaDB)
	tdb := triedb.NewDatabase(db, vm.config.DBConfig.TrieDBConfig)

	genesis := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, genesis); err != nil {
		return fmt.Errorf("json.Unmarshal(%T): %v", genesis, err)
	}
	config, _, err := core.SetupGenesisBlock(db, tdb, genesis)
	if err != nil {
		return fmt.Errorf("core.SetupGenesisBlock(...): %v", err)
	}

	inner, err := NewVM(ctx, vm.hooks, vm.config, snowCtx, config, db, genesis.ToBlock(), appSender)
	if err != nil {
		return err
	}
	vm.VM = inner
	return nil
}

// Shutdown gracefully closes the VM.
func (vm *SinceGenesis[_]) Shutdown(ctx context.Context) error {
	if vm.VM == nil {
		return nil
	}
	return vm.VM.Shutdown(ctx)
}
