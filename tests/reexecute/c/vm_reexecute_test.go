// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/units"
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
	labelsArg         string

	labels = map[string]string{
		"job":               "c-chain-reexecution",
		"is_ephemeral_node": "false",
		"chain":             "C",
	}
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
	flag.DurationVar(&executionTimeout, "execution-timeout", 0, "Benchmark execution timeout. After this timeout has elapsed, terminate the benchmark without error. If 0, no timeout is applied.")

	flag.BoolVar(&metricsEnabledArg, "metrics-enabled", true, "Enable metrics collection.")
	flag.StringVar(&labelsArg, "labels", "", "Comma separated KV list of metric labels to attach to all exported metrics. Ex. \"owner=tim,runner=snoopy\"")

	flag.Parse()

	customLabels, err := parseLabels(labelsArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse labels: %v\n", err)
		os.Exit(1)
	}
	maps.Copy(labels, customLabels)

	m.Run()
}

func BenchmarkReexecuteRange(b *testing.B) {
	require.Equalf(b, 1, b.N, "BenchmarkReexecuteRange expects to run a single iteration because it overwrites the input current-state, but found (b.N=%d)", b.N)
	b.Run(fmt.Sprintf("[%d,%d]", startBlockArg, endBlockArg), func(b *testing.B) {
		benchmarkReexecuteRange(b, sourceBlockDirArg, targetDirArg, startBlockArg, endBlockArg, chanSizeArg, metricsEnabledArg)
	})
}

func benchmarkReexecuteRange(b *testing.B, sourceBlockDir string, targetDir string, startBlock uint64, endBlock uint64, chanSize int, metricsEnabled bool) {
	r := require.New(b)
	ctx := context.Background()

	// Create the prefix gatherer passed to the VM and register it with the top-level,
	// labeled gatherer.
	prefixGatherer := metrics.NewPrefixGatherer()

	vmMultiGatherer := metrics.NewPrefixGatherer()
	r.NoError(prefixGatherer.Register("avalanche_evm", vmMultiGatherer))

	// consensusRegistry includes the chain="C" label and the prefix "avalanche_snowman".
	// The consensus registry is passed to the executor to mimic a subset of consensus metrics.
	consensusRegistry := prometheus.NewRegistry()
	r.NoError(prefixGatherer.Register("avalanche_snowman", consensusRegistry))

	if metricsEnabled {
		collectRegistry(b, "c-chain-reexecution", time.Minute, prefixGatherer, labels)
	}

	log := tests.NewDefaultLogger("c-chain-reexecution")

	var (
		targetDBDir  = filepath.Join(targetDir, "db")
		chainDataDir = filepath.Join(targetDir, "chain-data-dir")
	)

	log.Info("re-executing block range with params",
		zap.String("source-block-dir", sourceBlockDir),
		zap.String("target-db-dir", targetDBDir),
		zap.String("chain-data-dir", chainDataDir),
		zap.Uint64("start-block", startBlock),
		zap.Uint64("end-block", endBlock),
		zap.Int("chan-size", chanSize),
	)

	blockChan, err := createBlockChanFromLevelDB(b, sourceBlockDir, startBlock, endBlock, chanSize)
	r.NoError(err)

	dbLogger := tests.NewDefaultLogger("db")

	db, err := leveldb.New(targetDBDir, nil, dbLogger, prometheus.NewRegistry())
	r.NoError(err)
	defer func() {
		log.Info("shutting down DB")
		r.NoError(db.Close())
	}()

	vm, err := newMainnetCChainVM(
		ctx,
		db,
		chainDataDir,
		nil,
		vmMultiGatherer,
	)
	r.NoError(err)
	defer func() {
		log.Info("shutting down VM")
		r.NoError(vm.Shutdown(ctx))
	}()

	config := vmExecutorConfig{
		Log:              tests.NewDefaultLogger("vm-executor"),
		Registry:         consensusRegistry,
		ExecutionTimeout: executionTimeout,
		StartBlock:       startBlock,
		EndBlock:         endBlock,
	}
	executor, err := newVMExecutor(vm, config)
	r.NoError(err)

	start := time.Now()
	r.NoError(executor.executeSequence(ctx, blockChan))
	elapsed := time.Since(start)

	b.ReportMetric(0, "ns/op")                     // Set default ns/op to 0 to hide from the output
	getTopLevelMetrics(b, prefixGatherer, elapsed) // Report the desired top-level metrics
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

type blockResult struct {
	BlockBytes []byte
	Height     uint64
	Err        error
}

type vmExecutorConfig struct {
	Log logging.Logger
	// Registry is the registry to register the metrics with.
	Registry prometheus.Registerer
	// ExecutionTimeout is the maximum timeout to continue executing blocks.
	// If 0, no timeout is applied. If non-zero, the executor will exit early
	// WITHOUT error after hitting the timeout.
	// This is useful to provide consistent duration benchmarks.
	ExecutionTimeout time.Duration

	// [StartBlock, EndBlock] defines the range (inclusive) of blocks to execute.
	StartBlock, EndBlock uint64
}

type vmExecutor struct {
	config  vmExecutorConfig
	vm      block.ChainVM
	metrics *consensusMetrics
}

func newVMExecutor(vm block.ChainVM, config vmExecutorConfig) (*vmExecutor, error) {
	metrics, err := newConsensusMetrics(config.Registry)
	if err != nil {
		return nil, fmt.Errorf("failed to create consensus metrics: %w", err)
	}

	return &vmExecutor{
		vm:      vm,
		metrics: metrics,
		config:  config,
	}, nil
}

func (e *vmExecutor) execute(ctx context.Context, blockBytes []byte) error {
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

func (e *vmExecutor) executeSequence(ctx context.Context, blkChan <-chan blockResult) error {
	blkID, err := e.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}
	blk, err := e.vm.GetBlock(ctx, blkID)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block by blkID %s: %w", blkID, err)
	}

	start := time.Now()
	e.config.Log.Info("last accepted block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", blk.Height()),
	)

	if e.config.ExecutionTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.config.ExecutionTimeout)
		defer cancel()
	}

	for blkResult := range blkChan {
		if blkResult.Err != nil {
			return blkResult.Err
		}

		if blkResult.Height%1000 == 0 {
			eta := timer.EstimateETA(
				start,
				blkResult.Height-e.config.StartBlock,
				e.config.EndBlock-e.config.StartBlock,
			)
			e.config.Log.Info("executing block",
				zap.Uint64("height", blkResult.Height),
				zap.Duration("eta", eta),
			)
		}
		if err := e.execute(ctx, blkResult.BlockBytes); err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			e.config.Log.Info("exiting early due to context timeout",
				zap.Duration("elapsed", time.Since(start)),
				zap.Duration("execution-timeout", e.config.ExecutionTimeout),
				zap.Error(ctx.Err()),
			)
			return nil
		}
	}
	e.config.Log.Info("finished executing sequence")

	return nil
}

func createBlockChanFromLevelDB(tb testing.TB, sourceDir string, startBlock, endBlock uint64, chanSize int) (<-chan blockResult, error) {
	r := require.New(tb)
	ch := make(chan blockResult, chanSize)

	db, err := leveldb.New(sourceDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	if err != nil {
		return nil, fmt.Errorf("failed to create leveldb database from %q: %w", sourceDir, err)
	}
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
					BlockBytes: nil,
					Err:        fmt.Errorf("expected key length %d while looking for block at height %d, got %d", database.Uint64Size, currentHeight, len(key)),
				}
				return
			}
			height := binary.BigEndian.Uint64(key)
			if height != currentHeight {
				ch <- blockResult{
					BlockBytes: nil,
					Err:        fmt.Errorf("expected next height %d, got %d", currentHeight, height),
				}
				return
			}
			ch <- blockResult{
				BlockBytes: iter.Value(),
				Height:     height,
			}
			currentHeight++
			if currentHeight > endBlock {
				break
			}
		}
		if iter.Error() != nil {
			ch <- blockResult{
				BlockBytes: nil,
				Err:        fmt.Errorf("failed to iterate over blocks at height %d: %w", currentHeight, iter.Error()),
			}
			return
		}
	}()

	return ch, nil
}

func blockKey(height uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, height)
}

func TestExportBlockRange(t *testing.T) {
	exportBlockRange(t, sourceBlockDirArg, targetBlockDirArg, startBlockArg, endBlockArg, chanSizeArg)
}

func exportBlockRange(tb testing.TB, sourceDir string, targetDir string, startBlock, endBlock uint64, chanSize int) {
	r := require.New(tb)
	blockChan, err := createBlockChanFromLevelDB(tb, sourceDir, startBlock, endBlock, chanSize)
	r.NoError(err)

	db, err := leveldb.New(targetDir, nil, logging.NoLog{}, prometheus.NewRegistry())
	r.NoError(err)
	tb.Cleanup(func() {
		r.NoError(db.Close())
	})

	batch := db.NewBatch()
	for blkResult := range blockChan {
		r.NoError(batch.Put(blockKey(blkResult.Height), blkResult.BlockBytes))

		if batch.Size() > 10*units.MiB {
			r.NoError(batch.Write())
			batch = db.NewBatch()
		}
	}

	r.NoError(batch.Write())
}

type consensusMetrics struct {
	lastAcceptedHeight prometheus.Gauge
}

// newConsensusMetrics creates a subset of the metrics from snowman consensus
// [engine](../../snow/engine/snowman/metrics.go).
//
// The registry passed in is expected to be registered with the prefix
// "avalanche_snowman" and the chain label (ex. chain="C") that would be handled
// by the[chain manager](../../../chains/manager.go).
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

// collectRegistry starts prometheus and collects metrics from the provided gatherer.
// Attaches the provided labels + GitHub labels if available to the collected metrics.
func collectRegistry(tb testing.TB, name string, timeout time.Duration, gatherer prometheus.Gatherer, labels map[string]string) {
	r := require.New(tb)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	tb.Cleanup(cancel)

	r.NoError(tmpnet.StartPrometheus(ctx, tests.NewDefaultLogger("prometheus")))

	server, err := tests.NewPrometheusServer(gatherer)
	r.NoError(err)

	var sdConfigFilePath string
	tb.Cleanup(func() {
		// Ensure a final metrics scrape.
		// This default delay is set above the default scrape interval used by StartPrometheus.
		time.Sleep(tmpnet.NetworkShutdownDelay)

		r.NoError(errors.Join(
			server.Stop(),
			func() error {
				if sdConfigFilePath != "" {
					return os.Remove(sdConfigFilePath)
				}
				return nil
			}(),
		))
	})

	sdConfigFilePath, err = tmpnet.WritePrometheusSDConfig(name, tmpnet.SDConfig{
		Targets: []string{server.Address()},
		Labels:  labels,
	}, true /* withGitHubLabels */)
	r.NoError(err)
}

func parseLabels(labelsStr string) (map[string]string, error) {
	labels := make(map[string]string)
	if labelsStr == "" {
		return labels, nil
	}
	for i, label := range strings.Split(labelsStr, ",") {
		parts := strings.SplitN(label, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid label format at index %d: %q (expected key=value)", i, label)
		}
		labels[parts[0]] = parts[1]
	}
	return labels, nil
}

func getTopLevelMetrics(b *testing.B, registry prometheus.Gatherer, elapsed time.Duration) {
	r := require.New(b)

	gasUsed, err := getCounterMetricValue(registry, "avalanche_evm_eth_chain_block_gas_used_processed")
	r.NoError(err)
	mgasPerSecond := gasUsed / 1_000_000 / elapsed.Seconds()
	b.ReportMetric(mgasPerSecond, "mgas/s")
}

func getCounterMetricValue(registry prometheus.Gatherer, query string) (float64, error) {
	metricFamilies, err := registry.Gather()
	if err != nil {
		return 0, fmt.Errorf("failed to gather metrics: %w", err)
	}

	for _, mf := range metricFamilies {
		if mf.GetName() == query {
			return mf.GetMetric()[0].Counter.GetValue(), nil
		}
	}

	return 0, fmt.Errorf("metric %s not found", query)
}
