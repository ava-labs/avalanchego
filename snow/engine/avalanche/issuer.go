// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/set"
)

// issuer issues [vtx] into consensus after its dependencies are met.
type issuer struct {
	t                 *Transitive
	vtx               avalanche.Vertex
	issued, abandoned bool
	vtxDeps, txDeps   set.Set[ids.ID]
}

// Register that a vertex we were waiting on has been issued to consensus.
func (i *issuer) FulfillVtx(ctx context.Context, id ids.ID) {
	i.vtxDeps.Remove(id)
	i.Update(ctx)
}

// Register that a transaction we were waiting on has been issued to consensus.
func (i *issuer) FulfillTx(ctx context.Context, id ids.ID) {
	i.txDeps.Remove(id)
	i.Update(ctx)
}

// Abandon this attempt to issue
func (i *issuer) Abandon(ctx context.Context) {
	if !i.abandoned {
		vtxID := i.vtx.ID()
		i.t.pending.Remove(vtxID)
		i.abandoned = true
		i.t.vtxBlocked.Abandon(ctx, vtxID) // Inform vertices waiting on this vtx that it won't be issued
		i.t.metrics.blockerVtxs.Set(float64(i.t.vtxBlocked.Len()))
	}
}

// Issue the poll when all dependencies are met
func (i *issuer) Update(ctx context.Context) {
	if i.abandoned || i.issued || i.vtxDeps.Len() != 0 || i.txDeps.Len() != 0 || i.t.Consensus.VertexIssued(i.vtx) || i.t.errs.Errored() {
		return
	}

	vtxID := i.vtx.ID()

	// All dependencies have been met
	i.issued = true

	// check stop vertex validity
	err := i.vtx.Verify(ctx)
	if err != nil {
		if i.vtx.HasWhitelist() {
			// do not update "i.t.errs" since it's only used for critical errors
			// which will cause chain shutdown in the engine
			// (see "handleSyncMsg" and "handleChanMsg")
			i.t.Ctx.Log.Debug("stop vertex verification failed",
				zap.Stringer("vtxID", vtxID),
				zap.Error(err),
			)
			i.t.metrics.whitelistVtxIssueFailure.Inc()
		} else {
			i.t.Ctx.Log.Debug("vertex verification failed",
				zap.Stringer("vtxID", vtxID),
				zap.Error(err),
			)
		}

		i.t.vtxBlocked.Abandon(ctx, vtxID)
		return
	}

	i.t.pending.Remove(vtxID) // Remove from set of vertices waiting to be issued.

	// Make sure the transactions in this vertex are valid
	txs, err := i.vtx.Txs(ctx)
	if err != nil {
		i.t.errs.Add(err)
		return
	}
	validTxs := make([]snowstorm.Tx, 0, len(txs))
	for _, tx := range txs {
		if err := tx.Verify(ctx); err != nil {
			txID := tx.ID()
			i.t.Ctx.Log.Debug("transaction verification failed",
				zap.Stringer("txID", txID),
				zap.Error(err),
			)
			i.t.txBlocked.Abandon(ctx, txID)
		} else {
			validTxs = append(validTxs, tx)
		}
	}

	// Some of the transactions weren't valid. Abandon this vertex.
	// Take the valid transactions and issue a new vertex with them.
	if len(validTxs) != len(txs) {
		i.t.Ctx.Log.Debug("abandoning vertex",
			zap.String("reason", "transaction verification failed"),
			zap.Stringer("vtxID", vtxID),
		)
		if _, err := i.t.batch(ctx, validTxs, batchOption{}); err != nil {
			i.t.errs.Add(err)
		}
		i.t.vtxBlocked.Abandon(ctx, vtxID)
		i.t.metrics.blockerVtxs.Set(float64(i.t.vtxBlocked.Len()))
		return
	}

	i.t.Ctx.Log.Verbo("adding vertex to consensus",
		zap.Stringer("vtxID", vtxID),
	)

	// Add this vertex to consensus.
	if err := i.t.Consensus.Add(ctx, i.vtx); err != nil {
		i.t.errs.Add(err)
		return
	}

	// Issue a poll for this vertex.
	vdrIDs, err := i.t.Validators.Sample(i.t.Params.K) // Validators to sample
	if err != nil {
		i.t.Ctx.Log.Error("dropped query",
			zap.String("reason", "insufficient number of validators"),
			zap.Stringer("vtxID", vtxID),
		)
	}

	vdrBag := bag.Bag[ids.NodeID]{} // Validators to sample repr. as a set
	vdrBag.Add(vdrIDs...)

	i.t.RequestID++
	if err == nil && i.t.polls.Add(i.t.RequestID, vdrBag) {
		numPushTo := i.t.Params.MixedQueryNumPushVdr
		if !i.t.Validators.Contains(i.t.Ctx.NodeID) {
			numPushTo = i.t.Params.MixedQueryNumPushNonVdr
		}
		common.SendMixedQuery(
			ctx,
			i.t.Sender,
			vdrBag.List(), // Note that this doesn't contain duplicates; length may be < k
			numPushTo,
			i.t.RequestID,
			vtxID,
			i.vtx.Bytes(),
		)
	}

	// Notify vertices waiting on this one that it (and its transactions) have been issued.
	i.t.vtxBlocked.Fulfill(ctx, vtxID)
	for _, tx := range txs {
		i.t.txBlocked.Fulfill(ctx, tx.ID())
	}
	i.t.metrics.blockerTxs.Set(float64(i.t.txBlocked.Len()))
	i.t.metrics.blockerVtxs.Set(float64(i.t.vtxBlocked.Len()))

	if i.vtx.HasWhitelist() {
		i.t.Ctx.Log.Info("successfully issued stop vertex",
			zap.Stringer("vtxID", vtxID),
		)
		i.t.metrics.whitelistVtxIssueSuccess.Inc()
	}

	// Issue a repoll
	i.t.repoll(ctx)
}

type vtxIssuer struct{ i *issuer }

func (vi *vtxIssuer) Dependencies() set.Set[ids.ID] {
	return vi.i.vtxDeps
}

func (vi *vtxIssuer) Fulfill(ctx context.Context, id ids.ID) {
	vi.i.FulfillVtx(ctx, id)
}

func (vi *vtxIssuer) Abandon(ctx context.Context, _ ids.ID) {
	vi.i.Abandon(ctx)
}

func (vi *vtxIssuer) Update(ctx context.Context) {
	vi.i.Update(ctx)
}

type txIssuer struct{ i *issuer }

func (ti *txIssuer) Dependencies() set.Set[ids.ID] {
	return ti.i.txDeps
}

func (ti *txIssuer) Fulfill(ctx context.Context, id ids.ID) {
	ti.i.FulfillTx(ctx, id)
}

func (ti *txIssuer) Abandon(ctx context.Context, _ ids.ID) {
	ti.i.Abandon(ctx)
}

func (ti *txIssuer) Update(ctx context.Context) {
	ti.i.Update(ctx)
}
