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
	labelsArg         string
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
	flag.DurationVar(&executionTimeout, "execution-timeout", 0, "Benchmark execution timeout. After this timeout has elapsed, terminate the benchmark without error.")

	flag.BoolVar(&metricsEnabledArg, "metrics-enabled", true, "Enable metrics collection.")
	flag.StringVar(&labelsArg, "labels", "", "Comma separated KV list of metric labels to attach to all exported metrics.")

	flag.Parse()
	m.Run()
}

func BenchmarkReexecuteRange(b *testing.B) {
	benchmarkReexecuteRange(b, sourceBlockDirArg, targetDirArg, startBlockArg, endBlockArg, chanSizeArg, metricsEnabledArg)
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
		labels := map[string]string{
			"job":               "c-chain-reexecution",
			"is_ephemeral_node": "false",
			"chain":             "C",
		}
		if labelsArg != "" {
			customLabels := parseLabels(labelsArg)
			for customLabel, customValue := range customLabels {
				labels[customLabel] = customValue
			}
		}
		collectRegistry(b, "c-chain-reexecution", "127.0.0.1:9000", time.Minute, prefixGatherer, labels)
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

	config := vmExecutorConfig{
		Log:              tests.NewDefaultLogger("vm-executor"),
		Registry:         consensusRegistry,
		ExecutionTimeout: executionTimeout,
	}
	executor, err := newVMExecutor(sourceVM, config)
	r.NoError(err)

	start := time.Now()
	r.NoError(executor.executeSequence(ctx, blockChan, executionTimeout))
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

			Log:          log,
			SharedMemory: atomicMemory.NewSharedMemory(mainnetCChainID),
			BCLookup:     ids.NewAliaser(),
			Metrics:      metricsGatherer,

			WarpSigner: warpSigner,

			ValidatorState: &validatorstest.State{
				GetSubnetIDF: func(ctx context.Context, chainID ids.ID) (ids.ID, error) {
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
}

type vmExecutor struct {
	config  vmExecutorConfig
	vm      block.ChainVM
	metrics *consensusMetrics
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

func (e *vmExecutor) executeSequence(ctx context.Context, blkChan <-chan blockResult, executionTimeout time.Duration) error {
	blkID, err := e.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}
	blk, err := e.vm.GetBlock(ctx, blkID)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}

	start := time.Now()
	e.config.Log.Info("last accepted block", zap.String("blkID", blkID.String()), zap.Uint64("height", blk.Height()))

	for blkResult := range blkChan {
		if blkResult.Err != nil {
			return blkResult.Err
		}

		if blkResult.Height%1000 == 0 {
			e.config.Log.Info("executing block", zap.Uint64("height", blkResult.Height))
		}
		if err := e.execute(ctx, blkResult.BlockBytes); err != nil {
			return err
		}
		if executionTimeout > 0 && time.Since(start) > executionTimeout {
			e.config.Log.Info("exiting early due to execution timeout", zap.Duration("elapsed", time.Since(start)), zap.Duration("execution-timeout", executionTimeout))
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

		for i := startBlock; i <= endBlock; i++ {
			blockBytes, err := db.Get(binary.BigEndian.AppendUint64(nil, i))
			if err != nil {
				ch <- blockResult{
					BlockBytes: nil,
					Err:        fmt.Errorf("failed to get block %d: %w", i, err),
				}
				return
			}

			ch <- blockResult{
				BlockBytes: blockBytes,
				Height:     i,
				Err:        nil,
			}
		}
	}()

	return ch, nil
}

// collectRegistry starts prometheus and collects metrics from the provided gatherer.
// Attaches the provided labels + GitHub labels if available to the collected metrics.
func collectRegistry(tb testing.TB, name string, addr string, timeout time.Duration, gatherer prometheus.Gatherer, labels map[string]string) {
	r := require.New(tb)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	tb.Cleanup(cancel)

	r.NoError(tmpnet.StartPrometheus(ctx, tests.NewDefaultLogger("prometheus")))

	server := tests.NewPrometheusServer(addr, "/ext/metrics", gatherer)
	errChan, err := server.Start()
	r.NoError(err)

	var sdConfigFilePath string
	tb.Cleanup(func() {
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

func parseLabels(labelsStr string) map[string]string {
	labels := make(map[string]string)
	for _, label := range strings.Split(labelsStr, ",") {
		parts := strings.SplitN(label, "=", 2)
		if len(parts) != 2 {
			continue
		}
		labels[parts[0]] = parts[1]
	}
	return labels
}

func getTopLevelMetrics(b *testing.B, registry prometheus.Gatherer, elapsed time.Duration) {
	r := require.New(b)

	gasUsed, err := getCounterMetricValue(b, registry, "avalanche_evm_eth_chain_block_gas_used_processed")
	r.NoError(err)
	mgasPerSecond := gasUsed / 1_000_000 / elapsed.Seconds()
	b.ReportMetric(mgasPerSecond, "mgas/s")
}

func getCounterMetricValue(tb testing.TB, registry prometheus.Gatherer, query string) (float64, error) {
	metricFamilies, err := registry.Gather()
	r := require.New(tb)
	r.NoError(err)

	for _, mf := range metricFamilies {
		if mf.GetName() == query {
			return mf.GetMetric()[0].Counter.GetValue(), nil
		}
	}

	return 0, fmt.Errorf("metric %s not found", query)
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
		r.NoError(batch.Put(binary.BigEndian.AppendUint64(nil, blkResult.Height), blkResult.BlockBytes))

		if batch.Size() > 10*units.MiB {
			r.NoError(batch.Write())
			batch = db.NewBatch()
		}
	}

	r.NoError(batch.Write())
}
