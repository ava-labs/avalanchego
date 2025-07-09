// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ava-labs/coreth/plugin/factory"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	mainnetXChainID    = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
	mainnetCChainID    = ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")
	mainnetAvaxAssetID = ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
)

var (
	sourceBlockDirArg string
	targetBlockDirArg string
	targetDirArg      string
	startBlockArg     uint64
	endBlockArg       uint64
	chanSizeArg       int
	metricsEnabledArg bool
	executionTimeout  time.Duration
)

func TestMain(m *testing.M) {
	// Source directory must be a leveldb dir with the required blocks accessible via rawdb.ReadBlock.
	flag.StringVar(&sourceBlockDirArg, "source-block-dir", sourceBlockDirArg, "DB directory storing executable block range.")
	// Target block directory to write blocks into when executing TestExportBlockRange.
	flag.StringVar(&targetBlockDirArg, "target-block-dir", targetBlockDirArg, "DB directory to write blocks into when executing TestExportBlockRange.")

	// Target directory assumes the current-state directory contains a db directory and a chain-data-dir directory.
	// - db/
	// - chain-data-dir/
	flag.StringVar(&targetDirArg, "target-dir", targetDirArg, "Target directory for the current state including VM DB and Chain Data Directory.")
	flag.Uint64Var(&startBlockArg, "start-block", 101, "Start block to begin execution (exclusive).")
	flag.Uint64Var(&endBlockArg, "end-block", 200, "End block to end execution (inclusive).")
	flag.IntVar(&chanSizeArg, "chan-size", 100, "Size of the channel to use for block processing.")
	flag.BoolVar(&metricsEnabledArg, "metrics-enabled", true, "Enable metrics collection.")
	flag.DurationVar(&executionTimeout, "execution-timeout", 0, "Benchmark execution timeout. After this timeout has elapsed, terminate the benchmark without error.")
	flag.Parse()
	m.Run()
}

func TestReexecuteRange(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	// Create a multi gatherer to pass in to the VM with the expected "avalanche_evm" prefix
	// and chain="C" label.
	// Apply the chain="C" label at the top-level.
	cChainLabeledGatherer := metrics.NewLabelGatherer("chain")
	prefixGatherer := metrics.NewPrefixGatherer()
	r.NoError(cChainLabeledGatherer.Register("C", prefixGatherer))

	// Create the prefix gatherer passed to the VM and register it with the top-level,
	// labeled gatherer.
	vmMultiGatherer := metrics.NewPrefixGatherer()
	r.NoError(prefixGatherer.Register("avalanche_evm", vmMultiGatherer))

	// consensusRegistry includes the chain="C" label and the prefix "avalanche_snowman".
	// The consensus registry is passed to the executor to mimic a subset of consensus metrics.
	consensusRegistry := prometheus.NewRegistry()
	r.NoError(prefixGatherer.Register("avalanche_snowman", consensusRegistry))

	if metricsEnabledArg {
		collectRegistry(t, "c-chain-reexecution", "127.0.0.1:9000", time.Minute, cChainLabeledGatherer, map[string]string{
			"job": "c-chain-reexecution",
		})
	}

	var (
		targetDBDir  = filepath.Join(targetDirArg, "db")
		chainDataDir = filepath.Join(targetDBDir, "chain-data-dir")
	)

	blockChan, err := createBlockChanFromLevelDB(t, sourceBlockDirArg, startBlockArg, endBlockArg, chanSizeArg)
	r.NoError(err)

	dbLogger := tests.NewDefaultLogger("db")

	db, err := leveldb.New(targetDBDir, nil, dbLogger, prometheus.NewRegistry())
	r.NoError(err)
	defer func() {
		r.NoError(db.Close())
	}()

	sourceVM, err := newMainnetCChainVM(
		ctx,
		db,
		chainDataDir,
		[]byte(`{"pruning-enabled": false}`),
		vmMultiGatherer,
	)
	r.NoError(err)
	defer func() {
		r.NoError(sourceVM.Shutdown(ctx))
	}()

	executor, err := newVMExecutor(sourceVM, consensusRegistry)
	r.NoError(err)
	r.NoError(executor.executeSequence(ctx, blockChan, executionTimeout))
}

func TestExportBlockRange(t *testing.T) {
	exportBlockRange(t, sourceBlockDirArg, targetBlockDirArg, startBlockArg, endBlockArg, chanSizeArg)
}

func newMainnetCChainVM(
	ctx context.Context,
	vmAndSharedMemoryDB database.Database,
	chainDataDir string,
	configBytes []byte,
	metricsGatherer metrics.MultiGatherer,
) (block.ChainVM, error) {
	factory := factory.Factory{}
	vmIntf, err := factory.New(logging.NoLog{})
	if err != nil {
		return nil, fmt.Errorf("failed to create VM from factory: %w", err)
	}
	vm := vmIntf.(block.ChainVM)

	log := tests.NewDefaultLogger("mainnet-vm-reexecution")

	blsKey, err := localsigner.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create BLS key: %w", err)
	}

	blsPublicKey := blsKey.PublicKey()
	warpSigner := warp.NewSigner(blsKey, constants.MainnetID, mainnetCChainID)

	genesisConfig := genesis.GetConfig(constants.MainnetID)

	sharedMemoryDB := prefixdb.New([]byte("sharedmemory"), vmAndSharedMemoryDB)
	atomicMemory := atomic.NewMemory(sharedMemoryDB)

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

			Log:          log,
			SharedMemory: atomicMemory.NewSharedMemory(mainnetCChainID),
			BCLookup:     ids.NewAliaser(),
			Metrics:      metricsGatherer,

			WarpSigner: warpSigner,

			ValidatorState: &validatorstest.State{},
			ChainDataDir:   chainDataDir,
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

type BlockResult struct {
	BlockBytes []byte
	Height     uint64
	Err        error
}

type VMExecutor struct {
	log     logging.Logger
	vm      block.ChainVM
	metrics *consensusMetrics
}

type consensusMetrics struct {
	lastAcceptedHeight prometheus.Gauge
}

func newConsensusMetrics(registry prometheus.Registerer) (*consensusMetrics, error) {
	// Mimic the metrics from snowman consensus [engine](../../snow/engine/snowman/metrics.go).
	errs := wrappers.Errs{}
	m := &consensusMetrics{
		lastAcceptedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_accepted_height",
			Help: "last height accepted",
		}),
	}
	errs.Add(registry.Register(m.lastAcceptedHeight))
	return m, errs.Err
}

func newVMExecutor(vm block.ChainVM, registry prometheus.Registerer) (*VMExecutor, error) {
	metrics, err := newConsensusMetrics(registry)
	if err != nil {
		return nil, fmt.Errorf("failed to create consensus metrics: %w", err)
	}

	return &VMExecutor{
		vm:      vm,
		metrics: metrics,
		log:     tests.NewDefaultLogger("vm-executor"),
	}, nil
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

func (e *VMExecutor) executeSequence(ctx context.Context, blkChan <-chan BlockResult, executionTimeout time.Duration) error {
	blkID, err := e.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}
	blk, err := e.vm.GetBlock(ctx, blkID)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}

	start := time.Now()
	e.log.Info("last accepted block", zap.String("blkID", blkID.String()), zap.Uint64("height", blk.Height()))

	for blkResult := range blkChan {
		if blkResult.Err != nil {
			return blkResult.Err
		}

		if blkResult.Height%1000 == 0 {
			e.log.Info("executing block", zap.Uint64("height", blkResult.Height))
		}
		if err := e.execute(ctx, blkResult.BlockBytes); err != nil {
			return err
		}
		if executionTimeout > 0 && time.Since(start) > executionTimeout {
			e.log.Info("exiting early due to execution timeout", zap.Duration("elapsed", time.Since(start)), zap.Duration("execution-timeout", executionTimeout))
			return nil
		}
	}
	e.log.Info("finished executing sequence")

	return nil
}

// collectRegistry starts prometheus and collects metrics from the provided gatherer.
// Attaches the provided labels + GitHub labels if available to the collected metrics.
func collectRegistry(t *testing.T, name string, addr string, timeout time.Duration, gatherer prometheus.Gatherer, labels map[string]string) {
	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)

	r.NoError(tmpnet.StartPrometheus(ctx, tests.NewDefaultLogger("prometheus")))

	server := tests.NewPrometheusServer(addr, "/ext/metrics", gatherer)
	errChan, err := server.Start()
	r.NoError(err)

	var sdConfigFilePath string
	t.Cleanup(func() {
		// Ensure a final metrics scrape.
		// This default delay is set above the default scrape interval used by StartPrometheus.
		time.Sleep(tmpnet.NetworkShutdownDelay)

		r.NoError(server.Stop())
		r.NoError(<-errChan)

		if sdConfigFilePath != "" {
			r.NoError(os.Remove(sdConfigFilePath))
		}
	})

	sdConfigFilePath, err = tmpnet.WritePrometheusServiceDiscoveryConfigFile(name, []tmpnet.SDConfig{
		{
			Targets: []string{server.Address()},
			Labels:  labels,
		},
	}, true)
	r.NoError(err)
}

func createBlockChanFromLevelDB(t *testing.T, sourceDir string, startBlock, endBlock uint64, chanSize int) (<-chan BlockResult, error) {
	r := require.New(t)
	ch := make(chan BlockResult, chanSize)

	db, err := leveldb.New(sourceDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	if err != nil {
		return nil, fmt.Errorf("failed to create leveldb database from %q: %w", sourceDir, err)
	}
	t.Cleanup(func() {
		r.NoError(db.Close())
	})

	go func() {
		defer close(ch)

		for i := startBlock; i <= endBlock; i++ {
			blockBytes, err := db.Get(binary.BigEndian.AppendUint64(nil, i))
			if err != nil {
				ch <- BlockResult{
					BlockBytes: nil,
					Err:        err,
				}
				return
			}

			ch <- BlockResult{
				BlockBytes: blockBytes,
				Height:     i,
				Err:        nil,
			}
		}
	}()

	return ch, nil
}

func exportBlockRange(t *testing.T, sourceDir string, targetDir string, startBlock, endBlock uint64, chanSize int) {
	r := require.New(t)
	blockChan, err := createBlockChanFromLevelDB(t, sourceDir, startBlock, endBlock, chanSize)
	r.NoError(err)

	db, err := leveldb.New(targetDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(db.Close())
	})

	batch := db.NewBatch()
	for blkResult := range blockChan {
		r.NoError(batch.Put(binary.BigEndian.AppendUint64(nil, blkResult.Height), blkResult.BlockBytes))

		if batch.Size() > 10*units.MiB {
			r.NoError(batch.Write())
			batch = db.NewBatch()
		}
	}

	r.NoError(batch.Write())
}
