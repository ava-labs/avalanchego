// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"fmt"
	"math"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/prometheus/client_golang/prometheus"
)

const ()

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

	numProcessed uint32

	// outstandingRequests tracks which validators were asked for which containers in which requests
	outstandingRequests common.Requests

	finished   bool
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

// CurrentAcceptedFrontier ...
func (b *bootstrapper) CurrentAcceptedFrontier() ids.Set {
	acceptedFrontier := ids.Set{}
	acceptedFrontier.Add(b.VM.LastAccepted())
	return acceptedFrontier
}

// FilterAccepted ...
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
			b.process(blk)
		} else if err := b.fetch(blkID); err != nil {
			return err
		}
	}

	if numPending := b.outstandingRequests.Len(); numPending == 0 {
		// TODO: This typically indicates bootstrapping has failed, so this
		// should be handled appropriately
		return b.finish()
	}
	return nil
}

// Get a block and its ancestors
func (b *bootstrapper) fetch(blkID ids.ID) error {
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

// Put ...
func (b *bootstrapper) Put(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	b.BootstrapConfig.Context.Log.Verbo("Put called for blkID %s", blkID)

	vtx, err := b.VM.ParseBlock(blkBytes) // Persists the vtx. vtx.Status() not Unknown.
	if err != nil {
		b.BootstrapConfig.Context.Log.Debug("Failed to parse block: %w", err)
		b.BootstrapConfig.Context.Log.Verbo("block: %s", formatting.DumpBytes{Bytes: blkBytes})
		return b.GetFailed(vdr, requestID)
	}
	parsedBlockID := vtx.ID() // Actual ID of the block we just got

	// The validator that sent this message said the ID of the block inside was [blkID]
	// but actually it's [parsedBlockID]
	if !parsedBlockID.Equals(blkID) {
		return b.GetFailed(vdr, requestID)
	}

	expectedBlkID, ok := b.outstandingRequests.Remove(vdr, requestID)
	if !ok { // there was no outstanding request from this validator for a request with this ID
		if requestID != math.MaxUint32 { // request ID of math.MaxUint32 means the put was a gossip message. In that case, just return.
			b.BootstrapConfig.Context.Log.Debug("Unexpected Put. There is no outstanding request to %s with request ID %d", vdr, requestID)
		}
		return nil
	}

	if !expectedBlkID.Equals(parsedBlockID) {
		b.BootstrapConfig.Context.Log.Debug("Put(%s, %d) contains block %s but should contain block %s.", vdr, requestID, parsedBlockID, expectedBlkID)
		b.outstandingRequests.Add(vdr, requestID, expectedBlkID) // Just going to be removed by GetFailed
		return b.GetFailed(vdr, requestID)
	}

	return b.process(vtx)
}

// MultiPut ...
func (b *bootstrapper) MultiPut(vdr ids.ShortID, requestID uint32, blks [][]byte) error {
	b.BootstrapConfig.Context.Log.Verbo("in MultiPut(%s, %d). len(blks): %d", vdr, requestID, len(blks)) // TODO remove
	// Make sure this is in response to a request we made
	wantedBlkID, ok := b.outstandingRequests.Remove(vdr, requestID)
	if !ok {
		b.BootstrapConfig.Context.Log.Debug("received unexpected MultiPut from %s with ID %d", vdr, requestID)
		return nil
	}

	var wantedBlk snowman.Block = nil // the block that this MultiPut is in response to
	for i, blkBytes := range blks {
		if i > common.MaxContainersPerMultiPut {
			b.BootstrapConfig.Context.Log.Debug("MultiPut from %s contains more than maximum number of vertices. Request ID: %d", vdr, requestID)
			break
		}
		blk, err := b.VM.ParseBlock(blkBytes) // Persists the blk
		if err != nil {
			b.BootstrapConfig.Context.Log.Debug("Failed to parse block: %w", err)
			b.BootstrapConfig.Context.Log.Verbo("block: %s", formatting.DumpBytes{Bytes: blkBytes})
		}
		if blk.ID().Equals(wantedBlkID) {
			wantedBlk = blk // found the block we wanted
		}
	}

	// This MultiPut was supposed to include [wantedBlkID] but it didn't
	if wantedBlk == nil {
		b.outstandingRequests.Add(vdr, requestID, wantedBlkID) // immediately removed by getFailed
		return b.GetFailed(vdr, requestID)
	}

	return b.process(wantedBlk)
}

// GetFailed is called when a Get message we sent fails
func (b *bootstrapper) GetFailed(vdr ids.ShortID, requestID uint32) error {
	blkID, ok := b.outstandingRequests.Remove(vdr, requestID)
	if !ok {
		b.BootstrapConfig.Context.Log.Debug("GetFailed(%s, %d) called but there was no outstanding request to this validator with this ID", vdr, requestID)
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
			b.BootstrapConfig.Context.Log.Debug("processed %d blocks", b.numProcessed)
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
