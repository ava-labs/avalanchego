// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package fuzz runs avalanchego nodes in the test process and fuzzes them. A
// runner runs a random sequence of P-Chain ops that rapid draws, and checks
// invariants that must hold on every node after each op.
package fuzz

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/eventlog"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	pbuilder "github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	psigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
	pwallet "github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
	xwallet "github.com/ava-labs/avalanchego/wallet/chain/x"
	xbuilder "github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	xsigner "github.com/ava-labs/avalanchego/wallet/chain/x/signer"
)

// An outcome is how an op's tx ended. Runs count outcomes per op.
type outcome uint8

const (
	skipped   outcome = iota // the wallet could not build the tx
	refused                  // the node refused the tx at issuance
	dropped                  // a node dropped the tx, and no node committed it
	committed                // a node committed the tx
	sent                     // the node took the tx, and nothing decides it: a seen tx, or a proposed block
	undecided                // no check decided the tx while it was pending
	pending                  // the op's tx is pending, and a check records its outcome (resolve)
)

var outcomeNames = [...]string{
	skipped:   "skipped",
	refused:   "refused",
	dropped:   "dropped",
	committed: "committed",
	sent:      "sent",
	undecided: "undecided",
	pending:   "pending",
}

func (o outcome) String() string {
	return outcomeNames[o]
}

// A runner runs ops against one network and checks invariants after each op.
//
// The runner owns every P-Chain UTXO of its actors. Its model of them is the
// wallet's own UTXO set, so the check compares the wallet's bookkeeping with
// each node's. Nothing else may pay to or spend from the actors.
type runner struct {
	uris    []string             // each node's API, in the order of clients
	clients []*platformvm.Client // one per node
	nodeIDs []ids.NodeID         // in the order of clients
	// sinks holds each node's event sink, in the order of clients. A wait
	// for a node wakes on its lines (eventSink.wait).
	sinks []*eventSink
	// acps holds, by node ID, the ACPs that each node's config supports, for
	// checkHandshakes.
	acps map[ids.NodeID]set.Set[uint32]
	// gossipers holds, for each node, a Byzantine peer connected to it.
	gossipers []*byzantinePeer
	// stakingAddrs holds each node's staking address, where the peers of an
	// online staker connect (connectStaker) over network.
	stakingAddrs []netip.AddrPort
	network      *memNet
	// online holds, by node ID, the peers of each online staker, one on each
	// node, until the node that a check reads stops listing it (applySync).
	online map[ids.NodeID][]*byzantinePeer
	// nodeEvents holds each node's testNode.flushEvents, which a check calls
	// once (flushNodeEvents).
	nodeEvents []func() ([]byte, error)
	progress   progress
	// injector sets the network faults on the links between honest nodes,
	// through the proxy, and faults records them.
	injector *injector
	faults   faults
	// gossip follows the tx gossip that the nodes log, for checkGossip.
	gossip *gossipLog
	// apiTxs holds, by node, the P-Chain txs that the node's API took.
	apiTxs map[ids.NodeID][]ids.ID
	// proposer is the Byzantine validator that propose ops propose blocks
	// from. Its blocks can move the nodes' chain time past the wall clock
	// (issueAPI). blockInputs holds the inputs of the txs in the block that
	// a propose op is drawing, so that its next tx spends others
	// (blockTxs).
	proposer    *proposer
	blockInputs set.Set[ids.ID]
	// paths holds, by block ID, the txs that propose ops sent on the mempool
	// paths and in a block, until a node verifies the block (checkPaths).
	paths map[ids.ID]pathCase

	actors      []ids.ShortID
	utxos       common.UTXOs
	backend     pwallet.Backend
	pContext    *pbuilder.Context
	keys        *secp256k1fx.Keychain // the actors' keys
	signer      psigner.Signer
	avaxAssetID ids.ID
	xChainID    ids.ID
	xWallet     xwallet.Wallet
	xClient     *avm.Client

	minValidatorStake uint64
	minDelegatorStake uint64

	// The registries, one per entity kind, hold keys and lifecycle states. The
	// maps beside them hold what an op needs beyond the key. The block scan
	// writes them (check.go learn), so a tx registers whichever op or path
	// it came from.
	subnets registry[ids.ID]
	// converted holds the subnets that a committed ConvertSubnetToL1Tx made
	// L1s.
	converted set.Set[ids.ID]
	// subnetValidators holds, by subnet, the node IDs that accepted
	// AddSubnetValidatorTxs added and that the nodes still list. Permissioned subnet
	// validators stake no AVAX, so they need no return bookkeeping.
	subnetValidators map[ids.ID][]ids.NodeID
	l1               registry[ids.ID] // by validation ID
	l1Subnet         map[ids.ID]ids.ID
	// l1Accounts holds, by validation ID, the balance that each L1 validator
	// has left at a chain time (checkL1Balances). blockTime is the chain time
	// of the block that the scan applies, and l1Price the validator fee price
	// per second that the last check read.
	l1Accounts map[ids.ID]l1Account
	blockTime  uint64
	l1Price    uint64
	// autoRenew holds auto-renewed validators by the ID of the tx that added
	// them, the handle that SetAutoRenewedValidatorConfigTx uses.
	autoRenew registry[ids.ID]
	// validators holds the primary network validators the fuzzer added.
	// applySync ends one when the node that a check reads stops listing it.
	validators registry[ids.NodeID]
	// primaryValidators holds the current primary network validators,
	// sorted by node ID, as the first node that the last check read listed
	// them (readSync). The generator draws from it, so building a tx needs
	// no network.
	primaryValidators []platformvm.ClientPermissionlessValidator

	// The pools hold every value that the run has seen in an accepted tx or an
	// API reply, by Go type and by field path. The ID rule (drawID) draws from
	// all the IDs of a type; the known and historical tiers of a mutation draw
	// from all the values of a type and from those at one path (mutate.go).
	seenIDs      pool[ids.ID]
	seenNodeIDs  pool[ids.NodeID]
	seenShortIDs pool[ids.ShortID]
	seenInts     pool[uint64]
	seenOwners   pool[ownerValue]
	seenSigs     pool[[secp256k1.SignatureLen]byte]

	// spent maps each input ID that an accepted tx consumed to that tx.
	spent map[ids.ID]ids.ID
	// accepted holds every tx of the accepted blocks, in block order, and
	// acceptedIDs their IDs. issue's seen-tx source draws from accepted.
	accepted    []*platform.Tx
	acceptedIDs set.Set[ids.ID]

	// returns holds, by UTXO ID, the UTXOs that stakers get back when they end.
	returns map[ids.ID]*avax.UTXO
	// rewardIndex maps a staker tx to the first output index its rewards use.
	rewardIndex map[ids.ID]uint32
	// rewards maps each tx ID that an accepted reward tx pays under to the
	// first output index of its rewards.
	rewards map[ids.ID]uint32
	// refunds holds, by UTXO ID, what each committed disable refunds.
	refunds map[ids.ID]uint64
	// stakers follows each staker's end time and when it leaves.
	stakers stakers
	// accruals holds the accrued delegatee reward of each primary network
	// validator, by tx ID, as the last check read it.
	accruals map[ids.ID]accrual

	// payer is the actor that the last op drew to pay. Tests read it.
	payer ids.ShortID
	// pending holds the txs that ops sent and no check decided yet, in the
	// order they went out. fairDrops holds, by tx ID, the fair attempts that
	// a check decided dropped, whose commit the block scan still counts
	// (resolveCommitted).
	pending   []*pendingTx
	fairDrops map[ids.ID]*pendingTx

	// chains returns, for each node, the IDs of the P-Chain blocks that its
	// consensus accepted, in order (testNode.Accepted), and agreement holds
	// them to one chain (checkAgreement).
	chains    []func() []ids.ID
	agreement agreement
	// height is the height up to which the model applied the accepted blocks,
	// which it read from the node at index leader, the one that had accepted
	// the most (scan). queue holds the scanned txs whose postconditions wait
	// for a node at that height (readState).
	height uint64
	leader int
	queue  postconditionQueue
	value  *valueParts // the actors' value at the last check; nil before the first
	// flows is the value that accepted txs, rewards, and refunds moved since
	// value was read.
	flows flows
	// fees gives each tx's fee at the minimum gas price, the lowest that any
	// block charges (checkFee).
	fees fee.Calculator

	// events writes the fuzzer's events.log (events.go). ev is the event of
	// the op or check that is running, and step counts the ops so far.
	events *eventlog.Logger
	ev     *eventlog.Event
	step   int

	// commits counts the fair attempts of each tx type and their commits in
	// this run, and campaign across the runs of one test, if it is not nil
	// (checkCommits).
	commits  commitTally
	campaign *commitTally

	stats map[string]map[outcome]int // by op name
	// failed is the check that failed the run (fail), for its kill record
	// (recordFailure).
	failed string
	// reason is the check that refused the current op's tx, and the path
	// that the tx came in on. runOp counts it in reasons.
	reason  string
	reasons map[string]map[string]int // by op name
}

// makeXWallet builds an X-Chain wallet for addrs, reading their current
// X-Chain UTXOs from uri. kc signs its txs.
func makeXWallet(ctx context.Context, t testingT, uri string, addrs set.Set[ids.ShortID], kc *secp256k1fx.Keychain) xwallet.Wallet {
	state, err := primary.FetchState(ctx, uri, addrs)
	require.NoError(t, err)

	backend := xwallet.NewBackend(state.XCTX, common.NewChainUTXOs(state.XCTX.BlockchainID, state.UTXOs))
	return xwallet.NewWallet(xbuilder.New(addrs, state.XCTX, backend), xsigner.New(kc, backend), state.XClient, backend)
}

// newRunner creates actors and funds them from funder. nodes lists every node
// of the network, and gossipers holds a Byzantine peer connected to each node,
// in the same order.
func newRunner(t testingT, nodes []*testNode, gossipers []*byzantinePeer, funder *secp256k1.PrivateKey, actors int) *runner {
	const (
		// Enough that locked stake rarely leaves an actor unable to pay.
		funds = units.MegaAvax
		// Each actor's funds come in this many UTXOs. A pending tx hides the
		// UTXOs it spends from the builder until a check decides it
		// (pendingInputs), so with one UTXO an actor could pay for one tx
		// at a time. Each holds 50,000 AVAX, more than a stake draws.
		fundUTXOs = 20
		// Setup issues two txs and waits for each.
		setupTimeout = 30 * time.Second
		// The longest valid wait is a few seconds: a proposer slot of 1 s
		// and consensus (progress), plus the
		// slots of the Byzantine validator, which builds only in propose
		// ops. Each slot is its with the chance of its stake share, about an
		// eighth, so 12 of its slots in a row come with a chance of
		// 8^-12, about 1e-11.
		stallWindow = 25 * time.Second
	)

	ctx, done := deadline(t.Context(), t, "runner setup", setupTimeout)
	defer done()

	uris := make([]string, len(nodes))
	sinks := make([]*eventSink, len(nodes))
	for i, n := range nodes {
		uris[i] = n.APIURI()
		sinks[i] = n.events
	}

	keys := make([]*secp256k1.PrivateKey, actors)
	addrs := make([]ids.ShortID, actors)
	for i := range keys {
		key, err := secp256k1.NewPrivateKey()
		require.NoError(t, err)

		keys[i] = key
		addrs[i] = key.Address()
	}

	funderWallet, err := primary.MakePWallet(ctx, uris[0], secp256k1fx.NewKeychain(funder), primary.WalletConfig{})
	require.NoError(t, err)

	avaxAssetID := funderWallet.Builder().Context().AVAXAssetID
	var outs []*avax.TransferableOutput
	for _, addr := range addrs {
		for range fundUTXOs {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: avaxAssetID},
				Out:   &secp256k1fx.TransferOutput{Amt: funds / fundUTXOs, OutputOwners: *owner(addr)},
			})
		}
	}
	avax.SortTransferableOutputs(outs, platform.Codec)
	// The wallet would poll for the tx, so it assumes the tx decided, and
	// setup waits on node 0's lines.
	fundTx, err := funderWallet.IssueBaseTx(outs, common.WithContext(ctx), common.WithAssumeDecided())
	require.NoError(t, err)
	pClient := platformvm.NewClient(uris[0])
	var fundStatus status.Status
	sinks[0].wait(ctx, func() bool {
		res, err := pClient.GetTxStatus(ctx, fundTx.ID())
		require.NoError(t, err)
		fundStatus = res.Status
		return fundStatus != status.Processing
	})
	require.Equal(t, status.Committed, fundStatus)

	client, pContext, utxos, err := primary.FetchPState(ctx, uris[0], set.Of(addrs...))
	require.NoError(t, err)

	minValidatorStake, minDelegatorStake, err := client.GetMinStake(ctx, constants.PrimaryNetworkID)
	require.NoError(t, err)

	feeConfig, err := client.GetFeeConfig(ctx)
	require.NoError(t, err)

	_, l1Price, _, err := client.GetValidatorFeeState(ctx)
	require.NoError(t, err)

	height, err := client.GetHeight(ctx)
	require.NoError(t, err)

	clients := make([]*platformvm.Client, len(uris))
	nodeIDs := make([]ids.NodeID, len(uris))
	for i, uri := range uris {
		clients[i] = platformvm.NewClient(uri)
		nodeIDs[i], _, err = info.NewClient(uri).GetNodeID(ctx)
		require.NoError(t, err)
	}

	// Fund the actors on the X-Chain too, from the funder, so they can export
	// value X->P. The funder is a genesis key with X-Chain funds.
	funderXWallet := makeXWallet(ctx, t, uris[0], set.Of(funder.Address()), secp256k1fx.NewKeychain(funder))
	var xOuts []*avax.TransferableOutput
	for _, addr := range addrs {
		for range fundUTXOs {
			xOuts = append(xOuts, &avax.TransferableOutput{
				Asset: avax.Asset{ID: avaxAssetID},
				Out:   &secp256k1fx.TransferOutput{Amt: funds / fundUTXOs, OutputOwners: *owner(addr)},
			})
		}
	}
	xFundTx, err := funderXWallet.IssueBaseTx(xOuts, common.WithContext(ctx), common.WithAssumeDecided())
	require.NoError(t, err)
	xClient := avm.NewClient(uris[0], "X")
	// The X-Chain reports a tx it has not accepted as unknown, not processing.
	var xFundStatus choices.Status
	sinks[0].wait(ctx, func() bool {
		xFundStatus, err = xClient.GetTxStatus(ctx, xFundTx.ID())
		require.NoError(t, err)
		return xFundStatus == choices.Accepted
	})
	require.Equal(t, choices.Accepted, xFundStatus)

	// The actors' X-Chain wallet, built after funding so it sees the new UTXOs.
	xWallet := makeXWallet(ctx, t, uris[0], set.Of(addrs...), secp256k1fx.NewKeychain(keys...))

	// The X-Chain is the export target. Its blockchain ID comes from genesis,
	// so the runner reads it rather than assuming a value.
	chains, err := clients[0].GetBlockchains(ctx)
	require.NoError(t, err)
	var xChainID ids.ID
	for _, b := range chains {
		if b.VMID == constants.AVMID {
			xChainID = b.ID
		}
	}
	require.NotEqual(t, ids.Empty, xChainID, "no X-Chain in GetBlockchains")

	backend := pwallet.NewBackend(common.NewChainUTXOs(constants.PlatformChainID, utxos), map[ids.ID]fx.Owner{})
	kc := secp256k1fx.NewKeychain(keys...)
	r := &runner{
		uris:              uris,
		clients:           clients,
		nodeIDs:           nodeIDs,
		sinks:             sinks,
		gossipers:         gossipers,
		actors:            addrs,
		utxos:             utxos,
		backend:           backend,
		pContext:          pContext,
		fees:              fee.NewDynamicCalculator(pContext.ComplexityWeights, feeConfig.MinPrice),
		keys:              kc,
		signer:            psigner.New(kc, backend),
		avaxAssetID:       avaxAssetID,
		xChainID:          xChainID,
		xWallet:           xWallet,
		xClient:           xClient,
		minValidatorStake: minValidatorStake,
		minDelegatorStake: minDelegatorStake,
		spent:             make(map[ids.ID]ids.ID),
		returns:           make(map[ids.ID]*avax.UTXO),
		rewardIndex:       make(map[ids.ID]uint32),
		rewards:           make(map[ids.ID]uint32),
		refunds:           make(map[ids.ID]uint64),
		subnetValidators:  make(map[ids.ID][]ids.NodeID),
		l1Subnet:          make(map[ids.ID]ids.ID),
		l1Accounts:        make(map[ids.ID]l1Account),
		l1Price:           uint64(l1Price),
		gossip:            newGossipLog(set.Of(nodeIDs...)),
		apiTxs:            make(map[ids.NodeID][]ids.ID),
		paths:             make(map[ids.ID]pathCase),
		height:            height,
		stats:             make(map[string]map[outcome]int),
		reasons:           make(map[string]map[string]int),
	}
	r.progress = progress{window: stallWindow, faults: &r.faults}
	r.see(chains)

	return r
}

// runOp runs op as the next step. It writes the op's event as an issue op,
// and counts op's outcome under the name op returns, unless op's tx is
// pending: a check counts it once it decides the tx (resolve). op's API calls
// take ctx, which ends after a bound, so a node that stops answering fails
// the op.
func (r *runner) runOp(t *rapid.T, op func(context.Context, *rapid.T) (string, outcome)) {
	// An op sends its tx and does not wait for a decision.
	const opTimeout = time.Minute

	r.step++
	_, r.ev = r.events.Start(t.Context(), "issue")
	r.ev.Set("step", r.step)
	defer r.endEvent()

	ctx, done := deadline(t.Context(), t, fmt.Sprintf("op %d", r.step), opTimeout)
	defer done()

	stat, result := op(ctx, t)
	r.finishOp(t, stat, result)
}

// deadline returns a context for stage that ends after d. Call done when the
// stage ends. If the stage ran out of time, done fails t with a message that
// names the stage: the API call that failed names only itself.
func deadline(parent context.Context, t require.TestingT, stage string, d time.Duration) (ctx context.Context, done func()) {
	ctx, cancel := context.WithTimeout(parent, d)
	return ctx, func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Errorf("%s did not finish in %s", stage, d)
		}
		cancel()
	}
}

// finishOp adds to the running op's event and log that op ended in result,
// and counts it unless the op's tx is pending. op names the op, such as
// "issue CreateSubnetTx", and a mutation if the op made one.
func (r *runner) finishOp(t *rapid.T, op string, result outcome) {
	t.Logf("ran op %s: %s", op, result)
	r.ev.Set("outcome", result.String())
	if result != pending {
		r.record(op, result, r.reason)
	}
	r.reason = ""
}

// record counts that op ended in result, and reason, the check that decided
// it, unless that is "".
func (r *runner) record(op string, result outcome, reason string) {
	if r.stats[op] == nil {
		r.stats[op] = make(map[outcome]int)
	}
	r.stats[op][result]++
	if reason == "" {
		return
	}

	if r.reasons[op] == nil {
		r.reasons[op] = make(map[string]int)
	}
	r.reasons[op][reason]++
}

// tally counts a fair attempt of txType that ended in result, decided by
// reason, toward checkCommits. A refusal or a drop does not count if a fault
// cut an honest node off while the tx was pending, from from to to
// (faults.cutOff): a node behind the others is not the network's verdict. A
// commit always counts: a fault can keep a tx from committing, but not make
// one commit, and the check fails only a type with no commit. It reports
// whether it counted the attempt.
func (r *runner) tally(txType string, result outcome, reason string, from, to time.Time) bool {
	if result != committed && r.faults.cutOff(from, to) {
		return false
	}

	r.commits.add(txType, result, reason)
	if r.campaign != nil {
		r.campaign.add(txType, result, reason)
	}
	return true
}

// A source is where issue gets its tx.
type source uint8

const (
	newTx   source = iota // a new tx of a drawn type, built from the model
	seenTx                // a tx from an accepted block, sent again
	xExport               // an export from the X-Chain to the P-Chain
)

func (s source) String() string {
	return [...]string{
		newTx:   "new",
		seenTx:  "seen",
		xExport: "x-chain export",
	}[s]
}

// drawSource draws where issue gets its tx. New txs are 8 draws in 10, so
// they stay the large majority. An X-Chain export is the only source of the
// atomic UTXOs that an ImportTx spends.
func drawSource(t *rapid.T) source {
	switch i := uniform(10).Draw(t, "source"); {
	case i < 8:
		return newTx
	case i == 8:
		return seenTx
	default:
		return xExport
	}
}

// uniform returns a generator of the ints from 0 to n-1, each equally likely.
// rapid's own integers lean toward small values, so an index drawn with them
// would favor the first entries of a list. rapid draws a bool from one random
// bit, so the ints come from bools. It draws at least one bit even for n = 1,
// because rapid refuses a generator that draws nothing.
func uniform(n int) *rapid.Generator[int] {
	bitLen := max(1, bits.Len(uint(n-1)))
	return rapid.Custom(func(t *rapid.T) int {
		for {
			var i int
			for b := range bitLen {
				if rapid.Bool().Draw(t, "bit") {
					i |= 1 << b
				}
			}
			if i < n {
				return i
			}
		}
	})
}

// issue draws a source, and issues a tx from it. A seen tx needs an accepted
// tx, so before the first one the op issues a new tx instead. A new tx may
// have one field mutated (Stateful Fuzzing.md 2.2).
func (r *runner) issue(t *rapid.T) {
	switch drawSource(t) {
	case xExport:
		r.exportOp(t)
		return
	case seenTx:
		if tx, ok := r.drawSeenTx(t); ok {
			r.resendOp(t, tx)
			return
		}
	}

	// An index, not the prototype, so that rapid's log stays short.
	proto := txTypes[uniform(len(txTypes)).Draw(t, "txType")]
	if drawMutated(t) {
		r.mutateOp(t, proto)
		return
	}

	r.issueOp(t, proto)
}

// issueOp runs an issue op as the next step, with a new tx of proto's type.
// It counts the outcome under the type's name.
func (r *runner) issueOp(t *rapid.T, proto platform.UnsignedTx) {
	r.runOp(t, func(ctx context.Context, t *rapid.T) (string, outcome) {
		r.ev.Set("source", newTx.String())
		r.ev.Set("tx_type", txName(proto))
		return "issue " + txName(proto), r.issueType(ctx, t, proto)
	})
}

// drawSeenTx draws a tx from the accepted blocks that the checks scanned. It
// returns false if they held none.
func (r *runner) drawSeenTx(t *rapid.T) (*platform.Tx, bool) {
	if len(r.accepted) == 0 {
		return nil, false
	}

	// An index, not the tx, so that rapid's log stays short.
	return r.accepted[uniform(len(r.accepted)).Draw(t, "seenTx")], true
}

// resendOp runs an issue op as the next step that sends tx again, on the node
// and path that t draws. It does not wait for a decision: tx already
// committed, so GetTxStatus reports Committed whatever the node does with the
// copy. It predicts no verdict. The spent-once invariant judges a second
// inclusion.
func (r *runner) resendOp(t *rapid.T, tx *platform.Tx) {
	r.runOp(t, func(ctx context.Context, t *rapid.T) (string, outcome) {
		stat := "issue seen " + txName(tx.Unsigned)
		r.ev.Set("source", seenTx.String())
		r.ev.Set("tx_type", txName(tx.Unsigned))
		_, path, err := r.send(ctx, t, tx)
		if err != nil {
			t.Logf("node refused seen tx %s: %v", tx.ID(), err)
			r.decided(path, err.Error())
			return stat, refused
		}

		return stat, sent
	})
}

// exportOp runs an issue op as the next step that exports AVAX from the
// X-Chain to the P-Chain (exportToP).
func (r *runner) exportOp(t *rapid.T) {
	r.runOp(t, func(ctx context.Context, t *rapid.T) (string, outcome) {
		r.ev.Set("source", xExport.String())
		r.ev.Set("tx_type", "ExportTx")
		return "issue x-chain ExportTx", r.exportToP(ctx, t)
	})
}

// issueType builds a new tx of proto's type, signs it, and issues it
// (issueSigned). Its attempt is fair if each reference is an active entity of
// its kind (checkCommits).
func (r *runner) issueType(ctx context.Context, t *rapid.T, proto platform.UnsignedTx) outcome {
	g := &generator{r: r, t: t}
	if err := proto.Visit(g); err != nil {
		return r.skip(t, err)
	}

	var tally string
	if !g.offKind {
		tally = txName(proto)
	}

	return r.issueSigned(ctx, t, r.sign(t, g.tx), "issue "+txName(proto), tally)
}

// issueSigned sends tx (send). If a node took it, tx is pending under op
// until a check decides it, and issueSigned returns at once; otherwise it is
// refused. A fair attempt of the tx type tally, unless that is "", counts
// toward checkCommits.
func (r *runner) issueSigned(ctx context.Context, t *rapid.T, tx *platform.Tx, op, tally string) outcome {
	inputs := tx.InputIDs()
	conflict := inputs.Overlaps(r.pendingInputs())
	i, path, err := r.send(ctx, t, tx)
	if conflict {
		// A tx that spends an input of a pending tx, as a mutation can make
		// it (doubleSpend), goes to a second node too, so two nodes can hold
		// conflicting txs and race them into blocks. The spent-once invariant
		// judges what they accept (Stateful Fuzzing.md 2.1).
		j := (i + 1 + uniform(len(r.clients)-1).Draw(t, "secondNode")) % len(r.clients)
		second := rapid.SampledFrom([]string{pathAPI, pathGossip}).Draw(t, "secondPath")
		r.ev.Set("also_to", r.nodeDraw(j))
		r.ev.Set("also_path", second)
		switch errSecond := r.sendTo(ctx, t, j, second, tx); {
		case errSecond != nil:
			r.ev.Set("also_refused", errSecond.Error())
		case err != nil:
			r.ev.Set("refused_first", err.Error())
			path, err = second, nil
		}
	}
	now := time.Now()
	if err != nil {
		t.Logf("node refused tx %s: %v", tx.ID(), err)
		reason := r.decided(path, err.Error())
		if tally != "" {
			r.ev.Set("fair", r.tally(tally, refused, reason, now, now))
		}
		return refused
	}

	r.track(&pendingTx{tx: tx, op: op, step: r.step, path: path, tally: tally, sent: now})
	return pending
}

// exportToP exports AVAX from the X-Chain to the P-Chain, owned by an actor,
// and keeps the export pending until node 0's X-Chain accepts it (decide).
// The value leaves the X-Chain for shared memory, where a later import can
// bring it onto the P-Chain. The op writes no model: the check reads the
// atomic UTXO from shared memory (readX), and the value oracle counts it as
// in flight until an accepted ImportTx spends it.
//
// The X-Chain wallet holds every actor's UTXOs, so the op narrows the spend
// addresses to the payer and sends the change to the payer. Unlike on the
// P-Chain, that is safe: an X-Chain export authorizes no owner. The wallet
// takes the export as accepted once node 0 took it, so the next export can
// spend its change.
func (r *runner) exportToP(ctx context.Context, t *rapid.T) outcome {
	const op = "issue x-chain ExportTx"

	payer := r.drawPayer(t, r.actors)
	to := owner(r.drawRecipient(t))
	value := rapid.Uint64Range(1, units.KiloAvax).Draw(t, "amount")
	out := &avax.TransferableOutput{
		Asset: avax.Asset{ID: r.avaxAssetID},
		Out:   &secp256k1fx.TransferOutput{Amt: value, OutputOwners: *to},
	}
	tx, err := r.xWallet.IssueExportTx(
		constants.PlatformChainID,
		[]*avax.TransferableOutput{out},
		common.WithContext(ctx),
		common.WithCustomAddresses(set.Of(payer)),
		common.WithChangeOwner(owner(payer)),
		common.WithAssumeDecided(),
	)
	if err != nil {
		return r.skip(t, err)
	}

	// The X-Chain wallet issues through node 0's API.
	r.ev.Set("tx", tx.ID())
	r.ev.Set("to", r.nodeDraw(0))
	r.ev.Set("path", pathAPI)
	r.track(&pendingTx{id: tx.ID(), op: op, step: r.step, path: pathAPI, sent: time.Now()})

	return pending
}

// The paths that a tx can come in on.
const (
	pathAPI    = "api"
	pathGossip = "gossip"
)

// send sends tx to a node that t draws, on a path that t draws (sendTo). It
// returns the node, the path, and the API's error.
func (r *runner) send(ctx context.Context, t *rapid.T, tx *platform.Tx) (int, string, error) {
	i := rapid.IntRange(0, len(r.clients)-1).Draw(t, "node")
	path := rapid.SampledFrom([]string{pathAPI, pathGossip}).Draw(t, "path")
	r.ev.Set("tx", tx.ID())
	r.ev.Set("to", r.nodeDraw(i))
	r.ev.Set("path", path)

	return i, path, r.sendTo(ctx, t, i, path, tx)
}

// sendTo sends tx to node i on path: the node's API, or gossip from the
// Byzantine peer connected to the node. It returns the API's error. Gossip
// has no reply, so its error is always nil. It keeps each tx that the API
// takes for checkTxPush, unless a fault can have lost its push
// (faults.txPushExcused).
func (r *runner) sendTo(ctx context.Context, t *rapid.T, i int, path string, tx *platform.Tx) error {
	if path == pathAPI {
		err := r.issueAPI(ctx, t, i, tx)
		if err == nil && !r.faults.txPushExcused(time.Now()) {
			r.apiTxs[r.nodeIDs[i]] = append(r.apiTxs[r.nodeIDs[i]], tx.ID())
		}

		return err
	}

	require.NoError(t, r.gossipers[i].gossipTx(ctx, tx))
	return nil
}

// issueAPI issues tx through node i's API, and returns the API's error. It
// fails t if the node refuses a staker as too short that lasts long enough
// from after the refusal (checkStakeRefusal). A block that the proposer sent
// may have moved the nodes' chain time past the wall clock, so the refusal
// counts from the later of the two. For an ImportTx from the
// X-Chain, it also reads the node's atomic UTXOs before the issue and after a
// refusal, and fails t if the refusal says an input is missing that both reads
// list (checkImportRefusal).
func (r *runner) issueAPI(ctx context.Context, t *rapid.T, i int, tx *platform.Tx) error {
	utx, ok := tx.Unsigned.(*platform.ImportTx)
	if !ok || utx.SourceChain != r.xChainID {
		_, refusal := r.clients[i].IssueTx(ctx, tx.Bytes())
		if refusal == nil {
			return nil
		}

		// The latest chain time a node can hold: now, or the time of a block
		// that the proposer sent, if that is later (proposer.addSent).
		after := time.Now()
		if r.proposer.chainTime.After(after) {
			after = r.proposer.chainTime
		}
		if err := checkStakeRefusal(tx.Unsigned, refusal, after); err != nil {
			r.fail(t, "stake refusal", fmt.Errorf("node %d, tx %s: %w", i, tx.ID(), err))
		}

		return refusal
	}

	before := r.atomicUTXOIDs(ctx, t, i)
	_, refusal := r.clients[i].IssueTx(ctx, tx.Bytes())
	if refusal == nil {
		return nil
	}

	after := r.atomicUTXOIDs(ctx, t, i)
	if err := checkImportRefusal(utx, refusal, before, after, r.spent); err != nil {
		r.fail(t, "import refusal", fmt.Errorf("node %d, tx %s: %w", i, tx.ID(), err))
	}

	return refusal
}

// atomicUTXOIDs returns the input IDs of the actors' atomic UTXOs from the
// X-Chain that node i lists.
func (r *runner) atomicUTXOIDs(ctx context.Context, t *rapid.T, i int) set.Set[ids.ID] {
	utxos := common.NewUTXOs()
	require.NoError(t, primary.AddAllUTXOs(ctx, utxos, r.clients[i], platform.Codec, r.xChainID, constants.PlatformChainID, r.actors))
	listed, err := utxos.UTXOs(ctx, r.xChainID, constants.PlatformChainID)
	require.NoError(t, err)

	var inputIDs set.Set[ids.ID]
	for _, u := range listed {
		inputIDs.Add(u.InputID())
	}

	return inputIDs
}

// errImportRefusal means a node refused an import because shared memory
// lacks an input, while its API listed every input before and after.
var errImportRefusal = errors.New("node refused an import for a missing input that it lists")

// checkImportRefusal returns errImportRefusal if refusal, a node's API error
// for utx, says shared memory lacks an input of utx, while before and after,
// the node's atomic UTXOs listed before the issue and after the refusal, both
// hold every input, and spent, the inputs of accepted txs, holds none.
//
// A UTXO can be missing at the refusal and listed in both reads only if it
// left shared memory and came back. It cannot: an export puts it once, an
// accepted import removes it for good, and getUTXOs reads the same entries
// and index that the import's Get reads (chains/atomic/state.go SetValue,
// RemoveValue). Only a fresh verification counts: a node that dropped the tx
// before returns the reason it cached then, without the "failed
// verification" that a new check adds (vms/platformvm/network/gossip.go
// gossipMempool.Add).
func checkImportRefusal(utx *platform.ImportTx, refusal error, before, after set.Set[ids.ID], spent map[ids.ID]ids.ID) error {
	const (
		fresh   = "couldn't issue tx: failed verification: "
		missing = "failed to get shared memory: not found"
	)
	if msg := refusal.Error(); !strings.Contains(msg, fresh) || !strings.Contains(msg, missing) {
		return nil
	}

	for _, in := range utx.ImportedInputs {
		id := in.InputID()
		if _, ok := spent[id]; ok || !before.Contains(id) || !after.Contains(id) {
			return nil
		}
	}

	return fmt.Errorf("%w: %v", errImportRefusal, refusal)
}

// errStakeRefusal means a node refused a staker as too short, although it
// lasts every minimum stake duration from after the refusal.
var errStakeRefusal = errors.New("node refused a staker as too short that lasts long enough")

// checkStakeRefusal returns errStakeRefusal if refusal, a node's API error for
// utx, says that the staking period is too short, while utx ends at least the
// longer of the minimum stake durations after after, a time read once the
// refusal came back.
//
// Since Durango, a node measures the period from its chain time
// (txs/executor/staker_tx_verification.go). For the mempool, that is the wall
// clock, the parent block's timestamp, or the next staker change, whichever
// the chain time rules pick (state/chain_time_helpers.go NextBlockTime). The
// nodes run on this process's clock and build every block, so none of these
// is later than after. The chain time has nanoseconds: a staker that ends
// exactly the minimum after the current whole second is short from any later
// instant, so after is not rounded down. The longer minimum holds for every
// staker type, so the check needs no rule for which one applies.
func checkStakeRefusal(utx platform.UnsignedTx, refusal error, after time.Time) error {
	const tooShort = "staking period is too short"

	s, ok := utx.(platform.BoundedStaker)
	if !ok || !strings.Contains(refusal.Error(), tooShort) {
		return nil
	}

	longest := max(minStakeDuration, heliconMinStakeDuration)
	if s.EndTime().Sub(after) < longest {
		return nil
	}

	return fmt.Errorf("%w: it ends at %d, which is at least %s after the refusal at %s: %v", errStakeRefusal, s.EndTime().Unix(), longest, after.Format(time.RFC3339Nano), refusal)
}

// nodeDraw returns node i as a draw.
func (r *runner) nodeDraw(i int) draw {
	return draw{Ref: fmt.Sprintf("node %d", i), ID: r.nodeIDs[i].String()}
}

// idPattern and numberPattern match the IDs and amounts in a reason. They are
// package-level so they compile once, not once per refused tx.
var (
	idPattern     = regexp.MustCompile(`[1-9A-HJ-NP-Za-km-z]{30,}`)
	numberPattern = regexp.MustCompile(`[0-9]+`)
)

// decided records on the current op's event the check that refused its tx,
// and keeps it for the counts, which cut IDs and amounts from it (countable).
// It returns the reason as the counts keep it.
func (r *runner) decided(path, reason string) string {
	r.ev.Set("decided_by", path+": "+reason)
	r.reason = path + ": " + countable(reason)
	return r.reason
}

// countable cuts the IDs and amounts from reason, so that one check counts as
// one reason.
func countable(reason string) string {
	return numberPattern.ReplaceAllString(idPattern.ReplaceAllString(reason, "<id>"), "<n>")
}

// sign signs utx as the actors (signTx), and fails t if it cannot.
func (r *runner) sign(t *rapid.T, utx platform.UnsignedTx) *platform.Tx {
	tx, err := r.signTx(t.Context(), utx)
	require.NoError(t, err)

	return tx
}

// signTx signs utx as the actors. The wallet signs no proposer-only tx, and
// such a tx carries no credentials, so it goes out with none.
func (r *runner) signTx(ctx context.Context, utx platform.UnsignedTx) (*platform.Tx, error) {
	tx, err := psigner.SignUnsigned(ctx, r.signer, utx)
	if errors.Is(err, psigner.ErrUnsupportedTxType) {
		return platform.NewSignedTx(utx, platform.Codec, nil)
	}

	return tx, err
}

// skip records that the wallet refused to build the current op's tx. A drawn
// ID of the wrong kind often stops there, and that is the deciding check.
func (r *runner) skip(t *rapid.T, err error) outcome {
	t.Logf("wallet could not build tx: %v", err)
	r.decided("wallet", err.Error())
	return skipped
}

// drawRecipient draws the actor that an op pays to.
func (r *runner) drawRecipient(t *rapid.T) ids.ShortID {
	to := rapid.SampledFrom(r.actors).Draw(t, "to")
	r.logActor("recipient", to)
	return to
}

// drawPayer draws the actor that pays for the current op from candidates, and
// records it in r.payer.
func (r *runner) drawPayer(t *rapid.T, candidates []ids.ShortID) ids.ShortID {
	r.payer = rapid.SampledFrom(candidates).Draw(t, "payer")
	r.logActor("payer", r.payer)
	return r.payer
}

// logActor adds to the running op's draws that it drew addr as label.
func (r *runner) logActor(label string, addr ids.ShortID) {
	r.ev.Append("draws", draw{
		Label: label,
		Ref:   fmt.Sprintf("actor %d", slices.Index(r.actors, addr)),
		ID:    addr.String(),
	})
}

// payment returns one output that pays 1 nAVAX to 1000 AVAX to to.
func (r *runner) payment(t *rapid.T, to ids.ShortID) []*avax.TransferableOutput {
	return []*avax.TransferableOutput{
		{
			Asset: avax.Asset{ID: r.avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:          rapid.Uint64Range(1, units.KiloAvax).Draw(t, "amount"),
				OutputOwners: *owner(to),
			},
		},
	}
}

// builder returns a builder that spends only the UTXOs that payer alone can
// spend and that no pending tx (pendingInputs) and no tx of the block being
// drawn (blockInputs) spends, and that can authorize as any actor.
func (r *runner) builder(payer ids.ShortID) pbuilder.Builder {
	spent := r.pendingInputs()
	spent.Union(r.blockInputs)
	return pbuilder.New(set.Of(r.actors...), r.pContext, payerUTXOs{Backend: r.backend, payer: payer, spent: spent})
}

// payerUTXOs hides every UTXO that payer cannot spend alone, and those whose
// input IDs are in spent. Narrowing the builder's addresses instead
// (common.WithCustomAddresses) would also narrow who can authorize, because
// the builder matches subnet and validator owners against the same addresses.
type payerUTXOs struct {
	pbuilder.Backend
	payer ids.ShortID
	spent set.Set[ids.ID]
}

func (p payerUTXOs) UTXOs(ctx context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error) {
	utxos, err := p.Backend.UTXOs(ctx, sourceChainID)
	if err != nil {
		return nil, err
	}

	var own []*avax.UTXO
	for _, utxo := range utxos {
		if p.spent.Contains(utxo.InputID()) {
			continue
		}
		out := utxo.Out
		if locked, ok := out.(*stakeable.LockOut); ok {
			out = locked.TransferableOut
		}
		transfer, ok := out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}
		// The builder checks locktimes itself.
		if _, ok := common.MatchOwners(&transfer.OutputOwners, set.Of(p.payer), math.MaxUint64); ok {
			own = append(own, utxo)
		}
	}

	return own, nil
}

// options returns options that send change to payer.
func options(t *rapid.T, payer ids.ShortID) []common.Option {
	return []common.Option{
		common.WithContext(t.Context()),
		common.WithChangeOwner(owner(payer)),
	}
}

func owner(addr ids.ShortID) *secp256k1fx.OutputOwners {
	return &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{addr}}
}
