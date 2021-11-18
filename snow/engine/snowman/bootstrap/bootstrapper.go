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
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// Parameters for delaying bootstrapping to avoid potential CPU burns
const bootstrappingDelay = 10 * time.Second

var (
	_ common.Bootstrapper = &bootstrapper{}

	errUnexpectedTimeout = errors.New("unexpected timeout fired")
)

type Config struct {
	common.Config

	// Blocked tracks operations that are blocked on blocks
	Blocked *queue.JobsWithMissing

	VM      block.ChainVM
	Starter common.GearStarter

	Bootstrapped func()
}

func New(
	config Config,
	onFinished func(lastReqID uint32) error,
) (common.Bootstrapper, error) {
	return newBootstrapper(
		config,
		onFinished,
	)
}

type bootstrapper struct {
	Config
	common.BootstrapperGear
	common.Fetcher
	metrics

	// Greatest height of the blocks passed in ForceAccepted
	tipHeight uint64
	// Height of the last accepted block when bootstrapping starts
	startingHeight uint64
	// Blocks passed into ForceAccepted
	startingAcceptedFrontier ids.Set

	// number of state transitions executed
	executedStateTransitions int

	parser *parser

	awaitingTimeout bool
}

// new this engine.
func newBootstrapper(
	config Config,
	onFinished func(lastReqID uint32) error,
) (*bootstrapper, error) {
	b := &bootstrapper{
		Config: config,
		Fetcher: common.Fetcher{
			OnFinished: onFinished,
		},
		executedStateTransitions: math.MaxInt32,
		startingAcceptedFrontier: ids.Set{},
	}

	lastAcceptedID, err := b.VM.LastAccepted()
	if err != nil {
		return nil, fmt.Errorf("couldn't get last accepted ID: %s", err)
	}
	lastAccepted, err := b.VM.GetBlock(lastAcceptedID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get last accepted block: %s", err)
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
	if err := b.BootstrapperGear.Initialize(config.Config); err != nil {
		return nil, err
	}

	return b, nil
}

// CurrentAcceptedFrontier returns the last accepted block
func (b *bootstrapper) CurrentAcceptedFrontier() ([]ids.ID, error) {
	lastAccepted, err := b.VM.LastAccepted()
	return []ids.ID{lastAccepted}, err
}

// FilterAccepted returns the blocks in [containerIDs] that we have accepted
func (b *bootstrapper) FilterAccepted(containerIDs []ids.ID) []ids.ID {
	acceptedIDs := make([]ids.ID, 0, len(containerIDs))
	for _, blkID := range containerIDs {
		if blk, err := b.VM.GetBlock(blkID); err == nil && blk.Status() == choices.Accepted {
			acceptedIDs = append(acceptedIDs, blkID)
		}
	}
	return acceptedIDs
}

func (b *bootstrapper) ForceAccepted(acceptedContainerIDs []ids.ID) error {
	if err := b.VM.Bootstrapping(); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has started: %w",
			err)
	}

	b.NumFetched = 0

	pendingContainerIDs := b.Blocked.MissingIDs()

	// Append the list of accepted container IDs to pendingContainerIDs to ensure
	// we iterate over every container that must be traversed.
	pendingContainerIDs = append(pendingContainerIDs, acceptedContainerIDs...)
	toProcess := make([]snowman.Block, 0, len(acceptedContainerIDs))
	b.Config.Ctx.Log.Debug("Starting bootstrapping with %d pending blocks and %d from the accepted frontier",
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

	// Process received blocks
	for _, blk := range toProcess {
		if err := b.process(blk, nil); err != nil {
			return err
		}
	}

	if numPending := b.Blocked.NumMissingIDs(); numPending == 0 {
		return b.checkFinish()
	}
	return nil
}

// Get block [blkID] and its ancestors from a validator
func (b *bootstrapper) fetch(blkID ids.ID) error {
	// Make sure we haven't already requested this block
	if b.OutstandingRequests.Contains(blkID) {
		return nil
	}

	// Make sure we don't already have this block
	if _, err := b.VM.GetBlock(blkID); err == nil {
		if numPending := b.Blocked.NumMissingIDs(); numPending == 0 {
			return b.checkFinish()
		}
		return nil
	}

	validators, err := b.Config.Beacons.Sample(1) // validator to send request to
	if err != nil {
		return fmt.Errorf("dropping request for %s as there are no validators", blkID)
	}
	validatorID := validators[0].ID()
	b.RequestID++

	b.OutstandingRequests.Add(validatorID, b.RequestID, blkID)
	b.Config.Sender.SendGetAncestors(validatorID, b.RequestID, blkID) // request block and ancestors
	return nil
}

// GetAncestors implements the Engine interface
func (b *bootstrapper) GetAncestors(vdr ids.ShortID, requestID uint32, blkID ids.ID) error {
	ancestorsBytes, err := block.GetAncestors(
		b.VM,
		blkID,
		b.Config.MultiputMaxContainersSent,
		constants.MaxContainersLen,
		b.Config.MaxTimeGetAncestors,
	)
	if err != nil {
		b.Config.Ctx.Log.Verbo("couldn't get ancestors with %s. Dropping GetAncestors(%s, %d, %s)",
			err, vdr, requestID, blkID)
		return nil
	}

	b.getAncestorsBlks.Observe(float64(len(ancestorsBytes)))
	b.Config.Sender.SendMultiPut(vdr, requestID, ancestorsBytes)
	return nil
}

// MultiPut handles the receipt of multiple containers. Should be received in response to a GetAncestors message to [vdr]
// with request ID [requestID]
func (b *bootstrapper) MultiPut(vdr ids.ShortID, requestID uint32, blks [][]byte) error {
	lenBlks := len(blks)
	if lenBlks == 0 {
		b.Config.Ctx.Log.Debug("MultiPut(%s, %d) contains no blocks", vdr, requestID)
		return b.GetAncestorsFailed(vdr, requestID)
	}
	if lenBlks > b.Config.MultiputMaxContainersReceived {
		blks = blks[:b.Config.MultiputMaxContainersReceived]
		b.Config.Ctx.Log.Debug("ignoring %d containers in multiput(%s, %d)",
			lenBlks-b.Config.MultiputMaxContainersReceived, vdr, requestID)
	}

	// Make sure this is in response to a request we made
	wantedBlkID, ok := b.OutstandingRequests.Remove(vdr, requestID)
	if !ok { // this message isn't in response to a request we made
		b.Config.Ctx.Log.Debug("received unexpected MultiPut from %s with ID %d", vdr, requestID)
		return nil
	}

	blocks, err := block.BatchedParseBlock(b.VM, blks)
	if err != nil { // the provided blocks couldn't be parsed
		b.Config.Ctx.Log.Debug("failed to parse blocks in MultiPut from %s with ID %d", vdr, requestID)
		return b.fetch(wantedBlkID)
	}

	if len(blocks) == 0 {
		b.Config.Ctx.Log.Debug("parsing blocks returned an empty set of blocks from %s with ID %d", vdr, requestID)
		return b.fetch(wantedBlkID)
	}

	requestedBlock := blocks[0]
	if actualID := requestedBlock.ID(); actualID != wantedBlkID {
		b.Config.Ctx.Log.Debug("expected the first block to be the requested block, %s, but is %s",
			wantedBlkID, actualID)
		return b.fetch(wantedBlkID)
	}

	blockSet := make(map[ids.ID]snowman.Block, len(blocks))
	for _, block := range blocks[1:] {
		blockSet[block.ID()] = block
	}
	return b.process(requestedBlock, blockSet)
}

// GetAncestorsFailed is called when a GetAncestors message we sent fails
func (b *bootstrapper) GetAncestorsFailed(vdr ids.ShortID, requestID uint32) error {
	blkID, ok := b.OutstandingRequests.Remove(vdr, requestID)
	if !ok {
		b.Config.Ctx.Log.Debug("GetAncestorsFailed(%s, %d) called but there was no outstanding request to this validator with this ID",
			vdr, requestID)
		return nil
	}
	// Send another request for this
	return b.fetch(blkID)
}

func (b *bootstrapper) Timeout() error {
	if !b.awaitingTimeout {
		return errUnexpectedTimeout
	}
	b.awaitingTimeout = false

	if !b.Config.Subnet.IsBootstrapped() {
		return b.RestartBootstrap(true)
	}
	return b.finish()
}

// process a block
func (b *bootstrapper) process(blk snowman.Block, processingBlocks map[ids.ID]snowman.Block) error {
	status := blk.Status()
	blkID := blk.ID()
	blkHeight := blk.Height()
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
		b.NumFetched++                                      // Progress tracker
		if b.NumFetched%common.StatusUpdateFrequency == 0 { // Periodically print progress
			if !b.Restarted {
				b.Config.Ctx.Log.Info("fetched %d of %d blocks", b.NumFetched, b.tipHeight-b.startingHeight)
			} else {
				b.Config.Ctx.Log.Debug("fetched %d of %d blocks", b.NumFetched, b.tipHeight-b.startingHeight)
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

	if numPending := b.Blocked.NumMissingIDs(); numPending == 0 {
		return b.checkFinish()
	}
	return nil
}

// checkFinish repeatedly executes pending transactions and requests new frontier vertices until there aren't any new ones
// after which it finishes the bootstrap process
func (b *bootstrapper) checkFinish() error {
	if b.IsBootstrapped() || b.awaitingTimeout {
		return nil
	}

	if !b.Restarted {
		b.Config.Ctx.Log.Info("bootstrapping fetched %d blocks. Executing state transitions...", b.NumFetched)
	} else {
		b.Config.Ctx.Log.Debug("bootstrapping fetched %d blocks. Executing state transitions...", b.NumFetched)
	}

	executedBlocks, err := b.Blocked.ExecuteAll(b.Config.Ctx, b, b.Restarted, b.Config.Ctx.ConsensusDispatcher, b.Config.Ctx.DecisionDispatcher)
	if err != nil || b.Halted() {
		return err
	}

	previouslyExecuted := b.executedStateTransitions
	b.executedStateTransitions = executedBlocks

	// Note that executedBlocks < c*previouslyExecuted ( 0 <= c < 1 ) is enforced
	// so that the bootstrapping process will terminate even as new blocks are
	// being issued.
	if b.Config.RetryBootstrap && executedBlocks > 0 && executedBlocks < previouslyExecuted/2 {
		return b.RestartBootstrap(true)
	}

	// If there is an additional callback, notify them that this chain has been
	// synced.
	if b.Bootstrapped != nil {
		b.Bootstrapped()
	}

	// Notify the subnet that this chain is synced
	b.Config.Subnet.Bootstrapped(b.Config.Ctx.ChainID)

	// If the subnet hasn't finished bootstrapping, this chain should remain
	// syncing.
	if !b.Config.Subnet.IsBootstrapped() {
		if !b.Restarted {
			b.Config.Ctx.Log.Info("waiting for the remaining chains in this subnet to finish syncing")
		} else {
			b.Config.Ctx.Log.Debug("waiting for the remaining chains in this subnet to finish syncing")
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
	if err := b.VM.Bootstrapped(); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has finished: %w",
			err)
	}

	// Start consensus
	if err := b.OnFinished(b.RequestID); err != nil {
		return err
	}
	return nil
}

// Connected implements the Engine interface.
func (b *bootstrapper) Connected(nodeID ids.ShortID) error {
	if err := b.VM.Connected(nodeID); err != nil {
		return err
	}

	if err := b.Starter.AddWeightForNode(nodeID); err != nil {
		return err
	}

	if b.Starter.CanStart() {
		b.Starter.MarkStart()
		return b.Startup()
	}

	return nil
}

// Disconnected implements the Engine interface.
func (b *bootstrapper) Disconnected(nodeID ids.ShortID) error {
	if err := b.VM.Disconnected(nodeID); err != nil {
		return err
	}

	return b.Starter.RemoveWeightForNode(nodeID)
}

func (b *bootstrapper) GetVM() common.VM                { return b.VM }
func (b *bootstrapper) Context() *snow.ConsensusContext { return b.Config.Ctx }
func (b *bootstrapper) IsBootstrapped() bool            { return b.Config.Ctx.IsBootstrapped() }

func (b *bootstrapper) HealthCheck() (interface{}, error) {
	vmIntf, vmErr := b.VM.HealthCheck()
	intf := map[string]interface{}{
		"consensus": struct{}{},
		"vm":        vmIntf,
	}
	return intf, vmErr
}
