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
	"strconv"
	"testing"
	"time"

	"github.com/ava-labs/coreth/plugin/evm"
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
	flag.Parse()
	m.Run()
}

func TestReexecuteRange(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	var (
		targetDBDir  = filepath.Join(targetDirArg, "db")
		chainDataDir = filepath.Join(targetDBDir, "chain-data-dir")
	)

	// AvalancheGo passes in a prefix of avalanche_<vm_id> via a prefix gatherer to each VM. Create and register
	// an avalancheGoSimulatedPrefixGatherer to apply the expected prefix to the VM, so that dashboards created
	// for the C-Chain run via AvalancheGo work directly with the benchmark.
	avalancheGoSimulatedPrefixGatherer := metrics.NewPrefixGatherer()
	vmPrefixGatherer := metrics.NewPrefixGatherer()
	r.NoError(avalancheGoSimulatedPrefixGatherer.Register("avalanche_evm", vmPrefixGatherer))

	if metricsEnabledArg {
		collectRegistry(t, "benchmark-c-chain-reexecution", "127.0.0.1:9000", 2*time.Minute, avalancheGoSimulatedPrefixGatherer, map[string]string{
			"job":        "c-chain-reexecution",
			"start-time": strconv.FormatInt(time.Now().UnixMilli(), 10),
		})
	}

	blockChan, err := createBlockChanFromLevelDB(t, sourceBlockDirArg, startBlockArg, endBlockArg, chanSizeArg)
	r.NoError(err)

	dbLogger := tests.NewDefaultLogger("db")

	baseDBRegistry := prometheus.NewRegistry()

	db, err := leveldb.New(targetDBDir, nil, dbLogger, baseDBRegistry)
	r.NoError(err)
	r.NoError(vmPrefixGatherer.Register("vm", baseDBRegistry))
	t.Cleanup(func() {
		r.NoError(db.Close())
	})

	sourceVM, err := newMainnetCChainVM(
		ctx,
		db,
		chainDataDir,
		[]byte(`{"pruning-enabled": false}`),
		vmPrefixGatherer,
	)
	r.NoError(err)
	defer func() {
		r.NoError(sourceVM.Shutdown(ctx))
	}()

	executor := newVMExecutor(sourceVM)
	r.NoError(executor.executeSequence(ctx, blockChan))
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
) (*evm.VM, error) {
	vm := evm.VM{}

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

	return &vm, nil
}

type BlockResult struct {
	BlockBytes []byte
	Height     uint64
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

		if blkResult.Height%1000 == 0 {
			e.log.Info("executing block", zap.Uint64("height", blkResult.Height))
		} else {
			e.log.Debug("executing block", zap.Uint64("height", blkResult.Height))
		}
		if err := e.execute(ctx, blkResult.BlockBytes); err != nil {
			return err
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

	server := tests.NewPrometheusServer(addr, "/", gatherer)
	errChan, err := server.Start()
	r.NoError(err)

	t.Cleanup(func() {
		// Ensure a final metrics scrape.
		// This default delay is set above the default scrape interval used by StartPrometheus.
		time.Sleep(tmpnet.NetworkShutdownDelay)

		r.NoError(server.Stop())
		r.NoError(<-errChan)
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
