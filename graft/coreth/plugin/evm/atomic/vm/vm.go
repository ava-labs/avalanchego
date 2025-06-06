// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"fmt"

	avalanchedatabase "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	avalanchecommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/coreth/plugin/evm/atomic/txpool"
	"github.com/ava-labs/coreth/plugin/evm/extension"

	"github.com/ava-labs/libevm/common"
)

var (
	_ block.ChainVM                      = (*VM)(nil)
	_ block.BuildBlockWithContextChainVM = (*VM)(nil)
	_ block.StateSyncableVM              = (*VM)(nil)
)

// TODO: remove this
// InnerVM is the interface that must be implemented by the VM
// that's being wrapped by the extension
type InnerVM interface {
	extension.ExtensibleVM
	avalanchecommon.VM
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.StateSyncableVM

	// TODO: remove these
	AtomicBackend() *state.AtomicBackend
	AtomicMempool() *txpool.Mempool
}

type VM struct {
	InnerVM
	ctx *snow.Context
}

func WrapVM(vm InnerVM) *VM {
	return &VM{InnerVM: vm}
}

// Initialize implements the snowman.ChainVM interface
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db avalanchedatabase.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- avalanchecommon.Message,
	fxs []*avalanchecommon.Fx,
	appSender avalanchecommon.AppSender,
) error {
	vm.ctx = chainCtx

	var extDataHashes map[common.Hash]common.Hash
	// Set the chain config for mainnet/fuji chain IDs
	switch chainCtx.NetworkID {
	case constants.MainnetID:
		extDataHashes = mainnetExtDataHashes
	case constants.FujiID:
		extDataHashes = fujiExtDataHashes
	}
	// Free the memory of the extDataHash map
	fujiExtDataHashes = nil
	mainnetExtDataHashes = nil

	// Create the atomic extension structs
	// some of them need to be initialized after the inner VM is initialized
	blockExtender := newBlockExtender(extDataHashes, vm)

	extensionConfig := &extension.Config{
		BlockExtender: blockExtender,
	}
	if err := vm.SetExtensionConfig(extensionConfig); err != nil {
		return fmt.Errorf("failed to set extension config: %w", err)
	}

	// Initialize inner vm with the provided parameters
	if err := vm.InnerVM.Initialize(
		ctx,
		chainCtx,
		db,
		genesisBytes,
		upgradeBytes,
		configBytes,
		toEngine,
		fxs,
		appSender,
	); err != nil {
		return fmt.Errorf("failed to initialize inner VM: %w", err)
	}
	return nil
}

func (vm *VM) chainConfigExtra() *extras.ChainConfig {
	return params.GetExtra(vm.Ethereum().BlockChain().Config())
}
