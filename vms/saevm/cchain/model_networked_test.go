// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/holiman/uint256"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// nodeStorage is one node's independently drawn storage configuration. Nodes
// with different storage backends must still converge on identical state — a
// free invariant of the networked model.
type nodeStorage struct {
	kv             string // kvMemDB or kvLevelDB
	scheme         string // rawdb.HashScheme or customrawdb.FirewoodScheme
	commitInterval uint64
}

// networkedRunConfig is the once-per-check configuration of a networked model
// run. The embedded runConfig contributes the shared axes (accounts, genesis
// balances, dynamic-parameter votes, numValidators); its single-node storage
// axes (kv, scheme, commitInterval) and numAtomicKeys remain zero and are
// ignored — storage is drawn per node in perNode, and this suite issues no
// cross-chain txs.
type networkedRunConfig struct {
	runConfig

	numNonValidators int
	perNode          []nodeStorage
}

func (c networkedRunConfig) numNodes() int {
	return c.numValidators + c.numNonValidators
}

func genNodeStorage() *rapid.Generator[nodeStorage] {
	return rapid.Custom(func(rt *rapid.T) nodeStorage {
		return nodeStorage{
			// Weighted draws: repeats in the sample set set the odds, matching
			// genRunConfig. memdb/HashScheme dominate to keep the CI budget in
			// check while still reaching leveldb/Firewood regularly.
			kv: rapid.SampledFrom([]string{
				kvMemDB, kvMemDB, kvMemDB, kvMemDB, kvMemDB,
				kvMemDB, kvMemDB, kvMemDB, kvMemDB, kvMemDB,
				kvMemDB, kvMemDB, kvMemDB, kvMemDB, kvMemDB,
				kvMemDB, kvMemDB, kvMemDB, kvMemDB, kvLevelDB,
			}).Draw(rt, "kv"),
			scheme: rapid.SampledFrom([]string{
				rawdb.HashScheme, rawdb.HashScheme, rawdb.HashScheme,
				rawdb.HashScheme, rawdb.HashScheme, rawdb.HashScheme,
				rawdb.HashScheme, rawdb.HashScheme, rawdb.HashScheme,
				customrawdb.FirewoodScheme,
			}).Draw(rt, "scheme"),
			commitInterval: rapid.SampledFrom([]uint64{1, 4, 16, saedb.DefaultCommitInterval}).Draw(rt, "commitInterval"),
		}
	})
}

// storageOptions returns the sutOption applying s's storage axes to one
// node's VM config.
func (s nodeStorage) storageOptions() sutOption {
	return options.Func[sutConfig](func(sc *sutConfig) {
		sc.vmConfig.StateScheme = s.scheme
		sc.vmConfig.CommitInterval = s.commitInterval
	})
}

func genNetworkedRunConfig() *rapid.Generator[networkedRunConfig] {
	return rapid.Custom(func(rt *rapid.T) networkedRunConfig {
		c := networkedRunConfig{
			runConfig: runConfig{
				numAccounts: uint(rapid.IntRange(2, 6).Draw(rt, "numAccounts")), //#nosec G115 -- bounded draw, 2..6
				// 2 validators common, 3 rare: per-action cost scales with node
				// count, and most convergence bugs need only two views.
				numValidators: rapid.SampledFrom([]int{2, 2, 2, 3}).Draw(rt, "numValidators"),
			},
			numNonValidators: rapid.IntRange(0, 1).Draw(rt, "numNonValidators"),
		}
		numAccounts := int(c.numAccounts) //#nosec G115 -- bounded draw, 2..6
		c.balanceExps = rapid.SliceOfN(rapid.IntRange(9, 30), numAccounts, numAccounts).Draw(rt, "balanceExps")
		if rapid.Bool().Draw(rt, "voteGasTarget") {
			g := gas.Gas(rapid.Uint64Range(1_000_000, 100_000_000).Draw(rt, "gasTarget"))
			c.gasTarget = &g
		}
		if rapid.Bool().Draw(rt, "votePriceTarget") {
			p := gas.Price(rapid.Uint64Range(1, 1_000_000).Draw(rt, "priceTarget"))
			c.priceTarget = &p
		}
		if rapid.Bool().Draw(rt, "voteMinDelay") {
			d := rapid.Uint64Range(1, 10_000).Draw(rt, "minDelayMS")
			c.minDelayMS = &d
		}
		n := c.numNodes()
		c.perNode = rapid.SliceOfN(genNodeStorage(), n, n).Draw(rt, "perNode")
		return c
	})
}

func TestGenNetworkedRunConfig(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cfg := genNetworkedRunConfig().Draw(rt, "networkedRunConfig")
		require.Containsf(rt, []int{2, 3}, cfg.numValidators, "numValidators")
		require.Containsf(rt, []int{0, 1}, cfg.numNonValidators, "numNonValidators")
		require.Lenf(rt, cfg.perNode, cfg.numNodes(), "one storage draw per node")
		require.GreaterOrEqualf(rt, cfg.numAccounts, uint(2), "numAccounts lower bound")
		require.LessOrEqualf(rt, cfg.numAccounts, uint(6), "numAccounts upper bound")
		numAccounts := int(cfg.numAccounts) //#nosec G115 -- bounded draw, 2..6
		require.Lenf(rt, cfg.balanceExps, numAccounts, "one balance exponent per account")
		require.Zerof(rt, cfg.numAtomicKeys, "networked suite issues no atomic txs")
		for i, s := range cfg.perNode {
			require.Containsf(rt, []string{kvMemDB, kvLevelDB}, s.kv, "node %d kv store kind", i)
			require.Containsf(rt, []string{rawdb.HashScheme, customrawdb.FirewoodScheme}, s.scheme, "node %d trie scheme", i)
			require.NotZerof(rt, s.commitInterval, "node %d commit interval", i)
		}
	})
}

// TestModelNetworked drives randomized action sequences against a small
// network of in-process cchain nodes wired via saetest senders. Transactions
// travel through real push/pull gossip; block distribution is orchestrated by
// the machine (playing every node's consensus engine), so every scheduling
// decision is a labeled rapid draw and failures replay deterministically.
// After every action, all non-delayed nodes must agree exactly with each
// other and with the shared model. Reproduce failures with -rapid.seed /
// -rapid.failfile; explore more deeply with -rapid.checks.
func TestModelNetworked(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cfg := genNetworkedRunConfig().Draw(rt, "networkedRunConfig")
		nm := newNetworkedMachine(t, rt, cfg)
		defer nm.tb.close()
		rt.Repeat(nm.actions())
	})
}

// modelNode is one node of the networked machine: a SUT plus the per-node
// bookkeeping the machine needs to restart it and to track how far behind
// the canonical chain it is.
type modelNode struct {
	idx         int
	nodeID      ids.NodeID
	isValidator bool

	ctx context.Context
	sut *SUT

	storage nodeStorage
	db      database.Database
	dbDir   string
	dataDir string

	// acceptedCount is the number of post-genesis canonical blocks this node
	// has accepted. canonical[acceptedCount:] is its (implicit) pending
	// delivery queue while delayed.
	acceptedCount int
	delayed       bool
}

// acceptedBlock is one canonical block as raw material for delivery to any
// node: bytes survive node restarts, unlike *blocks.Block handles.
type acceptedBlock struct {
	id     ids.ID
	height uint64
	bytes  []byte
}

// networkedMachine drives N connected SUTs against one shared model. The
// model predicts chain state, which every converged node must match exactly.
type networkedMachine struct {
	*modelCore
	cfg networkedRunConfig

	clock   *saetest.Clock
	timeOpt sutOption
	vdrs    *warptest.Validators

	genesisID ids.ID
	nodes     []*modelNode
	canonical []acceptedBlock

	// pins maps an account with in-flight txs to the node all its txs are
	// issued to until the account drains. Eth-tx gossip reaches validators
	// only (see sae's TestGossip), so spreading one account's txs across
	// nodes would strand a nonce-gapped tx on a node that can never learn
	// the missing nonce — never promoted, never gossiped, builder sync hangs.
	pins map[common.Address]int
}

func newNetworkedMachine(t *testing.T, rt *rapid.T, cfg networkedRunConfig) *networkedMachine {
	tb := newScopedTB(t)
	tb.setRapidT(rt)

	keys := saetest.NewUNSAFEKeyChain(tb, cfg.numAccounts)
	timeOpt, clock := withVMTime(testStartTime)

	vdrIDs := make([]ids.NodeID, cfg.numValidators)
	for i := range vdrIDs {
		vdrIDs[i] = ids.GenerateTestNodeID()
	}

	m := &model{
		balances:    make(map[common.Address]*uint256.Int),
		nonces:      make(map[common.Address]uint64),
		pendingEth:  make(map[common.Hash]*issuedTx),
		pendingCost: make(map[common.Address]*uint256.Int),
		contracts:   make(map[common.Address]*contractState),
		target:      dynamic.InitialTargetExponent,
		price:       dynamic.InitialPriceExponent,
		delay:       dynamic.InitialDelayExponent,
	}
	nm := &networkedMachine{
		modelCore: &modelCore{
			tb:     tb,
			m:      m,
			wallet: saetest.NewWalletWithKeyChain(keys, types.LatestSigner(saetest.ChainConfig())),
			addrs:  keys.Addresses(),
		},
		cfg:     cfg,
		clock:   clock,
		timeOpt: timeOpt,
		vdrs:    warptest.NewValidatorsWithNodeIDs(tb, vdrIDs...),
		pins:    make(map[common.Address]int),
	}
	for i, addr := range nm.addrs {
		bal, overflow := uint256.FromBig(cfg.balance(i))
		require.Falsef(tb, overflow, "genesis balance of account %d overflows uint256", i)
		m.balances[addr] = bal
		m.pendingCost[addr] = new(uint256.Int)
	}
	if cfg.gasTarget != nil {
		d := dynamic.DesiredTargetExponent(*cfg.gasTarget)
		m.desiredTarget = &d
	}
	if cfg.priceTarget != nil {
		d := dynamic.DesiredPriceExponent(*cfg.priceTarget)
		m.desiredPrice = &d
	}
	if cfg.minDelayMS != nil {
		d := dynamic.DesiredDelayExponent(*cfg.minDelayMS)
		m.desiredDelay = &d
	}

	for i := range cfg.numNodes() {
		n := &modelNode{
			idx:         i,
			isValidator: i < cfg.numValidators,
			storage:     cfg.perNode[i],
			dataDir:     t.TempDir(),
		}
		if n.isValidator {
			n.nodeID = vdrIDs[i]
		} else {
			n.nodeID = ids.GenerateTestNodeID()
		}
		switch n.storage.kv {
		case kvLevelDB:
			n.dbDir = t.TempDir()
			db, err := leveldb.New(n.dbDir, nil, logging.NoLog{}, prometheus.NewRegistry())
			require.NoErrorf(tb, err, "leveldb.New(%q) for node %d", n.dbDir, i)
			n.db = db
			tb.Cleanup(func() { _ = n.db.Close() })
		default:
			n.db = memdb.New()
		}
		nm.nodes = append(nm.nodes, n)
		nm.openNode(i)
	}

	// All nodes must start from the same genesis block, or the network
	// exhibits very weird behavior.
	genesisID, err := nm.nodes[0].sut.LastAccepted(nm.nodes[0].ctx)
	require.NoErrorf(tb, err, "%T.LastAccepted() on node 0", nm.nodes[0].sut.VM)
	for _, n := range nm.nodes[1:] {
		got, err := n.sut.LastAccepted(n.ctx)
		require.NoErrorf(tb, err, "%T.LastAccepted() on node %d", n.sut.VM, n.idx)
		require.Equalf(tb, genesisID, got, "genesis ID of node %d", n.idx)
	}
	nm.genesisID = genesisID
	m.lastAcceptedID = genesisID

	// Fully connect the validator clique; non-validators connect only to
	// validators, mirroring production (and sae's newNetworkedSUTs).
	vdrSUTs := make([]*SUT, cfg.numValidators)
	for i := range cfg.numValidators {
		vdrSUTs[i] = nm.nodes[i].sut
	}
	saetest.Connect(tb, vdrSUTs...)
	for _, n := range nm.nodes[cfg.numValidators:] {
		saetest.ConnectTo(tb, n.sut, vdrSUTs...)
	}
	return nm
}

// openNode (re)creates node i's SUT against its persisted database, deriving
// every other option identically — the networked analogue of the single-node
// machine's openSUT.
func (nm *networkedMachine) openNode(i int) {
	n := nm.nodes[i]
	opts := []sutOption{
		nm.timeOpt,
		withNodeID(n.nodeID),
		withValidators(nm.vdrs),
		n.storage.storageOptions(),
		withChainDataDir(n.dataDir),
		withDB(n.db),
		// Benign on restart recovery; see the single-node machine's
		// baseOptions for the full analysis.
		withToleratedLogMessage("Execution queue buffer full"),
	}
	for j, addr := range nm.addrs {
		opts = append(opts, withAccount(addr, types.Account{Balance: nm.cfg.balance(j)}))
	}
	if nm.cfg.gasTarget != nil {
		opts = append(opts, withGasTarget(*nm.cfg.gasTarget))
	}
	if nm.cfg.priceTarget != nil {
		opts = append(opts, withPriceTarget(*nm.cfg.priceTarget))
	}
	if nm.cfg.minDelayMS != nil {
		opts = append(opts, withMinDelayTarget(*nm.cfg.minDelayMS))
	}
	ctx, sut := newSUT(nm.tb, opts...)
	n.ctx = ctx
	n.sut = sut
}

// tipID is the canonical chain tip (genesis before the first block).
func (nm *networkedMachine) tipID() ids.ID {
	if len(nm.canonical) == 0 {
		return nm.genesisID
	}
	return nm.canonical[len(nm.canonical)-1].id
}

// nonDelayedValidators returns build-eligible nodes in ascending index order.
func (nm *networkedMachine) nonDelayedValidators() []*modelNode {
	var out []*modelNode
	for _, n := range nm.nodes[:nm.cfg.numValidators] {
		if !n.delayed {
			out = append(out, n)
		}
	}
	return out
}

// check is the rapid invariant action, run around every other action: every
// non-delayed node agrees exactly with the shared model (and hence with every
// other converged node). Lagging nodes are checked by Task 4's prefix logic.
func (nm *networkedMachine) check(rt *rapid.T) {
	for _, n := range nm.nodes {
		if n.delayed {
			continue // replaced with checkLagging in Task 4
		}
		nm.checkState(rt, n.ctx, n.sut, n.db)
	}
}
