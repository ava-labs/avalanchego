// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/prometheus/client_golang/prometheus"
)

// BootstrapConfig ...
type BootstrapConfig struct {
	common.Config

	// Blocked tracks operations that are blocked on blocks
	Blocked *queue.Jobs

	VM ChainVM

	Bootstrapped func()
}

type bootstrapper struct {
	BootstrapConfig
	metrics
	common.Bootstrapper

	// true if all of the vertices in the original accepted frontier have been processed
	processedStartingAcceptedFrontier bool

	// Number of blocks processed
	numProcessed uint32

	// tracks which validators were asked for which containers in which requests
	outstandingRequests common.Requests

	// true if bootstrapping is done
	finished bool

	// Called when bootstrapping is done
	onFinished func() error
}

// Initialize this engine.
func (b *bootstrapper) Initialize(config BootstrapConfig) error {
	b.BootstrapConfig = config

	b.Blocked.SetParser(&parser{
		log:         config.Context.Log,
		numAccepted: b.numBootstrapped,
		numDropped:  b.numDropped,
		vm:          b.VM,
	})

	config.Bootstrapable = b
	b.Bootstrapper.Initialize(config.Config)
	return nil
}

// CurrentAcceptedFrontier returns the last accepted block
func (b *bootstrapper) CurrentAcceptedFrontier() ids.Set {
	acceptedFrontier := ids.Set{}
	acceptedFrontier.Add(b.VM.LastAccepted())
	return acceptedFrontier
}

// FilterAccepted returns the blocks in [containerIDs] that we have accepted
func (b *bootstrapper) FilterAccepted(containerIDs ids.Set) ids.Set {
	acceptedIDs := ids.Set{}
	for _, blkID := range containerIDs.List() {
		if blk, err := b.VM.GetBlock(blkID); err == nil && blk.Status() == choices.Accepted {
			acceptedIDs.Add(blkID)
		}
	}
	return acceptedIDs
}

// ForceAccepted ...
func (b *bootstrapper) ForceAccepted(acceptedContainerIDs ids.Set) error {
	for _, blkID := range acceptedContainerIDs.List() {
		if blk, err := b.VM.GetBlock(blkID); err == nil {
			if err := b.process(blk); err != nil {
				return err
			}
		} else if err := b.fetch(blkID); err != nil {
			return err
		}
	}

	b.processedStartingAcceptedFrontier = true
	if numPending := b.outstandingRequests.Len(); numPending == 0 {
		return b.finish()
	}
	return nil
}

// Get block [blkID] and its ancestors from a validator
func (b *bootstrapper) fetch(blkID ids.ID) error {
	// Make sure we haven't already requested this block
	if b.outstandingRequests.Contains(blkID) {
		return nil
	}

	// Make sure we don't already have this block
	if _, err := b.VM.GetBlock(blkID); err == nil {
		return nil
	}

	validators := b.BootstrapConfig.Validators.Sample(1) // validator to send request to
	if len(validators) == 0 {
		return fmt.Errorf("Dropping request for %s as there are no validators", blkID)
	}
	validatorID := validators[0].ID()
	b.RequestID++

	b.outstandingRequests.Add(validatorID, b.RequestID, blkID)
	b.BootstrapConfig.Sender.GetAncestors(validatorID, b.RequestID, blkID) // request block and ancestors
	return nil
}

// MultiPut handles the receipt of multiple containers. Should be received in response to a GetAncestors message to [vdr]
// with request ID [requestID]
func (b *bootstrapper) MultiPut(vdr ids.ShortID, requestID uint32, blks [][]byte) error {
	if lenBlks := len(blks); lenBlks > common.MaxContainersPerMultiPut {
		b.BootstrapConfig.Context.Log.Debug("MultiPut(%s, %d) contains more than maximum number of blocks", vdr, requestID)
		return b.GetAncestorsFailed(vdr, requestID)
	} else if lenBlks == 0 {
		b.BootstrapConfig.Context.Log.Debug("MultiPut(%s, %d) contains no blocks", vdr, requestID)
		return b.GetAncestorsFailed(vdr, requestID)
	}

	// Make sure this is in response to a request we made
	wantedBlkID, ok := b.outstandingRequests.Remove(vdr, requestID)
	if !ok { // this message isn't in response to a request we made
		b.BootstrapConfig.Context.Log.Debug("received unexpected MultiPut from %s with ID %d", vdr, requestID)
		return nil
	}

	wantedBlk, err := b.VM.ParseBlock(blks[0]) // the block we requested
	if err != nil {
		b.BootstrapConfig.Context.Log.Debug("Failed to parse requested block %s: %w", wantedBlkID, err)
		return b.fetch(wantedBlkID)
	} else if actualID := wantedBlk.ID(); !actualID.Equals(wantedBlkID) {
		b.BootstrapConfig.Context.Log.Debug("expected the first block to be the requested block, %s, but is %s", wantedBlk, actualID)
		return b.fetch(wantedBlkID)
	}

	for _, blkBytes := range blks {
		if _, err := b.VM.ParseBlock(blkBytes); err != nil { // persists the block
			b.BootstrapConfig.Context.Log.Debug("Failed to parse block: %w", err)
			b.BootstrapConfig.Context.Log.Verbo("block: %s", formatting.DumpBytes{Bytes: blkBytes})
		}
	}

	return b.process(wantedBlk)
}

// GetAncestorsFailed is called when a GetAncestors message we sent fails
func (b *bootstrapper) GetAncestorsFailed(vdr ids.ShortID, requestID uint32) error {
	blkID, ok := b.outstandingRequests.Remove(vdr, requestID)
	if !ok {
		b.BootstrapConfig.Context.Log.Debug("GetAncestorsFailed(%s, %d) called but there was no outstanding request to this validator with this ID", vdr, requestID)
		return nil
	}
	// Send another request for this
	return b.fetch(blkID)
}

// process a block
func (b *bootstrapper) process(blk snowman.Block) error {
	status := blk.Status()
	blkID := blk.ID()
	for status == choices.Processing {
		b.numProcessed++                                      // Progress tracker
		if b.numProcessed%common.StatusUpdateFrequency == 0 { // Periodically print progress
			b.BootstrapConfig.Context.Log.Info("processed %d blocks", b.numProcessed)
		}
		if err := b.Blocked.Push(&blockJob{
			numAccepted: b.numBootstrapped,
			numDropped:  b.numDropped,
			blk:         blk,
		}); err == nil {
			b.numBlocked.Inc()
		}

		if err := b.Blocked.Commit(); err != nil {
			return err
		}

		// Process this block's parent
		blk = blk.Parent()
		status = blk.Status()
		blkID = blk.ID()
	}

	switch status := blk.Status(); status {
	case choices.Unknown:
		if err := b.fetch(blkID); err != nil {
			return err
		}
	case choices.Rejected: // Should never happen
		return fmt.Errorf("bootstrapping wants to accept %s, however it was previously rejected", blkID)
	}

	if numPending := b.outstandingRequests.Len(); numPending == 0 && b.processedStartingAcceptedFrontier {
		return b.finish()
	}
	return nil
}

func (b *bootstrapper) finish() error {
	if b.finished {
		return nil
	}

	if err := b.executeAll(b.Blocked, b.numBlocked); err != nil {
		return err
	}

	// Start consensus
	if err := b.onFinished(); err != nil {
		return err
	}
	b.finished = true

	if b.Bootstrapped != nil {
		b.Bootstrapped()
	}
	return nil
}

func (b *bootstrapper) executeAll(jobs *queue.Jobs, numBlocked prometheus.Gauge) error {
	for job, err := jobs.Pop(); err == nil; job, err = jobs.Pop() {
		numBlocked.Dec()
		if err := jobs.Execute(job); err != nil {
			return err
		}
		if err := jobs.Commit(); err != nil {
			return err
		}
	}
	return nil
}
