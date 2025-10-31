// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/coreth/plugin/factory"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/reexecute"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	blockDirArg        string
	blockDirSrcArg     string
	blockDirDstArg     string
	currentStateDirArg string
	startBlockArg      uint64
	endBlockArg        uint64
	chanSizeArg        int
	executionTimeout   time.Duration
	labelsArg          string

	metricsServerEnabledArg    bool
	metricsServerPortArg       uint64
	metricsCollectorEnabledArg bool

	networkUUID string = uuid.NewString()
	labels             = map[string]string{
		"job":               "c-chain-reexecution",
		"is_ephemeral_node": "false",
		"chain":             "C",
		"network_uuid":      networkUUID,
	}

	configKey         = "config"
	defaultConfigKey  = "default"
	predefinedConfigs = map[string]string{
		defaultConfigKey: `{}`,
		"archive": `{
			"pruning-enabled": false
		}`,
		"firewood": `{
			"state-scheme": "firewood",
			"snapshot-cache": 0,
			"pruning-enabled": true,
			"state-sync-enabled": false
		}`,
	}

	configNameArg  string
	runnerNameArg  string
	configBytesArg []byte
)

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()

	flag.StringVar(&blockDirArg, "block-dir", blockDirArg, "Block DB directory to read from during re-execution.")
	flag.StringVar(&currentStateDirArg, "current-state-dir", currentStateDirArg, "Current state directory including VM DB and Chain Data Directory for re-execution.")
	flag.Uint64Var(&startBlockArg, "start-block", 101, "Start block to begin execution (exclusive).")
	flag.Uint64Var(&endBlockArg, "end-block", 200, "End block to end execution (inclusive).")
	flag.IntVar(&chanSizeArg, "chan-size", 100, "Size of the channel to use for block processing.")
	flag.DurationVar(&executionTimeout, "execution-timeout", 0, "Benchmark execution timeout. After this timeout has elapsed, terminate the benchmark without error. If 0, no timeout is applied.")

	flag.BoolVar(&metricsServerEnabledArg, "metrics-server-enabled", false, "Whether to enable the metrics server.")
	flag.Uint64Var(&metricsServerPortArg, "metrics-server-port", 0, "The port the metrics server will listen to.")
	flag.BoolVar(&metricsCollectorEnabledArg, "metrics-collector-enabled", false, "Whether to enable the metrics collector (if true, then metrics-server-enabled must be true as well).")
	flag.StringVar(&labelsArg, "labels", "", "Comma separated KV list of metric labels to attach to all exported metrics. Ex. \"owner=tim,runner=snoopy\"")

	predefinedConfigKeys := slices.Collect(maps.Keys(predefinedConfigs))
	predefinedConfigOptionsStr := fmt.Sprintf("[%s]", strings.Join(predefinedConfigKeys, ", "))
	flag.StringVar(&configNameArg, configKey, defaultConfigKey, fmt.Sprintf("Specifies the predefined config to use for the VM. Options include %s.", predefinedConfigOptionsStr))
	flag.StringVar(&runnerNameArg, "runner", "dev", "Name of the runner executing this test. Added as a metric label and to the sub-benchmark's name to differentiate results on the runner key.")

	// Flags specific to TestExportBlockRange.
	flag.StringVar(&blockDirSrcArg, "block-dir-src", blockDirSrcArg, "Source block directory to copy from when running TestExportBlockRange.")
	flag.StringVar(&blockDirDstArg, "block-dir-dst", blockDirDstArg, "Destination block directory to write blocks into when executing TestExportBlockRange.")

	flag.Parse()

	if metricsCollectorEnabledArg {
		metricsServerEnabledArg = true
	}

	customLabels, err := parseCustomLabels(labelsArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse labels: %v\n", err)
		os.Exit(1)
	}
	maps.Copy(labels, customLabels)

	// Set the config from the predefined configs and add to custom labels for the job.
	predefinedConfigStr, ok := predefinedConfigs[configNameArg]
	if !ok {
		fmt.Fprintf(os.Stderr, "invalid config name %q. Valid options include %s.\n", configNameArg, predefinedConfigOptionsStr)
		os.Exit(1)
	}
	labels[configKey] = configNameArg
	configBytesArg = []byte(predefinedConfigStr)

	// Set the runner name label on the metrics.
	labels["runner"] = runnerNameArg

	m.Run()
}

func BenchmarkReexecuteRange(b *testing.B) {
	require.Equalf(b, 1, b.N, "BenchmarkReexecuteRange expects to run a single iteration because it overwrites the input current-state, but found (b.N=%d)", b.N)
	b.Run(fmt.Sprintf("[%d,%d]-Config-%s-Runner-%s", startBlockArg, endBlockArg, configNameArg, runnerNameArg), func(b *testing.B) {
		benchmarkReexecuteRange(
			b,
			blockDirArg,
			currentStateDirArg,
			configBytesArg,
			startBlockArg,
			endBlockArg,
			chanSizeArg,
			metricsServerEnabledArg,
			metricsServerPortArg,
			metricsCollectorEnabledArg,
		)
	})
}

func benchmarkReexecuteRange(
	b *testing.B,
	blockDir string,
	currentStateDir string,
	configBytes []byte,
	startBlock uint64,
	endBlock uint64,
	chanSize int,
	metricsServerEnabled bool,
	metricsPort uint64,
	metricsCollectorEnabled bool,
) {
	r := require.New(b)
	ctx := b.Context()

	// Create the prefix gatherer passed to the VM and register it with the top-level,
	// labeled gatherer.
	prefixGatherer := metrics.NewPrefixGatherer()

	vmMultiGatherer := metrics.NewPrefixGatherer()
	r.NoError(prefixGatherer.Register("avalanche_evm", vmMultiGatherer))

	// consensusRegistry includes the chain="C" label and the prefix "avalanche_snowman".
	// The consensus registry is passed to the executor to mimic a subset of consensus metrics.
	consensusRegistry := prometheus.NewRegistry()
	r.NoError(prefixGatherer.Register("avalanche_snowman", consensusRegistry))

	genesisConfig := genesis.GetConfig(constants.MainnetID)

	log := tests.NewDefaultLogger("c-chain-reexecution")
	dbLogger := tests.NewDefaultLogger("db")

	var (
		vmDBDir      = filepath.Join(currentStateDir, "db")
		chainDataDir = filepath.Join(currentStateDir, "chain-data-dir")
	)
	db, err := leveldb.New(vmDBDir, nil, dbLogger, prometheus.NewRegistry())
	r.NoError(err)
	defer func() {
		log.Info("shutting down DB")
		r.NoError(db.Close())
	}()

	vmParams := reexecute.VMParams{
		GenesisBytes: []byte(genesisConfig.CChainGenesis),
		ConfigBytes:  configBytes,
		SubnetID:     constants.PrimaryNetworkID,
		ChainID:      reexecute.MainnetCChainID,
	}

	vm, err := reexecute.NewMainnetVM(
		ctx,
		&factory.Factory{},
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

	config := reexecute.BenchmarkExecutorConfig{
		BlockDir:                blockDir,
		StartBlock:              startBlock,
		EndBlock:                endBlock,
		ChanSize:                chanSize,
		ExecutionTimeout:        executionTimeout,
		PrefixGatherer:          prefixGatherer,
		ConsensusRegistry:       consensusRegistry,
		MetricsServerEnabled:    metricsServerEnabled,
		MetricsPort:             metricsPort,
		MetricsCollectorEnabled: metricsCollectorEnabled,
		MetricsLabels:           labels,
		SDConfigName:            "c-chain-reexecution",
		NetworkUUID:             networkUUID,
		DashboardPath:           "d/Gl1I20mnk/c-chain",
	}

	log.Info("re-executing block range with params",
		zap.String("block-dir", blockDir),
		zap.String("vm-db-dir", vmDBDir),
		zap.String("chain-data-dir", chainDataDir),
		zap.Uint64("start-block", startBlock),
		zap.Uint64("end-block", endBlock),
		zap.Int("chan-size", chanSize),
	)

	executor := reexecute.NewBenchmarkExecutor(config)

	start := time.Now()
	executor.Run(b, log, vm)
	elapsed := time.Since(start)

	b.ReportMetric(0, "ns/op")                     // Set default ns/op to 0 to hide from the output
	getTopLevelMetrics(b, prefixGatherer, elapsed) // Report the desired top-level metrics
}

func TestExportBlockRange(t *testing.T) {
	reexecute.ExportBlockRange(t, blockDirSrcArg, blockDirDstArg, startBlockArg, endBlockArg, chanSizeArg)
}

// parseCustomLabels parses a comma-separated list of key-value pairs into a map
// of custom labels.
func parseCustomLabels(labelsStr string) (map[string]string, error) {
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
