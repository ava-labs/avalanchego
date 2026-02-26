// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	ethcommon "github.com/ava-labs/libevm/common"
)

var _ adaptor.ChainVM[*blocks.Block] = (*SinceGenesis[hook.Transaction])(nil)

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
	fxs []*snowcommon.Fx,
	appSender snowcommon.AppSender,
) error {
	db := newEthDB(avaDB)
	config, err := createGenesisBlock(db, vm.config.DBConfig.TrieDBConfig(snowCtx), genesisBytes)
	if err != nil {
		return err
	}

	inner, err := NewVM(ctx, vm.hooks, vm.config, snowCtx, config, db, appSender)
	if err != nil {
		return err
	}
	vm.VM = inner
	return nil
}

func createGenesisBlock(db ethdb.Database, tdbConfig *triedb.Config, genesisBytes []byte) (_ *params.ChainConfig, err error) {
	tdb := triedb.NewDatabase(db, tdbConfig)
	defer func() {
		err = errors.Join(err, tdb.Close())
	}()

	genesis := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, genesis); err != nil {
		return nil, fmt.Errorf("json.Unmarshal(%T): %v", genesis, err)
	}
	config, hash, err := core.SetupGenesisBlock(db, tdb, genesis)
	if err != nil {
		return nil, fmt.Errorf("core.SetupGenesisBlock(...): %v", err)
	}

	// [NewVM] assumes that the genesis block is "finalized", which does not
	// happen in [core.SetupGenesisBlock]. This MUST only happen once.
	if rawdb.ReadFinalizedBlockHash(db) == (ethcommon.Hash{}) {
		rawdb.WriteFinalizedBlockHash(db, hash)
	}

	return config, nil
}

// Shutdown gracefully closes the VM.
func (vm *SinceGenesis[_]) Shutdown(ctx context.Context) error {
	if vm.VM == nil {
		return nil
	}
	return vm.VM.Shutdown(ctx)
}
