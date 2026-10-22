// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// A view is what one node reports about the actors at one height.
//
// UTXOs are kept as codec bytes. Parsed outputs differ from wallet-built ones
// in unexported fields, so only the bytes compare reliably.
type view struct {
	height uint64
	utxos  map[ids.ID][]byte
	l1     map[ids.ID]uint64 // validation ID to balance
}

// check fails the test if any invariant is broken:
//
//   - every node accepted the same block at every height;
//   - every node reports the same state at the same height;
//   - the nodes hold exactly the UTXOs the model holds, plus stake returns,
//     rewards, and refunds the model expects;
//   - the actors' value grew by no more than their rewards;
//   - no node committed a forbidden tx.
func (r *Runner) check() {
	r.syncModel()
	r.checkBlocks()
	views := r.snapshot()
	for i, v := range views[1:] {
		require.Equal(r.tc, views[0], v, "nodes 0 and %d disagree at height %d", i+1, views[0].height)
	}

	rewards := r.reconcile(views[0].utxos)
	r.checkValue(views[0], rewards)
	r.checkForbidden()
}

func (r *Runner) checkBlocks() {
	ctx := r.tc.DefaultContext()
	top, err := r.clients[0].GetHeight(ctx)
	require.NoError(r.tc, err)
	for _, c := range r.clients[1:] {
		h, err := c.GetHeight(ctx)
		require.NoError(r.tc, err)
		top = min(top, h)
	}

	for h := r.height + 1; h <= top; h++ {
		want, err := r.clients[0].GetBlockByHeight(ctx, h)
		require.NoError(r.tc, err)
		for i, c := range r.clients[1:] {
			got, err := c.GetBlockByHeight(ctx, h)
			require.NoError(r.tc, err)
			require.Equal(r.tc, want, got, "nodes 0 and %d accepted different blocks at height %d", i+1, h)
		}
	}
	r.height = max(r.height, top)
}

// snapshot returns every node's view at one height. Heights only grow, so if
// every node reports the same height before and after the reads, no node
// accepted a block in between.
func (r *Runner) snapshot() []view {
	const attempts = 50

	for range attempts {
		before, ok := r.commonHeight()
		if !ok {
			time.Sleep(4 * pollInterval)
			continue
		}

		views := make([]view, len(r.clients))
		for i, c := range r.clients {
			views[i] = r.read(c)
			views[i].height = before
		}
		if after, ok := r.commonHeight(); ok && after == before {
			return views
		}
	}
	r.fatalf("nodes did not settle on one height")
	return nil
}

// commonHeight returns the nodes' height, and whether they all report it.
func (r *Runner) commonHeight() (uint64, bool) {
	ctx := r.tc.DefaultContext()
	want, err := r.clients[0].GetHeight(ctx)
	require.NoError(r.tc, err)
	for _, c := range r.clients[1:] {
		got, err := c.GetHeight(ctx)
		require.NoError(r.tc, err)
		if got != want {
			return 0, false
		}
	}

	return want, true
}

func (r *Runner) read(c *platformvm.Client) view {
	ctx := r.tc.DefaultContext()
	store := common.NewUTXOs()
	err := primary.AddAllUTXOs(ctx, store, c, platform.Codec, constants.PlatformChainID, constants.PlatformChainID, r.actors)
	require.NoError(r.tc, err)

	utxos, err := store.UTXOs(ctx, constants.PlatformChainID, constants.PlatformChainID)
	require.NoError(r.tc, err)

	v := view{
		utxos: make(map[ids.ID][]byte, len(utxos)),
		l1:    make(map[ids.ID]uint64, len(r.l1.all())),
	}
	for _, u := range utxos {
		v.utxos[u.InputID()] = r.bytes(u)
	}
	for _, l := range r.l1.all() {
		vdr, _, err := c.GetL1Validator(ctx, l.validationID)
		require.NoError(r.tc, err)

		v.l1[l.validationID] = vdr.Balance
	}

	return v
}

// reconcile matches the nodes' UTXOs against the model. Every model UTXO must
// exist unchanged. Every other UTXO must be a stake return, a reward, or a
// refund that the model expects. Explained UTXOs join the model, so the
// wallet can spend them. It returns the rewards found.
func (r *Runner) reconcile(utxos map[ids.ID][]byte) uint64 {
	ctx := r.tc.DefaultContext()
	model, err := r.utxos.UTXOs(ctx, constants.PlatformChainID, constants.PlatformChainID)
	require.NoError(r.tc, err)

	known := make(map[ids.ID]struct{}, len(model))
	for _, u := range model {
		id := u.InputID()
		known[id] = struct{}{}
		require.Equal(r.tc, r.bytes(u), utxos[id], "nodes disagree with the model on UTXO %s", id)
	}

	var rewards uint64
	for id, b := range utxos {
		if _, ok := known[id]; ok {
			continue
		}

		u := r.parse(b)
		first, isStaker := r.rewardIndex[u.TxID]
		bound, isRefund := r.refunds[id]
		var kind string
		switch want, isReturn := r.returns[id]; {
		case isReturn:
			require.Equal(r.tc, r.bytes(want), b, "stake return %s differs from the stake", id)
			delete(r.returns, id)
			kind = "stake return"
		case isStaker && u.OutputIndex >= first:
			rewards += amount(r.tc, u)
			kind = "reward"
		case isRefund:
			require.LessOrEqual(r.tc, amount(r.tc, u), bound, "refund %s exceeds the validator's balance", id)
			delete(r.refunds, id)
			kind = "refund"
		default:
			r.fatalf("unexplained UTXO %s: %x", id, b)
		}
		r.log.Info("found expected UTXO",
			zap.String("kind", kind),
			zap.Stringer("utxoID", id),
			zap.Uint64("amount", amount(r.tc, u)),
		)
		require.NoError(r.tc, r.utxos.AddUTXO(ctx, constants.PlatformChainID, constants.PlatformChainID, u))
	}

	return rewards
}

// checkValue fails the test if the actors' value grew by more than rewards.
// Value is UTXOs, L1 validator balances, and stake not yet returned. Fees
// only shrink it, so any other growth mints AVAX.
func (r *Runner) checkValue(v view, rewards uint64) {
	var total uint64
	for _, b := range v.utxos {
		total += amount(r.tc, r.parse(b))
	}
	for _, l := range r.l1.all() {
		l.balance = v.l1[l.validationID]
		total += l.balance
	}
	for _, u := range r.returns {
		total += amount(r.tc, u)
	}

	// The actors hold value on the X-Chain too, and value in flight between the
	// chains (exported, not yet imported). Counting all three makes a
	// cross-chain move conserve the total instead of looking like minting.
	ctx := r.tc.DefaultContext()
	for _, addr := range r.actors {
		reply, err := r.xClient.GetBalance(ctx, addr, r.avaxAssetID.String(), false)
		require.NoError(r.tc, err)
		total += uint64(reply.Balance)
	}
	total += r.inFlight

	require.LessOrEqual(r.tc, total-rewards, r.value, "actors' value grew from %d to %d with %d in rewards", r.value, total, rewards)
	r.value = total
}

func (r *Runner) checkForbidden() {
	for _, txID := range r.forbidden {
		for i, c := range r.clients {
			require.NotEqual(r.tc, status.Committed, r.txStatus(c, txID), "node %d committed forbidden tx %s", i, txID)
		}
	}
}

func (r *Runner) bytes(u *avax.UTXO) []byte {
	b, err := platform.Codec.Marshal(platform.CodecVersion, u)
	require.NoError(r.tc, err)
	return b
}

func (r *Runner) parse(b []byte) *avax.UTXO {
	var u avax.UTXO
	_, err := platform.Codec.Unmarshal(b, &u)
	require.NoError(r.tc, err)
	return &u
}

func amount(t require.TestingT, u *avax.UTXO) uint64 {
	out, ok := u.Out.(avax.Amounter)
	require.True(t, ok, "UTXO %s has no amount", u.InputID())
	return out.Amount()
}

// syncModel updates the registry's lifecycle from the chain, which ends
// stakers on its own. It reads only a published surface and runs before the
// oracles, so it adds one GetCurrentValidators call per check.
func (r *Runner) syncModel() {
	vdrs, err := r.clients[0].GetCurrentValidators(r.tc.DefaultContext(), constants.PrimaryNetworkID, nil)
	require.NoError(r.tc, err)

	current := set.NewSet[ids.NodeID](len(vdrs))
	for _, v := range vdrs {
		current.Add(v.NodeID)
	}
	for _, nodeID := range r.validators.keys(active) {
		if !current.Contains(nodeID) {
			r.validators.transition(nodeID, ended)
		}
	}
}
