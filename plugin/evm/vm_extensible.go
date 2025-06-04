// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/plugin/evm/extension"
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

// IsBootstrapped returns true if the VM has finished bootstrapping
func (vm *VM) IsBootstrapped() bool {
	return vm.bootstrapped.Get()
}
