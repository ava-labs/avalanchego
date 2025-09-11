// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/bootstrapper"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/bootstrap/interval"
	"github.com/ava-labs/avalanchego/utils/bimap"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
)

const (
	// Delay bootstrapping to avoid potential CPU burns
	bootstrappingDelay = 10 * time.Second

	// statusUpdateFrequency is how many containers should be processed between
	// logs
	statusUpdateFrequency = 5000

	// maxOutstandingBroadcastRequests is the maximum number of requests to have
	// outstanding when broadcasting.
	maxOutstandingBroadcastRequests = 50

	// minimumLogInterval is the minimum time between log entries to avoid noise
	minimumLogInterval = 5 * time.Second

	epsilon = 1e-6 // small amount to add to time to avoid division by 0
)

var (
	_ common.BootstrapableEngine = (*Bootstrapper)(nil)

	errUnexpectedTimeout = errors.New("unexpected timeout fired")
)

// bootstrapper repeatedly performs the bootstrapping protocol.
//
//  1. Wait until a sufficient amount of stake is connected.
//  2. Sample a small number of nodes to get the last accepted block ID
//  3. Verify against the full network that the last accepted block ID received
//     in step 2 is an accepted block.
//  4. Sync the full ancestry of the last accepted block.
//  5. Execute all the fetched blocks that haven't already been executed.
//  6. Restart the bootstrapping protocol until the number of blocks being
//     accepted during a bootstrapping round stops decreasing.
//
// Note: Because of step 6, the bootstrapping protocol will generally be
// performed multiple times.
//
// Invariant: The VM is not guaranteed to be initialized until Start has been
// called, so it must be guaranteed the VM is not used until after Start.
type Bootstrapper struct {
	Config

	*metrics
	TimeoutRegistrar common.TimeoutRegistrar
	// list of NoOpsHandler for messages dropped by bootstrapper
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler
	common.AppHandler

	requestID uint32 // Tracks the last requestID that was used in a request

	started   bool
	restarted bool

	minority bootstrapper.Poll
	majority bootstrapper.Poll

	// Greatest height of the blocks passed in startSyncing
	tipHeight uint64
	// Height of the last accepted block when bootstrapping starts
	startingHeight uint64
	// Number of blocks that were fetched on startSyncing
	initiallyFetched uint64
	// Time that startSyncing was last called
	startTime time.Time
	// Time of the last progress update for accurate ETA calculation
	lastProgressUpdateTime time.Time

	// ETA tracker for more accurate time estimates
	etaTracker *timer.EtaTracker

	// tracks which validators were asked for which containers in which requests
	outstandingRequests     *bimap.BiMap[common.Request, ids.ID]
	outstandingRequestTimes map[common.Request]time.Time

	// number of state transitions executed
	executedStateTransitions uint64
	awaitingTimeout          bool

	tree            *interval.Tree
	missingBlockIDs set.Set[ids.ID]

	// bootstrappedOnce ensures that the [Bootstrapped] callback is only invoked
	// once, even if bootstrapping is retried.
	bootstrappedOnce sync.Once

	// Called when bootstrapping is done on a specific chain
	onFinished func(ctx context.Context, lastReqID uint32) error

	nonVerifyingParser block.Parser
}

func New(config Config, onFinished func(ctx context.Context, lastReqID uint32) error) (*Bootstrapper, error) {
	metrics, err := newMetrics(config.Ctx.Registerer)
	bs := &Bootstrapper{
		nonVerifyingParser:          config.NonVerifyingParse,
		Config:                      config,
		metrics:                     metrics,
		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(config.Ctx.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(config.Ctx.Log),
		PutHandler:                  common.NewNoOpPutHandler(config.Ctx.Log),
		QueryHandler:                common.NewNoOpQueryHandler(config.Ctx.Log),
		ChitsHandler:                common.NewNoOpChitsHandler(config.Ctx.Log),
		AppHandler:                  config.VM,

		minority: bootstrapper.Noop,
		majority: bootstrapper.Noop,

		outstandingRequests:     bimap.New[common.Request, ids.ID](),
		outstandingRequestTimes: make(map[common.Request]time.Time),

		executedStateTransitions: math.MaxInt,
		onFinished:               onFinished,
		lastProgressUpdateTime:   time.Now(),
		etaTracker:               timer.NewEtaTracker(10, 1.2),
	}

	timeout := func() {
		config.Ctx.Lock.Lock()
		defer config.Ctx.Lock.Unlock()

		if err := bs.Timeout(); err != nil {
			bs.Config.Ctx.Log.Warn("Encountered error during bootstrapping: %w", zap.Error(err))
		}
	}
	bs.TimeoutRegistrar = common.NewTimeoutScheduler(timeout, config.BootstrapTracker.AllBootstrapped())

	return bs, err
}

func (b *Bootstrapper) Context() *snow.ConsensusContext {
	return b.Ctx
}

func (b *Bootstrapper) Clear(context.Context) error {
	b.Ctx.Lock.Lock()
	defer b.Ctx.Lock.Unlock()

	return database.AtomicClear(b.DB, b.DB)
}

func (b *Bootstrapper) Start(ctx context.Context, startReqID uint32) error {
	b.Ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_CHAIN,
		State: snow.Bootstrapping,
	})
	if err := b.VM.SetState(ctx, snow.Bootstrapping); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has started: %w", err)
	}

	lastAccepted, err := b.getLastAccepted(ctx)
	if err != nil {
		return err
	}

	lastAcceptedHeight := lastAccepted.Height()
	b.Ctx.Log.Info("starting bootstrapper",
		zap.Stringer("lastAcceptedID", lastAccepted.ID()),
		zap.Uint64("lastAcceptedHeight", lastAcceptedHeight),
	)

	// Set the starting height
	b.startingHeight = lastAcceptedHeight
	b.requestID = startReqID

	b.tree, err = interval.NewTree(b.DB)
	if err != nil {
		return fmt.Errorf("failed to initialize interval tree: %w", err)
	}

	b.missingBlockIDs, err = getMissingBlockIDs(ctx, b.DB, b.nonVerifyingParser, b.tree, b.startingHeight)
	if err != nil {
		return fmt.Errorf("failed to initialize missing block IDs: %w", err)
	}

	return b.tryStartBootstrapping(ctx)
}

func (b *Bootstrapper) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	if err := b.VM.Connected(ctx, nodeID, nodeVersion); err != nil {
		return err
	}

	if err := b.StartupTracker.Connected(ctx, nodeID, nodeVersion); err != nil {
		return err
	}

	return b.tryStartBootstrapping(ctx)
}

func (b *Bootstrapper) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if err := b.VM.Disconnected(ctx, nodeID); err != nil {
		return err
	}
	return b.StartupTracker.Disconnected(ctx, nodeID)
}

// tryStartBootstrapping will start bootstrapping the first time it is called
// while the startupTracker is reporting that the protocol should start.
func (b *Bootstrapper) tryStartBootstrapping(ctx context.Context) error {
	if b.started || !b.StartupTracker.ShouldStart() {
		return nil
	}

	b.started = true
	return b.startBootstrapping(ctx)
}

func (b *Bootstrapper) startBootstrapping(ctx context.Context) error {
	currentBeacons := b.Beacons.GetMap(b.Ctx.SubnetID)
	nodeWeights := make(map[ids.NodeID]uint64, len(currentBeacons))
	for nodeID, beacon := range currentBeacons {
		nodeWeights[nodeID] = beacon.Weight
	}

	frontierNodes, err := bootstrapper.Sample(nodeWeights, b.SampleK)
	if err != nil {
		return err
	}

	b.Ctx.Log.Debug("sampled nodes to seed bootstrapping frontier",
		zap.Reflect("sampledNodes", frontierNodes),
		zap.Int("numNodes", len(nodeWeights)),
	)

	b.minority = bootstrapper.NewMinority(
		b.Ctx.Log,
		frontierNodes,
		maxOutstandingBroadcastRequests,
	)
	b.majority = bootstrapper.NewMajority(
		b.Ctx.Log,
		nodeWeights,
		maxOutstandingBroadcastRequests,
	)

	if accepted, finalized := b.majority.Result(ctx); finalized {
		b.Ctx.Log.Info("bootstrapping skipped",
			zap.String("reason", "no provided bootstraps"),
		)
		return b.startSyncing(ctx, accepted)
	}

	b.requestID++
	return b.sendBootstrappingMessagesOrFinish(ctx)
}

func (b *Bootstrapper) sendBootstrappingMessagesOrFinish(ctx context.Context) error {
	if peers := b.minority.GetPeers(ctx); peers.Len() > 0 {
		b.Sender.SendGetAcceptedFrontier(ctx, peers, b.requestID)
		return nil
	}

	potentialAccepted, finalized := b.minority.Result(ctx)
	if !finalized {
		// We haven't finalized the accepted frontier, so we should wait for the
		// outstanding requests.
		return nil
	}

	if peers := b.majority.GetPeers(ctx); peers.Len() > 0 {
		b.Sender.SendGetAccepted(ctx, peers, b.requestID, potentialAccepted)
		return nil
	}

	accepted, finalized := b.majority.Result(ctx)
	if !finalized {
		// We haven't finalized the accepted set, so we should wait for the
		// outstanding requests.
		return nil
	}

	numAccepted := len(accepted)
	if numAccepted == 0 {
		b.Ctx.Log.Debug("restarting bootstrap",
			zap.String("reason", "no blocks accepted"),
			zap.Int("numBeacons", b.Beacons.NumValidators(b.Ctx.SubnetID)),
		)
		// Invariant: These functions are mutually recursive. However, when
		// [startBootstrapping] calls [sendMessagesOrFinish], it is guaranteed
		// to exit when sending GetAcceptedFrontier requests.
		return b.startBootstrapping(ctx)
	}

	return b.startSyncing(ctx, accepted)
}

func (b *Bootstrapper) AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	if requestID != b.requestID {
		b.Ctx.Log.Debug("received out-of-sync AcceptedFrontier message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.requestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if err := b.minority.RecordOpinion(ctx, nodeID, set.Of(containerID)); err != nil {
		return err
	}
	return b.sendBootstrappingMessagesOrFinish(ctx)
}

func (b *Bootstrapper) GetAcceptedFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if requestID != b.requestID {
		b.Ctx.Log.Debug("received out-of-sync GetAcceptedFrontierFailed message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.requestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if err := b.minority.RecordOpinion(ctx, nodeID, nil); err != nil {
		return err
	}
	return b.sendBootstrappingMessagesOrFinish(ctx)
}

func (b *Bootstrapper) Accepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs set.Set[ids.ID]) error {
	if requestID != b.requestID {
		b.Ctx.Log.Debug("received out-of-sync Accepted message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.requestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if err := b.majority.RecordOpinion(ctx, nodeID, containerIDs); err != nil {
		return err
	}
	return b.sendBootstrappingMessagesOrFinish(ctx)
}

func (b *Bootstrapper) GetAcceptedFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if requestID != b.requestID {
		b.Ctx.Log.Debug("received out-of-sync GetAcceptedFailed message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.requestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if err := b.majority.RecordOpinion(ctx, nodeID, nil); err != nil {
		return err
	}
	return b.sendBootstrappingMessagesOrFinish(ctx)
}

func (b *Bootstrapper) startSyncing(ctx context.Context, acceptedBlockIDs []ids.ID) error {
	knownBlockIDs := genesis.GetCheckpoints(b.Ctx.NetworkID, b.Ctx.ChainID)
	b.missingBlockIDs.Union(knownBlockIDs)
	b.missingBlockIDs.Add(acceptedBlockIDs...)
	numMissingBlockIDs := b.missingBlockIDs.Len()

	log := b.Ctx.Log.Info
	if b.restarted {
		log = b.Ctx.Log.Debug
	}
	log("starting to fetch blocks",
		zap.Int("numKnownBlocks", knownBlockIDs.Len()),
		zap.Int("numAcceptedBlocks", len(acceptedBlockIDs)),
		zap.Int("numMissingBlocks", numMissingBlockIDs),
	)

	toProcess := make([]snowman.Block, 0, numMissingBlockIDs)
	for blkID := range b.missingBlockIDs {
		// TODO: if `GetBlock` returns an error other than
		// `database.ErrNotFound`, then the error should be propagated.
		blk, err := b.VM.GetBlock(ctx, blkID)
		if err != nil {
			if err := b.fetch(ctx, blkID); err != nil {
				return err
			}
			continue
		}
		toProcess = append(toProcess, blk)
	}

	b.initiallyFetched = b.tree.Len()
	b.startTime = time.Now()

	// Add the first sample to the EtaTracker to establish an accurate baseline
	// It's okay to call this a few times if startSyncing is called more than once.
	b.etaTracker.AddSample(b.initiallyFetched, b.tipHeight-b.startingHeight, b.startTime)

	// Process received blocks
	for _, blk := range toProcess {
		if err := b.process(ctx, blk, nil); err != nil {
			return err
		}
	}

	return b.tryStartExecuting(ctx)
}

// Get block [blkID] and its ancestors from a validator
func (b *Bootstrapper) fetch(ctx context.Context, blkID ids.ID) error {
	// Make sure we haven't already requested this block
	if b.outstandingRequests.HasValue(blkID) {
		return nil
	}

	nodeID, ok := b.PeerTracker.SelectPeer()
	if !ok {
		// If we aren't connected to any peers, we send a request to ourself
		// which is guaranteed to fail. We send this message to use the message
		// timeout as a retry mechanism. Once we are connected to another node
		// again we will select them to sample from.
		nodeID = b.Ctx.NodeID
	}

	b.PeerTracker.RegisterRequest(nodeID)

	b.requestID++
	request := common.Request{
		NodeID:    nodeID,
		RequestID: b.requestID,
	}
	b.outstandingRequests.Put(request, blkID)
	b.outstandingRequestTimes[request] = time.Now()
	b.Config.Sender.SendGetAncestors(ctx, nodeID, b.requestID, blkID) // request block and ancestors
	return nil
}

// Ancestors handles the receipt of multiple containers. Should be received in
// response to a GetAncestors message to [nodeID] with request ID [requestID]
func (b *Bootstrapper) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, blks [][]byte) error {
	// Make sure this is in response to a request we made
	request := common.Request{
		NodeID:    nodeID,
		RequestID: requestID,
	}
	wantedBlkID, ok := b.outstandingRequests.DeleteKey(request)
	if !ok { // this message isn't in response to a request we made
		b.Ctx.Log.Debug("received unexpected Ancestors",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}
	requestTime := b.outstandingRequestTimes[request]
	delete(b.outstandingRequestTimes, request)

	lenBlks := len(blks)
	if lenBlks == 0 {
		b.Ctx.Log.Debug("received Ancestors with no block",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)

		b.PeerTracker.RegisterFailure(nodeID)

		// Send another request for this
		return b.fetch(ctx, wantedBlkID)
	}

	if lenBlks > b.Config.AncestorsMaxContainersReceived {
		blks = blks[:b.Config.AncestorsMaxContainersReceived]
		b.Ctx.Log.Debug("ignoring containers in Ancestors",
			zap.Int("numContainers", lenBlks-b.Config.AncestorsMaxContainersReceived),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
	}

	blocks, err := block.BatchedParseBlock(ctx, b.VM, blks)
	if err != nil { // the provided blocks couldn't be parsed
		b.Ctx.Log.Debug("failed to parse blocks in Ancestors",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Error(err),
		)
		b.PeerTracker.RegisterFailure(nodeID)
		return b.fetch(ctx, wantedBlkID)
	}

	if len(blocks) == 0 {
		b.Ctx.Log.Debug("parsing blocks returned an empty set of blocks",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		b.PeerTracker.RegisterFailure(nodeID)
		return b.fetch(ctx, wantedBlkID)
	}

	requestedBlock := blocks[0]
	if actualID := requestedBlock.ID(); actualID != wantedBlkID {
		b.Ctx.Log.Debug("first block is not the requested block",
			zap.Stringer("expectedBlkID", wantedBlkID),
			zap.Stringer("blkID", actualID),
		)
		b.PeerTracker.RegisterFailure(nodeID)
		return b.fetch(ctx, wantedBlkID)
	}

	var (
		numBytes  = len(requestedBlock.Bytes())
		ancestors = make(map[ids.ID]snowman.Block, len(blocks))
	)
	for _, block := range blocks[1:] {
		numBytes += len(block.Bytes())
		ancestors[block.ID()] = block
	}

	// TODO: Calculate bandwidth based on the blocks that were persisted to
	// disk.
	var (
		requestLatency = time.Since(requestTime).Seconds() + epsilon
		bandwidth      = float64(numBytes) / requestLatency
	)
	b.PeerTracker.RegisterResponse(nodeID, bandwidth)

	if err := b.process(ctx, requestedBlock, ancestors); err != nil {
		return err
	}

	return b.tryStartExecuting(ctx)
}

func (b *Bootstrapper) GetAncestorsFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	request := common.Request{
		NodeID:    nodeID,
		RequestID: requestID,
	}
	blkID, ok := b.outstandingRequests.DeleteKey(request)
	if !ok {
		b.Ctx.Log.Debug("unexpectedly called GetAncestorsFailed",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}
	delete(b.outstandingRequestTimes, request)

	// This node timed out their request.
	b.PeerTracker.RegisterFailure(nodeID)

	// Send another request for this
	return b.fetch(ctx, blkID)
}

// process a series of consecutive blocks starting at [blk].
//
//   - blk is a block that is assumed to have been marked as acceptable by the
//     bootstrapping engine.
//   - ancestors is a set of blocks that can be used to optimistically lookup
//     parent blocks. This enables the engine to process multiple blocks without
//     relying on the VM to have stored blocks during `ParseBlock`.
func (b *Bootstrapper) process(
	ctx context.Context,
	blk snowman.Block,
	ancestors map[ids.ID]snowman.Block,
) error {
	lastAccepted, err := b.getLastAccepted(ctx)
	if err != nil {
		return err
	}

	numPreviouslyFetched := b.tree.Len()

	batch := b.DB.NewBatch()
	missingBlockID, foundNewMissingID, err := process(
		batch,
		b.tree,
		b.missingBlockIDs,
		lastAccepted.Height(),
		blk,
		ancestors,
	)
	if err != nil {
		return err
	}

	// Update metrics and log statuses
	{
		numFetched := b.tree.Len()
		b.numFetched.Add(float64(b.tree.Len() - numPreviouslyFetched))

		height := blk.Height()
		b.tipHeight = max(b.tipHeight, height)

		// Check if it's time to log progress (both progress-based and time-based frequency)
		now := time.Now()
		shouldLog := numPreviouslyFetched/statusUpdateFrequency != numFetched/statusUpdateFrequency &&
			now.Sub(b.lastProgressUpdateTime) >= minimumLogInterval

		if shouldLog {
			totalBlocksToFetch := b.tipHeight - b.startingHeight

			eta, progressPercentage := b.etaTracker.AddSample(
				numFetched,
				totalBlocksToFetch,
				now,
			)

			// Update the last progress update time and previous progress for next iteration
			b.lastProgressUpdateTime = now

			// Only log if we have a valid ETA estimate
			if eta != nil {
				logger := b.Ctx.Log.Info
				if b.restarted {
					// Lower log level for restarted bootstrapping.
					logger = b.Ctx.Log.Debug
				}
				logger("fetching blocks",
					zap.Uint64("numFetchedBlocks", numFetched),
					zap.Uint64("numTotalBlocks", totalBlocksToFetch),
					zap.Duration("eta", *eta),
					zap.Float64("pctComplete", progressPercentage),
				)
			}
		}
	}

	if err := batch.Write(); err != nil || !foundNewMissingID {
		return err
	}

	b.missingBlockIDs.Add(missingBlockID)
	// Attempt to fetch the newly discovered block
	return b.fetch(ctx, missingBlockID)
}

// tryStartExecuting executes all pending blocks if there are no more blocks
// being fetched. After executing all pending blocks it will either restart
// bootstrapping, or transition into normal operations.
func (b *Bootstrapper) tryStartExecuting(ctx context.Context) error {
	if numMissingBlockIDs := b.missingBlockIDs.Len(); numMissingBlockIDs != 0 {
		return nil
	}

	if b.Ctx.State.Get().State == snow.NormalOp || b.awaitingTimeout {
		return nil
	}

	lastAccepted, err := b.getLastAccepted(ctx)
	if err != nil {
		return err
	}

	log := b.Ctx.Log.Info
	if b.restarted {
		log = b.Ctx.Log.Debug
	}

	numToExecute := b.tree.Len()
	err = execute(
		ctx,
		b.Halted,
		log,
		b.DB,
		&parseAcceptor{
			parser:      b.nonVerifyingParser,
			ctx:         b.Ctx,
			numAccepted: b.numAccepted,
		},
		b.tree,
		lastAccepted.Height(),
	)
	if err != nil {
		// If a fatal error has occurred, include the last accepted block
		// information.
		lastAccepted, lastAcceptedErr := b.getLastAccepted(ctx)
		if lastAcceptedErr != nil {
			return fmt.Errorf("%w after %w", lastAcceptedErr, err)
		}
		return fmt.Errorf("%w with last accepted %s (height=%d)",
			err,
			lastAccepted.ID(),
			lastAccepted.Height(),
		)
	}
	if b.Halted() {
		return nil
	}

	previouslyExecuted := b.executedStateTransitions
	b.executedStateTransitions = numToExecute

	// Note that executedBlocks < c*previouslyExecuted ( 0 <= c < 1 ) is enforced
	// so that the bootstrapping process will terminate even as new blocks are
	// being issued.
	if numToExecute > 0 && numToExecute < previouslyExecuted/2 {
		return b.restartBootstrapping(ctx)
	}

	// If there is an additional callback, notify them that this chain has been
	// synced.
	if b.Bootstrapped != nil {
		b.bootstrappedOnce.Do(b.Bootstrapped)
	}

	// Notify the subnet that this chain is synced
	b.Config.BootstrapTracker.Bootstrapped(b.Ctx.ChainID)

	// If the subnet hasn't finished bootstrapping, this chain should remain
	// syncing.
	if !b.Config.BootstrapTracker.IsBootstrapped() {
		log("waiting for the remaining chains in this subnet to finish syncing")
		// Restart bootstrapping after [bootstrappingDelay] to keep up to date
		// on the latest tip.
		b.awaitingTimeout = true
		b.TimeoutRegistrar.RegisterTimeout(bootstrappingDelay)
		return nil
	}
	return b.onFinished(ctx, b.requestID)
}

func (b *Bootstrapper) getLastAccepted(ctx context.Context) (snowman.Block, error) {
	lastAcceptedID, err := b.VM.LastAccepted(ctx)
	if err != nil {
		return nil, fmt.Errorf("couldn't get last accepted ID: %w", err)
	}
	lastAccepted, err := b.VM.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get last accepted block %s: %w", lastAcceptedID, err)
	}
	return lastAccepted, nil
}

func (b *Bootstrapper) Timeout() error {
	if !b.awaitingTimeout {
		return errUnexpectedTimeout
	}
	b.awaitingTimeout = false

	if !b.Config.BootstrapTracker.IsBootstrapped() {
		return b.restartBootstrapping(context.TODO())
	}
	return b.onFinished(context.TODO(), b.requestID)
}

func (b *Bootstrapper) restartBootstrapping(ctx context.Context) error {
	b.Ctx.Log.Debug("Checking for new frontiers")
	b.restarted = true
	b.outstandingRequests = bimap.New[common.Request, ids.ID]()
	b.outstandingRequestTimes = make(map[common.Request]time.Time)
	return b.startBootstrapping(ctx)
}

func (b *Bootstrapper) Notify(_ context.Context, msg common.Message) error {
	if msg != common.StateSyncDone {
		b.Ctx.Log.Info("received an unexpected message from the VM",
			zap.Stringer("msg", msg),
		)
		return nil
	}

	b.Ctx.StateSyncing.Set(false)
	return nil
}

func (b *Bootstrapper) HealthCheck(ctx context.Context) (interface{}, error) {
	b.Ctx.Lock.Lock()
	defer b.Ctx.Lock.Unlock()

	vmIntf, vmErr := b.VM.HealthCheck(ctx)
	intf := map[string]interface{}{
		"consensus": struct{}{},
		"vm":        vmIntf,
	}
	return intf, vmErr
}

func (b *Bootstrapper) Shutdown(ctx context.Context) error {
	b.Ctx.Log.Info("shutting down bootstrapper")

	b.Ctx.Lock.Lock()
	defer b.Ctx.Lock.Unlock()

	return b.VM.Shutdown(ctx)
}

func (*Bootstrapper) Gossip(context.Context) error {
	return nil
}
