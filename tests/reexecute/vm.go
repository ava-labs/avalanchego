// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reexecute

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/MetalBlockchain/metalgo/api/metrics"
	"github.com/MetalBlockchain/metalgo/chains/atomic"
	"github.com/MetalBlockchain/metalgo/database"
	"github.com/MetalBlockchain/metalgo/database/prefixdb"
	"github.com/MetalBlockchain/metalgo/genesis"
	"github.com/MetalBlockchain/metalgo/graft/coreth/plugin/factory"
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/snow/engine/enginetest"
	"github.com/MetalBlockchain/metalgo/snow/engine/snowman/block"
	"github.com/MetalBlockchain/metalgo/snow/validators/validatorstest"
	"github.com/MetalBlockchain/metalgo/tests"
	"github.com/MetalBlockchain/metalgo/upgrade"
	"github.com/MetalBlockchain/metalgo/utils/constants"
	"github.com/MetalBlockchain/metalgo/utils/crypto/bls/signer/localsigner"
	"github.com/MetalBlockchain/metalgo/utils/logging"
	"github.com/MetalBlockchain/metalgo/vms/metervm"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/warp"
)

var (
	mainnetXChainID    = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
	mainnetCChainID    = ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")
	mainnetAvaxAssetID = ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
)

func NewMainnetCChainVM(
	ctx context.Context,
	vmAndSharedMemoryDB database.Database,
	chainDataDir string,
	configBytes []byte,
	vmMultiGatherer metrics.MultiGatherer,
	meterVMRegistry prometheus.Registerer,
) (block.ChainVM, error) {
	factory := factory.Factory{}
	vmIntf, err := factory.New(logging.NoLog{})
	if err != nil {
		return nil, fmt.Errorf("failed to create VM from factory: %w", err)
	}
	vm := vmIntf.(block.ChainVM)

	blsKey, err := localsigner.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create BLS key: %w", err)
	}

	blsPublicKey := blsKey.PublicKey()
	warpSigner := warp.NewSigner(blsKey, constants.MainnetID, mainnetCChainID)

	genesisConfig := genesis.GetConfig(constants.MainnetID)

	sharedMemoryDB := prefixdb.New([]byte("sharedmemory"), vmAndSharedMemoryDB)
	atomicMemory := atomic.NewMemory(sharedMemoryDB)

	chainIDToSubnetID := map[ids.ID]ids.ID{
		mainnetXChainID: constants.PrimaryNetworkID,
		mainnetCChainID: constants.PrimaryNetworkID,
		ids.Empty:       constants.PrimaryNetworkID,
	}

	vm = metervm.NewBlockVM(vm, meterVMRegistry)

	if err := vm.Initialize(
		ctx,
		&snow.Context{
			NetworkID:       constants.MainnetID,
			SubnetID:        constants.PrimaryNetworkID,
			ChainID:         mainnetCChainID,
			NodeID:          ids.GenerateTestNodeID(),
			PublicKey:       blsPublicKey,
			NetworkUpgrades: upgrade.Mainnet,

			XChainID:    mainnetXChainID,
			CChainID:    mainnetCChainID,
			AVAXAssetID: mainnetAvaxAssetID,

			Log:          tests.NewDefaultLogger("mainnet-vm-reexecution"),
			SharedMemory: atomicMemory.NewSharedMemory(mainnetCChainID),
			BCLookup:     ids.NewAliaser(),
			Metrics:      vmMultiGatherer,

			WarpSigner: warpSigner,

			ValidatorState: &validatorstest.State{
				GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
					subnetID, ok := chainIDToSubnetID[chainID]
					if ok {
						return subnetID, nil
					}
					return ids.Empty, fmt.Errorf("unknown chainID: %s", chainID)
				},
			},
			ChainDataDir: chainDataDir,
		},
		prefixdb.New([]byte("vm"), vmAndSharedMemoryDB),
		[]byte(genesisConfig.CChainGenesis),
		nil,
		configBytes,
		nil,
		&enginetest.Sender{},
	); err != nil {
		return nil, fmt.Errorf("failed to initialize VM: %w", err)
	}

	return vm, nil
}
