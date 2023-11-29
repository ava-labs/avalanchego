// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/bootstrapper"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
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
	common.Halter
	*metrics

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

	// tracks which validators were asked for which containers in which requests
	outstandingRequests *bimap.BiMap[common.Request, ids.ID]

	// number of state transitions executed
	executedStateTransitions int

	parser *parser

	awaitingTimeout bool

	// fetchFrom is the set of nodes that we can fetch the next container from.
	// When a container is fetched, the nodeID is removed from [fetchFrom] to
	// attempt to limit a single request to a peer at any given time. When the
	// response is received, either and Ancestors or an AncestorsFailed, the
	// nodeID will be added back to [fetchFrom] unless the Ancestors message is
	// empty. This is to attempt to prevent requesting containers from that peer
	// again.
	fetchFrom set.Set[ids.NodeID]

	// bootstrappedOnce ensures that the [Bootstrapped] callback is only invoked
	// once, even if bootstrapping is retried.
	bootstrappedOnce sync.Once

	// Called when bootstrapping is done on a specific chain
	onFinished func(ctx context.Context, lastReqID uint32) error
}

func New(config Config, onFinished func(ctx context.Context, lastReqID uint32) error) (*Bootstrapper, error) {
	metrics, err := newMetrics("bs", config.Ctx.Registerer)
	return &Bootstrapper{
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

		outstandingRequests: bimap.New[common.Request, ids.ID](),

		executedStateTransitions: math.MaxInt,
		onFinished:               onFinished,
	}, err
}

func (b *Bootstrapper) Context() *snow.ConsensusContext {
	return b.Ctx
}

func (b *Bootstrapper) Clear(context.Context) error {
	b.Ctx.Lock.Lock()
	defer b.Ctx.Lock.Unlock()

	if err := b.Config.Blocked.Clear(); err != nil {
		return err
	}
	return b.Config.Blocked.Commit()
}

func (b *Bootstrapper) Start(ctx context.Context, startReqID uint32) error {
	b.Ctx.Log.Info("starting bootstrapper")

	b.Ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping,
	})
	if err := b.VM.SetState(ctx, snow.Bootstrapping); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has started: %w",
			err)
	}

	b.parser = &parser{
		log:         b.Ctx.Log,
		numAccepted: b.numAccepted,
		numDropped:  b.numDropped,
		vm:          b.VM,
	}
	if err := b.Blocked.SetParser(ctx, b.parser); err != nil {
		return err
	}

	// Set the starting height
	lastAcceptedID, err := b.VM.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("couldn't get last accepted ID: %w", err)
	}
	lastAccepted, err := b.VM.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		return fmt.Errorf("couldn't get last accepted block: %w", err)
	}
	b.startingHeight = lastAccepted.Height()
	b.requestID = startReqID

	return b.tryStartBootstrapping(ctx)
}

func (b *Bootstrapper) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	if err := b.VM.Connected(ctx, nodeID, nodeVersion); err != nil {
		return err
	}

	if err := b.StartupTracker.Connected(ctx, nodeID, nodeVersion); err != nil {
		return err
	}
	// Ensure fetchFrom reflects proper validator list
	if _, ok := b.Beacons.GetValidator(b.Ctx.SubnetID, nodeID); ok {
		b.fetchFrom.Add(nodeID)
	}

	return b.tryStartBootstrapping(ctx)
}

func (b *Bootstrapper) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if err := b.VM.Disconnected(ctx, nodeID); err != nil {
		return err
	}

	if err := b.StartupTracker.Disconnected(ctx, nodeID); err != nil {
		return err
	}

	b.markUnavailable(nodeID)
	return nil
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
			zap.Int("numBeacons", b.Beacons.Count(b.Ctx.SubnetID)),
		)
		// Invariant: These functions are mutualy recursive. However, when
		// [startBootstrapping] calls [sendMessagesOrFinish], it is guaranteed
		// to exit when sending GetAcceptedFrontier requests.
		return b.startBootstrapping(ctx)
	}

	if !b.restarted {
		b.Ctx.Log.Info("bootstrapping started syncing",
			zap.Int("numAccepted", numAccepted),
		)
	} else {
		b.Ctx.Log.Debug("bootstrapping started syncing",
			zap.Int("numAccepted", numAccepted),
		)
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

func (b *Bootstrapper) startSyncing(ctx context.Context, acceptedContainerIDs []ids.ID) error {
	// Initialize the fetch from set to the currently preferred peers
	b.fetchFrom = b.StartupTracker.PreferredPeers()

	pendingContainerIDs := b.Blocked.MissingIDs()
	// Append the list of accepted container IDs to pendingContainerIDs to ensure
	// we iterate over every container that must be traversed.
	pendingContainerIDs = append(pendingContainerIDs, acceptedContainerIDs...)
	b.Ctx.Log.Debug("starting bootstrapping",
		zap.Int("numPendingBlocks", len(pendingContainerIDs)),
		zap.Int("numAcceptedBlocks", len(acceptedContainerIDs)),
	)

	toProcess := make([]snowman.Block, 0, len(pendingContainerIDs))
	for _, blkID := range pendingContainerIDs {
		b.Blocked.AddMissingID(blkID)

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

	b.initiallyFetched = b.Blocked.PendingJobs()
	b.startTime = time.Now()

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

	// Make sure we don't already have this block
	if _, err := b.VM.GetBlock(ctx, blkID); err == nil {
		return b.tryStartExecuting(ctx)
	}

	validatorID, ok := b.fetchFrom.Peek()
	if !ok {
		return fmt.Errorf("dropping request for %s as there are no validators", blkID)
	}

	// We only allow one outbound request at a time from a node
	b.markUnavailable(validatorID)

	b.requestID++

	b.outstandingRequests.Put(
		common.Request{
			NodeID:    validatorID,
			RequestID: b.requestID,
		},
		blkID,
	)
	b.Config.Sender.SendGetAncestors(ctx, validatorID, b.requestID, blkID) // request block and ancestors
	return nil
}

// Ancestors handles the receipt of multiple containers. Should be received in
// response to a GetAncestors message to [nodeID] with request ID [requestID]
func (b *Bootstrapper) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, blks [][]byte) error {
	// Make sure this is in response to a request we made
	wantedBlkID, ok := b.outstandingRequests.DeleteKey(common.Request{
		NodeID:    nodeID,
		RequestID: requestID,
	})
	if !ok { // this message isn't in response to a request we made
		b.Ctx.Log.Debug("received unexpected Ancestors",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	lenBlks := len(blks)
	if lenBlks == 0 {
		b.Ctx.Log.Debug("received Ancestors with no block",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)

		b.markUnavailable(nodeID)

		// Send another request for this
		return b.fetch(ctx, wantedBlkID)
	}

	// This node has responded - so add it back into the set
	b.fetchFrom.Add(nodeID)

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
		return b.fetch(ctx, wantedBlkID)
	}

	if len(blocks) == 0 {
		b.Ctx.Log.Debug("parsing blocks returned an empty set of blocks",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return b.fetch(ctx, wantedBlkID)
	}

	requestedBlock := blocks[0]
	if actualID := requestedBlock.ID(); actualID != wantedBlkID {
		b.Ctx.Log.Debug("first block is not the requested block",
			zap.Stringer("expectedBlkID", wantedBlkID),
			zap.Stringer("blkID", actualID),
		)
		return b.fetch(ctx, wantedBlkID)
	}

	blockSet := make(map[ids.ID]snowman.Block, len(blocks))
	for _, block := range blocks[1:] {
		blockSet[block.ID()] = block
	}
	return b.process(ctx, requestedBlock, blockSet)
}

func (b *Bootstrapper) GetAncestorsFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	blkID, ok := b.outstandingRequests.DeleteKey(common.Request{
		NodeID:    nodeID,
		RequestID: requestID,
	})
	if !ok {
		b.Ctx.Log.Debug("unexpectedly called GetAncestorsFailed",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	// This node timed out their request, so we can add them back to [fetchFrom]
	b.fetchFrom.Add(nodeID)

	// Send another request for this
	return b.fetch(ctx, blkID)
}

// markUnavailable removes [nodeID] from the set of peers used to fetch
// ancestors. If the set becomes empty, it is reset to the currently preferred
// peers so bootstrapping can continue.
func (b *Bootstrapper) markUnavailable(nodeID ids.NodeID) {
	b.fetchFrom.Remove(nodeID)

	// if [fetchFrom] has become empty, reset it to the currently preferred
	// peers
	if b.fetchFrom.Len() == 0 {
		b.fetchFrom = b.StartupTracker.PreferredPeers()
	}
}

// process a series of consecutive blocks starting at [blk].
//
//   - blk is a block that is assumed to have been marked as acceptable by the
//     bootstrapping engine.
//   - processingBlocks is a set of blocks that can be used to lookup blocks.
//     This enables the engine to process multiple blocks without relying on the
//     VM to have stored blocks during `ParseBlock`.
//
// If [blk]'s height is <= the last accepted height, then it will be removed
// from the missingIDs set.
func (b *Bootstrapper) process(ctx context.Context, blk snowman.Block, processingBlocks map[ids.ID]snowman.Block) error {
	for {
		blkID := blk.ID()
		if b.Halted() {
			// We must add in [blkID] to the set of missing IDs so that we are
			// guaranteed to continue processing from this state when the
			// bootstrapper is restarted.
			b.Blocked.AddMissingID(blkID)
			return b.Blocked.Commit()
		}

		b.Blocked.RemoveMissingID(blkID)

		status := blk.Status()
		// The status should never be rejected here - but we check to fail as
		// quickly as possible
		if status == choices.Rejected {
			return fmt.Errorf("bootstrapping wants to accept %s, however it was previously rejected", blkID)
		}

		blkHeight := blk.Height()
		if status == choices.Accepted || blkHeight <= b.startingHeight {
			// We can stop traversing, as we have reached the accepted frontier
			if err := b.Blocked.Commit(); err != nil {
				return err
			}
			return b.tryStartExecuting(ctx)
		}

		// If this block is going to be accepted, make sure to update the
		// tipHeight for logging
		if blkHeight > b.tipHeight {
			b.tipHeight = blkHeight
		}

		pushed, err := b.Blocked.Push(ctx, &blockJob{
			log:         b.Ctx.Log,
			numAccepted: b.numAccepted,
			numDropped:  b.numDropped,
			blk:         blk,
			vm:          b.VM,
		})
		if err != nil {
			return err
		}

		if !pushed {
			// We can stop traversing, as we have reached a block that we
			// previously pushed onto the jobs queue
			if err := b.Blocked.Commit(); err != nil {
				return err
			}
			return b.tryStartExecuting(ctx)
		}

		// We added a new block to the queue, so track that it was fetched
		b.numFetched.Inc()

		// Periodically log progress
		blocksFetchedSoFar := b.Blocked.Jobs.PendingJobs()
		if blocksFetchedSoFar%statusUpdateFrequency == 0 {
			totalBlocksToFetch := b.tipHeight - b.startingHeight
			eta := timer.EstimateETA(
				b.startTime,
				blocksFetchedSoFar-b.initiallyFetched, // Number of blocks we have fetched during this run
				totalBlocksToFetch-b.initiallyFetched, // Number of blocks we expect to fetch during this run
			)
			b.fetchETA.Set(float64(eta))

			if !b.restarted {
				b.Ctx.Log.Info("fetching blocks",
					zap.Uint64("numFetchedBlocks", blocksFetchedSoFar),
					zap.Uint64("numTotalBlocks", totalBlocksToFetch),
					zap.Duration("eta", eta),
				)
			} else {
				b.Ctx.Log.Debug("fetching blocks",
					zap.Uint64("numFetchedBlocks", blocksFetchedSoFar),
					zap.Uint64("numTotalBlocks", totalBlocksToFetch),
					zap.Duration("eta", eta),
				)
			}
		}

		// Attempt to traverse to the next block
		parentID := blk.Parent()

		// First check if the parent is in the processing blocks set
		parent, ok := processingBlocks[parentID]
		if ok {
			blk = parent
			continue
		}

		// If the parent is not available in processing blocks, attempt to get
		// the block from the vm
		parent, err = b.VM.GetBlock(ctx, parentID)
		if err == nil {
			blk = parent
			continue
		}
		// TODO: report errors that aren't `database.ErrNotFound`

		// If the block wasn't able to be acquired immediately, attempt to fetch
		// it
		b.Blocked.AddMissingID(parentID)
		if err := b.fetch(ctx, parentID); err != nil {
			return err
		}

		if err := b.Blocked.Commit(); err != nil {
			return err
		}
		return b.tryStartExecuting(ctx)
	}
}

// tryStartExecuting executes all pending blocks if there are no more blocks
// being fetched. After executing all pending blocks it will either restart
// bootstrapping, or transition into normal operations.
func (b *Bootstrapper) tryStartExecuting(ctx context.Context) error {
	if numPending := b.Blocked.NumMissingIDs(); numPending != 0 {
		return nil
	}

	if b.Ctx.State.Get().State == snow.NormalOp || b.awaitingTimeout {
		return nil
	}

	if !b.restarted {
		b.Ctx.Log.Info("executing blocks",
			zap.Uint64("numPendingJobs", b.Blocked.PendingJobs()),
		)
	} else {
		b.Ctx.Log.Debug("executing blocks",
			zap.Uint64("numPendingJobs", b.Blocked.PendingJobs()),
		)
	}

	executedBlocks, err := b.Blocked.ExecuteAll(
		ctx,
		b.Config.Ctx,
		b,
		b.restarted,
		b.Ctx.BlockAcceptor,
	)
	if err != nil || b.Halted() {
		return err
	}

	previouslyExecuted := b.executedStateTransitions
	b.executedStateTransitions = executedBlocks

	// Note that executedBlocks < c*previouslyExecuted ( 0 <= c < 1 ) is enforced
	// so that the bootstrapping process will terminate even as new blocks are
	// being issued.
	if executedBlocks > 0 && executedBlocks < previouslyExecuted/2 {
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
		if !b.restarted {
			b.Ctx.Log.Info("waiting for the remaining chains in this subnet to finish syncing")
		} else {
			b.Ctx.Log.Debug("waiting for the remaining chains in this subnet to finish syncing")
		}
		// Restart bootstrapping after [bootstrappingDelay] to keep up to date
		// on the latest tip.
		b.Config.Timer.RegisterTimeout(bootstrappingDelay)
		b.awaitingTimeout = true
		return nil
	}
	b.fetchETA.Set(0)
	return b.onFinished(ctx, b.requestID)
}

func (b *Bootstrapper) Timeout(ctx context.Context) error {
	if !b.awaitingTimeout {
		return errUnexpectedTimeout
	}
	b.awaitingTimeout = false

	if !b.Config.BootstrapTracker.IsBootstrapped() {
		return b.restartBootstrapping(ctx)
	}
	b.fetchETA.Set(0)
	return b.onFinished(ctx, b.requestID)
}

func (b *Bootstrapper) restartBootstrapping(ctx context.Context) error {
	b.Ctx.Log.Debug("Checking for new frontiers")
	b.restarted = true
	return b.startBootstrapping(ctx)
}

func (b *Bootstrapper) Notify(_ context.Context, msg common.Message) error {
	if msg != common.StateSyncDone {
		b.Ctx.Log.Warn("received an unexpected message from the VM",
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
