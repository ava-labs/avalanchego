// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	sourceDBCacheSize = 128
	sourceDBHandles   = 1024
)

var (
	mainnetXChainID    = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
	mainnetCChainID    = ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")
	mainnetAvaxAssetID = ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
)

var (
	sourceBlockDir string
	targetDir      string
	startBlock     uint64
	endBlock       uint64
	chanSize       int
	metricsEnabled bool
)

func TestMain(m *testing.M) {
	// Source directory must be a leveldb dir with the required blocks accessible via rawdb.ReadBlock.
	flag.StringVar(&sourceBlockDir, "source-block-dir", sourceBlockDir, "DB directory storing executable block range.")
	// Target directory assumes the same structure as bench.import_cchain_data.sh:
	// - vmdb/
	// - chain-data-dir/
	flag.StringVar(&targetDir, "target-dir", targetDir, "Target directory for the current state including VM DB and Chain Data Directory.")
	flag.Uint64Var(&startBlock, "start-block", 101, "Start block to begin execution (exclusive).")
	flag.Uint64Var(&endBlock, "end-block", 200, "End block to end execution (inclusive).")
	flag.IntVar(&chanSize, "chan-size", 100, "Size of the channel to use for block processing.")
	flag.BoolVar(&metricsEnabled, "metrics-enabled", true, "Enable metrics collection.")

	flag.Parse()
	m.Run()
}

func TestPrometheusIntegration(t *testing.T) {
	t.Skip("TODO: remove after debugging metrics setup")
	r := require.New(t)

	prefixGatherer := metrics.NewPrefixGatherer()
	registry := prometheus.NewRegistry()
	r.NoError(prefixGatherer.Register("dummy", registry))

	CollectRegistry(t, "test", "127.0.0.1:9000", time.Minute, prefixGatherer, map[string]string{
		"job":      "test-prometheus2",
		"service":  "test",
		"instance": "testPrometheusIntegration2",
	})

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "thingy_majig",
		Help: "help",
	})
	registry.MustRegister(counter)
	counter.Inc()

	for i := 0; i < 3; i++ {
		time.Sleep(time.Second * 5)
		counter.Inc()
	}

	prefixedMetrics, err := prefixGatherer.Gather()
	r.NoError(err)
	for _, metricFamily := range prefixedMetrics {
		fmt.Println(metricFamily.GetName())
	}

	registryMetrics, err := registry.Gather()
	r.NoError(err)
	for _, metricFamily := range registryMetrics {
		fmt.Println(metricFamily.GetName())
	}

	t.Fail()
}

func CollectRegistry(t *testing.T, name string, addr string, timeout time.Duration, gatherer prometheus.Gatherer, labels map[string]string) {
	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)

	r.NoError(tmpnet.StartPrometheus(ctx, tests.NewDefaultLogger("prometheus")))

	server := tests.NewPrometheusServer(addr, "/", gatherer)
	errChan, err := server.Start()
	r.NoError(err)

	t.Cleanup(func() {
		r.NoError(server.Stop())
		r.NoError(<-errChan)
	})

	// TODO: remove this after debugging metrics showing up correctly
	t.Cleanup(func() {
		metrics, err := gatherer.Gather()
		r.NoError(err)
		t.Log("metrics:")
		for _, metricFamily := range metrics {
			t.Log(metricFamily.GetName())
		}
	})

	sdConfigFilePath, err := tmpnet.WritePrometheusServiceDiscoveryConfigFile(name, []tmpnet.SDConfig{
		{
			Targets: []string{addr},
			Labels:  labels,
		},
	}, true)
	r.NoError(err)
	t.Cleanup(func() {
		os.Remove(sdConfigFilePath)
	})
}

// TODO:
// - add task to combine s3 pull + reexecution
// - separate general purpose VM setup from C-Chain specific setup
// - update C-Chain dashboard to make it useful for the benchmark
// - add s5cmd to flake
func TestReexecuteRange(t *testing.T) {
	r := require.New(t)

	var (
		targetDBDir  = filepath.Join(targetDir, "vmdb")
		chainDataDir = filepath.Join(targetDBDir, "chain-data-dir")
	)

	// AvalancheGo passes in a prefix of avalanche_<vm_id> via a prefix gatherer to each VM. Create and register
	// an avalancheGoSimulatedPrefixGatherer to apply the expected prefix to the VM, so that dashboards created
	// for the C-Chain run via AvalancheGo work directly with the benchmark.
	avalancheGoSimulatedPrefixGatherer := metrics.NewPrefixGatherer()
	vmPrefixGatherer := metrics.NewPrefixGatherer()
	r.NoError(avalancheGoSimulatedPrefixGatherer.Register("avalanche_evm", vmPrefixGatherer))

	if metricsEnabled {
		CollectRegistry(t, "benchmark-c-chain-reexecution", "127.0.0.1:9000", 2*time.Minute, avalancheGoSimulatedPrefixGatherer, map[string]string{
			"job":     "benchmark-c-chain-reexecution",
			"service": "benchmark-c-chain-reexecution",
		})
	}

	blockChan, err := createBlockChanFromRawDB(sourceBlockDir, startBlock, endBlock, chanSize)
	r.NoError(err)

	// WIP:
	// start a prometheus metrics server to collect metrics from the VM + Firewood throughout the test
	// Set up ingestion, so that I can view them in the existing CI grafana instance
	// Update the prefix/labels to match C-Chain / EVM as they will appear on a mainnet node ie. avalanchego.avalanche_C_metric_name as opposed to
	// metric_name because the prefix is not present when started in the test

	sourceVM, err := newMainnetCChainVM(
		context.Background(),
		targetDBDir,
		t.TempDir(),
		chainDataDir,
		[]byte(`{"pruning-enabled": false}`),
		vmPrefixGatherer,
	)
	r.NoError(err)
	defer sourceVM.Shutdown(context.Background())

	executor := newVMExecutor(sourceVM)
	err = executor.executeSequence(context.Background(), blockChan)
	r.NoError(err)
}

func newMainnetCChainVM(
	ctx context.Context,
	dbDir string,
	atomicMemoryDBDir string,
	chainDataDir string,
	configBytes []byte,
	metricsGatherer metrics.MultiGatherer,
) (*evm.VM, error) {
	vm := evm.VM{}

	log := tests.NewDefaultLogger("mainnet-vm-reexecution")

	baseDBRegistry := prometheus.NewRegistry()
	db, err := leveldb.New(dbDir, nil, log, baseDBRegistry)
	if err != nil {
		return nil, fmt.Errorf("failed to create base level db: %w", err)
	}
	if err := metricsGatherer.Register("vm", baseDBRegistry); err != nil {
		return nil, fmt.Errorf("failed to register vm metrics: %w", err)
	}

	blsKey, err := localsigner.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create BLS key: %w", err)
	}

	blsPublicKey := blsKey.PublicKey()
	warpSigner := warp.NewSigner(blsKey, constants.MainnetID, mainnetCChainID)

	genesisConfig := genesis.GetConfig(constants.MainnetID)

	sharedMemoryRegistry := prometheus.NewRegistry()
	sharedMemoryDB, err := leveldb.New(atomicMemoryDBDir, nil, log, sharedMemoryRegistry)
	if err != nil {
		return nil, fmt.Errorf("failed to create shared memory db: %w", err)
	}
	if err := metricsGatherer.Register("sharedmemorydb", sharedMemoryRegistry); err != nil {
		return nil, fmt.Errorf("failed to register shared memory metrics: %w", err)
	}
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
		db,
		[]byte(genesisConfig.CChainGenesis),
		nil,
		configBytes,
		make(chan common.Message, 1),
		nil,
		&enginetest.Sender{},
	); err != nil {
		return nil, fmt.Errorf("failed to initialize VM: %w", err)
	}

	return &vm, nil
}

type BlockResult struct {
	BlockBytes []byte
	Err        error
}

type VMExecutor struct {
	log logging.Logger
	vm  block.ChainVM
}

func newVMExecutor(vm block.ChainVM) *VMExecutor {
	return &VMExecutor{
		vm:  vm,
		log: tests.NewDefaultLogger("vm-executor"),
	}
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

	return nil
}

func (e *VMExecutor) executeSequence(ctx context.Context, blkChan <-chan BlockResult) error {
	blkID, err := e.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}
	blk, err := e.vm.GetBlock(ctx, blkID)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}

	e.log.Info("last accepted block", zap.String("blkID", blkID.String()), zap.Uint64("height", blk.Height()))

	for blkResult := range blkChan {
		if blkResult.Err != nil {
			return blkResult.Err
		}

		e.log.Info("executing block", zap.String("blkID", blkID.String()), zap.Uint64("height", blk.Height()))
		if err := e.execute(ctx, blkResult.BlockBytes); err != nil {
			return err
		}
	}

	return nil
}

func createBlockChanFromRawDB(sourceDir string, startBlock, endBlock uint64, chanSize int) (<-chan BlockResult, error) {
	ch := make(chan BlockResult, chanSize)

	db, err := rawdb.NewLevelDBDatabase(sourceDir, sourceDBCacheSize, sourceDBHandles, "", true)
	if err != nil {
		return nil, fmt.Errorf("failed to create leveldb database from %q: %w", sourceDir, err)
	}

	go func() {
		defer func() {
			_ = db.Close() // TODO: handle error
			close(ch)
		}()

		for i := startBlock; i <= endBlock; i++ {
			block := rawdb.ReadBlock(db, rawdb.ReadCanonicalHash(db, i), i)
			blockBytes, err := rlp.EncodeToBytes(block)
			if err != nil {
				ch <- BlockResult{
					BlockBytes: nil,
					Err:        err,
				}
				return
			}

			ch <- BlockResult{
				BlockBytes: blockBytes,
				Err:        nil,
			}
		}
	}()

	return ch, nil
}
