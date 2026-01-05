// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/reexecute"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/timer"
)

const pprofDir = "pprof"

var (
	blockDirArg        string
	currentStateDirArg string
	startBlockArg      uint64
	endBlockArg        uint64
	chanSizeArg        int
	executionTimeout   time.Duration
	labelsArg          string

	pprofEnabled               bool
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
		"firewood-archive": `{
			"state-scheme": "firewood",
			"snapshot-cache": 0,
			"pruning-enabled": false,
			"state-sync-enabled": false
		}`,
	}

	configNameArg  string
	runnerTypeArg  string
	configBytesArg []byte

	benchmarkOutputFileArg string
)

func init() {
	evm.RegisterAllLibEVMExtras()

	flag.StringVar(&blockDirArg, "block-dir", blockDirArg, "Block DB directory to read from during re-execution.")
	flag.StringVar(&currentStateDirArg, "current-state-dir", currentStateDirArg, "Current state directory including VM DB and Chain Data Directory for re-execution.")
	flag.Uint64Var(&startBlockArg, "start-block", 101, "Start block to begin execution (exclusive).")
	flag.Uint64Var(&endBlockArg, "end-block", 200, "End block to end execution (inclusive).")
	flag.IntVar(&chanSizeArg, "chan-size", 100, "Size of the channel to use for block processing.")
	flag.DurationVar(&executionTimeout, "execution-timeout", 0, "Benchmark execution timeout. After this timeout has elapsed, terminate the benchmark without error. If 0, no timeout is applied.")

	flag.BoolVar(&pprofEnabled, "pprof", true, "Enable cpu, memory and lock profiling.")
	flag.BoolVar(&metricsServerEnabledArg, "metrics-server-enabled", false, "Whether to enable the metrics server.")
	flag.Uint64Var(&metricsServerPortArg, "metrics-server-port", 0, "The port the metrics server will listen to.")
	flag.BoolVar(&metricsCollectorEnabledArg, "metrics-collector-enabled", false, "Whether to enable the metrics collector (if true, then metrics-server-enabled must be true as well).")
	flag.StringVar(&labelsArg, "labels", "", "Comma separated KV list of metric labels to attach to all exported metrics. Ex. \"owner=tim,runner=snoopy\"")

	predefinedConfigKeys := slices.Collect(maps.Keys(predefinedConfigs))
	predefinedConfigOptionsStr := fmt.Sprintf("[%s]", strings.Join(predefinedConfigKeys, ", "))
	flag.StringVar(&configNameArg, configKey, defaultConfigKey, fmt.Sprintf("Specifies the predefined config to use for the VM. Options include %s.", predefinedConfigOptionsStr))
	flag.StringVar(&runnerTypeArg, "runner", "dev", "Type/label of the runner executing this test. Added as a metric label and to the benchmark name for grouping results.")

	flag.StringVar(&benchmarkOutputFileArg, "benchmark-output-file", benchmarkOutputFileArg, "Filepath where benchmark results will be written to.")

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

	// Set the runner label on the metrics.
	labels["runner"] = runnerTypeArg
}

func main() {
	tc := tests.NewTestContext(tests.NewDefaultLogger("c-chain-reexecution"))
	tc.SetDefaultContextParent(context.Background())
	defer tc.RecoverAndExit()

	benchmarkName := fmt.Sprintf(
		"BenchmarkReexecuteRange/[%d,%d]-Config-%s-Runner-%s",
		startBlockArg,
		endBlockArg,
		configNameArg,
		runnerTypeArg,
	)

	benchmarkReexecuteRange(
		tc,
		benchmarkName,
		blockDirArg,
		currentStateDirArg,
		configBytesArg,
		startBlockArg,
		endBlockArg,
		chanSizeArg,
		metricsServerEnabledArg,
		metricsServerPortArg,
		metricsCollectorEnabledArg,
		benchmarkOutputFileArg,
	)
}

func benchmarkReexecuteRange(
	tc tests.TestContext,
	benchmarkName string,
	blockDir string,
	currentStateDir string,
	configBytes []byte,
	startBlock uint64,
	endBlock uint64,
	chanSize int,
	metricsServerEnabled bool,
	metricsPort uint64,
	metricsCollectorEnabled bool,
	benchmarkOutputFile string,
) {
	r := require.New(tc)
	ctx := tc.GetDefaultContextParent()

	// Create the prefix gatherer passed to the VM and register it with the top-level,
	// labeled gatherer.
	prefixGatherer := metrics.NewPrefixGatherer()

	vmMultiGatherer := metrics.NewPrefixGatherer()
	r.NoError(prefixGatherer.Register("avalanche_evm", vmMultiGatherer))

	meterVMRegistry := prometheus.NewRegistry()
	r.NoError(prefixGatherer.Register("avalanche_meterchainvm", meterVMRegistry))

	// consensusRegistry includes the chain="C" label and the prefix "avalanche_snowman".
	// The consensus registry is passed to the executor to mimic a subset of consensus metrics.
	consensusRegistry := prometheus.NewRegistry()
	r.NoError(prefixGatherer.Register("avalanche_snowman", consensusRegistry))

	if metricsServerEnabled {
		serverAddr := startServer(tc, prefixGatherer, metricsPort)

		if metricsCollectorEnabled {
			startCollector(tc, "c-chain-reexecution", labels, serverAddr)
		}
	}

	var (
		vmDBDir      = filepath.Join(currentStateDir, "db")
		chainDataDir = filepath.Join(currentStateDir, "chain-data-dir")
		pprofDirPath string
	)

	log := tc.Log()
	logFields := []zap.Field{
		zap.String("runner", runnerTypeArg),
		zap.String("config", configNameArg),
		zap.String("labels", labelsArg),
		zap.Bool("metrics-server-enabled", metricsServerEnabled),
		zap.Uint64("metrics-server-port", metricsPort),
		zap.Bool("metrics-collector-enabled", metricsCollectorEnabled),
		zap.String("block-dir", blockDir),
		zap.String("vm-db-dir", vmDBDir),
		zap.String("chain-data-dir", chainDataDir),
		zap.Uint64("start-block", startBlock),
		zap.Uint64("end-block", endBlock),
		zap.Int("chan-size", chanSize),
	}

	if pprofEnabled {
		cwd, err := os.Getwd()
		r.NoError(err, "failed to get current working directory for pprof")
		pprofDirPath = filepath.Join(cwd, pprofDir)

		p := profiler.New(pprofDirPath)
		r.NoError(p.StartCPUProfiler())
		defer func() {
			r.NoError(p.StopCPUProfiler())
			r.NoError(p.MemoryProfile())
			r.NoError(p.LockProfile())
		}()

		logFields = append(logFields, zap.String("pprof-dir", pprofDirPath))
	}
	log.Info("re-executing block range with params", logFields...)

	blockChan, err := reexecute.CreateBlockChanFromLevelDB(tc, blockDir, startBlock, endBlock, chanSize)
	r.NoError(err)

	dbLogger := tests.NewDefaultLogger("db")

	db, err := leveldb.New(vmDBDir, nil, dbLogger, prometheus.NewRegistry())
	r.NoError(err)
	defer func() {
		log.Info("shutting down DB")
		r.NoError(db.Close())
	}()

	vm, err := reexecute.NewMainnetCChainVM(
		ctx,
		db,
		chainDataDir,
		configBytes,
		vmMultiGatherer,
		meterVMRegistry,
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

	benchmarkTool := newBenchmarkTool(benchmarkName)
	getTopLevelMetrics(tc, benchmarkTool, prefixGatherer, elapsed) // Report the desired top-level metrics

	benchmarkTool.logResults(log)
	if len(benchmarkOutputFile) != 0 {
		r.NoError(benchmarkTool.saveToFile(benchmarkOutputFile))
	}
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
	config     vmExecutorConfig
	vm         block.ChainVM
	metrics    *consensusMetrics
	etaTracker *timer.EtaTracker
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
		// ETA tracker uses a 10-sample moving window to smooth rate estimates,
		// and a 1.2 slowdown factor to slightly pad ETA early in the run,
		// tapering to 1.0 as progress approaches 100%.
		etaTracker: timer.NewEtaTracker(10, 1.2),
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

func (e *vmExecutor) executeSequence(ctx context.Context, blkChan <-chan reexecute.BlockResult) error {
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

	// Initialize ETA tracking with a baseline sample at 0 progress
	totalWork := e.config.EndBlock - e.config.StartBlock
	e.etaTracker.AddSample(0, totalWork, start)

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
			completed := blkResult.Height - e.config.StartBlock
			etaPtr, progressPercentage := e.etaTracker.AddSample(completed, totalWork, time.Now())
			if etaPtr != nil {
				e.config.Log.Info("executing block",
					zap.Uint64("height", blkResult.Height),
					zap.Float64("progress_pct", progressPercentage),
					zap.Duration("eta", *etaPtr),
				)
			} else {
				e.config.Log.Info("executing block",
					zap.Uint64("height", blkResult.Height),
					zap.Float64("progress_pct", progressPercentage),
				)
			}
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

// startServer starts a Prometheus server for the provided gatherer and returns
// the server address.
func startServer(
	tc tests.TestContext,
	gatherer prometheus.Gatherer,
	port uint64,
) string {
	r := require.New(tc)

	server, err := tests.NewPrometheusServerWithPort(gatherer, port)
	r.NoError(err)

	tc.Log().Info("metrics endpoint available",
		zap.String("url", fmt.Sprintf("http://%s/ext/metrics", server.Address())),
	)

	tc.DeferCleanup(func() {
		r.NoError(server.Stop())
	})

	return server.Address()
}

// startCollector starts a Prometheus collector configured to scrape the server
// listening on serverAddr. startCollector also attaches the provided labels +
// Github labels if available to the collected metrics.
func startCollector(tc tests.TestContext, name string, labels map[string]string, serverAddr string) {
	r := require.New(tc)

	logger := tests.NewDefaultLogger("prometheus")
	r.NoError(tmpnet.StartPrometheus(tc.DefaultContext(), logger))

	var sdConfigFilePath string
	tc.DeferCleanup(func() {
		// Ensure a final metrics scrape.
		// This default delay is set above the default scrape interval used by StartPrometheus.
		time.Sleep(tmpnet.NetworkShutdownDelay)

		r.NoError(func() error {
			if sdConfigFilePath != "" {
				return os.Remove(sdConfigFilePath)
			}
			return nil
		}(),
		)

		r.NoError(tmpnet.CheckMetricsExist(tc.DefaultContext(), logger, networkUUID))
	})

	sdConfigFilePath, err := tmpnet.WritePrometheusSDConfig(name, tmpnet.SDConfig{
		Targets: []string{serverAddr},
		Labels:  labels,
	}, true /* withGitHubLabels */)
	r.NoError(err)

	var (
		dashboardPath = "d/Gl1I20mnk/c-chain"
		grafanaURI    = tmpnet.DefaultBaseGrafanaURI + dashboardPath
		startTime     = strconv.FormatInt(time.Now().UnixMilli(), 10)
	)

	tc.Log().Info("metrics available via grafana",
		zap.String(
			"url",
			tmpnet.NewGrafanaURI(networkUUID, startTime, "", grafanaURI),
		),
	)
}

// benchmarkResult represents a single benchmark measurement.
type benchmarkResult struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

// benchmarkTool collects and manages benchmark results for a named benchmark.
// It allows adding multiple results and saving them to a file in JSON format.
type benchmarkTool struct {
	name    string
	results []benchmarkResult
}

// newBenchmarkTool creates a new benchmarkTool instance with the given name.
// The name is used as the base name for all results collected by this tool.
// When results are added, the unit is appended to this base name.
func newBenchmarkTool(name string) *benchmarkTool {
	return &benchmarkTool{
		name:    name,
		results: make([]benchmarkResult, 0),
	}
}

// addResult adds a new benchmark result with the given value and unit.
// The result name is constructed by appending the unit to the benchmark name.
// Calling `addResult` is analogous to calling `b.ReportMetric()`.
func (b *benchmarkTool) addResult(value float64, unit string) {
	result := benchmarkResult{
		Name:  fmt.Sprintf("%s - %s", b.name, unit),
		Value: strconv.FormatFloat(value, 'f', -1, 64),
		Unit:  unit,
	}
	b.results = append(b.results, result)
}

// saveToFile writes all collected benchmark results to a JSON file at the
// specified path. The output is formatted with indentation for readability.
// Returns an error if marshaling or file writing fails.
func (b *benchmarkTool) saveToFile(path string) error {
	output, err := json.MarshalIndent(b.results, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, output, perms.ReadWrite)
}

// logResults logs all collected benchmark results using the provided logger.
func (b *benchmarkTool) logResults(log logging.Logger) {
	for _, r := range b.results {
		result := fmt.Sprintf("%s %s", r.Value, r.Unit)
		log.Info(b.name, zap.String("result", result))
	}
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
