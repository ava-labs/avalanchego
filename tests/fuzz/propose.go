// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/proposervm/acp181"

	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	pvmblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
	pvmproposer "github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

// errNoSlot means the proposer is not the expected proposer of any slot
// within the wait that the caller allows.
var errNoSlot = errors.New("no proposer slot within the wait")

// A proposer is a Byzantine genesis validator that proposes P-Chain blocks in
// its own proposer slots (Stateful Fuzzing.md 4.3). It has a peer on each
// honest node. Its peers answer every consensus query with a vote for the
// queried block, so it votes for each node's own preference and equivocates
// whenever the nodes differ. The alpha of 5 keeps a poll from deciding for a
// node or a group of nodes with too few honest votes (setTestDefaults).
type proposer struct {
	key   validatorKey
	peers []*byzantinePeer // one on each honest node, in node order

	// tips holds the tips that earlier propose ops read, and sent the blocks
	// that the proposer sent. A block mutation draws a parent from them.
	tips []parentBlock
	sent []parentBlock
	// chainTime is the latest time of a sent block that a node can have
	// taken (addSent).
	chainTime time.Time
}

// connectProposer connects a peer with key's staking keys, over network, to
// each honest node at addrs, in order. Call close to disconnect them.
func connectProposer(ctx context.Context, network *memNet, addrs []netip.AddrPort, key validatorKey) (*proposer, error) {
	p := &proposer{key: key}
	for _, addr := range addrs {
		b, err := connectValidator(ctx, network, addr, key, true)
		if err != nil {
			return nil, errors.Join(err, p.close(ctx))
		}
		p.peers = append(p.peers, b)
	}

	return p, nil
}

// close disconnects the proposer's peers.
func (p *proposer) close(ctx context.Context) error {
	return closePeers(ctx, p.peers)
}

// A parentBlock is a proposervm block that a proposed block builds on, with
// what the child's header takes from it.
type parentBlock struct {
	id    ids.ID
	inner platform.Block // the P-Chain block it wraps
	// time is the proposervm time that the child's slots count from, and
	// pChainHeight and epoch are the P-Chain height and epoch that the child
	// starts from.
	time         time.Time
	pChainHeight uint64
	epoch        pvmblock.Epoch
}

// tip returns the block that honest node i prefers on the P-Chain.
func (p *proposer) tip(ctx context.Context, i int) (parentBlock, error) {
	blkID, err := p.peers[i].preferred(ctx, constants.PlatformChainID)
	if err != nil {
		return parentBlock{}, err
	}

	return p.parent(ctx, i, blkID)
}

// parent fetches blkID from honest node i. An option block takes its time,
// P-Chain height, and epoch from the proposal block before it, as the
// proposervm does (vms/proposervm/post_fork_option.go).
func (p *proposer) parent(ctx context.Context, i int, blkID ids.ID) (parentBlock, error) {
	b, err := p.peers[i].getBlock(ctx, constants.PlatformChainID, blkID)
	if err != nil {
		return parentBlock{}, err
	}
	blk, err := pvmblock.Parse(b, constants.PlatformChainID)
	if err != nil {
		return parentBlock{}, err
	}
	inner, err := platform.ParseBlock(platform.Codec, blk.Block())
	if err != nil {
		return parentBlock{}, err
	}

	signed, ok := blk.(pvmblock.SignedBlock)
	if !ok {
		proposal, err := p.parent(ctx, i, blk.ParentID())
		if err != nil {
			return parentBlock{}, err
		}
		proposal.id, proposal.inner = blk.ID(), inner

		return proposal, nil
	}

	return parentBlock{
		id:           signed.ID(),
		inner:        inner,
		time:         signed.Timestamp(),
		pChainHeight: signed.PChainHeight(),
		epoch:        signed.PChainEpoch(),
	}, nil
}

// propose waits for the next slot, at most maxWait away, in which the
// proposer is the expected proposer of inner on parent (slot), and sends
// every honest node the proposervm block that wraps inner, signed for that
// slot. c reads the validator set that the slots are drawn from.
func (p *proposer) propose(
	ctx context.Context,
	c *platformvm.Client,
	parent parentBlock,
	inner platform.Block,
	maxWait time.Duration,
) (pvmblock.SignedBlock, error) {
	at, err := p.slot(ctx, c, parent, inner, maxWait)
	if err != nil {
		return nil, err
	}
	if err := sleepUntil(ctx, at); err != nil {
		return nil, err
	}

	return p.send(ctx, parent, at, inner)
}

// slot returns the start of the first slot, from the one that holds now to
// the last that starts within maxWait, in which the proposer is the expected
// proposer of inner on parent (ownSlot). c reads the validator set that the
// slots are drawn from.
//
// The child keeps the parent's P-Chain height, which the proposervm allows
// (vms/proposervm/block.go Verify), and the windower draws its proposer by
// inner's height, as the proposervm does, so a mutated height moves the slot.
func (p *proposer) slot(
	ctx context.Context,
	c *platformvm.Client,
	parent parentBlock,
	inner platform.Block,
	maxWait time.Duration,
) (time.Time, error) {
	window, err := pvmproposer.WindowDurationOf(testUpgrades())
	if err != nil {
		return time.Time{}, err
	}
	vdrs, err := c.GetValidatorsAt(ctx, constants.PrimaryNetworkID, platformapi.Height(parent.pChainHeight))
	if err != nil {
		return time.Time{}, err
	}
	windower := pvmproposer.New(validatorsAt(vdrs), constants.PrimaryNetworkID, constants.PlatformChainID, window, logging.NoLog{})

	return p.ownSlot(parent.time, time.Now(), maxWait, func(slot uint64) (ids.NodeID, error) {
		return windower.ExpectedProposer(ctx, inner.Height(), parent.pChainHeight, slot)
	})
}

// ownSlot returns the start of the first slot after parentTime, from the one
// that holds now to the last that starts within maxWait of now, whose
// expected proposer is the proposer. The slot that holds now started before
// now, and the proposervm takes a block timed at its start: it checks the
// proposer of the block's slot, not how late the block comes
// (vms/proposervm/block.go verifyPostDurangoBlockDelay). It returns errNoSlot
// if there is no such slot.
func (p *proposer) ownSlot(parentTime, now time.Time, maxWait time.Duration, expected func(slot uint64) (ids.NodeID, error)) (
	time.Time,
	error,
) {
	window, err := pvmproposer.WindowDurationOf(testUpgrades())
	if err != nil {
		return time.Time{}, err
	}

	last := now.Add(maxWait)
	for slot := window.TimeToSlot(parentTime, now); ; slot++ {
		at := parentTime.Add(time.Duration(slot) * time.Duration(window))
		if at.After(last) {
			return time.Time{}, fmt.Errorf("%w of %s", errNoSlot, maxWait)
		}
		proposerID, err := expected(slot)
		if err != nil {
			return time.Time{}, err
		}
		if proposerID == p.key.nodeID {
			return at, nil
		}
	}
}

// sleepUntil returns at at, or with ctx's error if ctx ends first.
func sleepUntil(ctx context.Context, at time.Time) error {
	select {
	case <-time.After(time.Until(at)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// send wraps inner in a proposervm block on parent at time at, signs it, and
// sends it to every honest node as a push query.
func (p *proposer) send(ctx context.Context, parent parentBlock, at time.Time, inner platform.Block) (pvmblock.SignedBlock, error) {
	cert, err := staking.ParseCertificate(p.key.cert.Leaf.Raw)
	if err != nil {
		return nil, err
	}

	epoch := acp181.NewEpoch(testUpgrades(), parent.pChainHeight, parent.epoch, parent.time, at)
	blk, err := pvmblock.Build(
		parent.id,
		at,
		parent.pChainHeight,
		epoch,
		cert,
		inner.Bytes(),
		constants.PlatformChainID,
		p.key.cert.PrivateKey.(crypto.Signer),
	)
	if err != nil {
		return nil, err
	}

	for _, b := range p.peers {
		if err := b.proposeBlock(ctx, constants.PlatformChainID, blk.Bytes()); err != nil {
			return nil, err
		}
	}

	return blk, nil
}

// addSent records blk, which the proposer sent at at. A node takes a block's
// time only if it is at most the sync bound after the node's clock when it
// verifies the block (txs/executor/state_changes.go VerifyNewChainTime). A
// node verifies blk when the push query brings it: the proposer serves no
// block, so a node can get it later only from a node that took it. So blk
// raises p.chainTime only if its time is at most the sync bound after
// at. A mutated block can be days ahead; counting it once the clock nears it
// would excuse refusals that it never caused.
func (p *proposer) addSent(blk parentBlock, at time.Time) {
	p.sent = append(p.sent, blk)
	timed, ok := blk.inner.(platform.BanffBlock)
	if !ok {
		return
	}

	if t := timed.Timestamp(); t.After(p.chainTime) && !t.After(at.Add(txexecutor.SyncBound)) {
		p.chainTime = t
	}
}

// validatorsAt returns a validator state that holds vdrs at every height, for
// a windower that draws proposers from one height.
func validatorsAt(vdrs map[ids.NodeID]*validators.GetValidatorOutput) validators.State {
	return &validatorstest.State{
		GetSubnetIDF:      func(context.Context, ids.ID) (ids.ID, error) { return constants.PrimaryNetworkID, nil },
		GetMinimumHeightF: func(context.Context) (uint64, error) { return 0, nil },
		GetCurrentHeightF: func(context.Context) (uint64, error) { return 0, nil },
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return vdrs, nil
		},
	}
}

// innerBlock returns the P-Chain block that holds txs on parent, at the
// parent's time or now, whichever is later, as an honest builder times it.
// If txs hold a proposer-only tx, the first one leads a proposal block, and
// the others are its decision txs; otherwise the block is a standard block.
func innerBlock(parent parentBlock, txs []*platform.Tx, now time.Time) (platform.Block, error) {
	timed, ok := parent.inner.(platform.BanffBlock)
	if !ok {
		return nil, fmt.Errorf("parent %s has no timestamp", parent.inner.ID())
	}
	at := time.Unix(max(timed.Timestamp().Unix(), now.Unix()), 0)
	parentID, height := parent.inner.ID(), parent.inner.Height()+1

	i := slices.IndexFunc(txs, func(tx *platform.Tx) bool {
		switch tx.Unsigned.(type) {
		case *platform.AdvanceTimeTx, *platform.RewardValidatorTx, *platform.RewardAutoRenewedValidatorTx:
			return true
		default:
			return false
		}
	})
	if i < 0 {
		return platform.NewBanffStandardBlock(at, parentID, height, txs)
	}

	decisions := slices.Delete(slices.Clone(txs), i, i+1)
	return platform.NewBanffProposalBlock(at, parentID, height, txs[i], decisions)
}

// proposeOp runs a propose op as the next step: the Byzantine validator
// proposes a P-Chain block to every honest node (Stateful Fuzzing.md 2.1).
// It reads a node's preferred tip, runs the mempool paths, holds every link
// between honest nodes, checks that the node still prefers the tip, and sends
// the block, so that no honest block is accepted on that tip in between; then
// it releases the links. The block's txs come from the generator and the
// mutation tiers (blockTxs), and one of its own fields may be mutated
// (mutateBlock). The op predicts no verdict: the invariants judge whatever the
// honest nodes accept.
//
// The op does not wait for the proposer's slot: it is skipped unless the slot
// that holds now is the proposer's. A wait would take about 8 slots of 1 s,
// since the proposer has an eighth of the stake, while a skipped op takes
// about 8 ms. And the slot that holds now is the proposer's about 2 times in
// 3: an honest node builds as soon as its slot comes, so a tip with pending
// txs sits mostly in the proposer's slots. So even a wait for a slot about to
// start costs more than drawing again.
func (r *runner) proposeOp(t *rapid.T) {
	const opTimeout = time.Minute

	r.step++
	_, r.ev = r.events.Start(t.Context(), "propose")
	r.ev.Set("step", r.step)
	defer r.endEvent()

	ctx, done := deadline(t.Context(), t, fmt.Sprintf("op %d", r.step), opTimeout)
	defer done()

	stat, result := r.propose(ctx, t)
	r.finishOp(t, stat, result)
}

// propose builds a block on a tip that t draws and sends it (proposeOp). It
// returns the op's name and outcome.
//
// The links are held only until the block is out: a hold that lasts under
// half a ping period closes no link, so the checks that a closed link would
// excuse keep judging (faults). The mempool paths run before the hold, since
// a gossip verdict can take seconds. An honest block on the tip in that time
// moves the node's preference, and the op tries again on the new tip if the
// proposer's slot holds now there, with no paths: the first try left the tx
// in the mempools.
func (r *runner) propose(ctx context.Context, t *rapid.T) (string, outcome) {
	const stat = "propose"

	i := uniform(len(r.clients)).Draw(t, "tipFrom")
	r.ev.Set("tip_from", r.nodeDraw(i))
	txs := r.blockTxs(t)
	mutated := drawMutated(t)
	for attempt := 1; ; attempt++ {
		r.ev.Set("attempts", attempt)
		tip, err := r.proposer.tip(ctx, i)
		require.NoError(t, err)

		inner, err := innerBlock(tip, txs, time.Now())
		if err != nil {
			r.decided("proposer", err.Error())
			return stat, skipped
		}
		name := stat + " " + reflect.TypeOf(inner).Elem().Name()
		parent := tip
		if mutated {
			var m mutation
			parent, inner, m = r.mutateBlock(t, tip, inner)
			r.ev.Set("mutation", m)
			name += " mutated " + m.label()
		}

		// Node i verified the tip, so it has reached the P-Chain height that
		// the slots are drawn at. A mutated parent may sit above that height,
		// where the node gives no validator set to draw the slots from.
		at, err := r.proposer.slot(ctx, r.clients[i], parent, inner, 0)
		switch {
		case errors.Is(err, errNoSlot):
			r.proposer.tips = append(r.proposer.tips, tip)
			r.decided("proposer", "no slot now")
			return name, skipped
		case err != nil && mutated:
			r.decided("proposer", err.Error())
			return name, skipped
		}
		require.NoError(t, err)

		var paths []verdict
		if tx, ok := r.pathTx(inner); ok && !mutated && attempt == 1 {
			paths = r.mempoolPaths(ctx, t, tip, tx)
		}
		held := time.Now()
		// Hold every honest link. release gives each route back its earlier
		// delay.
		r.injector.holdAll()
		release := r.injector.release
		if r.tipMoved(ctx, t, i, tip) {
			release()
			continue
		}
		r.proposer.tips = append(r.proposer.tips, tip)
		r.ev.Set("paths", paths)
		r.ev.Set("parent", parent.id)
		r.ev.Set("inner", inner.ID())
		r.ev.Set("txs", logTxs(inner.Txs()))
		blk, err := r.proposer.send(ctx, parent, at, inner)
		sentAt := time.Now()
		release()
		r.ev.Set("held", time.Since(held).String())
		require.NoError(t, err)

		r.ev.Set("block", blk.ID())
		// Its new txs are pending like an op's, so that no new tx spends
		// their inputs while the block is undecided. The op counts the
		// block, not its txs.
		for _, tx := range inner.Txs() {
			if !r.acceptedIDs.Contains(tx.ID()) {
				r.track(&pendingTx{tx: tx, step: r.step, path: pathBlock, sent: time.Now()})
			}
		}
		if len(paths) > 0 {
			r.paths[blk.ID()] = pathCase{tx: inner.Txs()[0].ID(), mempool: paths}
		}
		r.proposer.addSent(parentBlock{
			id:           blk.ID(),
			inner:        inner,
			time:         blk.Timestamp(),
			pChainHeight: blk.PChainHeight(),
			epoch:        blk.PChainEpoch(),
		}, sentAt)

		return name, sent
	}
}

// tipMoved reports whether honest node i no longer prefers tip.
func (r *runner) tipMoved(ctx context.Context, t *rapid.T, i int, tip parentBlock) bool {
	preferred, err := r.proposer.peers[i].preferred(ctx, constants.PlatformChainID)
	require.NoError(t, err)

	return preferred != tip.id
}

// pathTx returns the tx of blk that the mempool paths judge (mempoolPaths):
// the first tx of a standard block, if it is new. In a standard block it runs
// first, on the parent's state at the block's time. A proposal block runs its
// proposer-only tx, which every mempool refuses, and a seen tx already
// committed, so neither is judged.
func (r *runner) pathTx(blk platform.Block) (*platform.Tx, bool) {
	std, ok := blk.(*platform.BanffStandardBlock)
	if !ok || len(std.Transactions) == 0 {
		return nil, false
	}

	tx := std.Transactions[0]
	return tx, !r.acceptedIDs.Contains(tx.ID())
}

// blockTxs draws the txs of a proposed block: 1 to 3, each a new tx from the
// generator, mutated 1 time in 3 as issue mutates, and 1 time in 10 a seen tx
// instead. A tx that the wallet cannot build is left out, and a block can end
// up with none: it is valid only if its time moves a staker
// (vms/platformvm/block/executor/verifier.go standardBlock). A new tx spends no input that an earlier tx of the block spends
// (blockInputs), as an honest builder's block would not; a mutation can
// still make two txs conflict.
func (r *runner) blockTxs(t *rapid.T) []*platform.Tx {
	defer r.blockInputs.Clear()

	var txs []*platform.Tx
	for range 1 + uniform(3).Draw(t, "blockTxs") {
		tx, ok := r.blockTx(t)
		if !ok {
			continue
		}
		txs = append(txs, tx)
		r.blockInputs.Union(tx.InputIDs())
	}

	return txs
}

// blockTx draws one tx of a proposed block (blockTxs). It returns false if
// the wallet could not build the tx.
func (r *runner) blockTx(t *rapid.T) (*platform.Tx, bool) {
	if uniform(10).Draw(t, "seenTxInBlock") == 0 {
		if tx, ok := r.drawSeenTx(t); ok {
			return tx, true
		}
	}

	proto := txTypes[uniform(len(txTypes)).Draw(t, "txType")]
	utx, err := r.build(t, proto)
	if err != nil {
		t.Logf("wallet could not build a %s for the block: %v", txName(proto), err)
		return nil, false
	}
	if !drawMutated(t) {
		return r.sign(t, utx), true
	}

	tx, m := r.mutate(t, utx)
	r.ev.Append("tx_mutations", m)

	return tx, true
}

// mutateBlock returns a copy of blk with one of its own fields changed, the
// parent that the copy builds on, and the mutation. The fields are its time,
// parent, height, and tx list (blockFields); the txs' own fields are the tx
// mutations' (mutate). A parent draw sets the block's height to follow the new
// parent, since the two move together, and keeps the time and txs. The block
// is encoded again, so its ID follows the change. The proposer signs the block
// for its slot after, so no part of the repair is skipped: a block with a bad
// proposervm signature reaches only the proposervm's parse. blk does not
// change.
func (r *runner) mutateBlock(t *rapid.T, parent parentBlock, blk platform.Block) (parentBlock, platform.Block, mutation) {
	c, err := platform.ParseBlock(platform.Codec, blk.Bytes())
	require.NoError(t, err)

	fields := blockFields(c)
	f := fields[uniform(len(fields)).Draw(t, "blockField")]
	next := parent
	draws := r.blockTiers(f, parent, &next)
	var sources [tierCount]bool
	for tr, d := range draws {
		sources[tr] = d != nil
	}
	tr := drawTier(t, sources)
	m := mutation{Path: f.path, Tier: tr.String(), Old: render(f), Repair: fullRepair.String(), key: f.key}
	m.New = draws[tr](t)

	// The draw set the parent ID on c; the height follows from the new parent.
	if next.id != parent.id {
		setHeight(c, next.inner.Height()+1)
	}

	b, err := platform.Codec.Marshal(platform.CodecVersion, &c)
	require.NoError(t, err)
	mutated, err := platform.ParseBlock(platform.Codec, b)
	require.NoError(t, err)

	return next, mutated, m
}

// notTxField accepts the serialized struct fields of a block other than its
// transactions, whose own fields belong to the tx mutations.
func notTxField(f reflect.StructField) bool {
	return serialized(f) && f.Type != reflect.TypeFor[*platform.Tx]()
}

// blockFields returns the fields of blk that a block mutation can change:
// its time, parent, height, and tx list. The fields inside its txs are not.
func blockFields(blk platform.Block) []field {
	var fields []field
	name := reflect.TypeOf(blk).Elem().Name()
	walk(reflect.ValueOf(blk), name, name, notTxField, func(f field) {
		if f.v.CanSet() && !strings.Contains(f.path, "[") {
			fields = append(fields, f)
		}
	})

	return fields
}

// setHeight sets blk's height field (CommonBlock.Hght).
func setHeight(blk platform.Block, height uint64) {
	name := reflect.TypeOf(blk).Elem().Name()
	walk(reflect.ValueOf(blk), name, name, notTxField, func(f field) {
		if f.kind == intField && strings.HasSuffix(f.key, ".Hght") {
			f.v.SetUint(height)
		}
	})
}

// blockTiers returns, for each tier, how to draw the new value of f, a field
// of a block on parent (blockFields). A parent draw also sets next to the
// parent it drew.
//
//   - The parent: the tip (live), an older tip (historical), which is likely
//     an accepted block by now, or a block the proposer sent (known), a
//     sibling of an accepted block or one that lost.
//   - The time: the parent's time or now (live), next to either of them or to
//     the block's own time (neighbor), any integer seen (known), or an edge
//     (random).
//   - The height: as the time, but with no threshold of its own.
//   - The tx list: one tx dropped, duplicated, or swapped (neighbor).
func (r *runner) blockTiers(f field, parent parentBlock, next *parentBlock) [tierCount]tierDraw {
	switch name := f.key[strings.LastIndexByte(f.key, '.')+1:]; {
	case f.kind == listField:
		return r.listTiers(f)
	case name == "PrntID":
		return r.parentTiers(f, parent, next)
	case name == "Time":
		parentTime := uint64(parent.inner.(platform.BanffBlock).Timestamp().Unix())
		return intDraws(f, []uint64{uint64(time.Now().Unix()), parentTime}, nil, r.seenInts.all.order)
	default:
		return intDraws(f, nil, nil, r.seenInts.all.order)
	}
}

// parentTiers draws a parent from the tips that propose ops read and the
// blocks that the proposer sent (blockTiers).
func (r *runner) parentTiers(f field, tip parentBlock, next *parentBlock) [tierCount]tierDraw {
	set := func(p parentBlock) string {
		*next = p
		f.v.Set(reflect.ValueOf(p.inner.ID()))

		return p.inner.ID().String()
	}
	older := slices.DeleteFunc(slices.Clone(r.proposer.tips), func(p parentBlock) bool { return p.id == tip.id })

	var draws [tierCount]tierDraw
	draws[live] = func(*rapid.T) string { return set(tip) }
	if len(older) > 0 {
		draws[historical] = func(t *rapid.T) string { return set(pick(t, older, "historicalParent")) }
	}
	if sent := r.proposer.sent; len(sent) > 0 {
		draws[known] = func(t *rapid.T) string { return set(pick(t, sent, "knownParent")) }
	}

	return draws
}
