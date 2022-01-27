// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/version"
)

// Parameters for delaying bootstrapping to avoid potential CPU burns
const bootstrappingDelay = 10 * time.Second

var (
	_ common.BootstrapableEngine = &bootstrapper{}

	errUnexpectedTimeout = errors.New("unexpected timeout fired")
)

func New(config Config, onFinished func(lastReqID uint32) error) (common.BootstrapableEngine, error) {
	b := &bootstrapper{
		Config: config,

		PutHandler:   common.NewNoOpPutHandler(config.Ctx.Log),
		QueryHandler: common.NewNoOpQueryHandler(config.Ctx.Log),
		ChitsHandler: common.NewNoOpChitsHandler(config.Ctx.Log),
		AppHandler:   common.NewNoOpAppHandler(config.Ctx.Log),

		Fetcher: common.Fetcher{
			OnFinished: onFinished,
		},
		executedStateTransitions: math.MaxInt32,
		startingAcceptedFrontier: ids.Set{},
	}

	lastAcceptedID, err := b.VM.LastAccepted()
	if err != nil {
		return nil, fmt.Errorf("couldn't get last accepted ID: %w", err)
	}
	lastAccepted, err := b.VM.GetBlock(lastAcceptedID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get last accepted block: %w", err)
	}
	b.startingHeight = lastAccepted.Height()

	if err := b.metrics.Initialize("bs", config.Ctx.Registerer); err != nil {
		return nil, err
	}

	b.parser = &parser{
		log:         config.Ctx.Log,
		numAccepted: b.numAccepted,
		numDropped:  b.numDropped,
		vm:          b.VM,
	}
	if err := b.Blocked.SetParser(b.parser); err != nil {
		return nil, err
	}

	config.Bootstrapable = b
	b.Bootstrapper = common.NewCommonBootstrapper(config.Config)
	return b, nil
}

type bootstrapper struct {
	Config

	// list of NoOpsHandler for messages dropped by bootstrapper
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler
	common.AppHandler

	common.Bootstrapper
	common.Fetcher
	metrics

	started bool

	// Greatest height of the blocks passed in ForceAccepted
	tipHeight uint64
	// Height of the last accepted block when bootstrapping starts
	startingHeight uint64
	// Blocks passed into ForceAccepted
	startingAcceptedFrontier ids.Set
	// Number of blocks that were fetched on ForceAccepted
	initiallyFetched uint64
	// Time that ForceAccepted was last called
	startTime time.Time

	// number of state transitions executed
	executedStateTransitions int

	parser *parser

	awaitingTimeout bool
}

// Ancestors handles the receipt of multiple containers. Should be received in response to a GetAncestors message to [vdr]
// with request ID [requestID]
func (b *bootstrapper) Ancestors(vdr ids.ShortID, requestID uint32, blks [][]byte) error {
	lenBlks := len(blks)
	if lenBlks == 0 {
		b.Ctx.Log.Debug("Ancestors(%s, %d) contains no blocks", vdr, requestID)
		return b.GetAncestorsFailed(vdr, requestID)
	}
	if lenBlks > b.Config.AncestorsMaxContainersReceived {
		blks = blks[:b.Config.AncestorsMaxContainersReceived]
		b.Ctx.Log.Debug("ignoring %d containers in Ancestors(%s, %d)",
			lenBlks-b.Config.AncestorsMaxContainersReceived, vdr, requestID)
	}

	// Make sure this is in response to a request we made
	wantedBlkID, ok := b.OutstandingRequests.Remove(vdr, requestID)
	if !ok { // this message isn't in response to a request we made
		b.Ctx.Log.Debug("received unexpected Ancestors from %s with ID %d", vdr, requestID)
		return nil
	}

	blocks, err := block.BatchedParseBlock(b.VM, blks)
	if err != nil { // the provided blocks couldn't be parsed
		b.Ctx.Log.Debug("failed to parse blocks in Ancestors from %s with ID %d", vdr, requestID)
		return b.fetch(wantedBlkID)
	}

	if len(blocks) == 0 {
		b.Ctx.Log.Debug("parsing blocks returned an empty set of blocks from %s with ID %d", vdr, requestID)
		return b.fetch(wantedBlkID)
	}

	requestedBlock := blocks[0]
	if actualID := requestedBlock.ID(); actualID != wantedBlkID {
		b.Ctx.Log.Debug("expected the first block to be the requested block, %s, but is %s",
			wantedBlkID, actualID)
		return b.fetch(wantedBlkID)
	}

	blockSet := make(map[ids.ID]snowman.Block, len(blocks))
	for _, block := range blocks[1:] {
		blockSet[block.ID()] = block
	}
	return b.process(requestedBlock, blockSet)
}

// GetAncestorsFailed implements the AncestorsHandler interface
func (b *bootstrapper) GetAncestorsFailed(vdr ids.ShortID, requestID uint32) error {
	blkID, ok := b.OutstandingRequests.Remove(vdr, requestID)
	if !ok {
		b.Ctx.Log.Debug("GetAncestorsFailed(%s, %d) called but there was no outstanding request to this validator with this ID",
			vdr, requestID)
		return nil
	}
	// Send another request for this
	return b.fetch(blkID)
}

// Connected implements the InternalHandler interface.
func (b *bootstrapper) Connected(nodeID ids.ShortID, nodeVersion version.Application) error {
	if err := b.VM.Connected(nodeID, nodeVersion); err != nil {
		return err
	}

	if err := b.WeightTracker.AddWeightForNode(nodeID); err != nil {
		return err
	}

	if b.WeightTracker.EnoughConnectedWeight() && !b.started {
		b.started = true
		return b.Startup()
	}

	return nil
}

// Disconnected implements the InternalHandler interface.
func (b *bootstrapper) Disconnected(nodeID ids.ShortID) error {
	if err := b.VM.Disconnected(nodeID); err != nil {
		return err
	}

	return b.WeightTracker.RemoveWeightForNode(nodeID)
}

// Timeout implements the InternalHandler interface.
func (b *bootstrapper) Timeout() error {
	if !b.awaitingTimeout {
		return errUnexpectedTimeout
	}
	b.awaitingTimeout = false

	if !b.Config.Subnet.IsBootstrapped() {
		return b.Restart(true)
	}
	return b.finish()
}

// Gossip implements the InternalHandler interface.
func (b *bootstrapper) Gossip() error { return nil }

// Shutdown implements the InternalHandler interface.
func (b *bootstrapper) Shutdown() error { return nil }

// Notify implements the InternalHandler interface.
func (b *bootstrapper) Notify(common.Message) error { return nil }

// Context implements the common.Engine interface.
func (b *bootstrapper) Context() *snow.ConsensusContext { return b.Config.Ctx }

// Start implements the common.Engine interface.
func (b *bootstrapper) Start(startReqID uint32) error {
	b.Ctx.Log.Info("Starting bootstrap...")
	b.Ctx.SetState(snow.Bootstrapping)
	b.Config.SharedCfg.RequestID = startReqID

	if b.WeightTracker.EnoughConnectedWeight() {
		return nil
	}

	return b.Startup()
}

// HealthCheck implements the common.Engine interface.
func (b *bootstrapper) HealthCheck() (interface{}, error) {
	vmIntf, vmErr := b.VM.HealthCheck()
	intf := map[string]interface{}{
		"consensus": struct{}{},
		"vm":        vmIntf,
	}
	return intf, vmErr
}

// GetVM implements the common.Engine interface.
func (b *bootstrapper) GetVM() common.VM { return b.VM }

// ForceAccepted implements common.Bootstrapable interface
func (b *bootstrapper) ForceAccepted(acceptedContainerIDs []ids.ID) error {
	if err := b.VM.SetState(snow.Bootstrapping); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has started: %w",
			err)
	}

	pendingContainerIDs := b.Blocked.MissingIDs()

	// Append the list of accepted container IDs to pendingContainerIDs to ensure
	// we iterate over every container that must be traversed.
	pendingContainerIDs = append(pendingContainerIDs, acceptedContainerIDs...)
	toProcess := make([]snowman.Block, 0, len(acceptedContainerIDs))
	b.Ctx.Log.Debug("Starting bootstrapping with %d pending blocks and %d from the accepted frontier",
		len(pendingContainerIDs), len(acceptedContainerIDs))
	for _, blkID := range pendingContainerIDs {
		b.startingAcceptedFrontier.Add(blkID)
		if blk, err := b.VM.GetBlock(blkID); err == nil {
			if height := blk.Height(); height > b.tipHeight {
				b.tipHeight = height
			}
			if blk.Status() == choices.Accepted {
				b.Blocked.RemoveMissingID(blkID)
			} else {
				toProcess = append(toProcess, blk)
			}
		} else {
			b.Blocked.AddMissingID(blkID)
			if err := b.fetch(blkID); err != nil {
				return err
			}
		}
	}

	b.initiallyFetched = b.Blocked.PendingJobs()
	b.startTime = time.Now()

	// Process received blocks
	for _, blk := range toProcess {
		if err := b.process(blk, nil); err != nil {
			return err
		}
	}

	return b.checkFinish()
}

// Get block [blkID] and its ancestors from a validator
func (b *bootstrapper) fetch(blkID ids.ID) error {
	// Make sure we haven't already requested this block
	if b.OutstandingRequests.Contains(blkID) {
		return nil
	}

	// Make sure we don't already have this block
	if _, err := b.VM.GetBlock(blkID); err == nil {
		return b.checkFinish()
	}

	validators, err := b.Config.Beacons.Sample(1) // validator to send request to
	if err != nil {
		return fmt.Errorf("dropping request for %s as there are no validators", blkID)
	}
	validatorID := validators[0].ID()
	b.Config.SharedCfg.RequestID++

	b.OutstandingRequests.Add(validatorID, b.Config.SharedCfg.RequestID, blkID)
	b.Config.Sender.SendGetAncestors(validatorID, b.Config.SharedCfg.RequestID, blkID) // request block and ancestors
	return nil
}

// Clear implements common.Bootstrapable interface
func (b *bootstrapper) Clear() error {
	if err := b.Config.Blocked.Clear(); err != nil {
		return err
	}
	return b.Config.Blocked.Commit()
}

// process a block
func (b *bootstrapper) process(blk snowman.Block, processingBlocks map[ids.ID]snowman.Block) error {
	status := blk.Status()
	blkID := blk.ID()
	blkHeight := blk.Height()
	totalBlocksToFetch := b.tipHeight - b.startingHeight

	if blkHeight > b.tipHeight && b.startingAcceptedFrontier.Contains(blkID) {
		b.tipHeight = blkHeight
	}

	for status == choices.Processing {
		if b.Halted() {
			return nil
		}

		b.Blocked.RemoveMissingID(blkID)

		pushed, err := b.Blocked.Push(&blockJob{
			parser:      b.parser,
			numAccepted: b.numAccepted,
			numDropped:  b.numDropped,
			blk:         blk,
			vm:          b.VM,
		})
		if err != nil {
			return err
		}

		// Traverse to the next block regardless of if the block is pushed
		blkID = blk.Parent()
		processingBlock, ok := processingBlocks[blkID]
		// first check processing blocks
		if ok {
			blk = processingBlock
			status = blk.Status()
		} else {
			// if not available in processing blocks, get block
			blk, err = b.VM.GetBlock(blkID)
			if err != nil {
				status = choices.Unknown
			} else {
				status = blk.Status()
			}
		}

		if !pushed {
			// If this block is already on the queue, then we can stop
			// traversing here.
			break
		}

		b.numFetched.Inc()

		blocksFetchedSoFar := b.Blocked.Jobs.PendingJobs()
		if blocksFetchedSoFar%common.StatusUpdateFrequency == 0 { // Periodically print progress
			eta := timer.EstimateETA(
				b.startTime,
				blocksFetchedSoFar-b.initiallyFetched, // Number of blocks we have fetched during this run
				totalBlocksToFetch-b.initiallyFetched, // Number of blocks we expect to fetch during this run
			)

			if !b.Config.SharedCfg.Restarted {
				b.Ctx.Log.Info("fetched %d of %d blocks. ETA = %s", blocksFetchedSoFar, totalBlocksToFetch, eta)
			} else {
				b.Ctx.Log.Debug("fetched %d of %d blocks. ETA = %s", blocksFetchedSoFar, totalBlocksToFetch, eta)
			}
		}
	}

	switch status {
	case choices.Unknown:
		b.Blocked.AddMissingID(blkID)
		if err := b.fetch(blkID); err != nil {
			return err
		}
	case choices.Rejected: // Should never happen
		return fmt.Errorf("bootstrapping wants to accept %s, however it was previously rejected", blkID)
	}

	if err := b.Blocked.Commit(); err != nil {
		return err
	}

	return b.checkFinish()
}

// checkFinish repeatedly executes pending transactions and requests new frontier vertices until there aren't any new ones
// after which it finishes the bootstrap process
func (b *bootstrapper) checkFinish() error {
	if numPending := b.Blocked.NumMissingIDs(); numPending != 0 {
		return nil
	}

	if b.IsBootstrapped() || b.awaitingTimeout {
		return nil
	}

	if !b.Config.SharedCfg.Restarted {
		b.Ctx.Log.Info("bootstrapping fetched %d blocks. Executing state transitions...", b.Blocked.PendingJobs())
	} else {
		b.Ctx.Log.Debug("bootstrapping fetched %d blocks. Executing state transitions...", b.Blocked.PendingJobs())
	}

	executedBlocks, err := b.Blocked.ExecuteAll(
		b.Config.Ctx,
		b,
		b.Config.SharedCfg.Restarted,
		b.Ctx.ConsensusDispatcher,
		b.Ctx.DecisionDispatcher,
	)
	if err != nil || b.Halted() {
		return err
	}

	previouslyExecuted := b.executedStateTransitions
	b.executedStateTransitions = executedBlocks

	// Note that executedBlocks < c*previouslyExecuted ( 0 <= c < 1 ) is enforced
	// so that the bootstrapping process will terminate even as new blocks are
	// being issued.
	if b.Config.RetryBootstrap && executedBlocks > 0 && executedBlocks < previouslyExecuted/2 {
		return b.Restart(true)
	}

	// If there is an additional callback, notify them that this chain has been
	// synced.
	if b.Bootstrapped != nil {
		b.Bootstrapped()
	}

	// Notify the subnet that this chain is synced
	b.Config.Subnet.Bootstrapped(b.Ctx.ChainID)

	// If the subnet hasn't finished bootstrapping, this chain should remain
	// syncing.
	if !b.Config.Subnet.IsBootstrapped() {
		if !b.Config.SharedCfg.Restarted {
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

	return b.finish()
}

func (b *bootstrapper) finish() error {
	if err := b.VM.SetState(snow.NormalOp); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has finished: %w",
			err)
	}

	// Start consensus
	return b.OnFinished(b.Config.SharedCfg.RequestID)
}
