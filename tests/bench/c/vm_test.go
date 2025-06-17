// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"testing"

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
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
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
)

func TestMain(m *testing.M) {
	// Source directory must be a leveldb dir with the required blocks accessible via rawdb.ReadBlock.
	flag.StringVar(&sourceBlockDir, "source-block-dir", sourceBlockDir, "DB directory storing executable block range.")
	// Target directory assumes the same structure as bench.import_cchain_data.sh:
	// - vmdb/
	// - chain-data-dir/
	flag.StringVar(&targetDir, "target-dir", targetDir, "Target directory for the current state including VM DB and Chain Data Directory.")
	flag.Uint64Var(&startBlock, "start-block", 100, "Start block to begin execution (exclusive).")
	flag.Uint64Var(&endBlock, "end-block", 200, "End block to end execution (inclusive).")

	flag.Parse()
	m.Run()
}

// TODO:
// - pull metrics into tmpnet ingestion and integrate w/ existing dashboards
// - add scoped access tokens to AvalancheGo CI for required S3 bucket ONLY
// - separate general purpose VM setup from C-Chain specific setup
func TestReexecuteRange(t *testing.T) {
	r := require.New(t)

	var (
		targetDBDir = filepath.Join(targetDir, "vmdb")
		firewoodDB  = filepath.Join(targetDBDir, "chain-data-dir")
	)

	blockChan, err := createBlockChanFromRawDB(sourceBlockDir, 1, 100, 100)
	r.NoError(err)

	sourceVM, err := newMainnetCChainVM(
		context.Background(),
		targetDBDir,
		t.TempDir(),
		firewoodDB,
		[]byte(`{"pruning-enabled": false}`),
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
) (*evm.VM, error) {
	vm := evm.VM{}

	log := tests.NewDefaultLogger("mainnet-vm-reexecution")

	baseDBRegistry := prometheus.NewRegistry()
	db, err := leveldb.New(dbDir, nil, log, baseDBRegistry)
	if err != nil {
		return nil, fmt.Errorf("failed to create base level db: %w", err)
	}

	blsKey, err := localsigner.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create BLS key: %w", err)
	}

	blsPublicKey := blsKey.PublicKey()
	warpSigner := warp.NewSigner(blsKey, constants.MainnetID, mainnetCChainID)

	genesisConfig := genesis.GetConfig(constants.MainnetID)

	sharedMemoryDB, err := leveldb.New(atomicMemoryDBDir, nil, log, prometheus.NewRegistry() /* ignore metrics from shared memory db */)
	if err != nil {
		return nil, fmt.Errorf("failed to create shared memory db: %w", err)
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
			Metrics:      metrics.NewPrefixGatherer(),

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
	vm block.ChainVM
}

func newVMExecutor(vm block.ChainVM) *VMExecutor {
	return &VMExecutor{
		vm: vm,
	}
}

func (e *VMExecutor) execute(ctx context.Context, blockBytes []byte) error {
	blk, err := e.vm.ParseBlock(ctx, blockBytes)
	if err != nil {
		return err
	}

	if err := blk.Verify(ctx); err != nil {
		return err
	}

	return blk.Accept(ctx)
}

func (e *VMExecutor) executeSequence(ctx context.Context, blkChan <-chan BlockResult) error {
	for blkResult := range blkChan {
		if blkResult.Err != nil {
			return blkResult.Err
		}
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
		return nil, err
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
