// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

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
	"github.com/ava-labs/avalanchego/snow/engine/snowman/ancestor"
	"github.com/ava-labs/avalanchego/snow/event"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/bimap"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	nonVerifiedCacheSize = 64 * units.MiB

	// putGossipPeriod specifies the number of times Gossip will be called per
	// Put gossip. This is done to avoid splitting Gossip into multiple
	// functions and to allow more frequent pull gossip than push gossip.
	putGossipPeriod = 10
)

var _ Engine = (*Transitive)(nil)

func New(config Config) (Engine, error) {
	return newTransitive(config)
}

func cachedBlockSize(_ ids.ID, blk snowman.Block) int {
	return ids.IDLen + len(blk.Bytes()) + constants.PointerOverhead
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

	requestID uint32

	gossipCounter int

	// track outstanding preference requests
	polls poll.Set

	// blocks that have we have sent get requests for but haven't yet received
	blkReqs            *bimap.BiMap[common.Request, ids.ID]
	blkReqSourceMetric map[common.Request]prometheus.Counter

	// blocks that are queued to be issued to consensus once missing dependencies are fetched
	// Block ID --> Block
	pending map[ids.ID]snowman.Block

	// Block ID --> Parent ID
	nonVerifieds ancestor.Tree

	// Block ID --> Block.
	// A block is put into this cache if it was not able to be issued. A block
	// fails to be issued if verification on the block or one of its ancestors
	// occurs.
	nonVerifiedCache cache.Cacher[ids.ID, snowman.Block]

	// acceptedFrontiers of the other validators of this chain
	acceptedFrontiers tracker.Accepted

	// operations that are blocked on a block being issued. This could be
	// issuing another block, responding to a query, or applying votes to consensus
	blocked event.Blocker

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
		cache.NewSizedLRU[ids.ID, snowman.Block](
			nonVerifiedCacheSize,
			cachedBlockSize,
		),
	)
	if err != nil {
		return nil, err
	}

	acceptedFrontiers := tracker.NewAccepted()
	config.Validators.RegisterCallbackListener(config.Ctx.SubnetID, acceptedFrontiers)

	factory := poll.NewEarlyTermNoTraversalFactory(
		config.Params.AlphaPreference,
		config.Params.AlphaConfidence,
	)
	polls, err := poll.NewSet(
		factory,
		config.Ctx.Log,
		"",
		config.Ctx.Registerer,
	)
	if err != nil {
		return nil, err
	}

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
		nonVerifieds:                ancestor.NewTree(),
		nonVerifiedCache:            nonVerifiedCache,
		acceptedFrontiers:           acceptedFrontiers,
		polls:                       polls,
		blkReqs:                     bimap.New[common.Request, ids.ID](),
		blkReqSourceMetric:          make(map[common.Request]prometheus.Counter),
	}

	return t, t.metrics.Initialize("", config.Ctx.Registerer)
}

func (t *Transitive) Gossip(ctx context.Context) error {
	lastAcceptedID, lastAcceptedHeight := t.Consensus.LastAccepted()
	if numProcessing := t.Consensus.NumProcessing(); numProcessing == 0 {
		t.Ctx.Log.Verbo("sampling from validators",
			zap.Stringer("validators", t.Validators),
		)

		// Uniform sampling is used here to reduce bandwidth requirements of
		// nodes with a large amount of stake weight.
		vdrID, ok := t.ConnectedValidators.SampleValidator()
		if !ok {
			t.Ctx.Log.Error("skipping block gossip",
				zap.String("reason", "no connected validators"),
			)
			return nil
		}

		nextHeightToAccept, err := math.Add64(lastAcceptedHeight, 1)
		if err != nil {
			t.Ctx.Log.Error("skipping block gossip",
				zap.String("reason", "block height overflow"),
				zap.Stringer("blkID", lastAcceptedID),
				zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
				zap.Error(err),
			)
			return nil
		}

		t.requestID++
		t.Sender.SendPullQuery(
			ctx,
			set.Of(vdrID),
			t.requestID,
			t.Consensus.Preference(),
			nextHeightToAccept,
		)
	} else {
		t.Ctx.Log.Debug("skipping block gossip",
			zap.String("reason", "blocks currently processing"),
			zap.Int("numProcessing", numProcessing),
		)
	}

	// TODO: Remove periodic push gossip after v1.11.x is activated
	t.gossipCounter++
	t.gossipCounter %= putGossipPeriod
	if t.gossipCounter > 0 {
		return nil
	}

	lastAccepted, err := t.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		t.Ctx.Log.Warn("dropping gossip request",
			zap.String("reason", "block couldn't be loaded"),
			zap.Stringer("blkID", lastAcceptedID),
			zap.Error(err),
		)
		return nil
	}
	t.Ctx.Log.Verbo("gossiping accepted block to the network",
		zap.Stringer("blkID", lastAcceptedID),
	)
	t.Sender.SendGossip(ctx, lastAccepted.Bytes())
	return nil
}

func (t *Transitive) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkBytes []byte) error {
	blk, err := t.VM.ParseBlock(ctx, blkBytes)
	if err != nil {
		if t.Ctx.Log.Enabled(logging.Verbo) {
			t.Ctx.Log.Verbo("failed to parse block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Binary("block", blkBytes),
				zap.Error(err),
			)
		} else {
			t.Ctx.Log.Debug("failed to parse block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Error(err),
			)
		}
		// because GetFailed doesn't utilize the assumption that we actually
		// sent a Get message, we can safely call GetFailed here to potentially
		// abandon the request.
		return t.GetFailed(ctx, nodeID, requestID)
	}

	var (
		req = common.Request{
			NodeID:    nodeID,
			RequestID: requestID,
		}
		issuedMetric prometheus.Counter
	)
	switch expectedBlkID, ok := t.blkReqs.GetValue(req); {
	case ok:
		actualBlkID := blk.ID()
		if actualBlkID != expectedBlkID {
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

		issuedMetric = t.blkReqSourceMetric[req]
	case requestID == constants.GossipMsgRequestID:
		issuedMetric = t.metrics.issued.WithLabelValues(putGossipSource)
	default:
		// This can happen if this block was provided to this engine while a Get
		// request was outstanding. For example, the block may have been locally
		// built or the node may have received a PushQuery with this block.
		//
		// Note: It is still possible this block will be issued here, because
		// the block may have previously failed verification.
		issuedMetric = t.metrics.issued.WithLabelValues(unknownSource)
	}

	if t.wasIssued(blk) {
		t.metrics.numUselessPutBytes.Add(float64(len(blkBytes)))
	}

	// issue the block into consensus. If the block has already been issued,
	// this will be a noop. If this block has missing dependencies, vdr will
	// receive requests to fill the ancestry. dependencies that have already
	// been fetched, but with missing dependencies themselves won't be requested
	// from the vdr.
	if _, err := t.issueFrom(ctx, nodeID, blk, issuedMetric); err != nil {
		return err
	}
	return t.buildBlocks(ctx)
}

func (t *Transitive) GetFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// We don't assume that this function is called after a failed Get message.
	// Check to see if we have an outstanding request and also get what the
	// request was for if it exists.
	req := common.Request{
		NodeID:    nodeID,
		RequestID: requestID,
	}
	blkID, ok := t.blkReqs.DeleteKey(req)
	if !ok {
		t.Ctx.Log.Debug("unexpected GetFailed",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}
	delete(t.blkReqSourceMetric, req)

	// Because the get request was dropped, we no longer expect blkID to be issued.
	t.blocked.Abandon(ctx, blkID)
	t.metrics.numRequests.Set(float64(t.blkReqs.Len()))
	t.metrics.numBlockers.Set(float64(t.blocked.Len()))
	return t.buildBlocks(ctx)
}

func (t *Transitive) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID, requestedHeight uint64) error {
	t.sendChits(ctx, nodeID, requestID, requestedHeight)

	issuedMetric := t.metrics.issued.WithLabelValues(pushGossipSource)

	// Try to issue [blkID] to consensus.
	// If we're missing an ancestor, request it from [vdr]
	if _, err := t.issueFromByID(ctx, nodeID, blkID, issuedMetric); err != nil {
		return err
	}

	return t.buildBlocks(ctx)
}

func (t *Transitive) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkBytes []byte, requestedHeight uint64) error {
	t.sendChits(ctx, nodeID, requestID, requestedHeight)

	blk, err := t.VM.ParseBlock(ctx, blkBytes)
	// If parsing fails, we just drop the request, as we didn't ask for it
	if err != nil {
		if t.Ctx.Log.Enabled(logging.Verbo) {
			t.Ctx.Log.Verbo("failed to parse block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Binary("block", blkBytes),
				zap.Error(err),
			)
		} else {
			t.Ctx.Log.Debug("failed to parse block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Error(err),
			)
		}
		return nil
	}

	if t.wasIssued(blk) {
		t.metrics.numUselessPushQueryBytes.Add(float64(len(blkBytes)))
	}

	issuedMetric := t.metrics.issued.WithLabelValues(pushGossipSource)

	// issue the block into consensus. If the block has already been issued,
	// this will be a noop. If this block has missing dependencies, nodeID will
	// receive requests to fill the ancestry. dependencies that have already
	// been fetched, but with missing dependencies themselves won't be requested
	// from the vdr.
	if _, err := t.issueFrom(ctx, nodeID, blk, issuedMetric); err != nil {
		return err
	}

	return t.buildBlocks(ctx)
}

func (t *Transitive) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID) error {
	t.acceptedFrontiers.SetLastAccepted(nodeID, acceptedID)

	t.Ctx.Log.Verbo("called Chits for the block",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Stringer("preferredID", preferredID),
		zap.Stringer("preferredIDAtHeight", preferredIDAtHeight),
		zap.Stringer("acceptedID", acceptedID),
	)

	issuedMetric := t.metrics.issued.WithLabelValues(pullGossipSource)

	addedPreferred, err := t.issueFromByID(ctx, nodeID, preferredID, issuedMetric)
	if err != nil {
		return err
	}

	var (
		addedPreferredIDAtHeight = addedPreferred
		// Invariant: The order of [responseOptions] must be [preferredID] then
		// (optionally) [preferredIDAtHeight]. During vote application, the
		// first vote that can be applied will be used. So, the votes should be
		// populated in order of decreasing height.
		responseOptions = []ids.ID{preferredID}
	)
	if preferredID != preferredIDAtHeight {
		addedPreferredIDAtHeight, err = t.issueFromByID(ctx, nodeID, preferredIDAtHeight, issuedMetric)
		if err != nil {
			return err
		}
		responseOptions = append(responseOptions, preferredIDAtHeight)
	}

	// Will record chits once [preferredID] and [preferredIDAtHeight] have been
	// issued into consensus
	v := &voter{
		t:               t,
		vdr:             nodeID,
		requestID:       requestID,
		responseOptions: responseOptions,
	}

	// Wait until [preferredID] and [preferredIDAtHeight] have been issued to
	// consensus before applying this chit.
	if !addedPreferred {
		v.deps.Add(preferredID)
	}
	if !addedPreferredIDAtHeight {
		v.deps.Add(preferredIDAtHeight)
	}

	t.blocked.Register(ctx, v)
	t.metrics.numBlockers.Set(float64(t.blocked.Len()))
	return t.buildBlocks(ctx)
}

func (t *Transitive) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	lastAccepted, ok := t.acceptedFrontiers.LastAccepted(nodeID)
	if ok {
		return t.Chits(ctx, nodeID, requestID, lastAccepted, lastAccepted, lastAccepted)
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

func (*Transitive) Halt(context.Context) {}

func (t *Transitive) Shutdown(ctx context.Context) error {
	t.Ctx.Log.Info("shutting down consensus engine")

	t.Ctx.Lock.Lock()
	defer t.Ctx.Lock.Unlock()

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
	t.requestID = startReqID
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
			issuedMetric := t.metrics.issued.WithLabelValues(builtSource)
			for _, blk := range options {
				// note that deliver will set the VM's preference
				if err := t.deliver(ctx, t.Ctx.NodeID, blk, false, issuedMetric); err != nil {
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
	t.Ctx.Lock.Lock()
	defer t.Ctx.Lock.Unlock()

	t.Ctx.Log.Verbo("running health check",
		zap.Uint32("requestID", t.requestID),
		zap.Int("gossipCounter", t.gossipCounter),
		zap.Stringer("polls", t.polls),
		zap.Reflect("outstandingBlockRequests", t.blkReqs),
		zap.Stringer("blockedJobs", &t.blocked),
		zap.Int("pendingBuildBlocks", t.pendingBuildBlocks),
	)

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
	return intf, fmt.Errorf("vm: %w ; consensus: %w", vmErr, consensusErr)
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

func (t *Transitive) sendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32, requestedHeight uint64) {
	lastAcceptedID, lastAcceptedHeight := t.Consensus.LastAccepted()
	// If we aren't fully verifying blocks, only vote for blocks that are widely
	// preferred by the validator set.
	if t.Ctx.StateSyncing.Get() || t.Config.PartialSync {
		acceptedAtHeight, err := t.VM.GetBlockIDAtHeight(ctx, requestedHeight)
		if err != nil {
			// Because we only return accepted state here, it's fairly likely
			// that the requested height is higher than the last accepted block.
			// That means that this code path is actually quite common.
			t.Ctx.Log.Debug("failed fetching accepted block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint64("requestedHeight", requestedHeight),
				zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
				zap.Stringer("lastAcceptedID", lastAcceptedID),
				zap.Error(err),
			)
			acceptedAtHeight = lastAcceptedID
		}
		t.Sender.SendChits(ctx, nodeID, requestID, lastAcceptedID, acceptedAtHeight, lastAcceptedID)
		return
	}

	var (
		preference         = t.Consensus.Preference()
		preferenceAtHeight ids.ID
	)
	if requestedHeight < lastAcceptedHeight {
		var err error
		preferenceAtHeight, err = t.VM.GetBlockIDAtHeight(ctx, requestedHeight)
		if err != nil {
			// If this chain is pruning historical blocks, it's expected for a
			// node to be unable to fetch some block IDs. In this case, we fall
			// back to returning the last accepted ID.
			//
			// Because it is possible for a byzantine node to spam requests at
			// old heights on a pruning network, we log this as debug. However,
			// this case is unexpected to be hit by correct peers.
			t.Ctx.Log.Debug("failed fetching accepted block",
				zap.Stringer("nodeID", nodeID),
				zap.Uint64("requestedHeight", requestedHeight),
				zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
				zap.Stringer("lastAcceptedID", lastAcceptedID),
				zap.Error(err),
			)
			t.numMissingAcceptedBlocks.Inc()

			preferenceAtHeight = lastAcceptedID
		}
	} else {
		var ok bool
		preferenceAtHeight, ok = t.Consensus.PreferenceAtHeight(requestedHeight)
		if !ok {
			t.Ctx.Log.Debug("failed fetching processing block",
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
	t.Sender.SendChits(ctx, nodeID, requestID, preference, preferenceAtHeight, lastAcceptedID)
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

		issuedMetric := t.metrics.issued.WithLabelValues(builtSource)
		added, err := t.issueWithAncestors(ctx, blk, issuedMetric)
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
		t.sendQuery(ctx, prefID, nil, false)
	}
}

// issueFromByID attempts to issue the branch ending with a block [blkID] into consensus.
// If we do not have [blkID], request it.
// Returns true if the block is processing in consensus or is decided.
func (t *Transitive) issueFromByID(
	ctx context.Context,
	nodeID ids.NodeID,
	blkID ids.ID,
	issuedMetric prometheus.Counter,
) (bool, error) {
	blk, err := t.GetBlock(ctx, blkID)
	if err != nil {
		t.sendRequest(ctx, nodeID, blkID, issuedMetric)
		return false, nil
	}
	return t.issueFrom(ctx, nodeID, blk, issuedMetric)
}

// issueFrom attempts to issue the branch ending with block [blkID] to consensus.
// Returns true if the block is processing in consensus or is decided.
// If a dependency is missing, request it from [vdr].
func (t *Transitive) issueFrom(
	ctx context.Context,
	nodeID ids.NodeID,
	blk snowman.Block,
	issuedMetric prometheus.Counter,
) (bool, error) {
	// issue [blk] and its ancestors to consensus.
	blkID := blk.ID()
	for !t.wasIssued(blk) {
		if err := t.issue(ctx, nodeID, blk, false, issuedMetric); err != nil {
			return false, err
		}

		blkID = blk.Parent()
		var err error
		blk, err = t.GetBlock(ctx, blkID)

		// If we don't have this ancestor, request it from [vdr]
		if err != nil || !blk.Status().Fetched() {
			t.sendRequest(ctx, nodeID, blkID, issuedMetric)
			return false, nil
		}
	}

	// Remove any outstanding requests for this block
	if req, ok := t.blkReqs.DeleteValue(blkID); ok {
		delete(t.blkReqSourceMetric, req)
	}

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
func (t *Transitive) issueWithAncestors(
	ctx context.Context,
	blk snowman.Block,
	issuedMetric prometheus.Counter,
) (bool, error) {
	blkID := blk.ID()
	// issue [blk] and its ancestors into consensus
	status := blk.Status()
	for status.Fetched() && !t.wasIssued(blk) {
		err := t.issue(ctx, t.Ctx.NodeID, blk, true, issuedMetric)
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
	if t.blkReqs.HasValue(blkID) {
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
// If [push] is true, a push query will be used. Otherwise, a pull query will be
// used.
func (t *Transitive) issue(
	ctx context.Context,
	nodeID ids.NodeID,
	blk snowman.Block,
	push bool,
	issuedMetric prometheus.Counter,
) error {
	blkID := blk.ID()

	// mark that the block is queued to be added to consensus once its ancestors have been
	t.pending[blkID] = blk

	// Remove any outstanding requests for this block
	if req, ok := t.blkReqs.DeleteValue(blkID); ok {
		delete(t.blkReqSourceMetric, req)
	}

	// Will add [blk] to consensus once its ancestors have been
	i := &issuer{
		t:            t,
		nodeID:       nodeID,
		blk:          blk,
		issuedMetric: issuedMetric,
		push:         push,
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
func (t *Transitive) sendRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	blkID ids.ID,
	issuedMetric prometheus.Counter,
) {
	// There is already an outstanding request for this block
	if t.blkReqs.HasValue(blkID) {
		return
	}

	t.requestID++
	req := common.Request{
		NodeID:    nodeID,
		RequestID: t.requestID,
	}
	t.blkReqs.Put(req, blkID)
	t.blkReqSourceMetric[req] = issuedMetric

	t.Ctx.Log.Verbo("sending Get request",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", t.requestID),
		zap.Stringer("blkID", blkID),
	)
	t.Sender.SendGet(ctx, nodeID, t.requestID, blkID)

	// Tracks performance statistics
	t.metrics.numRequests.Set(float64(t.blkReqs.Len()))
}

// Send a query for this block. If push is set to true, blkBytes will be used to
// send a PushQuery. Otherwise, blkBytes will be ignored and a PullQuery will be
// sent.
func (t *Transitive) sendQuery(
	ctx context.Context,
	blkID ids.ID,
	blkBytes []byte,
	push bool,
) {
	t.Ctx.Log.Verbo("sampling from validators",
		zap.Stringer("validators", t.Validators),
	)

	vdrIDs, err := t.Validators.Sample(t.Ctx.SubnetID, t.Params.K)
	if err != nil {
		t.Ctx.Log.Error("dropped query for block",
			zap.String("reason", "insufficient number of validators"),
			zap.Stringer("blkID", blkID),
			zap.Int("size", t.Params.K),
		)
		return
	}

	_, lastAcceptedHeight := t.Consensus.LastAccepted()
	nextHeightToAccept, err := math.Add64(lastAcceptedHeight, 1)
	if err != nil {
		t.Ctx.Log.Error("dropped query for block",
			zap.String("reason", "block height overflow"),
			zap.Stringer("blkID", blkID),
			zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
			zap.Error(err),
		)
		return
	}

	vdrBag := bag.Of(vdrIDs...)
	t.requestID++
	if !t.polls.Add(t.requestID, vdrBag) {
		t.Ctx.Log.Error("dropped query for block",
			zap.String("reason", "failed to add poll"),
			zap.Stringer("blkID", blkID),
			zap.Uint32("requestID", t.requestID),
		)
		return
	}

	vdrSet := set.Of(vdrIDs...)
	if push {
		t.Sender.SendPushQuery(ctx, vdrSet, t.requestID, blkBytes, nextHeightToAccept)
	} else {
		t.Sender.SendPullQuery(ctx, vdrSet, t.requestID, blkID, nextHeightToAccept)
	}
}

// issue [blk] to consensus
// If [push] is true, a push query will be used. Otherwise, a pull query will be
// used.
func (t *Transitive) deliver(
	ctx context.Context,
	nodeID ids.NodeID,
	blk snowman.Block,
	push bool,
	issuedMetric prometheus.Counter,
) error {
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
	blkAdded, err := t.addUnverifiedBlockToConsensus(ctx, nodeID, blk, issuedMetric)
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
				blkAdded, err := t.addUnverifiedBlockToConsensus(ctx, nodeID, blk, issuedMetric)
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
		t.sendQuery(ctx, blkID, blk.Bytes(), push)
	}

	t.blocked.Fulfill(ctx, blkID)
	for _, blk := range added {
		blkID := blk.ID()
		if t.Consensus.IsPreferred(blk) {
			t.sendQuery(ctx, blkID, blk.Bytes(), push)
		}

		t.removeFromPending(blk)
		t.blocked.Fulfill(ctx, blkID)
		if req, ok := t.blkReqs.DeleteValue(blkID); ok {
			delete(t.blkReqSourceMetric, req)
		}
	}
	for _, blk := range dropped {
		blkID := blk.ID()
		t.removeFromPending(blk)
		t.blocked.Abandon(ctx, blkID)
		if req, ok := t.blkReqs.DeleteValue(blkID); ok {
			delete(t.blkReqSourceMetric, req)
		}
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
func (t *Transitive) addUnverifiedBlockToConsensus(
	ctx context.Context,
	nodeID ids.NodeID,
	blk snowman.Block,
	issuedMetric prometheus.Counter,
) (bool, error) {
	blkID := blk.ID()
	blkHeight := blk.Height()

	// make sure this block is valid
	if err := blk.Verify(ctx); err != nil {
		t.Ctx.Log.Debug("block verification failed",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("blkID", blkID),
			zap.Uint64("height", blkHeight),
			zap.Error(err),
		)

		// if verify fails, then all descendants are also invalid
		t.addToNonVerifieds(blk)
		return false, nil
	}

	issuedMetric.Inc()
	t.nonVerifieds.Remove(blkID)
	t.nonVerifiedCache.Evict(blkID)
	t.metrics.numNonVerifieds.Set(float64(t.nonVerifieds.Len()))
	t.metrics.issuerStake.Observe(float64(t.Validators.GetWeight(t.Ctx.SubnetID, nodeID)))
	t.Ctx.Log.Verbo("adding block to consensus",
		zap.Stringer("nodeID", nodeID),
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", blkHeight),
	)
	return true, t.Consensus.Add(ctx, &memoryBlock{
		Block:   blk,
		metrics: &t.metrics,
		tree:    t.nonVerifieds,
	})
}
