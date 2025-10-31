// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reexecute

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
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
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	MainnetCChainID = ids.FromStringOrPanic("2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5")

	mainnetXChainID    = ids.FromStringOrPanic("2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM")
	mainnetAvaxAssetID = ids.FromStringOrPanic("FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z")
)

type BenchmarkExecutorConfig struct {
	// The directory where blocks will be sourced from.
	BlockDir string

	// StartBlock is the height of the first block that will be executed.
	StartBlock uint64
	// EndBlock is the height of the last block that will be executed.
	EndBlock uint64
	// ChanSize is the size of the channel to use for block processing.
	ChanSize int
	// ExecutionTimeout is the duration to run the reexecution test for before
	// termininating (without error). If 0, no timeout is applied.
	ExecutionTimeout time.Duration

	// PrefixGatherer is the top-most gatherer where all metrics can be derived
	// from (e.g. VM metrics, consensus metrics). If MetricsServerEnabled is
	// true, then the metrics server will export metrics from this gatherer.
	PrefixGatherer metrics.MultiGatherer

	// MetricsServerEnabled determines whether to enable a Prometheus server
	// exporting VM metrics.
	MetricsServerEnabled bool
	// MetricsPort is the port where the metrics server will listen to.
	MetricsPort uint64
	// MetricsCollectorEnabled determines whether to start a Prometheus
	// collector.
	MetricsCollectorEnabled bool
	// MetricsLabels represents the set of labels that will be attached to all
	// exported metrics.
	MetricsLabels map[string]string
	// SDConfigName is the name of the SDConfig file.
	SDConfigName string
	// NetworkUUID is the unique identifier corresponding to the benchmark.
	NetworkUUID string
	// DashboardPath is the relative Grafana dashboard path used to construct
	// the metrics visualization URL when MetricsCollectorEnabled is true.
	// Expected format: "d/dashboard-id/dashboard-name" (e.g., "d/Gl1I20mnk/c-chain").
	DashboardPath string
}

// BenchmarkExecutor is a tool for executing a sequence of blocks against a VM.
type BenchmarkExecutor struct {
	config BenchmarkExecutorConfig
}

func NewBenchmarkExecutor(config BenchmarkExecutorConfig) BenchmarkExecutor {
	return BenchmarkExecutor{config: config}
}

// Run executes a sequence of blocks from StartBlock to EndBlock against the
// provided VM. It also manages metrics collection and reporting by optionally
// starting a Prometheus server and collector based on the executor's
// configuration.
func (e BenchmarkExecutor) Run(b testing.TB, log logging.Logger, vm block.ChainVM) {
	r := require.New(b)
	ctx := b.Context()

	if e.config.MetricsServerEnabled {
		serverAddr := startServer(b, log, e.config.PrefixGatherer, e.config.MetricsPort)

		if e.config.MetricsCollectorEnabled {
			startCollector(
				b,
				log,
				e.config.SDConfigName,
				e.config.MetricsLabels,
				serverAddr,
				e.config.NetworkUUID,
				e.config.DashboardPath,
			)
		}
	}

	blockChan, err := createBlockChanFromLevelDB(
		b,
		e.config.BlockDir,
		e.config.StartBlock,
		e.config.EndBlock,
		e.config.ChanSize,
	)
	r.NoError(err)

	consensusRegistry := prometheus.NewRegistry()
	r.NoError(e.config.PrefixGatherer.Register("avalanche_snowman", consensusRegistry))

	vmExecutor, err := newVMExecutor(
		tests.NewDefaultLogger("vm-executor"),
		vm,
		consensusRegistry,
		e.config.ExecutionTimeout,
		e.config.StartBlock,
		e.config.EndBlock,
	)
	r.NoError(err)

	r.NoError(vmExecutor.executeSequence(ctx, blockChan))
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

// startServer starts a Prometheus server for the provided gatherer and returns
// the server address.
func startServer(
	tb testing.TB,
	log logging.Logger,
	gatherer prometheus.Gatherer,
	port uint64,
) string {
	r := require.New(tb)

	server, err := tests.NewPrometheusServerWithPort(gatherer, port)
	r.NoError(err)

	log.Info("metrics endpoint available",
		zap.String("url", fmt.Sprintf("http://%s/ext/metrics", server.Address())),
	)

	tb.Cleanup(func() {
		r.NoError(server.Stop())
	})

	return server.Address()
}

// startCollector starts a Prometheus collector configured to scrape the server
// listening on serverAddr. startCollector also attaches the provided labels +
// Github labels if available to the collected metrics.
func startCollector(
	tb testing.TB,
	log logging.Logger,
	name string,
	labels map[string]string,
	serverAddr string,
	networkUUID string,
	dashboardPath string,
) {
	r := require.New(tb)

	startPromCtx, cancel := context.WithTimeout(tb.Context(), tests.DefaultTimeout)
	defer cancel()

	logger := tests.NewDefaultLogger("prometheus")
	r.NoError(tmpnet.StartPrometheus(startPromCtx, logger))

	var sdConfigFilePath string
	tb.Cleanup(func() {
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

		//nolint:usetesting // t.Context() is already canceled inside the cleanup function
		checkMetricsCtx, cancel := context.WithTimeout(context.Background(), tests.DefaultTimeout)
		defer cancel()
		r.NoError(tmpnet.CheckMetricsExist(checkMetricsCtx, logger, networkUUID))
	})

	sdConfigFilePath, err := tmpnet.WritePrometheusSDConfig(name, tmpnet.SDConfig{
		Targets: []string{serverAddr},
		Labels:  labels,
	}, true /* withGitHubLabels */)
	r.NoError(err)

	var (
		grafanaURI = tmpnet.DefaultBaseGrafanaURI + dashboardPath
		startTime  = strconv.FormatInt(time.Now().UnixMilli(), 10)
	)

	log.Info("metrics available via grafana",
		zap.String(
			"url",
			tmpnet.NewGrafanaURI(networkUUID, startTime, "", grafanaURI),
		),
	)
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
