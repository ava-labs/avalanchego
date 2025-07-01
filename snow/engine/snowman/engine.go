// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/poll"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/ancestor"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/job"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/bimap"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	nonVerifiedCacheSize = 64 * units.MiB
	errInsufficientStake = "insufficient connected stake"
)

var _ common.Engine = (*Engine)(nil)

func cachedBlockSize(_ ids.ID, blk snowman.Block) int {
	return ids.IDLen + len(blk.Bytes()) + constants.PointerOverhead
}

// Engine implements the Engine interface by attempting to fetch all
// Engine dependencies.
type Engine struct {
	Config
	*metrics

	// list of NoOpsHandler for messages dropped by engine
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.AncestorsHandler
	common.AppHandler
	validators.Connector

	requestID uint32

	// track outstanding preference requests
	polls poll.Set

	// blocks that have we have sent get requests for but haven't yet received
	blkReqs            *bimap.BiMap[common.Request, ids.ID]
	blkReqSourceMetric map[common.Request]prometheus.Counter

	// blocks that are queued to be issued to consensus once missing dependencies are fetched
	// Block ID --> Block
	pending map[ids.ID]snowman.Block

	// Block ID --> Parent ID
	unverifiedIDToAncestor ancestor.Tree

	// Block ID --> Block.
	//
	// A block is put into this cache if its ancestry was fetched, but the block
	// was not able to be issued. A block may fail to be issued if verification
	// on the block or one of its ancestors returns an error.
	unverifiedBlockCache cache.Cacher[ids.ID, snowman.Block]

	// acceptedFrontiers of the other validators of this chain
	acceptedFrontiers tracker.Accepted

	// operations that are blocked on a block being issued. This could be
	// issuing another block, responding to a query, or applying votes to consensus
	blocked *job.Scheduler[ids.ID]

	// number of times build block needs to be called once the number of
	// processing blocks has gone below the optimal number.
	pendingBuildBlocks int
}

func New(config Config) (*Engine, error) {
	config.Ctx.Log.Info("initializing consensus engine")

	nonVerifiedCache, err := metercacher.New[ids.ID, snowman.Block](
		"non_verified_cache",
		config.Ctx.Registerer,
		lru.NewSizedCache(nonVerifiedCacheSize, cachedBlockSize),
	)
	if err != nil {
		return nil, err
	}

	acceptedFrontiers := tracker.NewAccepted()
	config.Validators.RegisterSetCallbackListener(config.Ctx.SubnetID, acceptedFrontiers)

	factory, err := poll.NewEarlyTermFactory(
		config.Params.AlphaPreference,
		config.Params.AlphaConfidence,
		config.Ctx.Registerer,
		config.Consensus,
	)
	if err != nil {
		return nil, err
	}
	polls, err := poll.NewSet(
		factory,
		config.Ctx.Log,
		config.Ctx.Registerer,
	)
	if err != nil {
		return nil, err
	}

	metrics, err := newMetrics(config.Ctx.Registerer)
	if err != nil {
		return nil, err
	}

	e := &Engine{
		Config:                      config,
		metrics:                     metrics,
		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(config.Ctx.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(config.Ctx.Log),
		AcceptedFrontierHandler:     common.NewNoOpAcceptedFrontierHandler(config.Ctx.Log),
		AcceptedHandler:             common.NewNoOpAcceptedHandler(config.Ctx.Log),
		AncestorsHandler:            common.NewNoOpAncestorsHandler(config.Ctx.Log),
		AppHandler:                  config.VM,
		Connector:                   config.VM,
		pending:                     make(map[ids.ID]snowman.Block),
		unverifiedIDToAncestor:      ancestor.NewTree(),
		unverifiedBlockCache:        nonVerifiedCache,
		acceptedFrontiers:           acceptedFrontiers,
		blocked:                     job.NewScheduler[ids.ID](),
		polls:                       polls,
		blkReqs:                     bimap.New[common.Request, ids.ID](),
		blkReqSourceMetric:          make(map[common.Request]prometheus.Counter),
	}

	return e, nil
}

func (e *Engine) Gossip(ctx context.Context) error {
	lastAcceptedID, lastAcceptedHeight := e.Consensus.LastAccepted()
	if numProcessing := e.Consensus.NumProcessing(); numProcessing != 0 {
		e.Ctx.Log.Debug("skipping block gossip",
			zap.String("reason", "blocks currently processing"),
			zap.Int("numProcessing", numProcessing),
		)

		// repoll is called here to unblock the engine if it previously errored
		// when attempting to issue a query. This can happen if a subnet was
		// temporarily misconfigured and there were no validators.
		e.repoll(ctx)
		return nil
	}

	e.Ctx.Log.Verbo("sampling from validators",
		zap.Stringer("validators", e.Validators),
	)

	// Uniform sampling is used here to reduce bandwidth requirements of
	// nodes with a large amount of stake weight.
	vdrID, ok := e.ConnectedValidators.SampleValidator()
	if !ok {
		e.Ctx.Log.Debug("skipping block gossip",
			zap.String("reason", "no connected validators"),
		)
		return nil
	}

	nextHeightToAccept, err := math.Add(lastAcceptedHeight, 1)
	if err != nil {
		e.Ctx.Log.Error("skipping block gossip",
			zap.String("reason", "block height overflow"),
			zap.Stringer("blkID", lastAcceptedID),
			zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
			zap.Error(err),
		)
		return nil
	}

	e.requestID++
	e.Sender.SendPullQuery(
		ctx,
		set.Of(vdrID),
		e.requestID,
		e.Consensus.Preference(),
		nextHeightToAccept,
	)
	return nil
}

func (e *Engine) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkBytes []byte) error {
	blk, err := e.VM.ParseBlock(ctx, blkBytes)
	if err != nil {
		if e.Ctx.Log.Enabled(logging.Verbo) {
			e.Ctx.Log.Verbo("failed to parse block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Binary("block", blkBytes),
				zap.Error(err),
			)
		} else {
			e.Ctx.Log.Debug("failed to parse block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Error(err),
			)
		}
		// because GetFailed doesn't utilize the assumption that we actually
		// sent a Get message, we can safely call GetFailed here to potentially
		// abandon the request.
		return e.GetFailed(ctx, nodeID, requestID)
	}

	var (
		req = common.Request{
			NodeID:    nodeID,
			RequestID: requestID,
		}
		issuedMetric prometheus.Counter
	)
	switch expectedBlkID, ok := e.blkReqs.GetValue(req); {
	case ok:
		actualBlkID := blk.ID()
		if actualBlkID != expectedBlkID {
			e.Ctx.Log.Debug("incorrect block returned in Put",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Stringer("blkID", actualBlkID),
				zap.Stringer("expectedBlkID", expectedBlkID),
			)
			// We assume that [blk] is useless because it doesn't match what we
			// expected.
			return e.GetFailed(ctx, nodeID, requestID)
		}

		issuedMetric = e.blkReqSourceMetric[req]
	default:
		// This can happen if this block was provided to this engine while a Get
		// request was outstanding. For example, the block may have been locally
		// built or the node may have received a PushQuery with this block.
		//
		// Note: It is still possible this block will be issued here, because
		// the block may have previously failed verification.
		issuedMetric = e.metrics.issued.WithLabelValues(unknownSource)
	}

	if !e.shouldIssueBlock(blk) {
		e.metrics.numUselessPutBytes.Add(float64(len(blkBytes)))
	}

	// issue the block into consensus. If the block has already been issued,
	// this will be a noop. If this block has missing dependencies, vdr will
	// receive requests to fill the ancestry. dependencies that have already
	// been fetched, but with missing dependencies themselves won't be requested
	// from the vdr.
	if err := e.issueFrom(ctx, nodeID, blk, issuedMetric); err != nil {
		return err
	}
	return e.executeDeferredWork(ctx)
}

func (e *Engine) GetFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// We don't assume that this function is called after a failed Get message.
	// Check to see if we have an outstanding request and also get what the
	// request was for if it exists.
	req := common.Request{
		NodeID:    nodeID,
		RequestID: requestID,
	}
	blkID, ok := e.blkReqs.DeleteKey(req)
	if !ok {
		e.Ctx.Log.Debug("unexpected GetFailed",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}
	delete(e.blkReqSourceMetric, req)

	// Because the get request was dropped, we no longer expect blkID to be
	// issued.
	if err := e.blocked.Abandon(ctx, blkID); err != nil {
		return err
	}
	return e.executeDeferredWork(ctx)
}

func (e *Engine) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID, requestedHeight uint64) error {
	e.sendChits(ctx, nodeID, requestID, requestedHeight)

	issuedMetric := e.metrics.issued.WithLabelValues(pushGossipSource)

	// Try to issue [blkID] to consensus.
	// If we're missing an ancestor, request it from [vdr]
	if err := e.issueFromByID(ctx, nodeID, blkID, issuedMetric); err != nil {
		return err
	}

	return e.executeDeferredWork(ctx)
}

func (e *Engine) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkBytes []byte, requestedHeight uint64) error {
	e.sendChits(ctx, nodeID, requestID, requestedHeight)

	blk, err := e.VM.ParseBlock(ctx, blkBytes)
	// If parsing fails, we just drop the request, as we didn't ask for it
	if err != nil {
		if e.Ctx.Log.Enabled(logging.Verbo) {
			e.Ctx.Log.Verbo("failed to parse block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Binary("block", blkBytes),
				zap.Error(err),
			)
		} else {
			e.Ctx.Log.Debug("failed to parse block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Error(err),
			)
		}
		return nil
	}

	if !e.shouldIssueBlock(blk) {
		e.metrics.numUselessPushQueryBytes.Add(float64(len(blkBytes)))
	}

	issuedMetric := e.metrics.issued.WithLabelValues(pushGossipSource)

	// issue the block into consensus. If the block has already been issued,
	// this will be a noop. If this block has missing dependencies, nodeID will
	// receive requests to fill the ancestry. dependencies that have already
	// been fetched, but with missing dependencies themselves won't be requested
	// from the vdr.
	if err := e.issueFrom(ctx, nodeID, blk, issuedMetric); err != nil {
		return err
	}

	return e.executeDeferredWork(ctx)
}

func (e *Engine) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID, acceptedHeight uint64) error {
	e.acceptedFrontiers.SetLastAccepted(nodeID, acceptedID, acceptedHeight)

	e.Ctx.Log.Verbo("called Chits for the block",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Stringer("preferredID", preferredID),
		zap.Stringer("preferredIDAtHeight", preferredIDAtHeight),
		zap.Stringer("acceptedID", acceptedID),
		zap.Uint64("acceptedHeight", acceptedHeight),
	)

	issuedMetric := e.metrics.issued.WithLabelValues(pullGossipSource)
	if err := e.issueFromByID(ctx, nodeID, preferredID, issuedMetric); err != nil {
		return err
	}

	var (
		preferredIDAtHeightShouldBlock bool
		// Invariant: The order of [responseOptions] must be [preferredID] then
		// (optionally) [preferredIDAtHeight]. During vote application, the
		// first vote that can be applied will be used. So, the votes should be
		// populated in order of decreasing height.
		responseOptions = []ids.ID{preferredID}
	)
	if preferredID != preferredIDAtHeight {
		if err := e.issueFromByID(ctx, nodeID, preferredIDAtHeight, issuedMetric); err != nil {
			return err
		}
		preferredIDAtHeightShouldBlock = e.canDependOn(preferredIDAtHeight)
		responseOptions = append(responseOptions, preferredIDAtHeight)
	}

	// Will record chits once [preferredID] and [preferredIDAtHeight] have been
	// issued into consensus
	v := &voter{
		e:               e,
		nodeID:          nodeID,
		requestID:       requestID,
		responseOptions: responseOptions,
	}

	// Wait until [preferredID] and [preferredIDAtHeight] have been issued to
	// consensus before applying this chit.
	var deps []ids.ID
	if e.canDependOn(preferredID) {
		deps = append(deps, preferredID)
	}
	if preferredIDAtHeightShouldBlock {
		deps = append(deps, preferredIDAtHeight)
	}

	if err := e.blocked.Schedule(ctx, v, deps...); err != nil {
		return err
	}
	return e.executeDeferredWork(ctx)
}

func (e *Engine) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	lastAcceptedID, lastAcceptedHeight, ok := e.acceptedFrontiers.LastAccepted(nodeID)
	if ok {
		return e.Chits(ctx, nodeID, requestID, lastAcceptedID, lastAcceptedID, lastAcceptedID, lastAcceptedHeight)
	}

	v := &voter{
		e:         e,
		nodeID:    nodeID,
		requestID: requestID,
	}
	if err := e.blocked.Schedule(ctx, v); err != nil {
		return err
	}
	return e.executeDeferredWork(ctx)
}

func (e *Engine) Shutdown(ctx context.Context) error {
	e.Ctx.Log.Info("shutting down consensus engine")

	e.Ctx.Lock.Lock()
	defer e.Ctx.Lock.Unlock()

	return e.VM.Shutdown(ctx)
}

func (e *Engine) Notify(ctx context.Context, msg common.Message) error {
	switch msg {
	case common.PendingTxs:
		// the pending txs message means we should attempt to build a block.
		e.pendingBuildBlocks++
		return e.executeDeferredWork(ctx)
	case common.StateSyncDone:
		e.Ctx.StateSyncing.Set(false)
		return nil
	default:
		e.Ctx.Log.Warn("received an unexpected message from the VM",
			zap.Stringer("messageString", msg),
		)
		return nil
	}
}

func (e *Engine) Context() *snow.ConsensusContext {
	return e.Ctx
}

func (e *Engine) Start(ctx context.Context, startReqID uint32) error {
	e.requestID = startReqID
	lastAcceptedID, err := e.VM.LastAccepted(ctx)
	if err != nil {
		return err
	}

	lastAccepted, err := e.VM.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		e.Ctx.Log.Error("failed to get last accepted block",
			zap.Error(err),
		)
		return err
	}

	// initialize consensus to the last accepted blockID
	lastAcceptedHeight := lastAccepted.Height()
	if err := e.Consensus.Initialize(e.Ctx, e.Params, lastAcceptedID, lastAcceptedHeight, lastAccepted.Timestamp()); err != nil {
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
			if err := e.VM.SetPreference(ctx, lastAcceptedID); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			issuedMetric := e.metrics.issued.WithLabelValues(builtSource)
			for _, blk := range options {
				// note that deliver will set the VM's preference
				if err := e.deliver(ctx, e.Ctx.NodeID, blk, false, issuedMetric); err != nil {
					return err
				}
			}
		}
	} else if err := e.VM.SetPreference(ctx, lastAcceptedID); err != nil {
		return err
	}

	e.Ctx.Log.Info("starting consensus",
		zap.Stringer("lastAcceptedID", lastAcceptedID),
		zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
	)
	e.metrics.bootstrapFinished.Set(1)

	e.Ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.NormalOp,
	})
	if err := e.VM.SetState(ctx, snow.NormalOp); err != nil {
		return fmt.Errorf("failed to notify VM that consensus is starting: %w",
			err)
	}
	return e.executeDeferredWork(ctx)
}

func (e *Engine) HealthCheck(ctx context.Context) (interface{}, error) {
	e.Ctx.Lock.Lock()
	defer e.Ctx.Lock.Unlock()

	e.Ctx.Log.Verbo("running health check",
		zap.Uint32("requestID", e.requestID),
		zap.Stringer("polls", e.polls),
		zap.Reflect("outstandingBlockRequests", e.blkReqs),
		zap.Int("numMissingDependencies", e.blocked.NumDependencies()),
		zap.Int("pendingBuildBlocks", e.pendingBuildBlocks),
	)

	consensusIntf, consensusErr := e.Consensus.HealthCheck(ctx)
	vmIntf, vmErr := e.VM.HealthCheck(ctx)
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
	return intf, fmt.Errorf("vm: %w ; consensus: %w", vmErr, consensusErr)
}

func (e *Engine) executeDeferredWork(ctx context.Context) error {
	if err := e.buildBlocks(ctx); err != nil {
		return err
	}

	e.metrics.numRequests.Set(float64(e.blkReqs.Len()))
	e.metrics.numBlocked.Set(float64(len(e.pending)))
	e.metrics.numBlockers.Set(float64(e.blocked.NumDependencies()))
	e.metrics.numNonVerifieds.Set(float64(e.unverifiedIDToAncestor.Len()))
	return nil
}

func (e *Engine) getBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	if blk, ok := e.pending[blkID]; ok {
		return blk, nil
	}
	if blk, ok := e.unverifiedBlockCache.Get(blkID); ok {
		return blk, nil
	}

	return e.VM.GetBlock(ctx, blkID)
}

func (e *Engine) sendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32, requestedHeight uint64) {
	lastAcceptedID, lastAcceptedHeight := e.Consensus.LastAccepted()
	// If we aren't fully verifying blocks, only vote for blocks that are widely
	// preferred by the validator set.
	if e.Ctx.StateSyncing.Get() || e.Config.PartialSync {
		acceptedAtHeight, err := e.VM.GetBlockIDAtHeight(ctx, requestedHeight)
		if err != nil {
			// Because we only return accepted state here, it's fairly likely
			// that the requested height is higher than the last accepted block.
			// That means that this code path is actually quite common.
			e.Ctx.Log.Debug("unable to retrieve accepted block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint64("requestedHeight", requestedHeight),
				zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
				zap.Stringer("lastAcceptedID", lastAcceptedID),
				zap.Error(err),
			)
			acceptedAtHeight = lastAcceptedID
		}
		e.Sender.SendChits(ctx, nodeID, requestID, lastAcceptedID, acceptedAtHeight, lastAcceptedID, lastAcceptedHeight)
		return
	}

	var (
		preference         = e.Consensus.Preference()
		preferenceAtHeight ids.ID
	)
	if requestedHeight < lastAcceptedHeight {
		var err error
		preferenceAtHeight, err = e.VM.GetBlockIDAtHeight(ctx, requestedHeight)
		if err != nil {
			// If this chain is pruning historical blocks, it's expected for a
			// node to be unable to fetch some block IDs. In this case, we fall
			// back to returning the last accepted ID.
			//
			// Because it is possible for a byzantine node to spam requests at
			// old heights on a pruning network, we log this as debug. However,
			// this case is unexpected to be hit by correct peers.
			e.Ctx.Log.Debug("unable to retrieve accepted block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint64("requestedHeight", requestedHeight),
				zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
				zap.Stringer("lastAcceptedID", lastAcceptedID),
				zap.Error(err),
			)
			e.numMissingAcceptedBlocks.Inc()

			preferenceAtHeight = lastAcceptedID
		}
	} else {
		var ok bool
		preferenceAtHeight, ok = e.Consensus.PreferenceAtHeight(requestedHeight)
		if !ok {
			e.Ctx.Log.Debug("processing block not found",
				zap.Stringer("nodeID", nodeID),
				zap.Uint64("requestedHeight", requestedHeight),
				zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
				zap.Stringer("preferredID", preference),
			)
			// If the requested height is higher than our preferred tip, we
			// don't prefer anything at the requested height yet.
			preferenceAtHeight = preference
		}
	}
	e.Sender.SendChits(ctx, nodeID, requestID, preference, preferenceAtHeight, lastAcceptedID, lastAcceptedHeight)
}

// Build blocks if they have been requested and the number of processing blocks
// is less than optimal.
func (e *Engine) buildBlocks(ctx context.Context) error {
	for e.pendingBuildBlocks > 0 && e.Consensus.NumProcessing() < e.Params.OptimalProcessing {
		e.pendingBuildBlocks--

		blk, err := e.VM.BuildBlock(ctx)
		if err != nil {
			e.Ctx.Log.Debug("failed building block",
				zap.Error(err),
			)
			e.numBuildsFailed.Inc()
			return nil
		}
		e.numBuilt.Inc()

		blockTimeSkew := time.Since(blk.Timestamp())
		e.blockTimeSkew.Add(float64(blockTimeSkew))

		// The newly created block should be built on top of the preferred block.
		// Otherwise, the new block doesn't have the best chance of being confirmed.
		parentID := blk.Parent()
		if pref := e.Consensus.Preference(); parentID != pref {
			e.Ctx.Log.Warn("built block with unexpected parent",
				zap.Stringer("expectedParentID", pref),
				zap.Stringer("parentID", parentID),
			)
		}

		issuedMetric := e.metrics.issued.WithLabelValues(builtSource)
		if err := e.issueWithAncestors(ctx, blk, issuedMetric); err != nil {
			return err
		}

		// TODO: Technically this may incorrectly log a warning if the block
		// that was just built caused votes to be applied such that the block
		// was rejected or was accepted along with one of its children. This
		// should be cleaned up to never produce an invalid warning.
		if e.canIssueChildOn(blk.ID()) {
			e.Ctx.Log.Verbo("successfully issued new block from the VM")
		} else {
			e.Ctx.Log.Warn("block that was just built is not extendable")
		}
	}
	return nil
}

// Issue another poll to the network, asking what it prefers given the block we prefer.
// Helps move consensus along.
func (e *Engine) repoll(ctx context.Context) {
	// if we are issuing a repoll, we should gossip our current preferences to
	// propagate the most likely branch as quickly as possible
	prefID := e.Consensus.Preference()

	for i := e.polls.Len(); i < e.Params.ConcurrentRepolls; i++ {
		e.sendQuery(ctx, prefID, nil, false)
	}
}

// issueFromByID attempts to issue the branch ending with a block [blkID] into
// consensus.
// If we do not have [blkID], request it.
func (e *Engine) issueFromByID(
	ctx context.Context,
	nodeID ids.NodeID,
	blkID ids.ID,
	issuedMetric prometheus.Counter,
) error {
	blk, err := e.getBlock(ctx, blkID)
	if err != nil {
		// If the block is not locally available, request it from the peer.
		e.sendRequest(ctx, nodeID, blkID, issuedMetric)
		return nil //nolint:nilerr
	}
	return e.issueFrom(ctx, nodeID, blk, issuedMetric)
}

// issueFrom attempts to issue the branch ending with block [blkID] to
// consensus.
// If a dependency is missing, it will be requested it from [nodeID].
func (e *Engine) issueFrom(
	ctx context.Context,
	nodeID ids.NodeID,
	blk snowman.Block,
	issuedMetric prometheus.Counter,
) error {
	// issue [blk] and its ancestors to consensus.
	blkID := blk.ID()
	for e.shouldIssueBlock(blk) {
		err := e.issue(ctx, nodeID, blk, false, issuedMetric)
		if err != nil {
			return err
		}

		// If we don't have this ancestor, request it from [nodeID]
		blkID = blk.Parent()
		blk, err = e.getBlock(ctx, blkID)
		if err != nil {
			// If the block is not locally available, request it from the peer.
			e.sendRequest(ctx, nodeID, blkID, issuedMetric)
			return nil //nolint:nilerr
		}
	}

	// Remove any outstanding requests for this block
	if req, ok := e.blkReqs.DeleteValue(blkID); ok {
		delete(e.blkReqSourceMetric, req)
	}

	// If this block isn't pending, make sure nothing is blocked on it.
	if _, isPending := e.pending[blkID]; !isPending {
		return e.blocked.Abandon(ctx, blkID)
	}
	return nil
}

// issueWithAncestors attempts to issue the branch ending with [blk] to
// consensus.
// If a dependency is missing and the dependency hasn't been requested, the
// issuance will be abandoned.
func (e *Engine) issueWithAncestors(
	ctx context.Context,
	blk snowman.Block,
	issuedMetric prometheus.Counter,
) error {
	blkID := blk.ID()
	// issue [blk] and its ancestors into consensus
	for e.shouldIssueBlock(blk) {
		err := e.issue(ctx, e.Ctx.NodeID, blk, true, issuedMetric)
		if err != nil {
			return err
		}
		blkID = blk.Parent()
		blk, err = e.getBlock(ctx, blkID)
		if err != nil {
			break
		}
	}

	// There's an outstanding request for this block. We can wait for that
	// request to succeed or fail.
	if e.blkReqs.HasValue(blkID) {
		return nil
	}

	// If the block wasn't already issued, we have no reason to expect that it
	// will be able to be issued.
	return e.blocked.Abandon(ctx, blkID)
}

// Issue [blk] to consensus once its ancestors have been issued.
// If [push] is true, a push query will be used. Otherwise, a pull query will be
// used.
func (e *Engine) issue(
	ctx context.Context,
	nodeID ids.NodeID,
	blk snowman.Block,
	push bool,
	issuedMetric prometheus.Counter,
) error {
	blkID := blk.ID()

	// mark that the block is queued to be added to consensus once its ancestors have been
	e.pending[blkID] = blk

	// Remove any outstanding requests for this block
	if req, ok := e.blkReqs.DeleteValue(blkID); ok {
		delete(e.blkReqSourceMetric, req)
	}

	// Will add [blk] to consensus once its ancestors have been
	i := &issuer{
		e:            e,
		nodeID:       nodeID,
		blk:          blk,
		push:         push,
		issuedMetric: issuedMetric,
	}

	// We know that shouldIssueBlock(blk) is true. This means that parent is
	// either the last accepted block or is not decided.
	var deps []ids.ID
	if parentID := blk.Parent(); !e.canIssueChildOn(parentID) {
		e.Ctx.Log.Verbo("block waiting for parent to be issued",
			zap.Stringer("blkID", blkID),
			zap.Stringer("parentID", parentID),
		)
		deps = append(deps, parentID)
	}

	return e.blocked.Schedule(ctx, i, deps...)
}

// Request that [vdr] send us block [blkID]
func (e *Engine) sendRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	blkID ids.ID,
	issuedMetric prometheus.Counter,
) {
	// There is already an outstanding request for this block
	if e.blkReqs.HasValue(blkID) {
		return
	}

	e.requestID++
	req := common.Request{
		NodeID:    nodeID,
		RequestID: e.requestID,
	}
	e.blkReqs.Put(req, blkID)
	e.blkReqSourceMetric[req] = issuedMetric

	e.Ctx.Log.Verbo("sending Get request",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", e.requestID),
		zap.Stringer("blkID", blkID),
	)
	e.Sender.SendGet(ctx, nodeID, e.requestID, blkID)
}

// Send a query for this block. If push is set to true, blkBytes will be used to
// send a PushQuery. Otherwise, blkBytes will be ignored and a PullQuery will be
// sent.
func (e *Engine) sendQuery(
	ctx context.Context,
	blkID ids.ID,
	blkBytes []byte,
	push bool,
) {
	if e.abortDueToInsufficientConnectedStake(blkID) {
		return
	}

	e.Ctx.Log.Verbo("sampling from validators",
		zap.Stringer("validators", e.Validators),
	)

	vdrIDs, err := e.Validators.Sample(e.Ctx.SubnetID, e.Params.K)
	if err != nil {
		e.Ctx.Log.Warn("dropped query for block",
			zap.String("reason", "insufficient number of validators"),
			zap.Stringer("blkID", blkID),
			zap.Int("size", e.Params.K),
		)
		return
	}

	_, lastAcceptedHeight := e.Consensus.LastAccepted()
	nextHeightToAccept, err := math.Add(lastAcceptedHeight, 1)
	if err != nil {
		e.Ctx.Log.Error("dropped query for block",
			zap.String("reason", "block height overflow"),
			zap.Stringer("blkID", blkID),
			zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
			zap.Error(err),
		)
		return
	}

	vdrBag := bag.Of(vdrIDs...)
	e.requestID++
	if !e.polls.Add(e.requestID, vdrBag) {
		e.Ctx.Log.Error("dropped query for block",
			zap.String("reason", "failed to add poll"),
			zap.Stringer("blkID", blkID),
			zap.Uint32("requestID", e.requestID),
		)
		return
	}

	vdrSet := set.Of(vdrIDs...)
	if push {
		e.Sender.SendPushQuery(ctx, vdrSet, e.requestID, blkBytes, nextHeightToAccept)
	} else {
		e.Sender.SendPullQuery(ctx, vdrSet, e.requestID, blkID, nextHeightToAccept)
	}
}

func (e *Engine) abortDueToInsufficientConnectedStake(blkID ids.ID) bool {
	stakeConnectedRatio := e.Config.ConnectedValidators.ConnectedPercent()
	minConnectedStakeToQuery := float64(e.Params.AlphaConfidence) / float64(e.Params.K)

	if stakeConnectedRatio < minConnectedStakeToQuery {
		e.Ctx.Log.Debug("dropped query for block",
			zap.String("reason", errInsufficientStake),
			zap.Stringer("blkID", blkID),
			zap.Float64("ratio", stakeConnectedRatio),
		)
		return true
	}
	return false
}

// issue [blk] to consensus
// If [push] is true, a push query will be used. Otherwise, a pull query will be
// used.
func (e *Engine) deliver(
	ctx context.Context,
	nodeID ids.NodeID,
	blk snowman.Block,
	push bool,
	issuedMetric prometheus.Counter,
) error {
	// we are no longer waiting on adding the block to consensus, so it is no
	// longer pending
	blkID := blk.ID()
	delete(e.pending, blkID)

	parentID := blk.Parent()
	if !e.canIssueChildOn(parentID) || e.Consensus.Processing(blkID) {
		// If the parent isn't processing or the last accepted block, then this
		// block is effectively rejected.
		// Additionally, if [blkID] is already in the processing set, it
		// shouldn't be added to consensus again.
		return e.blocked.Abandon(ctx, blkID)
	}

	// By ensuring that the parent is either processing or accepted, it is
	// guaranteed that the parent was successfully verified. This means that
	// calling Verify on this block is allowed.
	blkAdded, err := e.addUnverifiedBlockToConsensus(ctx, nodeID, blk, issuedMetric)
	if err != nil {
		return err
	}
	if !blkAdded {
		return e.blocked.Abandon(ctx, blkID)
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
				blkAdded, err := e.addUnverifiedBlockToConsensus(ctx, nodeID, blk, issuedMetric)
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

	if err := e.VM.SetPreference(ctx, e.Consensus.Preference()); err != nil {
		return err
	}

	// If the block is now preferred, query the network for its preferences
	// with this new block.
	if e.Consensus.IsPreferred(blkID) {
		e.sendQuery(ctx, blkID, blk.Bytes(), push)
	}

	if err := e.blocked.Fulfill(ctx, blkID); err != nil {
		return err
	}
	for _, blk := range added {
		blkID := blk.ID()
		if e.Consensus.IsPreferred(blkID) {
			e.sendQuery(ctx, blkID, blk.Bytes(), push)
		}

		delete(e.pending, blkID)
		if err := e.blocked.Fulfill(ctx, blkID); err != nil {
			return err
		}
		if req, ok := e.blkReqs.DeleteValue(blkID); ok {
			delete(e.blkReqSourceMetric, req)
		}
	}
	for _, blk := range dropped {
		blkID := blk.ID()
		delete(e.pending, blkID)
		if err := e.blocked.Abandon(ctx, blkID); err != nil {
			return err
		}
		if req, ok := e.blkReqs.DeleteValue(blkID); ok {
			delete(e.blkReqSourceMetric, req)
		}
	}

	// It's possible that the blocks we just added to consensus were decided
	// immediately by votes that were pending their issuance. If this is the
	// case, we should not be requesting any chits.
	if e.Consensus.NumProcessing() == 0 {
		return nil
	}

	// If we should issue multiple queries at the same time, we need to repoll
	e.repoll(ctx)
	return nil
}

func (e *Engine) markAsUnverified(blk snowman.Block) {
	// If this block is processing, we don't need to add it to non-verifieds.
	blkID := blk.ID()
	if e.Consensus.Processing(blkID) {
		return
	}
	parentID := blk.Parent()
	// We might still need this block so we can bubble votes to the parent.
	//
	// If the non-verified set contains the parentID, then we know that the
	// parent is not decided and therefore blk is not decided.
	// Similarly, if the parent is processing, then the parent is not decided
	// and therefore blk is not decided.
	if e.unverifiedIDToAncestor.Has(parentID) || e.Consensus.Processing(parentID) {
		e.unverifiedIDToAncestor.Add(blkID, parentID)
		e.unverifiedBlockCache.Put(blkID, blk)
	}
}

// addUnverifiedBlockToConsensus returns whether the block was added and an
// error if one occurred while adding it to consensus.
func (e *Engine) addUnverifiedBlockToConsensus(
	ctx context.Context,
	nodeID ids.NodeID,
	blk snowman.Block,
	issuedMetric prometheus.Counter,
) (bool, error) {
	blkID := blk.ID()
	blkHeight := blk.Height()

	// make sure this block is valid
	if err := blk.Verify(ctx); err != nil {
		e.Ctx.Log.Debug("block verification failed",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("blkID", blkID),
			zap.Uint64("height", blkHeight),
			zap.Error(err),
		)

		// if verify fails, then all descendants are also invalid
		e.markAsUnverified(blk)
		return false, nil
	}

	issuedMetric.Inc()
	e.unverifiedIDToAncestor.Remove(blkID)
	e.unverifiedBlockCache.Evict(blkID)
	e.metrics.issuerStake.Observe(float64(e.Validators.GetWeight(e.Ctx.SubnetID, nodeID)))
	e.Ctx.Log.Verbo("adding block to consensus",
		zap.Stringer("nodeID", nodeID),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", blkHeight),
	)
	return true, e.Consensus.Add(&memoryBlock{
		Block:   blk,
		metrics: e.metrics,
		tree:    e.unverifiedIDToAncestor,
	})
}

// getProcessingAncestor finds [initialVote]'s most recent ancestor that is
// processing in consensus. If no ancestor could be found, false is returned.
//
// Note: If [initialVote] is processing, then [initialVote] will be returned.
func (e *Engine) getProcessingAncestor(initialVote ids.ID) (ids.ID, bool) {
	// If [bubbledVote] != [initialVote], it is guaranteed that [bubbledVote] is
	// in processing. Otherwise, we attempt to iterate through any blocks we
	// have at our disposal as a best-effort mechanism to find a valid ancestor.
	bubbledVote := e.unverifiedIDToAncestor.GetAncestor(initialVote)
	for {
		if e.Consensus.Processing(bubbledVote) {
			e.Ctx.Log.Verbo("applying vote",
				zap.Stringer("initialVoteID", initialVote),
				zap.Stringer("bubbledVoteID", bubbledVote),
			)
			if bubbledVote != initialVote {
				e.numProcessingAncestorFetchesSucceeded.Inc()
			} else {
				e.numProcessingAncestorFetchesUnneeded.Inc()
			}
			return bubbledVote, true
		}

		// If we haven't cached the block, drop [vote].
		blk, ok := e.unverifiedBlockCache.Get(bubbledVote)
		if !ok {
			e.Ctx.Log.Debug("dropping vote",
				zap.String("reason", "ancestor isn't cached"),
				zap.Stringer("initialVoteID", initialVote),
				zap.Stringer("bubbledVoteID", bubbledVote),
			)
			e.numProcessingAncestorFetchesFailed.Inc()
			return ids.Empty, false
		}

		if e.isDecided(blk) {
			e.Ctx.Log.Debug("dropping vote",
				zap.String("reason", "bubbled vote already decided"),
				zap.Stringer("initialVoteID", initialVote),
				zap.Stringer("bubbledVoteID", bubbledVote),
				zap.Uint64("height", blk.Height()),
			)
			e.numProcessingAncestorFetchesDropped.Inc()
			return ids.Empty, false
		}

		bubbledVote = blk.Parent()
	}
}

// shouldIssueBlock returns true if the provided block should be enqueued for
// issuance. If the block is already decided, already enqueued, or has already
// been issued, this function will return false.
func (e *Engine) shouldIssueBlock(blk snowman.Block) bool {
	if e.isDecided(blk) {
		return false
	}

	blkID := blk.ID()
	_, isPending := e.pending[blkID]
	return !isPending && // If the block is already pending, don't issue it again.
		!e.Consensus.Processing(blkID) // If the block was previously issued, don't issue it again.
}

// canDependOn reports true if it is guaranteed for the provided block ID to
// eventually either be fulfilled or abandoned.
func (e *Engine) canDependOn(blkID ids.ID) bool {
	_, isPending := e.pending[blkID]
	return isPending || e.blkReqs.HasValue(blkID)
}

// canIssueChildOn reports true if it is valid for a child of parentID to be
// verified and added to consensus.
func (e *Engine) canIssueChildOn(parentID ids.ID) bool {
	lastAcceptedID, _ := e.Consensus.LastAccepted()
	return parentID == lastAcceptedID || e.Consensus.Processing(parentID)
}

// isDecided reports true if the provided block's height implies that the block
// is either Accepted or Rejected.
func (e *Engine) isDecided(blk snowman.Block) bool {
	height := blk.Height()
	lastAcceptedID, lastAcceptedHeight := e.Consensus.LastAccepted()
	if height <= lastAcceptedHeight {
		return true // block is either accepted or rejected
	}

	// This is guaranteed not to underflow because the above check ensures
	// [height] > 0.
	parentHeight := height - 1
	parentID := blk.Parent()
	return parentHeight == lastAcceptedHeight && parentID != lastAcceptedID // the parent was rejected
}
