// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime/pprof"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/chains"
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
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm"

	nodeconfig "github.com/ava-labs/avalanchego/config/node"
)

// networkID is the ID of the test network. Mainnet, Fuji, and the local
// network have fixed genesis, so the nodes use another ID.
const networkID = 88888

// The test network's minimum stake durations. They are short so that stakers
// end, and return their stake, within a run, and the runner draws durations
// near them. Since Helicon, primary network validators have their own minimum;
// delegators and subnet validators keep the older one.
const (
	minStakeDuration        = 2 * time.Second
	heliconMinStakeDuration = time.Second
)

// maxValidatorStake is the test network's maximum weight of a validator with
// its delegations: the 1M AVAX that tmpnet.NewTestGenesis stakes on each
// genesis validator, plus room for 4,000 AVAX of delegations, which one
// delegation at the validator scale can exceed. With the default of 3M AVAX,
// the cap never bound. A cap below the genesis weight would refuse every
// delegation to a genesis validator, the only validators that last a run.
const maxValidatorStake = units.MegaAvax + 4*units.KiloAvax

// alpha is the test network's poll threshold for both preference and
// confidence (setTestDefaults says why). A node reaches it in every sample
// from honest votes alone while it has alpha links to honest nodes with no
// delay (injector.sync).
const alpha = 5

// The test network's ping period, pong timeout, and request timeout bounds
// (setTestDefaults says why). The checks read them too, to judge what a
// network fault can explain, and the wait op waits around them.
const (
	pingFrequency     = time.Second
	pongTimeout       = 10 * time.Second
	minRequestTimeout = 200 * time.Millisecond
	maxRequestTimeout = time.Second
)

// registerExtras registers the C-Chain's libevm extras. libevm accepts one
// registration per process. A real node's main does it once.
var registerExtras = sync.OnceFunc(libevm.RegisterExtras)

var _ snow.Acceptor = (*testNode)(nil)

// A testNode is a node that runs in this process.
type testNode struct {
	*node.Node

	done chan error // receives the result of Dispatch

	// The node logs its events to events, not to its events.log, which
	// flushEvents writes.
	events     *eventSink
	eventsPath string

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

// acceptedLen returns how many blocks n accepted on chainID.
func (n *testNode) acceptedLen(chainID ids.ID) int {
	n.mu.Lock()
	defer n.mu.Unlock()

	return len(n.accepted[chainID])
}

// Accepted returns the IDs of the blocks that n accepted on chainID, in the
// order that consensus accepted them. It covers the P-Chain, X-Chain, and
// C-Chain.
func (n *testNode) Accepted(chainID ids.ID) []ids.ID {
	n.mu.Lock()
	defer n.mu.Unlock()

	return slices.Clone(n.accepted[chainID])
}

// flushEvents appends the lines that n logged since the last call to its
// events.log, and returns them.
func (n *testNode) flushEvents() ([]byte, error) {
	b := n.events.drain()
	if len(b) == 0 {
		return nil, nil
	}

	f, err := os.OpenFile(n.eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, perms.ReadWrite)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close() // the write error says more
		return nil, err
	}

	return b, f.Close()
}

// An eventSink holds a node's event log in memory. A write to a file is a
// syscall per line, and a goroutine in a syscall does not count as durably
// blocked in a synctest bubble.
type eventSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
	// written holds a value once a line comes in, until wait takes it. It
	// holds one, so a burst of lines wakes wait once and Write never blocks.
	written chan struct{}
}

func newEventSink() *eventSink {
	return &eventSink{written: make(chan struct{}, 1)}
}

func (s *eventSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	n, err := s.buf.Write(p)
	s.mu.Unlock()

	select {
	case s.written <- struct{}{}:
	default:
	}

	return n, err
}

// wait calls cond until it reports true: once now, and again after the node
// writes a line. It reports false if ctx ends first. A line only hints that
// the node's state changed, so cond reads the state itself. A line written
// while cond runs is not missed: written keeps it until the next select. A
// line wakes one waiter, so only one goroutine may wait at a time.
//
// Unlike a sleep loop, wait lets the synctest clock jump to ctx's deadline
// when the node does nothing.
func (s *eventSink) wait(ctx context.Context, cond func() bool) bool {
	for !cond() {
		select {
		case <-s.written:
		case <-ctx.Done():
			return false
		}
	}

	return true
}

// drain returns the lines written since the last call. The node writes each
// line whole, so no line is cut.
func (s *eventSink) drain() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.buf.Bytes()
	s.buf = bytes.Buffer{}
	return b
}

// testingT is what node setup needs from a test. [testing.T] has it for
// tests that start one network, and [pgregory.net/rapid.T] for TestFuzz,
// which starts one per check.
type testingT interface {
	require.TestingT
	Helper()
	Logf(format string, args ...any)
	Cleanup(f func())
	Context() context.Context
	Failed() bool
}

// keptTempDir returns a new temp dir. Unlike t.TempDir, it is not removed
// when t fails, so the node logs, event log, and databases are there for
// triage.
func keptTempDir(t testingT) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "fuzz-")
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
func newValidatorKey(t testingT) (validatorKey, genesis.Staker) {
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
// keeps its data in its own dir under root. The nodes listen, and the API
// clients of this process dial them, on p's network. startNodes waits until
// every node is healthy, and returns the nodes, the Byzantine validators' keys, and a
// key that the genesis funds. t.Cleanup stops the nodes.
func startNodes(t testingT, root string, byz int, p *proxy) ([]*testNode, []validatorKey, *secp256k1.PrivateKey) {
	t.Helper()
	registerExtras()

	// The fewest honest nodes for which one alpha keeps both safety and
	// liveness against a Byzantine validator of the same stake
	// (setTestDefaults).
	const honest = 7

	keys := make([]validatorKey, honest+byz)
	stakers := make([]genesis.Staker, honest+byz)
	for i := range keys {
		keys[i], stakers[i] = newValidatorKey(t)
	}
	certs := keys[:honest]
	byzantine := keys[honest:]

	// The API clients send every request with http.DefaultClient.
	httpTransport := &http.Transport{DialContext: p.net.DialContext}
	defaultClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: httpTransport}
	t.Cleanup(func() {
		http.DefaultClient = defaultClient
		// Idle connections keep their goroutines, which a synctest bubble
		// does not let outlive the test.
		httpTransport.CloseIdleConnections()
	})

	funded, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)

	unparsed, err := tmpnet.NewTestGenesis(networkID, stakers, []*secp256k1.PrivateKey{funded})
	require.NoError(t, err)
	// The P-Chain's gas capacity starts at 0 and grows with chain time. The
	// nodes are healthy within a second of the genesis time, so without this
	// the first txs exceed the capacity.
	unparsed.StartTime -= 10

	parsed, err := unparsed.Parse()
	require.NoError(t, err)

	genesisBytes, avaxAssetID, err := genesis.FromConfig(&parsed)
	require.NoError(t, err)

	chainIDs, err := primaryChainIDs(genesisBytes)
	require.NoError(t, err)

	var nodes []*testNode
	t.Cleanup(func() {
		// A node stops in about a second. One that hangs, as on a livelocked
		// chain, must not keep the test from reporting why it failed.
		// Shutdown waits for the node, so each runs on its own goroutine.
		const stopTimeout = 30 * time.Second

		stops := make(map[ids.NodeID]chan struct{}, len(nodes))
		for _, n := range nodes {
			stop := make(chan struct{})
			go func() {
				n.Shutdown(0)
				<-n.done
				close(stop)
			}()
			stops[n.ID] = stop
		}
		awaitStopped(t, stops, stopTimeout)

		// The lines since the last flush are the latest, and a failed run
		// needs them most.
		for _, n := range nodes {
			_, err := n.flushEvents()
			require.NoError(t, err)
		}
	})

	var bootstrappers []genesis.Bootstrapper
	for _, k := range certs {
		cfg := defaultConfig(t, filepath.Join(root, k.nodeID.String()))

		cfg.GenesisBytes, cfg.AvaxAssetID = genesisBytes, avaxAssetID
		cfg.StakingTLSCert = *k.cert
		cfg.StakingSignerConfig = nodeconfig.StakingSignerConfig{
			KeyContent: base64.StdEncoding.EncodeToString(k.bls.ToBytes()),
		}
		// Each node bootstraps from the nodes started before it.
		cfg.Bootstrappers = slices.Clone(bootstrappers)
		setTestDefaults(&cfg, honest+byz)
		// flushEvents writes events.log here, and no log factory makes it.
		require.NoError(t, os.MkdirAll(cfg.LoggingConfig.Directory, perms.ReadWriteExecute))

		n := &testNode{
			done:       make(chan error, 1),
			events:     newEventSink(),
			eventsPath: filepath.Join(cfg.LoggingConfig.Directory, "events.log"),
			accepted:   make(map[ids.ID][]ids.ID),
		}
		dialer := &proxyDialer{proxy: p}
		cfg.DialContext, cfg.Listen, cfg.EventLog = dialer.DialContext, p.net.Listen, n.events
		nd, err := node.New(&cfg, noLogs{}, logging.NoLog{})
		require.NoError(t, err)
		// The node binds its staking port in New, and dials only once
		// Dispatch runs.
		dialer.from = nd.StakingAddress()
		n.Node = nd
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
		// Health changes only when the node's health worker runs, which logs
		// no line. The chains log pull gossip a few times a second, so the
		// wait wakes soon after.
		c := health.NewClient(n.APIURI())
		healthy := n.events.wait(ctx, func() bool {
			res, err := c.Health(ctx, nil)
			return err == nil && res.Healthy
		})
		require.True(t, healthy, "node %s is not healthy", n.ID)
	}

	return nodes, byzantine, funded
}

// defaultConfig returns the config that the command line gives a node with
// only the test network and dataDir set. The viper reads no environment
// variables or config file, and the staking keys are ephemeral, so the config
// depends on nothing outside dataDir, which holds only an empty plugin dir.
func defaultConfig(t testingT, dataDir string) nodeconfig.Config {
	pluginDir := filepath.Join(dataDir, "plugins")
	require.NoError(t, os.MkdirAll(pluginDir, perms.ReadWriteExecute))

	v := viper.New()
	require.NoError(t, v.BindPFlags(config.BuildFlagSet()))
	v.Set(config.NetworkNameKey, constants.NetworkName(networkID))
	v.Set(config.DataDirKey, dataDir)
	v.Set(config.PluginDirKey, pluginDir)
	v.Set(config.StakingEphemeralCertEnabledKey, true)
	v.Set(config.StakingEphemeralSignerEnabledKey, true)
	cfg, err := config.GetNodeConfig(v)
	require.NoError(t, err)

	return cfg
}

// noLogs makes loggers that drop everything. The nodes debug through
// events.log, and a file logger leaves a goroutine running after Close, which
// a synctest bubble does not allow.
type noLogs struct{}

var _ logging.Factory = noLogs{}

func (noLogs) Make(string) (logging.Logger, error) {
	return logging.NoLog{}, nil
}

func (noLogs) MakeChain(string) (logging.Logger, error) {
	return logging.NoLog{}, nil
}

func (noLogs) SetLogLevel(string, logging.Level) error {
	return nil
}

func (noLogs) SetDisplayLevel(string, logging.Level) error {
	return nil
}

func (noLogs) GetLogLevel(string) (logging.Level, error) {
	return logging.Off, nil
}

func (noLogs) GetDisplayLevel(string) (logging.Level, error) {
	return logging.Off, nil
}

func (noLogs) GetLoggerNames() []string {
	return nil
}

func (noLogs) Close() {}

// awaitStopped waits up to d in all for each node's channel in stops to
// close, which it does once the node stopped, and returns the nodes that did.
// If a node does not, it logs the nodes and the stack of every goroutine
// through t, and returns without them.
func awaitStopped(t testingT, stops map[ids.NodeID]chan struct{}, d time.Duration) set.Set[ids.NodeID] {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()

	stopped := set.NewSet[ids.NodeID](len(stops))
	var hung []ids.NodeID
	for id, c := range stops {
		select {
		case <-c:
			stopped.Add(id)
			continue
		case <-ctx.Done():
		}

		// After the bound both cases are ready, and select picks one at
		// random. A node that stopped is not hung.
		select {
		case <-c:
			stopped.Add(id)
		default:
			hung = append(hung, id)
		}
	}
	if len(hung) == 0 {
		return stopped
	}

	var stacks strings.Builder
	_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2) // a strings.Builder never fails a write
	t.Logf("nodes %v did not stop in %s. The cleanup continues without them. The goroutines are:\n%s", hung, d, stacks.String())

	return stopped
}

// setTestDefaults changes cfg from the defaults to what a short test
// network on one machine needs. validators is the number of genesis
// validators.
func setTestDefaults(cfg *nodeconfig.Config, validators int) {
	// Port 0 lets the OS pick free ports.
	cfg.PublicIP = "127.0.0.1"
	cfg.ListenHost, cfg.ListenPort = "127.0.0.1", 0
	cfg.HTTPHost, cfg.HTTPPort = "127.0.0.1", 0

	// The upgrades that e2e runs with by default: the latest is not
	// scheduled.
	cfg.UpgradeConfig = testUpgrades()

	cfg.MinStakeDuration = minStakeDuration
	cfg.HeliconMinStakeDuration = heliconMinStakeDuration
	cfg.MaxValidatorStake = maxValidatorStake
	// A few conversions fill the active L1 validators, so a conversion or an
	// increase meets the capacity. The default capacity of 20,000 is never
	// reached. The target stays below the capacity, as on real networks; the
	// excess then grows by at most 1 a second, and the price stays at its
	// minimum within a run.
	cfg.ValidatorFeeConfig.Capacity = 2
	cfg.ValidatorFeeConfig.Target = 1
	// A node whose mempool holds a tx that the block gas capacity left cannot
	// take tries to build at once, again and again, until chain time refills
	// the capacity (vms/platformvm/txs/mempool/mempool.go WaitForEvent,
	// block/builder/builder.go packEtnaBlockTxs). In the synctest bubble that
	// loop keeps time from ever moving on, so the capacity must never run out
	// within a run: 100M gas, refilled in a second.
	//
	// TODO: restore the default capacity once WaitForEvent waits for the
	// capacity to refill (Findings.md 1). Until then no run reaches a block or a
	// mempool held back by gas capacity.
	cfg.DynamicFeeConfig.MaxCapacity = 100_000_000
	cfg.DynamicFeeConfig.MaxPerSecond = 100_000_000

	// A 1 s proposer window leaves no room for the default block delay.
	cfg.ProposerMinBlockDelay = 0
	cfg.UseCurrentHeight = true
	// Pings carry the sender's view of the peer's uptime, and a receiver
	// closes the link on a bad one, so a ping bug shows up only if pings
	// fire within a run: every second, not every 22.5 s. The pong timeout is
	// also the read and write deadline (network/peer/peer.go nextTimeout), so
	// it does not keep its default 4:3 ratio to the ping period: a third of a
	// second of slack lets a busy machine close an honest link.
	cfg.NetworkConfig.PingFrequency = pingFrequency
	cfg.NetworkConfig.PingPongTimeout = pongTimeout

	// The bounds keep request timeouts under the ping period, as the defaults
	// (2 s to 10 s) keep them under 22.5 s. The timeout must start within
	// them, so it starts at the top, the nearest to the 5 s default.
	cfg.AdaptiveTimeoutConfig.InitialTimeout = maxRequestTimeout
	cfg.AdaptiveTimeoutConfig.MinimumTimeout = minRequestTimeout
	cfg.AdaptiveTimeoutConfig.MaximumTimeout = maxRequestTimeout

	// A tx reaches the next proposer by a push round if it came in by the
	// API, and by a pull round if a Byzantine peer gossiped it, since a node
	// does not push on what it got by gossip. Both run 10 times as often as
	// by default; the pull throttle keeps its ratio of requests to period.
	// Marshaling a map of numbers cannot fail.
	gossip, _ := json.Marshal(map[string]any{
		"network": map[string]any{
			"push-gossip-frequency":              50 * time.Millisecond,
			"pull-gossip-frequency":              150 * time.Millisecond,
			"pull-gossip-requests-per-validator": 24000,
		},
	})
	cfg.ChainConfigs = map[string]chains.ChainConfig{
		"P": {Config: gossip},
		"X": {Config: gossip},
	}

	// Test machines are often short of disk space.
	cfg.RequiredAvailableDiskSpacePercentage = 0
	cfg.WarningAvailableDiskSpacePercentage = 0

	// Each node writes logs/events.log under its data dir, which the
	// fuzzer's own events.log links to by tx and block IDs.
	cfg.LogEvents = true

	// A poll samples all genesis validators but one, by stake and without
	// replacement (snow/validators/set.go): 7 honest nodes and 1
	// Byzantine one of the same stake (startNodes). With alpha = 5:
	//   - two groups of honest nodes that cut links split cannot each reach
	//     alpha with the Byzantine vote, which needs alpha above half the
	//     honest nodes plus one, and a sample only removes votes;
	//   - a node with one cut link reaches alpha in every sample from honest
	//     votes alone, with the Byzantine validator silent: K less it and
	//     the cut peer is 5;
	//   - the connected-stake health threshold,
	//     alpha/K*(1-buffer)+buffer = 0.77
	//     (snow/consensus/snowball/parameters.go MinPercentConnectedHealthy),
	//     stays under 7/8, the honest share that startNodes waits for before
	//     any Byzantine peer connects.
	// With fewer honest nodes no alpha meets the first two while K is below
	// the number of validators, and sampling all of them would take the
	// randomness out of polls.
	params := snowball.DefaultParameters
	params.K = validators - 1
	params.AlphaPreference = alpha
	params.AlphaConfidence = alpha
	cfg.SubnetConfigs = map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {SnowParameters: &params},
	}
}

// testUpgrades returns the upgrade schedule the test nodes run with: every
// upgrade but the latest is active from genesis, and a proposer window lasts
// one second, the shortest that the proposervm takes.
func testUpgrades() upgrade.Config {
	upgrades := tmpnet.UpgradeConfig(-1)
	// tmpnet activates them at upgrade.InitiallyActiveTime, in 2020, but a
	// synctest bubble's clock starts in 2000.
	upgradetest.SetTimesTo(&upgrades, upgradetest.Latest-1, time.Unix(0, 0))
	// A tx that only a node outside the current slot holds waits for that
	// node's slot.
	upgrades.ProposerWindowDuration = time.Second
	return upgrades
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
