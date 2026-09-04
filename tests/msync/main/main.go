// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load/contracts"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	blockchainID                          = "C"
	defaultTargetBytes             int64  = 10 * 1024 * 1024
	defaultMinBootstrapHeight      uint64 = 300
	defaultBatchSize                      = 5
	defaultWritesPerTx             int64  = 250
	defaultLoadWriteSlots          int64  = 64
	defaultLoadModifySlots         int64  = 32
	defaultStateSyncMinBlocks      uint64 = 32
	defaultStateSyncCommitInterval uint64 = 16
	defaultStateHistory            uint64 = 128
	defaultPollingDelay                   = 2 * time.Second
	defaultStateScheme                    = rawdb.HashScheme

	// defaultStatePollInterval is how often the harness re-reads chain state it
	// is waiting to become visible.
	defaultStatePollInterval = 100 * time.Millisecond

	// Transaction gas limits are set explicitly rather than left to
	// eth_estimateGas. The SAE C-Chain never sets rpc.Config.GasCap (see
	// vms/saevm/cchain.config), so libevm falls back to MaxUint64/2 as the
	// estimator's ceiling and can return a limit the mempool rejects outright
	// with "exceeds block gas limit".
	//
	// SAE charges at least ceil(gasLimit/params.Lambda) for every transaction,
	// so each limit is sized to the work its call performs rather than set to a
	// uniform maximum.
	deployGasLimit          uint64 = 3_000_000
	contractCallGasOverhead uint64 = 200_000
	trieWriteGasPerValue    uint64 = 25_000
	loadWriteGasPerSlot     uint64 = 25_000
	loadModifyGasPerSlot    uint64 = 10_000
	transferGasLimit        uint64 = 21_000

	// defaultSyncSampleInterval is how often the harness polls the bootstrap
	// node while waiting for it to become healthy. Only the
	// --restart-during-sync trigger depends on catching a sync in progress
	// within one interval; the sync duration comes from the VM's own
	// started/finished timestamp metrics and is exact regardless.
	defaultSyncSampleInterval = 100 * time.Millisecond
)

var (
	defaultGasFeeCap   = big.NewInt(300_000_000_000)
	defaultGasTipCap   = big.NewInt(1_000_000_000)
	defaultTransferWei = big.NewInt(1)

	flagVars *e2e.FlagVars

	targetBytes             int64
	minBootstrapHeight      uint64
	batchSize               int
	writesPerTx             int64
	loadWriteSlots          int64
	loadModifySlots         int64
	stateSyncMinBlocks      uint64
	stateSyncCommitInterval uint64
	stateScheme             string
	restartDuringSync       bool

	// Derived from --state-scheme in main before the network is configured.
	schemeConfig stateSchemeConfig

	// saeCChain reports whether the C-Chain will be served by the SAE VM
	// (vms/saevm/cchain) rather than by coreth. Derived from
	// --activate-latest-after in main before the network is configured.
	saeCChain bool

	// midChainTransition reports whether the network starts on coreth and
	// transitions to the SAE VM mid-run. Derived from --activate-latest-after
	// in main before the network is configured.
	midChainTransition bool

	// stateSyncSupported reports whether the C-Chain serving this run can state
	// sync the requested state scheme. It drives both whether the bootstrap node
	// is configured to sync and whether the sync is asserted: a run whose scheme
	// cannot be synced validates a plain bootstrap instead.
	//
	// The SAE C-Chain syncs the hash scheme only, because
	// statesync.SummaryHandler.StateSync builds a hashdb syncer unconditionally;
	// see the Firewood TODO in vms/saevm/statesync/server.go.
	stateSyncSupported bool

	// codeRequestCountMetric and blockRequestCountMetric prove that the
	// validators served code and block backfill requests. They default to the
	// coreth names and are substituted for an SAE C-Chain run, whose sync
	// handlers (vms/evm/sync, wired in vms/saevm/statesync) export their own
	// metrics.
	codeRequestCountMetric  = "avalanche_evm_eth_code_request_count"
	blockRequestCountMetric = "avalanche_evm_eth_block_request_count"
)

// Sync lifecycle metrics reported by the SAE C-Chain's summary handler
// (vms/saevm/statesync), through which this harness observes a sync: that one
// started, what summary it targeted, whether it is still running, and when and
// how it ended. Coreth registers none of them, so their presence also marks
// the coreth-to-SAE transition. The transition segment of the name comes from
// vms/transitionvm, which registers the post-transition chain's metrics under
// "transition".
const (
	syncInProgressMetric    = "avalanche_evm_transition_statesync_in_progress"
	syncSummaryHeightMetric = "avalanche_evm_transition_statesync_summary_height"
	syncStartedMetric       = "avalanche_evm_transition_statesync_started_timestamp"
	syncFinishedMetric      = "avalanche_evm_transition_statesync_finished_timestamp"
	syncFailedMetric        = "avalanche_evm_transition_statesync_failed"
)

// chainMetricLabels selects the C-Chain's series from a node's metrics.
var chainMetricLabels = prometheus.Labels{"chain": blockchainID}

// syncObservation is what the harness measured directly while waiting for the
// bootstrap node: when it started the node and when the node first reported
// healthy. Everything about the sync itself is read from the VM's lifecycle
// metrics (see [readSyncLifecycle]) rather than sampled, so it needs no
// observation here.
type syncObservation struct {
	nodeStartedAt time.Time
	healthyAt     time.Time
}

// bootstrapDuration reports how long the bootstrap node took to become healthy,
// which covers node startup and post-sync bootstrapping as well as the sync.
func (o syncObservation) bootstrapDuration() time.Duration {
	return o.healthyAt.Sub(o.nodeStartedAt)
}

// syncLifecycle is the sync lifecycle read from a node's metrics: the gauges
// named by [syncInProgressMetric] and its siblings, recorded by the SAE
// C-Chain's summary handler around the sync it launches.
type syncLifecycle struct {
	inProgress    bool
	summaryHeight uint64
	// startedSeconds and finishedSeconds are unix seconds, 0 until the sync
	// reaches the corresponding transition. They are the VM's own timestamps,
	// so the duration they span is exact rather than bounded by a poller's
	// sampling interval.
	startedSeconds  float64
	finishedSeconds float64
	failed          bool
}

// syncDuration reports how long the sync took, and whether it ran to an end at
// all.
func (l syncLifecycle) syncDuration() (time.Duration, bool) {
	if l.startedSeconds == 0 || l.finishedSeconds == 0 {
		return 0, false
	}
	return time.Duration((l.finishedSeconds - l.startedSeconds) * float64(time.Second)), true
}

// readSyncLifecycle reads the sync lifecycle gauges from a node's metrics. ok
// is false when the gauges are absent, meaning the C-Chain is not (yet) served
// by the SAE VM.
func readSyncLifecycle(ctx context.Context, node *tmpnet.Node) (syncLifecycle, bool, error) {
	nodeMetrics, err := tests.GetNodeMetrics(ctx, node.URI)
	if err != nil {
		return syncLifecycle{}, false, err
	}
	inProgress, ok := tests.GetMetricValue(nodeMetrics, syncInProgressMetric, chainMetricLabels)
	if !ok {
		return syncLifecycle{}, false, nil
	}

	// The remaining gauges register together with in_progress, so a missing
	// one is a bug rather than a pre-transition read.
	values := make(map[string]float64, 4)
	for _, name := range []string{syncSummaryHeightMetric, syncStartedMetric, syncFinishedMetric, syncFailedMetric} {
		value, ok := tests.GetMetricValue(nodeMetrics, name, chainMetricLabels)
		if !ok {
			return syncLifecycle{}, false, fmt.Errorf("node %s reports %q but not %q", node.NodeID, syncInProgressMetric, name)
		}
		values[name] = value
	}
	return syncLifecycle{
		inProgress:      inProgress != 0,
		summaryHeight:   uint64(values[syncSummaryHeightMetric]),
		startedSeconds:  values[syncStartedMetric],
		finishedSeconds: values[syncFinishedMetric],
		failed:          values[syncFailedMetric] != 0,
	}, true, nil
}

// stateSchemeConfig captures everything that differs between the state schemes
// this harness supports: the chain configuration the nodes must run with, and
// the metrics proving the scheme's sync path was exercised.
type stateSchemeConfig struct {
	// chainConfig is the scheme-specific C-Chain configuration applied to both
	// the serving nodes and the bootstrap node.
	chainConfig tmpnet.ConfigMap
	// bootstrapRequestMetric is a bootstrap-node metric proving that the
	// scheme's state requests were made.
	bootstrapRequestMetric string
	// servingRequestMetrics are validator metrics proving that the scheme's
	// state requests were served. Code and block serving evidence is checked
	// for every scheme and so is not repeated here.
	servingRequestMetrics []string
}

// newStateSchemeConfig returns the configuration for the requested state scheme.
func newStateSchemeConfig(scheme string) (stateSchemeConfig, error) {
	switch scheme {
	case customrawdb.FirewoodScheme:
		return stateSchemeConfig{
			chainConfig: tmpnet.ConfigMap{
				"state-scheme": customrawdb.FirewoodScheme,
				// Firewood requires a disabled snapshot cache and unset
				// missing-trie population or the VM fails to initialize.
				//
				// These stay set in SAE mode: transitionvm initializes coreth as
				// the pre-transition VM even when Helicon is active at genesis,
				// so coreth still enforces them. The SAE C-Chain ignores a
				// Firewood snapshot cache itself (see saedb.Config.snapConfig)
				// and ignores the keys it does not declare.
				"snapshot-cache":         0,
				"populate-missing-tries": nil,
				// Firewood uses the state history as its in-memory revision count.
				"state-history": defaultStateHistory,
			},
			bootstrapRequestMetric: "avalanche_evm_sync_firewood_sync_requests_made",
			// Firewood range proofs are served over a dedicated p2p handler
			// rather than the leafs request handler.
			servingRequestMetrics: nil,
		}, nil
	case rawdb.HashScheme:
		return stateSchemeConfig{
			chainConfig: tmpnet.ConfigMap{
				// Leave the snapshot cache at its default so the serving nodes
				// can answer leafs requests from their snapshots.
				"state-scheme": rawdb.HashScheme,
			},
			bootstrapRequestMetric: "avalanche_evm_eth_sync_state_trie_leaves_requested",
			servingRequestMetrics:  []string{"avalanche_evm_eth_leafs_request_count"},
		}, nil
	default:
		return stateSchemeConfig{}, fmt.Errorf(
			"unsupported state scheme %q: must be one of %q or %q",
			scheme,
			customrawdb.FirewoodScheme,
			rawdb.HashScheme,
		)
	}
}

type deployedContracts struct {
	trieAddress common.Address
	trie        *contracts.TrieStressTest
	loadAddress common.Address
	load        *contracts.LoadSimulator
}

type workloadSnapshot struct {
	trieArrayLength      *big.Int
	latestWriteValue     *big.Int
	latestModifyValue    *big.Int
	latestEmptySlot      *big.Int
	latestUnmodifiedSlot *big.Int
	transferRecipient    common.Address
	transferBalance      *big.Int
}

func init() {
	flagVars = e2e.RegisterFlags(
		e2e.WithDefaultOwner("avalanchego-msync-e2e"),
		// Sync the latest state format by default rather than the previous
		// upgrade's.
		e2e.WithDefaultActivateLatestAfter(0),
	)

	flag.Int64Var(
		&targetBytes,
		"target-bytes",
		defaultTargetBytes,
		"target growth in bytes for the measured node data before validating bootstrap; set to 0 to disable the size threshold",
	)
	flag.Uint64Var(
		&minBootstrapHeight,
		"min-bootstrap-height",
		defaultMinBootstrapHeight,
		"minimum C-Chain height required before validating bootstrap to ensure recent block backfill is exercised",
	)
	flag.IntVar(
		&batchSize,
		"batch-size",
		defaultBatchSize,
		"number of mixed-workload iterations to issue per measurement batch",
	)
	flag.Int64Var(
		&writesPerTx,
		"writes-per-tx",
		defaultWritesPerTx,
		"number of trie writes to perform per TrieStressTest transaction",
	)
	flag.Int64Var(
		&loadWriteSlots,
		"load-write-slots",
		defaultLoadWriteSlots,
		"number of LoadSimulator slots to populate per write transaction",
	)
	flag.Int64Var(
		&loadModifySlots,
		"load-modify-slots",
		defaultLoadModifySlots,
		"number of LoadSimulator slots to modify per modify transaction",
	)
	flag.Uint64Var(
		&stateSyncMinBlocks,
		"state-sync-min-blocks",
		defaultStateSyncMinBlocks,
		"minimum number of blocks ahead required for the bootstrap node to choose state sync",
	)
	flag.Uint64Var(
		&stateSyncCommitInterval,
		"state-sync-commit-interval",
		defaultStateSyncCommitInterval,
		"state sync summary interval to use for validator nodes and the bootstrap node",
	)
	flag.StringVar(
		&stateScheme,
		"state-scheme",
		defaultStateScheme,
		fmt.Sprintf(
			"state scheme to configure the network and bootstrap node with; one of %q or %q",
			customrawdb.FirewoodScheme,
			rawdb.HashScheme,
		),
	)

	flag.BoolVar(
		&restartDuringSync,
		"restart-during-sync",
		false,
		"stop and restart the bootstrap node the first time its health reports an in-progress state sync, validating recovery from an interrupted sync",
	)

	flag.Parse()
}

func main() {
	log := tests.NewDefaultLogger("msync-e2e")
	tc := tests.NewTestContext(log)
	defer tc.RecoverAndExit()

	require := require.New(tc)
	require.GreaterOrEqual(targetBytes, int64(0), "target-bytes must be non-negative")
	require.Positive(minBootstrapHeight, "min-bootstrap-height must be positive")
	require.Positive(batchSize, "batch-size must be positive")
	require.Positive(writesPerTx, "writes-per-tx must be positive")
	require.Positive(loadWriteSlots, "load-write-slots must be positive")
	require.Positive(loadModifySlots, "load-modify-slots must be positive")
	require.Positive(stateSyncMinBlocks, "state-sync-min-blocks must be positive")
	require.Positive(stateSyncCommitInterval, "state-sync-commit-interval must be positive")

	// tmpnet's --activate-latest-after schedules the latest upgrade, which is
	// Helicon: the upgrade that transitions the C-Chain from coreth to the SAE
	// VM. A negative value leaves it unscheduled, 0 activates it at genesis, and
	// a positive value activates it that long after the network starts.
	saeCChain = flagVars.ActivateLatestAfter() >= 0
	stateSyncSupported = !saeCChain || stateScheme == rawdb.HashScheme

	// A positive --activate-latest-after schedules Helicon after network
	// start, so the chain begins on coreth and transitions mid-run. This mode
	// additionally validates that nodes joining after the transition state
	// sync via the SAE C-Chain instead of full-bootstrapping.
	midChainTransition = flagVars.ActivateLatestAfter() > 0

	var schemeErr error
	schemeConfig, schemeErr = newStateSchemeConfig(stateScheme)
	require.NoError(schemeErr, "newStateSchemeConfig()")
	if saeCChain && stateSyncSupported {
		// The SAE C-Chain syncs over vms/evm/sync, whose metrics live under
		// the statesync namespace (see vms/saevm/statesync); substitute the
		// coreth names configured by newStateSchemeConfig. The transition
		// segment comes from vms/transitionvm, which registers the
		// post-transition chain's metrics under "transition".
		schemeConfig.bootstrapRequestMetric = "avalanche_evm_transition_statesync_sync_state_trie_leaves_requested"
		schemeConfig.servingRequestMetrics = []string{"avalanche_evm_transition_statesync_leafs_request_count"}
		codeRequestCountMetric = "avalanche_evm_transition_statesync_code_request_count"
		blockRequestCountMetric = "avalanche_evm_transition_statesync_block_request_count"
	}
	log.Info("configuring merkle sync harness",
		zap.String("stateScheme", stateScheme),
		zap.Bool("saeCChain", saeCChain),
		zap.Bool("midChainTransition", midChainTransition),
		zap.Bool("stateSyncSupported", stateSyncSupported),
		zap.Duration("activateLatestAfter", flagVars.ActivateLatestAfter()),
	)
	if !stateSyncSupported {
		// Skipping the sync evidence is the whole reason such a run is cheaper
		// than one that syncs, so say so rather than let a green run imply the
		// sync path was covered.
		log.Warn("the C-Chain serving this run cannot state sync the requested state scheme, so the run validates bootstrap and post-bootstrap state only; state sync status, summary heights and sync metrics are not asserted",
			zap.String("stateScheme", stateScheme),
			zap.Uint64("stateSyncMinBlocks", stateSyncMinBlocks),
			zap.Uint64("stateSyncCommitInterval", stateSyncCommitInterval),
		)
	}

	network := newMerkleSyncNetwork(flagVars.NetworkOwner())
	require.NoError(configureNetwork(tc, network))
	registerNetworkCleanup(tc, network)

	generationNode := network.Nodes[0]
	servingNode := network.Nodes[1]
	require.NoError(startGenerationNode(tc, network, generationNode))

	var (
		partialSeedDir    string
		preTransitionHead uint64
	)
	if midChainTransition {
		partialSeedDir, preTransitionHead = runCorethEraPhase(tc, network, generationNode)
	}

	pathsToMeasure := statePaths(generationNode)
	initialSize, err := totalSize(pathsToMeasure...)
	require.NoError(err)

	client := newWSClient(tc, []*tmpnet.Node{generationNode})
	chainID, err := client.ChainID(tc.DefaultContext())
	require.NoError(err)

	fundingKey := network.PreFundedKeys[0]
	transferRecipientKey := network.PreFundedKeys[1]
	transferRecipient := crypto.PubkeyToAddress(transferRecipientKey.ToECDSA().PublicKey)
	initialRecipientBalance, err := client.BalanceAt(tc.DefaultContext(), transferRecipient, nil)
	require.NoError(err)

	contracts := deployContracts(tc, client, chainID, fundingKey)
	requireGasLimitsFitBlock(tc, client)
	fundingAddr := crypto.PubkeyToAddress(fundingKey.ToECDSA().PublicKey)
	nonceBeforeExport, err := client.PendingNonceAt(tc.DefaultContext(), fundingAddr)
	require.NoError(err)
	issueAtomicExportTx(tc, network, fundingKey)
	// The export consumes an EVM nonce, but the executed state exposing that
	// nonce can lag the tx's acceptance. Wait for it so the first workload tx
	// doesn't reuse the export's nonce and get dropped from the pool.
	require.Eventually(func() bool {
		nonce, err := client.PendingNonceAt(tc.DefaultContext(), fundingAddr)
		return err == nil && nonce > nonceBeforeExport
	}, 30*time.Second, 100*time.Millisecond, "export tx nonce not reflected in pending state")
	snapshot := generateWorkload(tc, client, chainID, fundingKey, transferRecipient, contracts, pathsToMeasure, initialSize, initialRecipientBalance)

	require.NoError(stopNode(generationNode))
	require.NoError(copySharedState(generationNode, servingNode))
	require.NoError(startServingNetwork(tc, network, generationNode, servingNode))

	servingClient := newWSClient(tc, []*tmpnet.Node{generationNode})
	expectedSummaryHeight := refreshStateSummaries(tc, servingClient, chainID, fundingKey, transferRecipient)

	bootstrapNode, syncObservation := checkMerkleSyncBootstrap(tc, network)
	if bootstrapNode != nil {
		bootstrapClient := newWSClient(tc, []*tmpnet.Node{bootstrapNode})
		validatePostBootstrapState(tc, bootstrapClient, snapshot, contracts)
		syncEvidence := validateMerkleSyncEvidence(tc, network, bootstrapNode, expectedSummaryHeight)
		reportStateSyncDuration(tc, syncObservation, syncEvidence)

		switch {
		case midChainTransition && stateSyncSupported:
			// The bootstrap node stays up to serve this scenario: the partial
			// node is pinned to state sync from it alone, proving a node that
			// initialized via state sync can serve a full state sync.
			partialNode := checkPartialBootstrap(tc, network, bootstrapNode, partialSeedDir, preTransitionHead, expectedSummaryHeight)
			partialClient := newWSClient(tc, []*tmpnet.Node{partialNode})
			validatePostBootstrapState(tc, partialClient, snapshot, contracts)
			// See the comment on bootstrapNode below.
			partialNode.URI = ""
		case midChainTransition:
			tc.Log().Warn("skipping the partial-bootstrap scenario; the C-Chain cannot state sync the requested state scheme",
				zap.String("stateScheme", stateScheme),
			)
		}

		// SimpleTestContext cleanup runs in registration order rather than LIFO,
		// so the network-level cleanup may stop this ephemeral node before the
		// bootstrap helper's cleanup runs. Clearing the URI avoids a best-effort
		// metrics snapshot against an already-stopped node during cleanup.
		bootstrapNode.URI = ""
	}
}

func newMerkleSyncNetwork(owner string) *tmpnet.Network {
	return &tmpnet.Network{
		UUID:                tmpnet.NewDefaultNetwork(owner).UUID,
		Owner:               owner,
		Nodes:               tmpnet.NewNodesOrPanic(2),
		PrimaryChainConfigs: newMerkleSyncPrimaryChainConfigs(),
	}
}

func configureNetwork(tc tests.TestContext, network *tmpnet.Network) error {
	runtimeConfig, err := flagVars.NodeRuntimeConfig()
	if err != nil {
		return err
	}
	network.DefaultRuntimeConfig = *runtimeConfig

	upgrades := tmpnet.UpgradeConfig(flagVars.ActivateLatestAfter())
	tc.Log().Info("setting upgrades",
		zap.Reflect("upgrades", upgrades),
	)
	upgradeFlags, err := tmpnet.UpgradeFlags(upgrades)
	if err != nil {
		return err
	}
	if network.DefaultFlags == nil {
		network.DefaultFlags = upgradeFlags
	} else {
		network.DefaultFlags.SetDefaults(upgradeFlags)
	}

	if err := network.EnsureDefaultConfig(tc.DefaultContext(), tc.Log()); err != nil {
		return err
	}
	return network.Create(flagVars.RootNetworkDir())
}

func registerNetworkCleanup(tc tests.TestContext, network *tmpnet.Network) {
	require := require.New(tc)
	shutdownDelay := flagVars.NetworkShutdownDelay()
	tc.DeferCleanup(func() {
		if shutdownDelay > 0 {
			time.Sleep(shutdownDelay)
		}
		ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
		defer cancel()
		require.NoError(network.Stop(ctx))
	})
}

func statePaths(node *tmpnet.Node) []string {
	return []string{
		filepath.Join(node.DataDir, "db"),
		filepath.Join(node.DataDir, "chainData"),
	}
}

func startGenerationNode(tc tests.TestContext, network *tmpnet.Network, node *tmpnet.Node) error {
	node.Flags[config.SybilProtectionEnabledKey] = "false"
	return startNodes(tc, network, node)
}

func startServingNetwork(tc tests.TestContext, network *tmpnet.Network, generationNode *tmpnet.Node, servingNode *tmpnet.Node) error {
	delete(generationNode.Flags, config.SybilProtectionEnabledKey)
	return startNodes(tc, network, generationNode, servingNode)
}

func startNodes(tc tests.TestContext, network *tmpnet.Network, nodes ...*tmpnet.Node) error {
	for _, node := range nodes {
		if err := network.StartNode(tc.DefaultContext(), node); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
	defer cancel()
	return tmpnet.WaitForHealthyNodes(ctx, tc.Log(), nodes)
}

func stopNode(node *tmpnet.Node) error {
	ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
	defer cancel()
	return node.Stop(ctx)
}

func copySharedState(sourceNode *tmpnet.Node, targetNode *tmpnet.Node) error {
	for _, relativePath := range []string{"db", "chainData"} {
		sourcePath := filepath.Join(sourceNode.DataDir, relativePath)
		if _, err := os.Stat(sourcePath); err != nil {
			return fmt.Errorf("missing expected shared state path %q: %w", sourcePath, err)
		}
		targetPath := filepath.Join(targetNode.DataDir, relativePath)
		if err := os.RemoveAll(targetPath); err != nil {
			return err
		}
		if err := copyDir(sourcePath, targetPath); err != nil {
			return err
		}
	}
	return nil
}

func copyDir(sourceRoot string, targetRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		relPath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetRoot, relPath)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()

		targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer targetFile.Close()

		_, err = io.Copy(targetFile, sourceFile)
		return err
	})
}

// runCorethEraPhase drives the pre-transition (coreth) era of a mid-chain
// transition run: it issues transfers so the chain has pre-transition blocks,
// captures a copy of the generation node's shared state as the seed for the
// partial-bootstrap scenario, restarts the node, and forces blocks until the
// C-Chain transitions to the SAE VM. Blocks are only built when transactions
// are issued, so the chain cannot cross the transition time on its own.
//
// It returns the seed directory and the head height recorded in the seed.
func runCorethEraPhase(tc tests.TestContext, network *tmpnet.Network, generationNode *tmpnet.Node) (string, uint64) {
	require := require.New(tc)
	tc.By("driving the coreth era and capturing a partial-bootstrap seed")

	client := newWSClient(tc, []*tmpnet.Node{generationNode})
	chainID, err := client.ChainID(tc.DefaultContext())
	require.NoError(err)
	fundingKey := network.PreFundedKeys[0]
	transferRecipient := crypto.PubkeyToAddress(network.PreFundedKeys[1].ToECDSA().PublicKey)

	// A couple of accepted blocks make the seed a genuinely partial bootstrap
	// rather than a fresh database.
	var preTransitionHead uint64
	for range 2 {
		preTransitionHead = issueTransfer(tc, client, chainID, fundingKey, transferRecipient, big.NewInt(0))
	}
	require.Positive(preTransitionHead, "expected pre-transition blocks")

	// The seed must not contain the transition. The generation node is the
	// only validator, so no blocks are built while it is stopped and the
	// capture cannot race the transition time.
	require.NoError(stopNode(generationNode))
	seedDir := filepath.Join(network.Dir, "partial-seed")
	for _, relativePath := range []string{"db", "chainData"} {
		require.NoError(copyDir(
			filepath.Join(generationNode.DataDir, relativePath),
			filepath.Join(seedDir, relativePath),
		))
	}
	require.NoError(startNodes(tc, network, generationNode))

	// Both the seed and the restarted node must still be on coreth, or
	// --activate-latest-after was too short for this environment.
	transitioned, err := cChainTransitioned(tc.DefaultContext(), generationNode)
	require.NoError(err)
	require.False(transitioned, "the generation node transitioned before the seed was captured; increase --activate-latest-after")

	// An HTTP client, not a WS one, drives the forcing transfers below.
	// transitionvm's httpHandlers.Block()/Drain() gate ordinary per-request
	// HTTP calls across the transition, but, per the "blocking API requests"
	// comment in vms/transitionvm/vm.go, cannot do the same for a long-lived
	// connection: "Websockets are long-lived connections which are not able
	// to be gracefully terminated during the transition. This means that
	// websocket connections can (and will) cause this to timeout during the
	// transition." A transfer submitted over a WS connection during the
	// drain window is accepted by the pre-transition VM's mempool but then
	// silently lost when that VM shuts down, hanging any caller waiting on
	// its receipt.
	transitionClient := newHTTPClient(tc, generationNode)
	deadline := time.Now().Add(flagVars.ActivateLatestAfter() + e2e.DefaultTimeout)
	for {
		transitioned, err := cChainTransitioned(tc.DefaultContext(), generationNode)
		require.NoError(err)
		if transitioned {
			break
		}
		require.False(time.Now().After(deadline), "C-Chain did not transition to the SAE VM in time")
		// Fire-and-forget: waiting for the receipt here would reintroduce the
		// WS hang above (an HTTP send right before "blocking API requests"
		// can still be lost the same way). A transfer lost to that narrow
		// pre-Block() race is superseded by the next iteration's, which
		// re-reads the nonce via PendingNonceAt against whichever VM answers
		// it next.
		sendTransfer(tc, transitionClient, chainID, fundingKey, transferRecipient, big.NewInt(0))
		time.Sleep(time.Second)
	}
	tc.Log().Info("C-Chain transitioned to the SAE VM",
		zap.Uint64("preTransitionHead", preTransitionHead),
		zap.String("seedDir", seedDir),
	)
	return seedDir, preTransitionHead
}

// refreshStateSummaries advances the restarted serving topology past a fresh
// state sync summary boundary and returns that boundary, which the bootstrap
// node is expected to sync at or above.
//
// The head is deliberately not driven onto a boundary by issuing a counted
// number of transfers. One transfer does not reliably produce one block: the SAE
// C-Chain packs transactions on its own gas-time schedule and resolves `latest`
// to the last fully executed block, so the head both lags acceptance and moves
// in steps larger than one. Instead this targets the next boundary above the
// current head and issues transfers until the head has passed it.
//
// When the C-Chain cannot state sync the requested scheme there is no summary to
// refresh, so this only proves that the restarted topology still builds blocks
// and returns 0.
func refreshStateSummaries(
	tc tests.TestContext,
	client *ethclient.Client,
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
	transferRecipient common.Address,
) uint64 {
	require := require.New(tc)

	headBlock, err := client.BlockNumber(tc.DefaultContext())
	require.NoError(err)

	// The first commit boundary strictly above the current head, and so one that
	// only the post-restart blocks can produce.
	refreshedBoundary := headBlock - headBlock%stateSyncCommitInterval + stateSyncCommitInterval

	updatedHeadBlock := headBlock
	deadline := time.Now().Add(e2e.DefaultTimeout)
	for updatedHeadBlock < refreshedBoundary {
		if time.Now().After(deadline) {
			require.NoError(fmt.Errorf(
				"head block reached %d within %s, short of the refreshed summary boundary %d",
				updatedHeadBlock,
				e2e.DefaultTimeout,
				refreshedBoundary,
			))
		}
		issueTransfer(tc, client, chainID, fundingKey, transferRecipient, big.NewInt(0))
		updatedHeadBlock, err = client.BlockNumber(tc.DefaultContext())
		require.NoError(err)
	}

	require.Greater(updatedHeadBlock, headBlock, "expected post-restart blocks to advance the head block")
	if !stateSyncSupported {
		tc.Log().Info("forced post-restart blocks; the C-Chain cannot state sync the requested state scheme, so there is no summary to refresh",
			zap.String("stateScheme", stateScheme),
			zap.Uint64("initialHeadBlock", headBlock),
			zap.Uint64("updatedHeadBlock", updatedHeadBlock),
		)
		return 0
	}

	tc.Log().Info("forced post-restart blocks to produce a fresh state summary",
		zap.Uint64("initialHeadBlock", headBlock),
		zap.Uint64("updatedHeadBlock", updatedHeadBlock),
		zap.Uint64("refreshedBoundary", refreshedBoundary),
		zap.Uint64("stateSyncCommitInterval", stateSyncCommitInterval),
	)
	return refreshedBoundary
}

func newMerkleSyncPrimaryChainConfigs() map[string]tmpnet.ConfigMap {
	primaryChainConfigs := tmpnet.DefaultChainConfigs()
	if _, ok := primaryChainConfigs[blockchainID]; !ok {
		primaryChainConfigs[blockchainID] = make(tmpnet.ConfigMap)
	}

	maps.Copy(primaryChainConfigs[blockchainID], tmpnet.ConfigMap{
		"pruning-enabled": true,
		"commit-interval": stateSyncCommitInterval,
	})
	// The serving nodes start from genesis and MUST NOT state sync; both C-Chain
	// implementations accept this key.
	maps.Copy(primaryChainConfigs[blockchainID], tmpnet.ConfigMap{
		"state-sync-enabled": false,
	})
	if !saeCChain {
		// The SAE C-Chain takes its summary heights from commit-interval, set
		// above, and has no state-sync-commit-interval key.
		maps.Copy(primaryChainConfigs[blockchainID], tmpnet.ConfigMap{
			"state-sync-commit-interval": stateSyncCommitInterval,
		})
	}
	maps.Copy(primaryChainConfigs[blockchainID], schemeConfig.chainConfig)
	return primaryChainConfigs
}

func newWSClient(tc tests.TestContext, nodes []*tmpnet.Node) *ethclient.Client {
	require := require.New(tc)
	wsURIs, err := tmpnet.GetNodeWebsocketURIs(nodes, blockchainID)
	require.NoError(err)
	if len(wsURIs) == 0 {
		require.Len(nodes, 1)
		uri := strings.Replace(nodes[0].GetAccessibleURI(), "http://", "ws://", 1)
		uri = strings.Replace(uri, "https://", "wss://", 1)
		wsURIs = []string{uri + "/ext/bc/" + blockchainID + "/ws"}
	}

	client, err := ethclient.Dial(wsURIs[0])
	require.NoError(err)
	return client
}

// newHTTPClient returns an ethclient.Client connected to the given node's
// C-Chain RPC endpoint over HTTP. Unlike newWSClient's long-lived connection,
// each call is a discrete HTTP request, which lets transitionvm's
// httpHandlers.Block()/Drain() gate it during a VM transition; see the
// transitionClient comment in runCorethEraPhase.
func newHTTPClient(tc tests.TestContext, node *tmpnet.Node) *ethclient.Client {
	require := require.New(tc)
	client, err := ethclient.Dial(node.URI + "/ext/bc/" + blockchainID + "/rpc")
	require.NoError(err)
	return client
}

func deployContracts(
	tc tests.TestContext,
	client *ethclient.Client,
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
) deployedContracts {
	require := require.New(tc)
	txOpts, err := newTxOpts(tc, chainID, fundingKey, deployGasLimit)
	require.NoError(err)

	trieAddress, trieTx, trieContract, err := contracts.DeployTrieStressTest(txOpts, client)
	require.NoError(err)
	awaitDeployed(tc, client, trieTx, trieAddress)

	txOpts, err = newTxOpts(tc, chainID, fundingKey, deployGasLimit)
	require.NoError(err)
	loadAddress, loadTx, loadContract, err := contracts.DeployLoadSimulator(txOpts, client)
	require.NoError(err)
	awaitDeployed(tc, client, loadTx, loadAddress)

	tc.Log().Info("deployed contracts for merkle sync workload",
		zap.Stringer("trieStressAddress", trieAddress),
		zap.Stringer("trieStressTxID", trieTx.Hash()),
		zap.Stringer("loadSimulatorAddress", loadAddress),
		zap.Stringer("loadSimulatorTxID", loadTx.Hash()),
	)

	return deployedContracts{
		trieAddress: trieAddress,
		trie:        trieContract,
		loadAddress: loadAddress,
		load:        loadContract,
	}
}

// awaitDeployed blocks until tx is mined and the deployed contract's code is
// readable at the chain's latest state.
//
// bind.WaitDeployed is not used because it reads the code as soon as it sees a
// receipt, which SAE does not guarantee is enough: execution is asynchronous and
// streams per transaction, while the RPC resolves "latest" to the last fully
// executed block (see vms/saevm/blocks/access.go), so a receipt can be served
// before the state carrying it is what "latest" refers to. Polling closes that
// window and is a no-op on coreth, where the receipt already implies the state.
func awaitDeployed(
	tc tests.TestContext,
	client *ethclient.Client,
	tx *types.Transaction,
	address common.Address,
) {
	require := require.New(tc)

	receipt, err := bind.WaitMined(tc.DefaultContext(), client, tx)
	require.NoError(err, "bind.WaitMined()")
	require.Equal(types.ReceiptStatusSuccessful, receipt.Status, "deployment of %s reverted", address)

	var code []byte
	deadline := time.Now().Add(e2e.DefaultTimeout)
	for time.Now().Before(deadline) {
		code, err = client.CodeAt(tc.DefaultContext(), address, nil)
		require.NoError(err, "client.CodeAt()")
		if len(code) > 0 {
			return
		}
		time.Sleep(defaultStatePollInterval)
	}
	require.NotEmpty(code, "code for the contract deployed at %s never became readable", address)
}

func generateWorkload(
	tc tests.TestContext,
	client *ethclient.Client,
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
	transferRecipient common.Address,
	contracts deployedContracts,
	pathsToMeasure []string,
	initialSize int64,
	initialRecipientBalance *big.Int,
) workloadSnapshot {
	require := require.New(tc)

	var (
		totalMixedIterations int
		totalTrieWrites      int64
		totalLoadWrites      int64
		latestWriteValue     = big.NewInt(0)
		latestModifyValue    = big.NewInt(0)
		transferTotal        = new(big.Int)
		lastBlockNumber      uint64
	)

	tc.By("generating mixed workload until bootstrap thresholds are reached", func() {
		for {
			currentSize, err := totalSize(pathsToMeasure...)
			require.NoError(err)
			delta := currentSize - initialSize

			headBlock, err := client.BlockNumber(tc.DefaultContext())
			require.NoError(err)

			heightReady := headBlock >= minBootstrapHeight
			sizeReady := targetBytes == 0 || delta >= targetBytes
			workloadReady := totalMixedIterations > 0
			if heightReady && sizeReady && workloadReady {
				tc.Log().Info("reached merkle sync workload targets",
					zap.Uint64("targetHeight", minBootstrapHeight),
					zap.Uint64("headBlock", headBlock),
					zap.Int64("targetBytes", targetBytes),
					zap.Int64("initialBytes", initialSize),
					zap.Int64("currentBytes", currentSize),
					zap.Int64("deltaBytes", delta),
					zap.Int("totalMixedIterations", totalMixedIterations),
					zap.Int64("totalTrieWrites", totalTrieWrites),
					zap.Int64("totalLoadWrites", totalLoadWrites),
				)
				break
			}

			for range batchSize {
				iteration := totalMixedIterations + 1
				latestWriteValue = big.NewInt(int64(iteration))
				latestModifyValue = big.NewInt(int64(1_000_000 + iteration))

				lastBlockNumber = issueContractTx(tc, client, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
					return contracts.trie.WriteValues(txOpts, big.NewInt(writesPerTx))
				}, chainID, fundingKey, trieWriteGasLimit())
				totalTrieWrites += writesPerTx

				lastBlockNumber = issueContractTx(tc, client, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
					return contracts.load.Write(txOpts, big.NewInt(loadWriteSlots), latestWriteValue)
				}, chainID, fundingKey, loadWriteGasLimit())
				totalLoadWrites += loadWriteSlots

				if totalLoadWrites >= loadModifySlots {
					lastBlockNumber = issueContractTx(tc, client, func(txOpts *bind.TransactOpts) (*types.Transaction, error) {
						return contracts.load.Modify(txOpts, big.NewInt(loadModifySlots), latestModifyValue)
					}, chainID, fundingKey, loadModifyGasLimit())
				}

				lastBlockNumber = issueTransfer(tc, client, chainID, fundingKey, transferRecipient, defaultTransferWei)
				transferTotal.Add(transferTotal, defaultTransferWei)
				totalMixedIterations++
			}

			time.Sleep(defaultPollingDelay)

			currentSize, err = totalSize(pathsToMeasure...)
			require.NoError(err)
			headBlock, err = client.BlockNumber(tc.DefaultContext())
			require.NoError(err)
			tc.Log().Info("measured merkle sync workload progress",
				zap.Uint64("targetHeight", minBootstrapHeight),
				zap.Uint64("headBlock", headBlock),
				zap.Int64("targetBytes", targetBytes),
				zap.Int64("initialBytes", initialSize),
				zap.Int64("currentBytes", currentSize),
				zap.Int64("deltaBytes", currentSize-initialSize),
				zap.Int("totalMixedIterations", totalMixedIterations),
				zap.Int64("totalTrieWrites", totalTrieWrites),
				zap.Int64("totalLoadWrites", totalLoadWrites),
				zap.Int64("loadModifySlots", loadModifySlots),
				zap.Uint64("lastBlockNumber", lastBlockNumber),
			)
		}
	})

	return workloadSnapshot{
		trieArrayLength:      big.NewInt(totalTrieWrites),
		latestWriteValue:     new(big.Int).Set(latestWriteValue),
		latestModifyValue:    new(big.Int).Set(latestModifyValue),
		latestEmptySlot:      big.NewInt(2 + totalLoadWrites),
		latestUnmodifiedSlot: big.NewInt(2 + totalLoadWrites - 1),
		transferRecipient:    transferRecipient,
		transferBalance:      new(big.Int).Add(initialRecipientBalance, transferTotal),
	}
}

func issueContractTx(
	tc tests.TestContext,
	client *ethclient.Client,
	issue func(*bind.TransactOpts) (*types.Transaction, error),
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
	gasLimit uint64,
) uint64 {
	require := require.New(tc)
	txOpts, err := newTxOpts(tc, chainID, fundingKey, gasLimit)
	require.NoError(err)

	tx, err := issue(txOpts)
	require.NoError(err)

	receipt, err := bind.WaitMined(tc.DefaultContext(), client, tx)
	require.NoError(err)
	require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
	return receipt.BlockNumber.Uint64()
}

func issueTransfer(
	tc tests.TestContext,
	client *ethclient.Client,
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
	to common.Address,
	amount *big.Int,
) uint64 {
	require := require.New(tc)
	signedTx := sendTransfer(tc, client, chainID, fundingKey, to, amount)

	receipt, err := bind.WaitMined(tc.DefaultContext(), client, signedTx)
	require.NoError(err)
	require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
	return receipt.BlockNumber.Uint64()
}

// sendTransfer builds, signs, and submits a transfer transaction, without
// waiting for it to be mined. See issueTransfer for a variant that also waits
// for the transaction's receipt.
func sendTransfer(
	tc tests.TestContext,
	client *ethclient.Client,
	chainID *big.Int,
	fundingKey *secp256k1.PrivateKey,
	to common.Address,
	amount *big.Int,
) *types.Transaction {
	require := require.New(tc)
	from := crypto.PubkeyToAddress(fundingKey.ToECDSA().PublicKey)
	nonce, err := client.PendingNonceAt(tc.DefaultContext(), from)
	require.NoError(err)

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		To:        &to,
		Gas:       transferGasLimit,
		GasFeeCap: new(big.Int).Set(defaultGasFeeCap),
		GasTipCap: new(big.Int).Set(defaultGasTipCap),
		Value:     new(big.Int).Set(amount),
	})
	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), fundingKey.ToECDSA())
	require.NoError(err)
	require.NoError(client.SendTransaction(tc.DefaultContext(), signedTx))
	return signedTx
}

func checkMerkleSyncBootstrap(tc tests.TestContext, network *tmpnet.Network) (*tmpnet.Node, syncObservation) {
	require := require.New(tc)
	tc.By("checking if Firewood merkle sync bootstrap is possible with the current network state")

	subnetIDs := make([]string, len(network.Subnets))
	for i, subnet := range network.Subnets {
		subnetIDs[i] = subnet.SubnetID.String()
	}
	flags := tmpnet.FlagsMap{
		config.TrackSubnetsKey: strings.Join(subnetIDs, ","),
	}

	chainConfigContent, err := newBootstrapChainConfigContent(network, nil)
	require.NoError(err)
	flags[config.ChainConfigContentKey] = chainConfigContent

	node := tmpnet.NewEphemeralNode(flags)
	nodeStartedAt := time.Now()
	require.NoError(network.StartNode(tc.DefaultContext(), node))

	tc.DeferCleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
		defer cancel()
		require.NoError(node.Stop(ctx))
	})

	observation, err := awaitBootstrapNode(tc, network, node, nodeStartedAt)
	require.NoError(err, "awaitBootstrapNode()")

	for _, validator := range network.Nodes {
		if validator.IsEphemeral {
			continue
		}
		healthy, err := validator.IsHealthy(tc.DefaultContext())
		require.NoError(err)
		require.True(healthy, "primary validator %s is not healthy", validator.NodeID)
	}

	return node, observation
}

// checkPartialBootstrap starts a node seeded with pre-transition shared state
// and no transition marker — a node that bootstrapped partway on coreth and
// went offline across the transition — and validates that it eagerly
// transitions and state syncs instead of resuming execution.
//
// The node is pinned to state sync exclusively from sourceNode, the fresh
// bootstrap node that itself initialized via state sync, which proves a
// state-synced node can serve a full state sync. The pinning needs two layers,
// because summaries and sync data travel over different planes:
//   - the state-sync-ids/state-sync-ips node flags replace the snowman
//     syncer's summary beacons (snow/engine/snowman/syncer/config.go), so the
//     summary frontier and its acceptance vote come from sourceNode alone —
//     a non-validator is fine because the node manually tracks the given IP;
//   - the state-sync-ids C-Chain config key restricts the data plane — the
//     SAE C-Chain limits its sync PeerTracker to the listed peers
//     (vms/saevm/cchain/config.go), and coreth pins its sync client the same
//     way — so the leafs, code, and block requests hit sourceNode alone.
//
// The validators stay up on their executed state: they no longer serve any
// part of the sync, but the post-sync snowman bootstrapping and consensus
// still need them.
func checkPartialBootstrap(
	tc tests.TestContext,
	network *tmpnet.Network,
	sourceNode *tmpnet.Node,
	seedDir string,
	preTransitionHead uint64,
	expectedSummaryHeight uint64,
) *tmpnet.Node {
	require := require.New(tc)
	tc.By("checking that a partially-bootstrapped pre-transition node state syncs from the state-synced bootstrap node after the transition")

	require.NotZero(sourceNode.StakingAddress, "state sync source node %s has no staking address", sourceNode.NodeID)

	subnetIDs := make([]string, len(network.Subnets))
	for i, subnet := range network.Subnets {
		subnetIDs[i] = subnet.SubnetID.String()
	}
	flags := tmpnet.FlagsMap{
		config.TrackSubnetsKey: strings.Join(subnetIDs, ","),
		config.StateSyncIDsKey: sourceNode.NodeID.String(),
		config.StateSyncIPsKey: sourceNode.StakingAddress.String(),
	}
	chainConfigContent, err := newBootstrapChainConfigContent(network, tmpnet.ConfigMap{
		"state-sync-ids": []ids.NodeID{sourceNode.NodeID},
	})
	require.NoError(err)
	flags[config.ChainConfigContentKey] = chainConfigContent

	node := tmpnet.NewEphemeralNode(flags)
	// EnsureNodeConfig assigns the data dir so the seed can be planted before
	// the node starts.
	require.NoError(network.EnsureNodeConfig(node))
	for _, relativePath := range []string{"db", "chainData"} {
		require.NoError(copyDir(
			filepath.Join(seedDir, relativePath),
			filepath.Join(node.DataDir, relativePath),
		))
	}

	nodeStartedAt := time.Now()
	require.NoError(network.StartNode(tc.DefaultContext(), node))
	tc.DeferCleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), e2e.DefaultTimeout)
		defer cancel()
		require.NoError(node.Stop(ctx))
	})

	observation, err := awaitBootstrapNode(tc, network, node, nodeStartedAt)
	require.NoError(err, "awaitBootstrapNode()")

	syncEvidence := validateMerkleSyncEvidence(tc, network, node, expectedSummaryHeight)
	// Syncing above the seed's head proves the node synced rather than
	// resumed executing from its pre-transition state.
	require.Greater(syncEvidence.summaryHeight, preTransitionHead,
		"expected the partial node to sync a summary above its pre-transition head")
	reportStateSyncDuration(tc, observation, syncEvidence)
	return node
}

// awaitBootstrapNode blocks until the bootstrap node reports healthy, sampling
// the C-Chain's sync lifecycle metrics as it goes so that --restart-during-sync
// can catch the sync in progress. It mirrors [tmpnet.Node.WaitForHealthy],
// which cannot be used here because it allows no work between samples.
func awaitBootstrapNode(tc tests.TestContext, network *tmpnet.Network, node *tmpnet.Node, nodeStartedAt time.Time) (syncObservation, error) {
	ctx := tc.DefaultContext()
	observation := syncObservation{nodeStartedAt: nodeStartedAt}

	ticker := time.NewTicker(defaultSyncSampleInterval)
	defer ticker.Stop()

	restarted := !restartDuringSync
	for {
		if !restarted {
			// A metrics read that errors or predates the transition is not
			// terminal; the node may simply not be serving yet.
			if lifecycle, ok, err := readSyncLifecycle(ctx, node); err == nil && ok && lifecycle.inProgress {
				tc.Log().Info("restarting bootstrap node mid-sync",
					zap.Stringer("nodeID", node.NodeID),
					zap.Uint64("summaryHeight", lifecycle.summaryHeight),
				)
				if err := node.Stop(ctx); err != nil {
					return observation, fmt.Errorf("stopping node %s mid-sync: %w", node.NodeID, err)
				}
				if err := network.StartNode(ctx, node); err != nil {
					return observation, fmt.Errorf("restarting node %s mid-sync: %w", node.NodeID, err)
				}
				tc.Log().Info("restarted bootstrap node mid-sync", zap.Stringer("nodeID", node.NodeID))
				restarted = true
				continue
			}
		}

		reply, err := tmpnet.CheckNodeHealth(ctx, node.URI)
		switch {
		case errors.Is(err, tmpnet.ErrUnrecoverableNodeHealthCheck):
			return observation, fmt.Errorf("node %s saw unrecoverable health check: %w", node.NodeID, err)
		case err != nil:
			tc.Log().Verbo("failed to query bootstrap node health",
				zap.Stringer("nodeID", node.NodeID),
				zap.Error(err),
			)
		case reply.Healthy:
			observation.healthyAt = time.Now()
			return observation, nil
		}

		select {
		case <-ctx.Done():
			return observation, fmt.Errorf("failed to wait for health of node %s: %w", node.NodeID, ctx.Err())
		case <-ticker.C:
		}
	}
}

// newBootstrapChainConfigContent renders the chain config content for a
// bootstrapping node. extraCChainConfig entries, if any, are applied to the
// C-Chain config last, so they win over the shared configuration.
func newBootstrapChainConfigContent(network *tmpnet.Network, extraCChainConfig tmpnet.ConfigMap) (string, error) {
	chainConfigs := map[string]chains.ChainConfig{}
	for alias, flags := range network.PrimaryChainConfigs {
		nodeFlags := maps.Clone(flags)
		if alias == blockchainID {
			maps.Copy(nodeFlags, tmpnet.ConfigMap{
				"pruning-enabled": true,
				"commit-interval": stateSyncCommitInterval,
			})
			// A scheme the C-Chain cannot sync leaves the bootstrap node to
			// bootstrap from genesis instead.
			maps.Copy(nodeFlags, tmpnet.ConfigMap{
				"state-sync-enabled": stateSyncSupported,
			})
			if !saeCChain {
				// The SAE C-Chain has neither key: it always offers to sync
				// what it is given, and takes its summary heights from
				// commit-interval, set above.
				maps.Copy(nodeFlags, tmpnet.ConfigMap{
					"state-sync-min-blocks":      stateSyncMinBlocks,
					"state-sync-commit-interval": stateSyncCommitInterval,
				})
			}
			maps.Copy(nodeFlags, schemeConfig.chainConfig)
			maps.Copy(nodeFlags, extraCChainConfig)
		}
		marshaledFlags, err := json.Marshal(nodeFlags)
		if err != nil {
			return "", err
		}
		chainConfigs[alias] = chains.ChainConfig{Config: marshaledFlags}
	}

	marshaledConfigs, err := json.Marshal(chainConfigs)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(marshaledConfigs), nil
}

func validatePostBootstrapState(
	tc tests.TestContext,
	client *ethclient.Client,
	snapshot workloadSnapshot,
	contracts deployedContracts,
) {
	require := require.New(tc)
	ctx := tc.DefaultContext()

	trieCode, err := client.CodeAt(ctx, contracts.trieAddress, nil)
	require.NoError(err)
	require.NotEmpty(trieCode, "TrieStressTest code should exist after bootstrap")

	loadCode, err := client.CodeAt(ctx, contracts.loadAddress, nil)
	require.NoError(err)
	require.NotEmpty(loadCode, "LoadSimulator code should exist after bootstrap")

	trieLength, err := client.StorageAt(ctx, contracts.trieAddress, storageSlotKey(0), nil)
	require.NoError(err)
	require.Zero(snapshot.trieArrayLength.Cmp(new(big.Int).SetBytes(trieLength)), "unexpected TrieStressTest array length")

	latestEmptySlot, err := client.StorageAt(ctx, contracts.loadAddress, storageSlotKey(1), nil)
	require.NoError(err)
	require.Zero(snapshot.latestEmptySlot.Cmp(new(big.Int).SetBytes(latestEmptySlot)), "unexpected latestEmptySlot value")

	modifiedSlot, err := client.StorageAt(ctx, contracts.loadAddress, storageSlotKey(2), nil)
	require.NoError(err)
	require.Zero(snapshot.latestModifyValue.Cmp(new(big.Int).SetBytes(modifiedSlot)), "unexpected modified storage value")

	latestWrittenSlot, err := client.StorageAt(ctx, contracts.loadAddress, storageSlotBig(snapshot.latestUnmodifiedSlot), nil)
	require.NoError(err)
	require.Zero(snapshot.latestWriteValue.Cmp(new(big.Int).SetBytes(latestWrittenSlot)), "unexpected latest written storage value")

	balance, err := client.BalanceAt(ctx, snapshot.transferRecipient, nil)
	require.NoError(err)
	require.Zero(snapshot.transferBalance.Cmp(balance), "unexpected recipient balance after bootstrap")
}

func issueAtomicExportTx(
	tc tests.TestContext,
	network *tmpnet.Network,
	senderKey *secp256k1.PrivateKey,
) {
	require := require.New(tc)
	nodeURIs := network.GetNodeURIs()
	require.NotEmpty(nodeURIs)

	recipientKey := e2e.NewPrivateKey(tc)
	keychain := secp256k1fx.NewKeychain(senderKey, recipientKey)
	wallet := e2e.NewWallet(tc, keychain, nodeURIs[0])
	xContext := wallet.X().Builder().Context()

	exportOutputs := []*secp256k1fx.TransferOutput{{
		Amt: units.Avax,
		OutputOwners: secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				keychain.Keys[0].Address(),
			},
		},
	}}

	_, err := wallet.C().IssueExportTx(
		xContext.BlockchainID,
		exportOutputs,
		tc.WithDefaultContext(),
	)
	require.NoError(err)

	tc.Log().Info("issued C-Chain export transaction to populate atomic trie",
		zap.Stringer("destinationChainID", xContext.BlockchainID),
		zap.Uint64("amount", units.Avax),
	)
}

// validateMerkleSyncEvidence blocks until the bootstrap node reports the
// expected sync evidence, and returns the sync lifecycle that satisfied it.
func validateMerkleSyncEvidence(tc tests.TestContext, network *tmpnet.Network, bootstrapNode *tmpnet.Node, expectedSummaryHeight uint64) syncLifecycle {
	require := require.New(tc)

	deadline := time.Now().Add(e2e.DefaultTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		var lifecycle syncLifecycle
		lifecycle, lastErr = checkMerkleSyncEvidence(tc, network, bootstrapNode, expectedSummaryHeight)
		if lastErr == nil {
			return lifecycle
		}
		time.Sleep(defaultPollingDelay)
	}
	require.NoError(lastErr)
	return syncLifecycle{} // unreachable: the require above ends the test
}

func checkMerkleSyncEvidence(tc tests.TestContext, network *tmpnet.Network, bootstrapNode *tmpnet.Node, expectedSummaryHeight uint64) (syncLifecycle, error) {
	if stateSyncSupported {
		if err := checkSyncMetrics(tc, network, bootstrapNode); err != nil {
			return syncLifecycle{}, err
		}
	}

	if err := checkBootstrapReady(tc, bootstrapNode); err != nil {
		return syncLifecycle{}, err
	}
	if !stateSyncSupported || !saeCChain {
		// Either no sync was configured to happen, or the C-Chain is served by
		// coreth, which reports no sync lifecycle; the request metrics above
		// are then the only requesting-side evidence. What remains asserted is
		// that the node reached health, which checkBootstrapReady covers;
		// validatePostBootstrapState covers the bootstrapped state itself.
		return syncLifecycle{}, nil
	}

	lifecycle, ok, err := readSyncLifecycle(tc.DefaultContext(), bootstrapNode)
	if err != nil {
		return syncLifecycle{}, err
	}
	if !ok {
		return syncLifecycle{}, errors.New("expected the bootstrap node to report the SAE sync lifecycle metrics")
	}
	tc.Log().Info("read bootstrap node sync lifecycle metrics",
		zap.Bool("inProgress", lifecycle.inProgress),
		zap.Uint64("summaryHeight", lifecycle.summaryHeight),
		zap.Float64("startedSeconds", lifecycle.startedSeconds),
		zap.Float64("finishedSeconds", lifecycle.finishedSeconds),
		zap.Bool("failed", lifecycle.failed),
	)

	// A started, finished, and unfailed sync rules out the paths that would
	// otherwise leave the harness validating a plain bootstrap: sync disabled,
	// no summary offered, the summary declined, or the sync failed.
	if lifecycle.failed {
		return syncLifecycle{}, errors.New("expected bootstrap node state sync to succeed, but it failed")
	}
	if lifecycle.startedSeconds == 0 {
		return syncLifecycle{}, errors.New("expected bootstrap node to start a state sync, but none was started")
	}
	if lifecycle.inProgress || lifecycle.finishedSeconds == 0 {
		return syncLifecycle{}, errors.New("expected bootstrap node state sync to have finished")
	}
	// At or above, rather than equal: the serving nodes offer the highest commit
	// boundary they have accepted, which can be past the boundary the harness
	// forced if the chain kept building. Anything at or above that boundary
	// still rules out a summary predating the serving restart.
	if lifecycle.summaryHeight < expectedSummaryHeight {
		return syncLifecycle{}, fmt.Errorf(
			"expected bootstrap node to sync a summary at or above the refreshed boundary %d, got %d",
			expectedSummaryHeight,
			lifecycle.summaryHeight,
		)
	}
	if lifecycle.summaryHeight%stateSyncCommitInterval != 0 {
		return syncLifecycle{}, fmt.Errorf(
			"expected the synced summary height %d to be a multiple of the commit interval %d",
			lifecycle.summaryHeight,
			stateSyncCommitInterval,
		)
	}
	return lifecycle, nil
}

// checkSyncMetrics asserts that the bootstrap node made, and the validators
// served, the state sync requests for the configured scheme. The metric names
// are the C-Chain implementation's: coreth's by default, or the SAE C-Chain's
// (vms/saevm/statesync) when the run activates Helicon.
func checkSyncMetrics(tc tests.TestContext, network *tmpnet.Network, bootstrapNode *tmpnet.Node) error {
	bootstrapMetrics, err := tests.GetNodeMetrics(tc.DefaultContext(), bootstrapNode.URI)
	if err != nil {
		return err
	}
	stateRequests, ok := tests.GetMetricValue(bootstrapMetrics, schemeConfig.bootstrapRequestMetric, prometheus.Labels{"chain": blockchainID})
	if !ok {
		return fmt.Errorf("expected bootstrap node state sync metric %q", schemeConfig.bootstrapRequestMetric)
	}
	if stateRequests <= 0 {
		return fmt.Errorf("expected bootstrap node to make state sync requests reported by %q", schemeConfig.bootstrapRequestMetric)
	}

	validatorURIs := make([]string, 0, len(network.Nodes))
	for _, node := range network.Nodes {
		if node.IsEphemeral {
			continue
		}
		validatorURIs = append(validatorURIs, node.URI)
	}
	validatorMetrics, err := tests.GetNodesMetrics(tc.DefaultContext(), validatorURIs)
	if err != nil {
		return err
	}
	if sumMetric(validatorMetrics, codeRequestCountMetric, prometheus.Labels{"chain": blockchainID}) <= 0 {
		return errors.New("expected validators to serve code sync requests")
	}
	if sumMetric(validatorMetrics, blockRequestCountMetric, prometheus.Labels{"chain": blockchainID}) <= 0 {
		return errors.New("expected validators to serve block backfill requests")
	}
	for _, metricName := range schemeConfig.servingRequestMetrics {
		if sumMetric(validatorMetrics, metricName, prometheus.Labels{"chain": blockchainID}) <= 0 {
			return fmt.Errorf("expected validators to serve state sync requests reported by %q", metricName)
		}
	}
	return nil
}

// checkBootstrapReady confirms the bootstrap node reports healthy, which
// covers the chain's engine having reached normal operation. The VM-level
// state and scheme details previously read from the health API are gone: the
// SAE VM reports no health details, and the scheme's sync path is proven by
// the scheme-specific request metrics in checkSyncMetrics.
func checkBootstrapReady(tc tests.TestContext, bootstrapNode *tmpnet.Node) error {
	reply, err := tmpnet.CheckNodeHealth(tc.DefaultContext(), bootstrapNode.URI)
	if err != nil {
		return err
	}
	if !reply.Healthy {
		return fmt.Errorf("expected bootstrap node %s to be healthy", bootstrapNode.NodeID)
	}
	return nil
}

// cChainTransitioned reports whether the C-Chain is being served by the SAE
// VM: the SAE summary handler registers its sync lifecycle metrics when the VM
// initializes, and coreth registers none of them.
func cChainTransitioned(ctx context.Context, node *tmpnet.Node) (bool, error) {
	_, ok, err := readSyncLifecycle(ctx, node)
	return ok, err
}

// reportStateSyncDuration reports how long the bootstrap node's state sync
// took. The sync duration spans the VM's own started/finished timestamps and
// is exact; the bootstrap duration is measured by the harness and covers node
// startup and post-sync bootstrapping as well as the sync.
func reportStateSyncDuration(tc tests.TestContext, observation syncObservation, lifecycle syncLifecycle) {
	fields := []zap.Field{
		zap.String("stateScheme", stateScheme),
		zap.Uint64("summaryHeight", lifecycle.summaryHeight),
		zap.Duration("bootstrapDuration", observation.bootstrapDuration()),
	}

	if !stateSyncSupported {
		// No sync happened, so the bootstrap duration is all that was measured.
		tc.Log().Info("measured bootstrap duration; no state sync was configured to happen",
			fields...,
		)
		return
	}

	stateSyncDuration, ok := lifecycle.syncDuration()
	if !ok {
		// Coreth reports no sync lifecycle metrics, so its runs land here.
		tc.Log().Warn("the sync lifecycle was not reported, so the state sync duration could not be measured",
			fields...,
		)
		return
	}
	tc.Log().Info("measured state sync duration from the VM's own timestamps",
		append(fields,
			zap.Duration("stateSyncDuration", stateSyncDuration),
			zap.Float64("startedSeconds", lifecycle.startedSeconds),
			zap.Float64("finishedSeconds", lifecycle.finishedSeconds),
		)...,
	)
}

func sumMetric(allMetrics tests.NodesMetrics, metricName string, labels prometheus.Labels) float64 {
	var total float64
	for _, nodeMetrics := range allMetrics {
		value, ok := tests.GetMetricValue(nodeMetrics, metricName, labels)
		if ok {
			total += value
		}
	}
	return total
}

func storageSlotKey(slot uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(slot))
}

func storageSlotBig(slotValue *big.Int) common.Hash {
	return common.BigToHash(slotValue)
}

// newTxOpts returns transact options that use the provided gas limit rather
// than estimating one. See the gas limit constants for why estimation is
// avoided.
func newTxOpts(tc tests.TestContext, chainID *big.Int, fundingKey *secp256k1.PrivateKey, gasLimit uint64) (*bind.TransactOpts, error) {
	txOpts, err := bind.NewKeyedTransactorWithChainID(fundingKey.ToECDSA(), chainID)
	if err != nil {
		return nil, err
	}
	txOpts.Context = tc.DefaultContext()
	txOpts.GasFeeCap = new(big.Int).Set(defaultGasFeeCap)
	txOpts.GasTipCap = new(big.Int).Set(defaultGasTipCap)
	txOpts.GasLimit = gasLimit
	return txOpts, nil
}

// trieWriteGasLimit returns the gas limit for a TrieStressTest.WriteValues call
// of --writes-per-tx values.
func trieWriteGasLimit() uint64 {
	return contractCallGasOverhead + uint64(writesPerTx)*trieWriteGasPerValue
}

// loadWriteGasLimit returns the gas limit for a LoadSimulator.Write call of
// --load-write-slots slots.
func loadWriteGasLimit() uint64 {
	return contractCallGasOverhead + uint64(loadWriteSlots)*loadWriteGasPerSlot
}

// loadModifyGasLimit returns the gas limit for a LoadSimulator.Modify call of
// --load-modify-slots slots.
func loadModifyGasLimit() uint64 {
	return contractCallGasOverhead + uint64(loadModifySlots)*loadModifyGasPerSlot
}

// requireGasLimitsFitBlock fails the test if the workload's largest transaction
// could never be included in a block.
//
// The C-Chain implementations bound the per-block gas limit differently: coreth
// reports ACP-176's max capacity while the SAE C-Chain reports the worst-case
// block size derived from the gas rate (see vms/saevm/worstcase.State.GasLimit).
// Reading it from the chain therefore avoids encoding either bound here, and
// reports a misconfigured workload up front rather than as a rejected
// transaction mid-run.
//
// It must run once the chain has built a block: the genesis header carries the
// gas limit from the genesis file (100M for tmpnet) rather than the limit the
// running chain enforces, which would make this check vacuous.
func requireGasLimitsFitBlock(tc tests.TestContext, client *ethclient.Client) {
	require := require.New(tc)

	header, err := client.HeaderByNumber(tc.DefaultContext(), nil)
	require.NoError(err, "client.HeaderByNumber()")

	largestTxGasLimit := max(
		deployGasLimit,
		trieWriteGasLimit(),
		loadWriteGasLimit(),
		loadModifyGasLimit(),
		transferGasLimit,
	)
	tc.Log().Info("sized workload transaction gas limits",
		zap.Uint64("blockGasLimit", header.GasLimit),
		zap.Uint64("largestTxGasLimit", largestTxGasLimit),
		zap.Uint64("deployGasLimit", deployGasLimit),
		zap.Uint64("trieWriteGasLimit", trieWriteGasLimit()),
		zap.Uint64("loadWriteGasLimit", loadWriteGasLimit()),
		zap.Uint64("loadModifyGasLimit", loadModifyGasLimit()),
	)
	require.LessOrEqual(
		largestTxGasLimit,
		header.GasLimit,
		"workload transaction gas limit exceeds the chain's block gas limit; lower --writes-per-tx, --load-write-slots or --load-modify-slots",
	)
}

func totalSize(paths ...string) (int64, error) {
	var total int64
	for _, path := range paths {
		size, err := dirSize(path)
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}
