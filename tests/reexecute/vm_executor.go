// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reexecute

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
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
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	MainnetCChainID = ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")

	mainnetXChainID    = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
	mainnetAvaxAssetID = ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
)

type VMExecutor struct {
	log              logging.Logger
	vm               block.ChainVM
	metrics          *consensusMetrics
	executionTimeout time.Duration
	startBlock       uint64
	endBlock         uint64
	blkChan          <-chan blockResult
	etaTracker       *timer.EtaTracker
}

// NewVMExecutor creates a VMExecutor that reexecutes blocks from startBlock to
// endBlock. NewVMExecutor starts reading blocks from blockDir and sets up
// metrics for the reexecution test.
func NewVMExecutor(
	tb testing.TB,
	vm block.ChainVM,
	blockDir string,
	startBlock uint64,
	endBlock uint64,
	chanSize int,
	executionTimeout time.Duration,
	prefixGatherer metrics.MultiGatherer,
) *VMExecutor {
	r := require.New(tb)

	blockChan := createBlockChanFromLevelDB(
		tb,
		blockDir,
		startBlock,
		endBlock,
		chanSize,
	)

	consensusRegistry := prometheus.NewRegistry()
	r.NoError(prefixGatherer.Register("avalanche_snowman", consensusRegistry))

	metrics, err := newConsensusMetrics(consensusRegistry)
	r.NoError(err)

	return &VMExecutor{
		log:              tests.NewDefaultLogger("vm-executor"),
		vm:               vm,
		metrics:          metrics,
		executionTimeout: executionTimeout,
		startBlock:       startBlock,
		endBlock:         endBlock,
		blkChan:          blockChan,
		// ETA tracker uses a 10-sample moving window to smooth rate estimates,
		// and a 1.2 slowdown factor to slightly pad ETA early in the run,
		// tapering to 1.0 as progress approaches 100%.
		etaTracker: timer.NewEtaTracker(10, 1.2),
	}
}

// Run reexecutes the blocks against the VM. It parses, verifies, and accepts
// each block while tracking progress and respecting the execution timeout if configured.
func (e *VMExecutor) Run(ctx context.Context) error {
	blkID, err := e.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}
	blk, err := e.vm.GetBlock(ctx, blkID)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block by blkID %s: %w", blkID, err)
	}

	start := time.Now()
	e.log.Info("last accepted block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", blk.Height()),
	)

	// Initialize ETA tracking with a baseline sample at 0 progress
	totalWork := e.endBlock - e.startBlock
	e.etaTracker.AddSample(0, totalWork, start)

	if e.executionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.executionTimeout)
		defer cancel()
	}

	for blkResult := range e.blkChan {
		if blkResult.err != nil {
			return blkResult.err
		}

		if blkResult.height%1000 == 0 {
			completed := blkResult.height - e.startBlock
			etaPtr, progressPercentage := e.etaTracker.AddSample(completed, totalWork, time.Now())
			if etaPtr != nil {
				e.log.Info("executing block",
					zap.Uint64("height", blkResult.height),
					zap.Float64("progress_pct", progressPercentage),
					zap.Duration("eta", *etaPtr),
				)
			} else {
				e.log.Info("executing block",
					zap.Uint64("height", blkResult.height),
					zap.Float64("progress_pct", progressPercentage),
				)
			}
		}
		if err := e.execute(ctx, blkResult.blockBytes); err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			e.log.Info("exiting early due to context timeout",
				zap.Duration("elapsed", time.Since(start)),
				zap.Duration("execution-timeout", e.executionTimeout),
				zap.Error(ctx.Err()),
			)
			return nil
		}
	}
	e.log.Info("finished executing sequence")

	return nil
}

func (e *VMExecutor) execute(ctx context.Context, blockBytes []byte) error {
	blk, err := e.vm.ParseBlock(ctx, blockBytes)
	if err != nil {
		return fmt.Errorf("failed to parse block: %w", err)
	}
	if err := blk.Verify(ctx); err != nil {
		return fmt.Errorf("failed to verify block %s at height %d: %w", blk.ID(), blk.Height(), err)
	}

	if err := blk.Accept(ctx); err != nil {
		return fmt.Errorf("failed to accept block %s at height %d: %w", blk.ID(), blk.Height(), err)
	}
	e.metrics.lastAcceptedHeight.Set(float64(blk.Height()))

	return nil
}

type VMParams struct {
	GenesisBytes []byte
	UpgradeBytes []byte
	ConfigBytes  []byte
	SubnetID     ids.ID
	ChainID      ids.ID
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
	metricsGatherer metrics.MultiGatherer,
	vmParams VMParams,
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
	warpSigner := warp.NewSigner(blsKey, constants.MainnetID, vmParams.ChainID)

	sharedMemoryDB := prefixdb.New([]byte("sharedmemory"), db)
	atomicMemory := atomic.NewMemory(sharedMemoryDB)

	chainIDToSubnetID := map[ids.ID]ids.ID{
		mainnetXChainID:  constants.PrimaryNetworkID,
		MainnetCChainID:  constants.PrimaryNetworkID,
		vmParams.ChainID: vmParams.SubnetID,
		ids.Empty:        constants.PrimaryNetworkID,
	}

	if err := vm.Initialize(
		ctx,
		&snow.Context{
			NetworkID:       constants.MainnetID,
			SubnetID:        vmParams.SubnetID,
			ChainID:         vmParams.ChainID,
			NodeID:          ids.GenerateTestNodeID(),
			PublicKey:       blsPublicKey,
			NetworkUpgrades: upgrade.Mainnet,

			XChainID:    mainnetXChainID,
			CChainID:    MainnetCChainID,
			AVAXAssetID: mainnetAvaxAssetID,

			Log:          tests.NewDefaultLogger("mainnet-vm-reexecution"),
			SharedMemory: atomicMemory.NewSharedMemory(vmParams.ChainID),
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
		vmParams.GenesisBytes,
		vmParams.UpgradeBytes,
		vmParams.ConfigBytes,
		nil,
		&enginetest.Sender{},
	); err != nil {
		return nil, fmt.Errorf("failed to initialize VM: %w", err)
	}

	return vm, nil
}

type blockResult struct {
	blockBytes []byte
	height     uint64
	err        error
}

func createBlockChanFromLevelDB(tb testing.TB, sourceDir string, startBlock, endBlock uint64, chanSize int) <-chan blockResult {
	r := require.New(tb)
	ch := make(chan blockResult, chanSize)

	db, err := leveldb.New(sourceDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	r.NoError(err, "failed to create leveldb database from %q", sourceDir)
	tb.Cleanup(func() {
		r.NoError(db.Close())
	})

	go func() {
		defer close(ch)

		iter := db.NewIteratorWithStart(blockKey(startBlock))
		defer iter.Release()

		currentHeight := startBlock

		for iter.Next() {
			key := iter.Key()
			if len(key) != database.Uint64Size {
				ch <- blockResult{
					blockBytes: nil,
					err:        fmt.Errorf("expected key length %d while looking for block at height %d, got %d", database.Uint64Size, currentHeight, len(key)),
				}
				return
			}
			height := binary.BigEndian.Uint64(key)
			if height != currentHeight {
				ch <- blockResult{
					blockBytes: nil,
					err:        fmt.Errorf("expected next height %d, got %d", currentHeight, height),
				}
				return
			}
			ch <- blockResult{
				blockBytes: iter.Value(),
				height:     height,
			}
			currentHeight++
			if currentHeight > endBlock {
				break
			}
		}
		if iter.Error() != nil {
			ch <- blockResult{
				blockBytes: nil,
				err:        fmt.Errorf("failed to iterate over blocks at height %d: %w", currentHeight, iter.Error()),
			}
			return
		}
	}()

	return ch
}

func blockKey(height uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, height)
}

type consensusMetrics struct {
	lastAcceptedHeight prometheus.Gauge
}

// newConsensusMetrics creates a subset of the metrics from snowman consensus
// [engine](../../snow/engine/snowman/metrics.go).
//
// The registry passed in is expected to be registered with the prefix
// "avalanche_snowman" and the chain label (ex. chain="C") that would be handled
// by the [chain manager](../../chains/manager.go).
func newConsensusMetrics(registry prometheus.Registerer) (*consensusMetrics, error) {
	m := &consensusMetrics{
		lastAcceptedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_accepted_height",
			Help: "last height accepted",
		}),
	}
	if err := registry.Register(m.lastAcceptedHeight); err != nil {
		return nil, fmt.Errorf("failed to register last accepted height metric: %w", err)
	}
	return m, nil
}
