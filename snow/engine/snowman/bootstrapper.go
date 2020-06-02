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

	// blocks that have outstanding get requests
	blkReqs common.Requests

	VM ChainVM

	Bootstrapped func()
}

type bootstrapper struct {
	BootstrapConfig
	metrics
	common.Bootstrapper

	pending    ids.Set
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
	if err := b.VM.Bootstrapping(); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has started: %w",
			err)
	}

	for _, blkID := range acceptedContainerIDs.List() {
		if err := b.fetch(blkID); err != nil {
			return err
		}
	}

	if numPending := b.pending.Len(); numPending == 0 {
		// TODO: This typically indicates bootstrapping has failed, so this
		// should be handled appropriately
		return b.finish()
	}
	return nil
}

// Put ...
func (b *bootstrapper) Put(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	b.BootstrapConfig.Context.Log.Verbo("Put called for blkID %s", blkID)

	blk, err := b.VM.ParseBlock(blkBytes)
	if err != nil {
		b.BootstrapConfig.Context.Log.Debug("ParseBlock failed due to %s for block:\n%s",
			err,
			formatting.DumpBytes{Bytes: blkBytes})

		b.GetFailed(vdr, requestID)
		return nil
	}

	if !b.pending.Contains(blk.ID()) {
		b.BootstrapConfig.Context.Log.Debug("Validator %s sent an unrequested block:\n%s",
			vdr,
			formatting.DumpBytes{Bytes: blkBytes})

		b.GetFailed(vdr, requestID)
		return nil
	}

	return b.addBlock(blk)
}

// GetFailed ...
func (b *bootstrapper) GetFailed(vdr ids.ShortID, requestID uint32) error {
	blkID, ok := b.blkReqs.Remove(vdr, requestID)
	if !ok {
		b.BootstrapConfig.Context.Log.Debug("GetFailed called without sending the corresponding Get message from %s",
			vdr)
		return nil
	}
	b.sendRequest(blkID)
	return nil
}

func (b *bootstrapper) fetch(blkID ids.ID) error {
	if b.pending.Contains(blkID) {
		return nil
	}

	blk, err := b.VM.GetBlock(blkID)
	if err != nil {
		b.sendRequest(blkID)
		return nil
	}
	return b.storeBlock(blk)
}

func (b *bootstrapper) sendRequest(blkID ids.ID) {
	validators := b.BootstrapConfig.Validators.Sample(1)
	if len(validators) == 0 {
		b.BootstrapConfig.Context.Log.Error("Dropping request for %s as there are no validators", blkID)
		return
	}
	validatorID := validators[0].ID()
	b.RequestID++

	b.blkReqs.RemoveAny(blkID)
	b.blkReqs.Add(validatorID, b.RequestID, blkID)

	b.pending.Add(blkID)
	b.BootstrapConfig.Sender.Get(validatorID, b.RequestID, blkID)

	b.numPendingRequests.Set(float64(b.pending.Len()))
}

func (b *bootstrapper) addBlock(blk snowman.Block) error {
	if err := b.storeBlock(blk); err != nil {
		return err
	}

	if numPending := b.pending.Len(); numPending == 0 {
		return b.finish()
	}
	return nil
}

func (b *bootstrapper) storeBlock(blk snowman.Block) error {
	status := blk.Status()
	blkID := blk.ID()
	for status == choices.Processing {
		b.pending.Remove(blkID)

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

		blk = blk.Parent()
		status = blk.Status()
		blkID = blk.ID()
	}

	switch status := blk.Status(); status {
	case choices.Unknown:
		b.sendRequest(blkID)
	case choices.Accepted:
		b.BootstrapConfig.Context.Log.Verbo("Bootstrapping confirmed %s", blkID)
	case choices.Rejected:
		return fmt.Errorf("bootstrapping wants to accept %s, however it was previously rejected", blkID)
	}

	numPending := b.pending.Len()
	b.numPendingRequests.Set(float64(numPending))
	return nil
}

func (b *bootstrapper) finish() error {
	if b.finished {
		return nil
	}

	if err := b.executeAll(b.Blocked, b.numBlocked); err != nil {
		return err
	}

	if err := b.VM.Bootstrapped(); err != nil {
		return fmt.Errorf("failed to notify VM that bootstrapping has finished: %w",
			err)
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
