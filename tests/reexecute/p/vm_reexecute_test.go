// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"flag"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/reexecute"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
)

var mainnetPChainID = ids.FromStringOrPanic("11111111111111111111111111111111LpoYY")

var (
	blockDirArg        string
	currentStateDirArg string
	startBlockArg      uint64
	endBlockArg        uint64
	chanSizeArg        int
	executionTimeout   time.Duration

	metricsServerEnabledArg bool
	metricsServerPortArg    uint64

	runnerNameArg string
)

func TestMain(m *testing.M) {
	flag.StringVar(&blockDirArg, "block-dir", blockDirArg, "Block DB directory to read from during re-execution.")
	flag.StringVar(&currentStateDirArg, "current-state-dir", currentStateDirArg, "Current state directory including VM DB and Chain Data Directory for re-execution.")
	flag.Uint64Var(&startBlockArg, "start-block", 101, "Start block to begin execution (exclusive).")
	flag.Uint64Var(&endBlockArg, "end-block", 200, "End block to end execution (inclusive).")
	flag.IntVar(&chanSizeArg, "chan-size", 100, "Size of the channel to use for block processing.")
	flag.DurationVar(&executionTimeout, "execution-timeout", 0, "Benchmark execution timeout. After this timeout has elapsed, terminate the benchmark without error. If 0, no timeout is applied.")

	flag.BoolVar(&metricsServerEnabledArg, "metrics-server-enabled", false, "Whether to enable the metrics server.")
	flag.Uint64Var(&metricsServerPortArg, "metrics-server-port", 0, "The port the metrics server will listen to.")

	flag.StringVar(&runnerNameArg, "runner", "dev", "Name of the runner executing this test. Added as a metric label and to the sub-benchmark's name to differentiate results on the runner key.")

	m.Run()
}

func BenchmarkReexecuteRange(b *testing.B) {
	require.Equalf(b, 1, b.N, "BenchmarkReexecuteRange expects to run a single iteration because it overwrites the input current-state, but found (b.N=%d)", b.N)
	b.Run(fmt.Sprintf("[%d,%d]-Runner-%s", startBlockArg, endBlockArg, runnerNameArg), func(b *testing.B) {
		benchmarkReexecuteRange(
			b,
			blockDirArg,
			currentStateDirArg,
			startBlockArg,
			endBlockArg,
			chanSizeArg,
			metricsServerEnabledArg,
			metricsServerPortArg,
		)
	})
}

func benchmarkReexecuteRange(
	b *testing.B,
	blockDir string,
	currentStateDir string,
	startBlock uint64,
	endBlock uint64,
	chanSize int,
	metricsServerEnabled bool,
	metricsPort uint64,
) {
	r := require.New(b)
	ctx := b.Context()

	// Create the prefix gatherer passed to the VM and register it with the top-level,
	// labeled gatherer.
	prefixGatherer := metrics.NewPrefixGatherer()

	vmMultiGatherer := metrics.NewPrefixGatherer()
	// XXX: find out what the correct P-chain prefix is
	r.NoError(prefixGatherer.Register("avalanche_p", vmMultiGatherer))

	log := tests.NewDefaultLogger("p-chain-reexecution")
	if metricsServerEnabled {
		reexecute.StartServer(b, log, prefixGatherer, metricsPort)
	}

	var (
		vmDBDir      = filepath.Join(currentStateDir, "db")
		chainDataDir = filepath.Join(currentStateDir, "chain-data-dir")
	)

	log.Info("re-executing block range with params",
		zap.String("block-dir", blockDir),
		zap.String("vm-db-dir", vmDBDir),
		zap.String("chain-data-dir", chainDataDir),
		zap.Uint64("start-block", startBlock),
		zap.Uint64("end-block", endBlock),
		zap.Int("chan-size", chanSize),
	)

	dbLogger := tests.NewDefaultLogger("db")

	db, err := leveldb.New(vmDBDir, nil, dbLogger, prometheus.NewRegistry())
	r.NoError(err)
	defer func() {
		log.Info("shutting down DB")
		r.NoError(db.Close())
	}()

	genesisConfig := genesis.GetConfig(constants.MainnetID)
	genesisBytes, _, err := genesis.FromConfig(genesisConfig)
	r.NoError(err)

	vmParams := reexecute.VMParams{
		GenesisBytes: genesisBytes,
		SubnetID:     constants.PrimaryNetworkID,
		ChainID:      mainnetPChainID,
		ChainToSubnet: map[ids.ID]ids.ID{
			reexecute.MainnetXChainID: constants.PrimaryNetworkID,
			reexecute.MainnetCChainID: constants.PrimaryNetworkID,
			mainnetPChainID:           constants.PrimaryNetworkID,
			ids.Empty:                 constants.PrimaryNetworkID,
		},
	}

	vm, err := reexecute.NewMainnetVM(
		ctx,
		&platformvm.Factory{
			Internal: mainnetPChainInternalConfig(),
		},
		db,
		chainDataDir,
		vmMultiGatherer,
		vmParams,
	)
	r.NoError(err)
	defer func() {
		log.Info("shutting down VM")
		r.NoError(vm.Shutdown(ctx))
	}()

	executor := reexecute.NewVMExecutor(
		b,
		vm,
		blockDir,
		startBlock,
		endBlock,
		chanSize,
		executionTimeout,
		prefixGatherer,
	)

	r.NoError(executor.Run(ctx))
}

func mainnetPChainInternalConfig() config.Internal {
	return config.Internal{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             validators.NewManager(),
		DynamicFeeConfig:       genesis.MainnetParams.DynamicFeeConfig,
		ValidatorFeeConfig:     genesis.MainnetParams.ValidatorFeeConfig,
		MinValidatorStake:      genesis.MainnetParams.MinValidatorStake,
		MaxValidatorStake:      genesis.MainnetParams.MaxValidatorStake,
		MinDelegatorStake:      genesis.MainnetParams.MinDelegatorStake,
		MinStakeDuration:       genesis.MainnetParams.MinStakeDuration,
		MaxStakeDuration:       genesis.MainnetParams.MaxStakeDuration,
		RewardConfig:           genesis.MainnetParams.RewardConfig,
		UpgradeConfig:          upgradetest.GetConfig(upgradetest.Latest),
	}
}
