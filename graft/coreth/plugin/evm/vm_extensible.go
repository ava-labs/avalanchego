// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	vmsync "github.com/ava-labs/coreth/plugin/evm/sync"
	"github.com/prometheus/client_golang/prometheus"
)

var _ extension.InnerVM = (*VM)(nil)

var (
	errVMAlreadyInitialized      = errors.New("vm already initialized")
	errExtensionConfigAlreadySet = errors.New("extension config already set")
)

func (vm *VM) SetExtensionConfig(config *extension.Config) error {
	if vm.ctx != nil {
		return errVMAlreadyInitialized
	}
	if vm.extensionConfig != nil {
		return errExtensionConfigAlreadySet
	}
	vm.extensionConfig = config
	return nil
}

// All these methods below assumes that VM is already initialized

func (vm *VM) GetExtendedBlock(ctx context.Context, blkID ids.ID) (extension.ExtendedBlock, error) {
	// Since each internal handler used by [vm.State] always returns a block
	// with non-nil ethBlock value, GetBlockInternal should never return a
	// (*Block) with a nil ethBlock value.
	blk, err := vm.GetBlockInternal(ctx, blkID)
	if err != nil {
		return nil, err
	}

	return blk.(*wrappedBlock), nil
}

func (vm *VM) LastAcceptedExtendedBlock() extension.ExtendedBlock {
	lastAcceptedBlock := vm.LastAcceptedBlockInternal()
	if lastAcceptedBlock == nil {
		return nil
	}
	return lastAcceptedBlock.(*wrappedBlock)
}

func (vm *VM) ChainConfig() *params.ChainConfig {
	return vm.chainConfig
}

func (vm *VM) Blockchain() *core.BlockChain {
	return vm.blockChain
}

func (vm *VM) Config() config.Config {
	return vm.config
}

func (vm *VM) MetricRegistry() *prometheus.Registry {
	return vm.sdkMetrics
}

func (vm *VM) Validators() *p2p.Validators {
	return vm.P2PValidators()
}

func (vm *VM) VersionDB() *versiondb.Database {
	return vm.versiondb
}

func (vm *VM) SyncerClient() vmsync.Client {
	return vm.Client
}
