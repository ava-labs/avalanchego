// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package l1s implements the Subnet-EVM compatible L1 VM atop [sae.VM].
package l1s

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/core/txpool/legacypool"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/types"

	avadb "github.com/ava-labs/avalanchego/database"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

var _ adaptor.ChainVM[*blocks.Block] = (*VM)(nil)

// VM wraps an [sae.VM] with the pieces specific to L1s.
type VM struct {
	*sae.VM          // created by [VM.Initialize]
	*network.Network // created by [VM.Initialize]

	// now is the clock provided to the [sae.VM] and is used for block building.
	now func() time.Time
}

var ethDBPrefix = []byte("ethdb")

// Initialize initializes the VM.
func (vm *VM) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB avadb.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	_ []byte, // TODO: Parse the user config.
	_ []*snowcommon.Fx,
	appSender snowcommon.AppSender,
) error {
	// TODO: Read the airdrop file configured by the user config.
	genesis, err := parseGenesis(snowCtx, genesisBytes, upgradeBytes, nil)
	if err != nil {
		return fmt.Errorf("parsing genesis: %w", err)
	}

	// [prefixdb.NewNested] is used because subnet-evm is run as a plugin.
	// This means that the database's prefix is not compacted, because the
	// provided database is wrapped by the rpcchainvm.
	ethDB := types.NewEthDB(prefixdb.NewNested(ethDBPrefix, avaDB))
	if err := genesis.verifyAndWriteBlock(ethDB); err != nil {
		return fmt.Errorf("writing genesis block: %w", err)
	}

	// TODO: Allow local transactions to be enabled by the user config.
	mempoolConfig := legacypool.DefaultConfig
	mempoolConfig.NoLocals = true
	saeConfig := sae.Config{
		MempoolConfig: mempoolConfig,
		DBConfig: saedb.Config{
			TrieCacheMiB:     saedb.DefaultTrieCacheSizeMiB,
			SnapshotCacheMiB: saedb.DefaultSnapshotCacheSizeMiB,
			CommitInterval:   saedb.DefaultCommitInterval,
		},
		RPCConfig: rpc.Config{
			APIs: rpc.DefaultAPIs(),
		},
		Now: vm.now,
	}
	tdbConfig := saeConfig.DBConfig.TrieDBConfig(snowCtx.ChainDataDir, snowCtx.Log)
	if err := genesis.setupTrieDB(ethDB, tdbConfig); err != nil {
		return fmt.Errorf("setting up genesis trie: %w", err)
	}

	vm.Network, err = network.New(snowCtx, appSender)
	if err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	// TODO: Support state sync, which requires the [sae.VM] to be created
	// after the node has finished state syncing.
	vm.VM, err = sae.NewVM(ctx, newHooks(snowCtx, vm.now), saeConfig, snowCtx, genesis.Config, ethDB, vm.Network)
	if err != nil {
		return fmt.Errorf("creating SAE VM: %w", err)
	}
	return nil
}

// Shutdown gracefully closes the VM.
func (vm *VM) Shutdown(ctx context.Context) error {
	if vm.VM == nil {
		return nil
	}
	return vm.VM.Shutdown(ctx)
}
