// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reexecute

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	MainnetCChainID = ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")

	mainnetXChainID    = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
	mainnetAvaxAssetID = ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
)

type BenchmarkExecutorConfig struct {
	// The directory where blocks will be sourced from.
	BlockDir string

	// StartBlock is the height of the first block that will be executed.
	StartBlock uint64
	// EndBlock is the height of the last block that will be executed.
	EndBlock uint64
	// ChanSize is the size of the channel to use for block processing.
	ChanSize int
	// ExecutionTimeout is the duration to run the reexecution test for before
	// termininating (without error). If 0, no timeout is applied.
	ExecutionTimeout time.Duration

	// PrefixGatherer is the top-most gatherer where all metrics can be derived
	// from (e.g. VM metrics, consensus metrics). If MetricsServerEnabled is
	// true, then the metrics server will export metrics from this gatherer.
	PrefixGatherer metrics.MultiGatherer
	// ConsensusRegistry is the registry where a subset of the metrics from snowman consensus
	// [engine](../../snow/engine/snowman/metrics.go) will be registered.
	ConsensusRegistry prometheus.Registerer

	// MetricsServerEnabled determines whether to enable a Prometheus server
	// exporting VM metrics.
	MetricsServerEnabled bool
	// MetricsPort is the port where the metrics server will listen to.
	MetricsPort uint64
	// MetricsCollectorEnabled determines whether to start a Prometheus
	// collector.
	MetricsCollectorEnabled bool
	// MetricsLabels represents the set of labels that will be attached to all
	// exported metrics.
	MetricsLabels map[string]string
	// SDConfigName is the name of the SDConfig file.
	SDConfigName string
	// NetworkUUID is the unique identifier corresponding to the benchmark.
	NetworkUUID string
	// DashboardPath is the relative Grafana dashboard path used to construct
	// the metrics visualization URL when MetricsCollectorEnabled is true.
	// Expected format: "d/dashboard-id/dashboard-name" (e.g., "d/Gl1I20mnk/c-chain").
	DashboardPath string
}

// BenchmarkExecutor is a tool for executing a sequence of blocks against a VM.
type BenchmarkExecutor struct {
	config BenchmarkExecutorConfig
}

func NewBenchmarkExecutor(config BenchmarkExecutorConfig) BenchmarkExecutor {
	return BenchmarkExecutor{config: config}
}

// Run executes a sequence of blocks from StartBlock to EndBlock against the
// provided VM. It also manages metrics collection and reporting by optionally
// starting a Prometheus server and collector based on the executor's
// configuration.
func (e BenchmarkExecutor) Run(b testing.TB, log logging.Logger, vm block.ChainVM) {
	r := require.New(b)
	ctx := b.Context()

	if e.config.MetricsServerEnabled {
		serverAddr := startServer(b, log, e.config.PrefixGatherer, e.config.MetricsPort)

		if e.config.MetricsCollectorEnabled {
			startCollector(
				b,
				log,
				e.config.SDConfigName,
				e.config.MetricsLabels,
				serverAddr,
				e.config.NetworkUUID,
				e.config.DashboardPath,
			)
		}
	}

	blockChan, err := createBlockChanFromLevelDB(
		b,
		e.config.BlockDir,
		e.config.StartBlock,
		e.config.EndBlock,
		e.config.ChanSize,
	)
	r.NoError(err)

	vmExecutor, err := newVMExecutor(
		tests.NewDefaultLogger("vm-executor"),
		vm,
		e.config.ConsensusRegistry,
		e.config.ExecutionTimeout,
		e.config.StartBlock,
		e.config.EndBlock,
	)
	r.NoError(err)

	r.NoError(vmExecutor.executeSequence(ctx, blockChan))
}

// NewMainnetVM creates and initializes a VM configured for mainnet block
// reexecution tests. The VM is initialized with mainnet-specific settings
// including the mainnet network ID, upgrade schedule, and chain configurations.
// Both subnetID and chainID must correspond to subnets/chains that exist on mainnet.
func NewMainnetVM(
	ctx context.Context,
	factory vms.Factory,
	db database.Database,
	chainDataDir string,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	subnetID ids.ID,
	chainID ids.ID,
	metricsGatherer metrics.MultiGatherer,
) (block.ChainVM, error) {
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
	warpSigner := warp.NewSigner(blsKey, constants.MainnetID, chainID)

	sharedMemoryDB := prefixdb.New([]byte("sharedmemory"), db)
	atomicMemory := atomic.NewMemory(sharedMemoryDB)

	chainIDToSubnetID := map[ids.ID]ids.ID{
		mainnetXChainID: constants.PrimaryNetworkID,
		MainnetCChainID: constants.PrimaryNetworkID,
		chainID:         subnetID,
		ids.Empty:       constants.PrimaryNetworkID,
	}

	if err := vm.Initialize(
		ctx,
		&snow.Context{
			NetworkID:       constants.MainnetID,
			SubnetID:        subnetID,
			ChainID:         chainID,
			NodeID:          ids.GenerateTestNodeID(),
			PublicKey:       blsPublicKey,
			NetworkUpgrades: upgrade.Mainnet,

			XChainID:    mainnetXChainID,
			CChainID:    MainnetCChainID,
			AVAXAssetID: mainnetAvaxAssetID,

			Log:          tests.NewDefaultLogger("mainnet-vm-reexecution"),
			SharedMemory: atomicMemory.NewSharedMemory(chainID),
			BCLookup:     ids.NewAliaser(),
			Metrics:      metricsGatherer,

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
		prefixdb.New([]byte("vm"), db),
		genesisBytes,
		upgradeBytes,
		configBytes,
		nil,
		&enginetest.Sender{},
	); err != nil {
		return nil, fmt.Errorf("failed to initialize VM: %w", err)
	}

	return vm, nil
}
