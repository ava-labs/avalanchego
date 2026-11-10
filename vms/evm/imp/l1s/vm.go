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
	"github.com/ava-labs/avalanchego/vms/evm/adaptor"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blockdelay"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/hookadaptor"
	"github.com/ava-labs/avalanchego/vms/evm/network"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
	"github.com/ava-labs/avalanchego/vms/evm/types"

	avadb "github.com/ava-labs/avalanchego/database"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

var _ adaptor.ChainVM[*blocks.Block] = (*VM)(nil)

// VM wraps an [sae.VM] with the pieces specific to L1s.
type VM struct {
	*sae.VM          // created by [VM.Initialize]
	*network.Network // created by [VM.Initialize]

	// now is the clock provided to the [sae.VM] and is used for block building.
	now   func() time.Time
	pacer *blockdelay.Pacer // created by [VM.Initialize]
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
	genesis, err := parseGenesis(snowCtx, genesisBytes, upgradeBytes)
	if err != nil {
		return fmt.Errorf("parsing genesis: %w", err)
	}

	// When run as a plugin, the rpcchainvm wraps the provided database, so
	// [prefixdb.New] and [prefixdb.NewNested] are equivalent. Using
	// [prefixdb.NewNested] keeps the database format unchanged if this VM is
	// ever run in-process, where [prefixdb.New] would compress the prefixes.
	ethDB := types.NewEthDB(prefixdb.NewNested(ethDBPrefix, avaDB))
	if err := genesis.verifyAndWriteBlock(ethDB); err != nil {
		return fmt.Errorf("writing genesis block: %w", err)
	}

	// TODO: Allow the mempool to be configured by the user config.
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

	vm.pacer = blockdelay.NewPacer(vm.now)
	vm.Network, err = network.New(snowCtx, appSender)
	if err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	// TODO: Support state sync, which requires the [sae.VM] to be created
	// after the node has finished state syncing.
	vm.VM, err = sae.NewVM(ctx, hookadaptor.Convert(newHooks(snowCtx, vm.now)), saeConfig, snowCtx, genesis.Config, ethDB, vm.Network)
	if err != nil {
		return fmt.Errorf("creating SAE VM: %w", err)
	}
	return nil
}

// WaitForEvent waits until the ACP-226 minimum block delay since the preferred
// block has elapsed, then waits for the SAE VM to produce an event.
func (vm *VM) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	return vm.pacer.WaitForEvent(ctx, earliestBuildTime(vm.VM.GetPreference()), vm.VM.WaitForEvent)
}

// earliestBuildTime returns the earliest wall-clock time at which a child of b
// may be built.
func earliestBuildTime(b *blocks.Block) time.Time {
	h := b.Header()
	return blockdelay.Earliest(blockTime(h), delayExponent(h))
}

// Shutdown gracefully closes the VM.
func (vm *VM) Shutdown(ctx context.Context) error {
	if vm.VM == nil {
		return nil
	}
	return vm.VM.Shutdown(ctx)
}
