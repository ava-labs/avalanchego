// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/poll"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/events"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const nonVerifiedCacheSize = 128

var _ Engine = (*Transitive)(nil)

func New(config Config) (Engine, error) {
	return newTransitive(config)
}

// Transitive implements the Engine interface by attempting to fetch all
// Transitive dependencies.
type Transitive struct {
	Config
	metrics

	// list of NoOpsHandler for messages dropped by engine
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.AncestorsHandler
	common.AppHandler
	validators.Connector

	RequestID uint32

	// track outstanding preference requests
	polls poll.Set

	// blocks that have we have sent get requests for but haven't yet received
	blkReqs common.Requests

	// blocks that are queued to be issued to consensus once missing dependencies are fetched
	// Block ID --> Block
	pending map[ids.ID]snowman.Block

	// Block ID --> Parent ID
	nonVerifieds AncestorTree

	// Block ID --> Block.
	// A block is put into this cache if it was not able to be issued. A block
	// fails to be issued if verification on the block or one of its ancestors
	// occurs.
	nonVerifiedCache cache.Cacher[ids.ID, snowman.Block]

	// acceptedFrontiers of the other validators of this chain
	acceptedFrontiers tracker.Accepted

	// operations that are blocked on a block being issued. This could be
	// issuing another block, responding to a query, or applying votes to consensus
	blocked events.Blocker

	// number of times build block needs to be called once the number of
	// processing blocks has gone below the optimal number.
	pendingBuildBlocks int

	// errs tracks if an error has occurred in a callback
	errs wrappers.Errs
}

func newTransitive(config Config) (*Transitive, error) {
	config.Ctx.Log.Info("initializing consensus engine")

	nonVerifiedCache, err := metercacher.New[ids.ID, snowman.Block](
		"non_verified_cache",
		config.Ctx.Registerer,
		&cache.LRU[ids.ID, snowman.Block]{Size: nonVerifiedCacheSize},
	)
	if err != nil {
		return nil, err
	}

	acceptedFrontiers := tracker.NewAccepted()
	config.Validators.RegisterCallbackListener(acceptedFrontiers)

	factory := poll.NewEarlyTermNoTraversalFactory(config.Params.Alpha)
	t := &Transitive{
		Config:                      config,
		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(config.Ctx.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(config.Ctx.Log),
		AcceptedFrontierHandler:     common.NewNoOpAcceptedFrontierHandler(config.Ctx.Log),
		AcceptedHandler:             common.NewNoOpAcceptedHandler(config.Ctx.Log),
		AncestorsHandler:            common.NewNoOpAncestorsHandler(config.Ctx.Log),
		AppHandler:                  config.VM,
		Connector:                   config.VM,
		pending:                     make(map[ids.ID]snowman.Block),
		nonVerifieds:                NewAncestorTree(),
		nonVerifiedCache:            nonVerifiedCache,
		acceptedFrontiers:           acceptedFrontiers,
		polls: poll.NewSet(factory,
			config.Ctx.Log,
			"",
			config.Ctx.Registerer,
		),
	}

	return t, t.metrics.Initialize("", config.Ctx.Registerer)
}

func (t *Transitive) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkBytes []byte) error {
	blk, err := t.VM.ParseBlock(ctx, blkBytes)
	if err != nil {
		t.Ctx.Log.Debug("failed to parse block",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Error(err),
		)
		t.Ctx.Log.Verbo("failed to parse block",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Binary("block", blkBytes),
			zap.Error(err),
		)
		// because GetFailed doesn't utilize the assumption that we actually
		// sent a Get message, we can safely call GetFailed here to potentially
		// abandon the request.
		return t.GetFailed(ctx, nodeID, requestID)
	}

	actualBlkID := blk.ID()
	expectedBlkID, ok := t.blkReqs.Get(nodeID, requestID)
	// If the provided block is not the requested block, we need to explicitly
	// mark the request as failed to avoid having a dangling dependency.
	if ok && actualBlkID != expectedBlkID {
		t.Ctx.Log.Debug("incorrect block returned in Put",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("blkID", actualBlkID),
			zap.Stringer("expectedBlkID", expectedBlkID),
		)
		// We assume that [blk] is useless because it doesn't match what we
		// expected.
		return t.GetFailed(ctx, nodeID, requestID)
	}

	if t.wasIssued(blk) {
		t.metrics.numUselessPutBytes.Add(float64(len(blkBytes)))
	}

	// issue the block into consensus. If the block has already been issued,
	// this will be a noop. If this block has missing dependencies, vdr will
	// receive requests to fill the ancestry. dependencies that have already
	// been fetched, but with missing dependencies themselves won't be requested
	// from the vdr.
	if _, err := t.issueFrom(ctx, nodeID, blk); err != nil {
		return err
	}
	return t.buildBlocks(ctx)
}

func (t *Transitive) GetFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// We don't assume that this function is called after a failed Get message.
	// Check to see if we have an outstanding request and also get what the request was for if it exists.
	blkID, ok := t.blkReqs.Remove(nodeID, requestID)
	if !ok {
		t.Ctx.Log.Debug("unexpected GetFailed",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	// Because the get request was dropped, we no longer expect blkID to be issued.
	t.blocked.Abandon(ctx, blkID)
	t.metrics.numRequests.Set(float64(t.blkReqs.Len()))
	t.metrics.numBlockers.Set(float64(t.blocked.Len()))
	return t.buildBlocks(ctx)
}

func (t *Transitive) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID) error {
	t.sendChits(ctx, nodeID, requestID)

	// Try to issue [blkID] to consensus.
	// If we're missing an ancestor, request it from [vdr]
	if _, err := t.issueFromByID(ctx, nodeID, blkID); err != nil {
		return err
	}

	return t.buildBlocks(ctx)
}

func (t *Transitive) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkBytes []byte) error {
	t.sendChits(ctx, nodeID, requestID)

	blk, err := t.VM.ParseBlock(ctx, blkBytes)
	// If parsing fails, we just drop the request, as we didn't ask for it
	if err != nil {
		t.Ctx.Log.Debug("failed to parse block",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Error(err),
		)
		t.Ctx.Log.Verbo("failed to parse block",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Binary("block", blkBytes),
			zap.Error(err),
		)
		return nil
	}

	if t.wasIssued(blk) {
		t.metrics.numUselessPushQueryBytes.Add(float64(len(blkBytes)))
	}

	// issue the block into consensus. If the block has already been issued,
	// this will be a noop. If this block has missing dependencies, nodeID will
	// receive requests to fill the ancestry. dependencies that have already
	// been fetched, but with missing dependencies themselves won't be requested
	// from the vdr.
	if _, err := t.issueFrom(ctx, nodeID, blk); err != nil {
		return err
	}

	return t.buildBlocks(ctx)
}

func (t *Transitive) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, votes []ids.ID, accepted []ids.ID) error {
	t.acceptedFrontiers.SetAcceptedFrontier(nodeID, accepted)

	// Since this is a linear chain, there should only be one ID in the vote set
	if len(votes) != 1 {
		t.Ctx.Log.Debug("failing Chits",
			zap.String("reason", "expected only 1 vote"),
			zap.Int("numVotes", len(votes)),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		// because QueryFailed doesn't utilize the assumption that we actually
		// sent a Query message, we can safely call QueryFailed here to
		// potentially abandon the request.
		return t.QueryFailed(ctx, nodeID, requestID)
	}
	blkID := votes[0]

	t.Ctx.Log.Verbo("called Chits for the block",
		zap.Stringer("blkID", blkID),
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID))

	// Will record chits once [blkID] has been issued into consensus
	v := &voter{
		t:         t,
		vdr:       nodeID,
		requestID: requestID,
		response:  blkID,
	}

	added, err := t.issueFromByID(ctx, nodeID, blkID)
	if err != nil {
		return err
	}
	// Wait until [blkID] has been issued to consensus before applying this chit.
	if !added {
		v.deps.Add(blkID)
	}

	t.blocked.Register(ctx, v)
	t.metrics.numBlockers.Set(float64(t.blocked.Len()))
	return t.buildBlocks(ctx)
}

func (t *Transitive) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	lastAccepted := t.acceptedFrontiers.AcceptedFrontier(nodeID)
	if len(lastAccepted) == 1 {
		// Chits calls QueryFailed if [votes] doesn't have length 1, so this
		// check is required to avoid infinite mutual recursion.
		return t.Chits(ctx, nodeID, requestID, lastAccepted, lastAccepted)
	}

	t.blocked.Register(
		ctx,
		&voter{
			t:         t,
			vdr:       nodeID,
			requestID: requestID,
		},
	)
	t.metrics.numBlockers.Set(float64(t.blocked.Len()))
	return t.buildBlocks(ctx)
}

func (*Transitive) Timeout(context.Context) error {
	return nil
}

func (t *Transitive) Gossip(ctx context.Context) error {
	blkID, err := t.VM.LastAccepted(ctx)
	if err != nil {
		return err
	}

	blk, err := t.GetBlock(ctx, blkID)
	if err != nil {
		t.Ctx.Log.Warn("dropping gossip request",
			zap.String("reason", "block couldn't be loaded"),
			zap.Stringer("blkID", blkID),
			zap.Error(err),
		)
		return nil
	}
	t.Ctx.Log.Verbo("gossiping accepted block to the network",
		zap.Stringer("blkID", blkID),
	)
	t.Sender.SendGossip(ctx, blk.Bytes())
	return nil
}

func (*Transitive) Halt(context.Context) {}

func (t *Transitive) Shutdown(ctx context.Context) error {
	t.Ctx.Log.Info("shutting down consensus engine")
	return t.VM.Shutdown(ctx)
}

func (t *Transitive) Notify(ctx context.Context, msg common.Message) error {
	switch msg {
	case common.PendingTxs:
		// the pending txs message means we should attempt to build a block.
		t.pendingBuildBlocks++
		return t.buildBlocks(ctx)
	case common.StateSyncDone:
		t.Ctx.StateSyncing.Set(false)
		return nil
	default:
		t.Ctx.Log.Warn("received an unexpected message from the VM",
			zap.Stringer("messageString", msg),
		)
		return nil
	}
}

func (t *Transitive) Context() *snow.ConsensusContext {
	return t.Ctx
}

func (t *Transitive) Start(ctx context.Context, startReqID uint32) error {
	t.RequestID = startReqID
	lastAcceptedID, err := t.VM.LastAccepted(ctx)
	if err != nil {
		return err
	}

	lastAccepted, err := t.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		t.Ctx.Log.Error("failed to get last accepted block",
			zap.Error(err),
		)
		return err
	}

	// initialize consensus to the last accepted blockID
	if err := t.Consensus.Initialize(t.Ctx, t.Params, lastAcceptedID, lastAccepted.Height(), lastAccepted.Timestamp()); err != nil {
		return err
	}

	// to maintain the invariant that oracle blocks are issued in the correct
	// preferences, we need to handle the case that we are bootstrapping into an oracle block
	if oracleBlk, ok := lastAccepted.(snowman.OracleBlock); ok {
		options, err := oracleBlk.Options(ctx)
		switch {
		case err == snowman.ErrNotOracle:
			// if there aren't blocks we need to deliver on startup, we need to set
			// the preference to the last accepted block
			if err := t.VM.SetPreference(ctx, lastAcceptedID); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			for _, blk := range options {
				// note that deliver will set the VM's preference
				if err := t.deliver(ctx, blk); err != nil {
					return err
				}
			}
		}
	} else if err := t.VM.SetPreference(ctx, lastAcceptedID); err != nil {
		return err
	}

	t.Ctx.Log.Info("consensus starting",
		zap.Stringer("lastAcceptedBlock", lastAcceptedID),
	)
	t.metrics.bootstrapFinished.Set(1)

	t.Ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.NormalOp,
	})
	if err := t.VM.SetState(ctx, snow.NormalOp); err != nil {
		return fmt.Errorf("failed to notify VM that consensus is starting: %w",
			err)
	}
	return nil
}

func (t *Transitive) HealthCheck(ctx context.Context) (interface{}, error) {
	consensusIntf, consensusErr := t.Consensus.HealthCheck(ctx)
	vmIntf, vmErr := t.VM.HealthCheck(ctx)
	intf := map[string]interface{}{
		"consensus": consensusIntf,
		"vm":        vmIntf,
	}
	if consensusErr == nil {
		return intf, vmErr
	}
	if vmErr == nil {
		return intf, consensusErr
	}
	return intf, fmt.Errorf("vm: %w ; consensus: %v", vmErr, consensusErr)
}

func (t *Transitive) GetVM() common.VM {
	return t.VM
}

func (t *Transitive) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	if blk, ok := t.pending[blkID]; ok {
		return blk, nil
	}
	if blk, ok := t.nonVerifiedCache.Get(blkID); ok {
		return blk, nil
	}

	return t.VM.GetBlock(ctx, blkID)
}

func (t *Transitive) sendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32) {
	lastAccepted := t.Consensus.LastAccepted()
	if t.Ctx.StateSyncing.Get() {
		t.Sender.SendChits(ctx, nodeID, requestID, []ids.ID{lastAccepted}, []ids.ID{lastAccepted})
	} else {
		t.Sender.SendChits(ctx, nodeID, requestID, []ids.ID{t.Consensus.Preference()}, []ids.ID{lastAccepted})
	}
}

// Build blocks if they have been requested and the number of processing blocks
// is less than optimal.
func (t *Transitive) buildBlocks(ctx context.Context) error {
	if err := t.errs.Err; err != nil {
		return err
	}
	for t.pendingBuildBlocks > 0 && t.Consensus.NumProcessing() < t.Params.OptimalProcessing {
		t.pendingBuildBlocks--

		blk, err := t.VM.BuildBlock(ctx)
		if err != nil {
			t.Ctx.Log.Debug("failed building block",
				zap.Error(err),
			)
			t.numBuildsFailed.Inc()
			return nil
		}
		t.numBuilt.Inc()

		// a newly created block is expected to be processing. If this check
		// fails, there is potentially an error in the VM this engine is running
		if status := blk.Status(); status != choices.Processing {
			t.Ctx.Log.Warn("attempting to issue block with unexpected status",
				zap.Stringer("expectedStatus", choices.Processing),
				zap.Stringer("status", status),
			)
		}

		// The newly created block should be built on top of the preferred block.
		// Otherwise, the new block doesn't have the best chance of being confirmed.
		parentID := blk.Parent()
		if pref := t.Consensus.Preference(); parentID != pref {
			t.Ctx.Log.Warn("built block with unexpected parent",
				zap.Stringer("expectedParentID", pref),
				zap.Stringer("parentID", parentID),
			)
		}

		added, err := t.issueWithAncestors(ctx, blk)
		if err != nil {
			return err
		}

		// issuing the block shouldn't have any missing dependencies
		if added {
			t.Ctx.Log.Verbo("successfully issued new block from the VM")
		} else {
			t.Ctx.Log.Warn("built block with unissued ancestors")
		}
	}
	return nil
}

// Issue another poll to the network, asking what it prefers given the block we prefer.
// Helps move consensus along.
func (t *Transitive) repoll(ctx context.Context) {
	// if we are issuing a repoll, we should gossip our current preferences to
	// propagate the most likely branch as quickly as possible
	prefID := t.Consensus.Preference()

	for i := t.polls.Len(); i < t.Params.ConcurrentRepolls; i++ {
		t.pullQuery(ctx, prefID)
	}
}

// issueFromByID attempts to issue the branch ending with a block [blkID] into consensus.
// If we do not have [blkID], request it.
// Returns true if the block is processing in consensus or is decided.
func (t *Transitive) issueFromByID(ctx context.Context, nodeID ids.NodeID, blkID ids.ID) (bool, error) {
	blk, err := t.GetBlock(ctx, blkID)
	if err != nil {
		t.sendRequest(ctx, nodeID, blkID)
		return false, nil
	}
	return t.issueFrom(ctx, nodeID, blk)
}

// issueFrom attempts to issue the branch ending with block [blkID] to consensus.
// Returns true if the block is processing in consensus or is decided.
// If a dependency is missing, request it from [vdr].
func (t *Transitive) issueFrom(ctx context.Context, nodeID ids.NodeID, blk snowman.Block) (bool, error) {
	// issue [blk] and its ancestors to consensus.
	blkID := blk.ID()
	for !t.wasIssued(blk) {
		if err := t.issue(ctx, blk); err != nil {
			return false, err
		}

		blkID = blk.Parent()
		var err error
		blk, err = t.GetBlock(ctx, blkID)

		// If we don't have this ancestor, request it from [vdr]
		if err != nil || !blk.Status().Fetched() {
			t.sendRequest(ctx, nodeID, blkID)
			return false, nil
		}
	}

	// Remove any outstanding requests for this block
	t.blkReqs.RemoveAny(blkID)

	issued := t.Consensus.Decided(blk) || t.Consensus.Processing(blkID)
	if issued {
		// A dependency should never be waiting on a decided or processing
		// block. However, if the block was marked as rejected by the VM, the
		// dependencies may still be waiting. Therefore, they should abandoned.
		t.blocked.Abandon(ctx, blkID)
	}

	// Tracks performance statistics
	t.metrics.numRequests.Set(float64(t.blkReqs.Len()))
	t.metrics.numBlockers.Set(float64(t.blocked.Len()))
	return issued, t.errs.Err
}

// issueWithAncestors attempts to issue the branch ending with [blk] to consensus.
// Returns true if the block is processing in consensus or is decided.
// If a dependency is missing and the dependency hasn't been requested, the issuance will be abandoned.
func (t *Transitive) issueWithAncestors(ctx context.Context, blk snowman.Block) (bool, error) {
	blkID := blk.ID()
	// issue [blk] and its ancestors into consensus
	status := blk.Status()
	for status.Fetched() && !t.wasIssued(blk) {
		err := t.issue(ctx, blk)
		if err != nil {
			return false, err
		}
		blkID = blk.Parent()
		blk, err = t.GetBlock(ctx, blkID)
		if err != nil {
			status = choices.Unknown
			break
		}
		status = blk.Status()
	}

	// The block was issued into consensus. This is the happy path.
	if status != choices.Unknown && (t.Consensus.Decided(blk) || t.Consensus.Processing(blkID)) {
		return true, nil
	}

	// There's an outstanding request for this block.
	// We can just wait for that request to succeed or fail.
	if t.blkReqs.Contains(blkID) {
		return false, nil
	}

	// We don't have this block and have no reason to expect that we will get it.
	// Abandon the block to avoid a memory leak.
	t.blocked.Abandon(ctx, blkID)
	t.metrics.numBlockers.Set(float64(t.blocked.Len()))
	return false, t.errs.Err
}

// If the block has been decided, then it is marked as having been issued.
// If the block is processing, then it was issued.
// If the block is queued to be added to consensus, then it was issued.
func (t *Transitive) wasIssued(blk snowman.Block) bool {
	blkID := blk.ID()
	return t.Consensus.Decided(blk) || t.Consensus.Processing(blkID) || t.pendingContains(blkID)
}

// Issue [blk] to consensus once its ancestors have been issued.
func (t *Transitive) issue(ctx context.Context, blk snowman.Block) error {
	blkID := blk.ID()

	// mark that the block is queued to be added to consensus once its ancestors have been
	t.pending[blkID] = blk

	// Remove any outstanding requests for this block
	t.blkReqs.RemoveAny(blkID)

	// Will add [blk] to consensus once its ancestors have been
	i := &issuer{
		t:   t,
		blk: blk,
	}

	// block on the parent if needed
	parentID := blk.Parent()
	if parent, err := t.GetBlock(ctx, parentID); err != nil || !(t.Consensus.Decided(parent) || t.Consensus.Processing(parentID)) {
		t.Ctx.Log.Verbo("block waiting for parent to be issued",
			zap.Stringer("blkID", blkID),
			zap.Stringer("parentID", parentID),
		)
		i.deps.Add(parentID)
	}

	t.blocked.Register(ctx, i)

	// Tracks performance statistics
	t.metrics.numRequests.Set(float64(t.blkReqs.Len()))
	t.metrics.numBlocked.Set(float64(len(t.pending)))
	t.metrics.numBlockers.Set(float64(t.blocked.Len()))
	return t.errs.Err
}

// Request that [vdr] send us block [blkID]
func (t *Transitive) sendRequest(ctx context.Context, nodeID ids.NodeID, blkID ids.ID) {
	// There is already an outstanding request for this block
	if t.blkReqs.Contains(blkID) {
		return
	}

	t.RequestID++
	t.blkReqs.Add(nodeID, t.RequestID, blkID)
	t.Ctx.Log.Verbo("sending Get request",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", t.RequestID),
		zap.Stringer("blkID", blkID),
	)
	t.Sender.SendGet(ctx, nodeID, t.RequestID, blkID)

	// Tracks performance statistics
	t.metrics.numRequests.Set(float64(t.blkReqs.Len()))
}

// send a pull query for this block ID
func (t *Transitive) pullQuery(ctx context.Context, blkID ids.ID) {
	t.Ctx.Log.Verbo("sampling from validators",
		zap.Stringer("validators", t.Validators),
	)
	// The validators we will query
	vdrIDs, err := t.Validators.Sample(t.Params.K)
	if err != nil {
		t.Ctx.Log.Error("dropped query for block",
			zap.String("reason", "insufficient number of validators"),
			zap.Stringer("blkID", blkID),
		)
		return
	}

	vdrBag := bag.Bag[ids.NodeID]{}
	vdrBag.Add(vdrIDs...)

	t.RequestID++
	if t.polls.Add(t.RequestID, vdrBag) {
		vdrList := vdrBag.List()
		vdrSet := set.NewSet[ids.NodeID](len(vdrList))
		vdrSet.Add(vdrList...)
		t.Sender.SendPullQuery(ctx, vdrSet, t.RequestID, blkID)
	}
}

// Send a query for this block. Some validators will be sent
// a Push Query and some will be sent a Pull Query.
func (t *Transitive) sendMixedQuery(ctx context.Context, blk snowman.Block) {
	t.Ctx.Log.Verbo("sampling from validators",
		zap.Stringer("validators", t.Validators),
	)

	blkID := blk.ID()
	vdrIDs, err := t.Validators.Sample(t.Params.K)
	if err != nil {
		t.Ctx.Log.Error("dropped query for block",
			zap.String("reason", "insufficient number of validators"),
			zap.Stringer("blkID", blkID),
		)
		return
	}

	vdrBag := bag.Bag[ids.NodeID]{}
	vdrBag.Add(vdrIDs...)

	t.RequestID++
	if t.polls.Add(t.RequestID, vdrBag) {
		// Send a push query to some of the validators, and a pull query to the rest.
		numPushTo := t.Params.MixedQueryNumPushVdr
		if !t.Validators.Contains(t.Ctx.NodeID) {
			numPushTo = t.Params.MixedQueryNumPushNonVdr
		}
		common.SendMixedQuery(
			ctx,
			t.Sender,
			vdrBag.List(), // Note that this doesn't contain duplicates; length may be < k
			numPushTo,
			t.RequestID,
			blkID,
			blk.Bytes(),
		)
	}
}

// issue [blk] to consensus
func (t *Transitive) deliver(ctx context.Context, blk snowman.Block) error {
	blkID := blk.ID()
	if t.Consensus.Decided(blk) || t.Consensus.Processing(blkID) {
		return nil
	}

	// we are no longer waiting on adding the block to consensus, so it is no
	// longer pending
	t.removeFromPending(blk)
	parentID := blk.Parent()
	parent, err := t.GetBlock(ctx, parentID)
	// Because the dependency must have been fulfilled by the time this function
	// is called - we don't expect [err] to be non-nil. But it is handled for
	// completness and future proofing.
	if err != nil || !(parent.Status() == choices.Accepted || t.Consensus.Processing(parentID)) {
		// if the parent isn't processing or the last accepted block, then this
		// block is effectively rejected
		t.blocked.Abandon(ctx, blkID)
		t.metrics.numBlocked.Set(float64(len(t.pending))) // Tracks performance statistics
		t.metrics.numBlockers.Set(float64(t.blocked.Len()))
		return t.errs.Err
	}

	// By ensuring that the parent is either processing or accepted, it is
	// guaranteed that the parent was successfully verified. This means that
	// calling Verify on this block is allowed.
	blkAdded, err := t.addUnverifiedBlockToConsensus(ctx, blk)
	if err != nil {
		return err
	}
	if !blkAdded {
		t.blocked.Abandon(ctx, blkID)
		t.metrics.numBlocked.Set(float64(len(t.pending))) // Tracks performance statistics
		t.metrics.numBlockers.Set(float64(t.blocked.Len()))
		return t.errs.Err
	}

	// Add all the oracle blocks if they exist. We call verify on all the blocks
	// and add them to consensus before marking anything as fulfilled to avoid
	// any potential reentrant bugs.
	added := []snowman.Block{}
	dropped := []snowman.Block{}
	if blk, ok := blk.(snowman.OracleBlock); ok {
		options, err := blk.Options(ctx)
		if err != snowman.ErrNotOracle {
			if err != nil {
				return err
			}

			for _, blk := range options {
				blkAdded, err := t.addUnverifiedBlockToConsensus(ctx, blk)
				if err != nil {
					return err
				}
				if blkAdded {
					added = append(added, blk)
				} else {
					dropped = append(dropped, blk)
				}
			}
		}
	}

	if err := t.VM.SetPreference(ctx, t.Consensus.Preference()); err != nil {
		return err
	}

	// If the block is now preferred, query the network for its preferences
	// with this new block.
	if t.Consensus.IsPreferred(blk) {
		t.sendMixedQuery(ctx, blk)
	}

	t.blocked.Fulfill(ctx, blkID)
	for _, blk := range added {
		if t.Consensus.IsPreferred(blk) {
			t.sendMixedQuery(ctx, blk)
		}

		blkID := blk.ID()
		t.removeFromPending(blk)
		t.blocked.Fulfill(ctx, blkID)
		t.blkReqs.RemoveAny(blkID)
	}
	for _, blk := range dropped {
		blkID := blk.ID()
		t.removeFromPending(blk)
		t.blocked.Abandon(ctx, blkID)
		t.blkReqs.RemoveAny(blkID)
	}

	// If we should issue multiple queries at the same time, we need to repoll
	t.repoll(ctx)

	// Tracks performance statistics
	t.metrics.numRequests.Set(float64(t.blkReqs.Len()))
	t.metrics.numBlocked.Set(float64(len(t.pending)))
	t.metrics.numBlockers.Set(float64(t.blocked.Len()))
	return t.errs.Err
}

// Returns true if the block whose ID is [blkID] is waiting to be issued to consensus
func (t *Transitive) pendingContains(blkID ids.ID) bool {
	_, ok := t.pending[blkID]
	return ok
}

func (t *Transitive) removeFromPending(blk snowman.Block) {
	delete(t.pending, blk.ID())
}

func (t *Transitive) addToNonVerifieds(blk snowman.Block) {
	// don't add this blk if it's decided or processing.
	blkID := blk.ID()
	if t.Consensus.Decided(blk) || t.Consensus.Processing(blkID) {
		return
	}
	parentID := blk.Parent()
	// we might still need this block so we can bubble votes to the parent
	// only add blocks with parent already in the tree or processing.
	// decided parents should not be in this map.
	if t.nonVerifieds.Has(parentID) || t.Consensus.Processing(parentID) {
		t.nonVerifieds.Add(blkID, parentID)
		t.nonVerifiedCache.Put(blkID, blk)
		t.metrics.numNonVerifieds.Set(float64(t.nonVerifieds.Len()))
	}
}

// addUnverifiedBlockToConsensus returns whether the block was added and an
// error if one occurred while adding it to consensus.
func (t *Transitive) addUnverifiedBlockToConsensus(ctx context.Context, blk snowman.Block) (bool, error) {
	// make sure this block is valid
	if err := blk.Verify(ctx); err != nil {
		t.Ctx.Log.Debug("block verification failed",
			zap.Error(err),
		)

		// if verify fails, then all descendants are also invalid
		t.addToNonVerifieds(blk)
		return false, nil
	}

	blkID := blk.ID()
	t.nonVerifieds.Remove(blkID)
	t.nonVerifiedCache.Evict(blkID)
	t.metrics.numNonVerifieds.Set(float64(t.nonVerifieds.Len()))
	t.Ctx.Log.Verbo("adding block to consensus",
		zap.Stringer("blkID", blkID),
	)
	return true, t.Consensus.Add(ctx, &memoryBlock{
		Block:   blk,
		metrics: &t.metrics,
		tree:    t.nonVerifieds,
	})
}
