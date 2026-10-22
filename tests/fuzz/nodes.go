// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm"

	nodeconfig "github.com/ava-labs/avalanchego/config/node"
)

// networkID is the ID of the test network. Mainnet, Fuji, and the local
// network have fixed genesis, so the nodes use another ID.
const networkID = 88888

// registerExtras registers the C-Chain's libevm extras. libevm accepts one
// registration per process. A real node's main does it once.
var registerExtras = sync.OnceFunc(libevm.RegisterExtras)

var _ snow.Acceptor = (*testNode)(nil)

// A testNode is a node that runs in this process.
type testNode struct {
	*node.Node

	logs logging.Factory
	done chan error // receives the result of Dispatch

	mu       sync.Mutex
	accepted map[ids.ID][]ids.ID // chain ID to accepted block IDs, in order
}

// Accept records a block that consensus accepted.
func (n *testNode) Accept(ctx *snow.ConsensusContext, blkID ids.ID, _ []byte) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.accepted[ctx.ChainID] = append(n.accepted[ctx.ChainID], blkID)
	return nil
}

// Accepted returns the IDs of the blocks that n accepted on chainID, in the
// order that consensus accepted them. It covers the P-Chain, X-Chain, and
// C-Chain.
func (n *testNode) Accepted(chainID ids.ID) []ids.ID {
	n.mu.Lock()
	defer n.mu.Unlock()

	return slices.Clone(n.accepted[chainID])
}

// keptTempDir returns a new temp dir. Unlike t.TempDir, it stays when t
// fails, so node logs and databases are there for triage.
func keptTempDir(t testing.TB) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "fuzz-") //nolint:usetesting // t.TempDir deletes it on failure
	require.NoError(t, err)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("kept node data in %s", dir)
			return
		}
		_ = os.RemoveAll(dir) // the OS cleans temp dirs if this fails
	})

	return dir
}

// A validatorKey holds the staking keys of a genesis validator. The fuzzer
// holds the keys of every validator it runs, and of the Byzantine validators
// that have no node.
type validatorKey struct {
	nodeID ids.NodeID
	cert   *tls.Certificate
	bls    *localsigner.LocalSigner
}

// newValidatorKey generates a staking key pair and the matching genesis
// staker.
func newValidatorKey(t testing.TB) (validatorKey, genesis.Staker) {
	t.Helper()

	cert, err := staking.NewTLSCert()
	require.NoError(t, err)

	stakingCert, err := staking.ParseCertificate(cert.Leaf.Raw)
	require.NoError(t, err)

	bls, err := localsigner.New()
	require.NoError(t, err)

	pop, err := signer.NewProofOfPossession(bls)
	require.NoError(t, err)

	nodeID := ids.NodeIDFromCert(stakingCert)
	return validatorKey{nodeID: nodeID, cert: cert, bls: bls},
		genesis.Staker{
			NodeID:        nodeID,
			RewardAddress: ids.GenerateTestShortID(),
			DelegationFee: reward.PercentDenominator / 100,
			Signer:        pop,
		}
}

// startNodes starts honest validators in this process, and adds byz Byzantine
// validators to the genesis with no node behind them. They share a genesis,
// and every connection that one dials to another goes through p. Each node
// keeps its data in its own dir under root. startNodes waits until every node
// is healthy, and returns the nodes, the Byzantine validators' keys, and a
// key that the genesis funds. t.Cleanup stops the nodes.
func startNodes(t testing.TB, root string, byz int, p *proxy) ([]*testNode, []validatorKey, *secp256k1.PrivateKey) {
	t.Helper()
	registerExtras()

	// A few validators keep startup fast while leaving room for Byzantine
	// stake under the connected-stake health threshold.
	const honest = 3

	keys := make([]validatorKey, honest+byz)
	stakers := make([]genesis.Staker, honest+byz)
	for i := range keys {
		keys[i], stakers[i] = newValidatorKey(t)
	}
	certs := keys[:honest]
	byzantine := keys[honest:]

	funded, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)

	unparsed, err := tmpnet.NewTestGenesis(networkID, stakers, []*secp256k1.PrivateKey{funded})
	require.NoError(t, err)

	parsed, err := unparsed.Parse()
	require.NoError(t, err)

	genesisBytes, avaxAssetID, err := genesis.FromConfig(&parsed)
	require.NoError(t, err)

	chainIDs, err := primaryChainIDs(genesisBytes)
	require.NoError(t, err)

	var nodes []*testNode
	t.Cleanup(func() {
		for _, n := range nodes {
			n.Shutdown(0)
		}
		for _, n := range nodes {
			<-n.done
			n.logs.Close()
		}
	})

	var bootstrappers []genesis.Bootstrapper
	for _, k := range certs {
		cfg, err := config.NewDefault(networkID, filepath.Join(root, k.nodeID.String()))
		require.NoError(t, err)

		cfg.GenesisBytes, cfg.AvaxAssetID = genesisBytes, avaxAssetID
		cfg.StakingTLSCert = *k.cert
		cfg.StakingSignerConfig = nodeconfig.StakingSignerConfig{
			KeyContent: base64.StdEncoding.EncodeToString(k.bls.ToBytes()),
		}
		// Each node bootstraps from the nodes started before it.
		cfg.Bootstrappers = slices.Clone(bootstrappers)
		setTestDefaults(&cfg)
		require.NoError(t, os.MkdirAll(cfg.PluginDir, perms.ReadWriteExecute))

		logs := logging.NewFactory(cfg.LoggingConfig)
		log, err := logs.Make("main")
		require.NoError(t, err)

		nd, err := node.New(&cfg, logs, log, &node.Options{DialContext: p.DialContext})
		require.NoError(t, err)

		n := &testNode{
			Node:     nd,
			logs:     logs,
			done:     make(chan error, 1),
			accepted: make(map[ids.ID][]ids.ID),
		}
		// Nothing is accepted before Dispatch connects the node to its peers.
		for _, chainID := range chainIDs {
			require.NoError(t, nd.BlockAcceptorGroup.RegisterAcceptor(chainID, "fuzz", n, false))
		}
		nodes = append(nodes, n)
		go func() {
			n.done <- nd.Dispatch()
		}()

		bootstrappers = append(bootstrappers, genesis.Bootstrapper{
			ID: nd.ID,
			IP: nd.StakingAddress(),
		})
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	for _, n := range nodes {
		healthy, err := health.AwaitHealthy(ctx, health.NewClient(n.APIURI()), 250*time.Millisecond, nil)
		require.NoError(t, err, "node %s is not healthy", n.ID)
		require.True(t, healthy, "node %s is not healthy", n.ID)
	}

	return nodes, byzantine, funded
}

// setTestDefaults changes cfg from the defaults to what a short test
// network on one machine needs.
func setTestDefaults(cfg *nodeconfig.Config) {
	// Port 0 lets the OS pick free ports.
	cfg.PublicIP = "127.0.0.1"
	cfg.ListenHost, cfg.ListenPort = "127.0.0.1", 0
	cfg.HTTPHost, cfg.HTTPPort = "127.0.0.1", 0

	// The upgrades that e2e runs with by default: the latest is not
	// scheduled.
	cfg.UpgradeConfig = testUpgrades()

	// Stakers end, and get their stake back, within a run.
	cfg.MinStakeDuration = 2 * time.Second
	cfg.HeliconMinStakeDuration = time.Second

	// Blocks are built, peers reconnect, and health is checked faster than in
	// production.
	cfg.ProposerMinBlockDelay = 0
	cfg.UseCurrentHeight = true
	cfg.NetworkConfig.PeerListPullGossipFreq = 250 * time.Millisecond
	cfg.NetworkConfig.MaxReconnectDelay = time.Second
	cfg.HealthCheckFreq = 2 * time.Second

	// Test machines are often short of disk space.
	cfg.RequiredAvailableDiskSpacePercentage = 0
	cfg.WarningAvailableDiskSpacePercentage = 0

	cfg.LoggingConfig.LogLevel = logging.Debug
	cfg.LoggingConfig.DisplayLevel = logging.Off

	// A small sample size keeps the network healthy with a few validators,
	// and lets a quarter of the stake be Byzantine without crossing the
	// connected-stake threshold for health, which is
	// AlphaConfidence/K*(1-buffer)+buffer. With K=3 that is 0.73, under the
	// 0.75 that three of four equal validators reach.
	params := snowball.DefaultParameters
	params.K = 3
	params.AlphaPreference = 2
	params.AlphaConfidence = 2
	cfg.SubnetConfigs = map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {SnowParameters: &params},
	}
}

// testUpgrades returns the upgrade schedule the test nodes run with: every
// upgrade but the latest is active from genesis.
func testUpgrades() upgrade.Config {
	return tmpnet.UpgradeConfig(-1)
}

// primaryChainIDs returns the IDs of the P-Chain, X-Chain, and C-Chain that
// genesisBytes creates.
func primaryChainIDs(genesisBytes []byte) ([]ids.ID, error) {
	chainIDs := []ids.ID{constants.PlatformChainID}
	for _, vmID := range []ids.ID{constants.AVMID, constants.EVMID} {
		tx, err := genesis.VMGenesis(genesisBytes, vmID)
		if err != nil {
			return nil, err
		}
		chainIDs = append(chainIDs, tx.ID())
	}
	return chainIDs, nil
}
