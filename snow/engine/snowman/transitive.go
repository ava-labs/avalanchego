// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/events"
	"github.com/ava-labs/gecko/utils/formatting"
)

// Transitive implements the Engine interface by attempting to fetch all
// transitive dependencies.
type Transitive struct {
	Config
	bootstrapper

	polls polls // track people I have asked for their preference

	blkReqs, pending ids.Set // prevent asking validators for the same block

	blocked events.Blocker // track operations that are blocked on blocks

	bootstrapped bool
}

// Initialize implements the Engine interface
func (t *Transitive) Initialize(config Config) {
	config.Context.Log.Info("Initializing Snowman consensus")

	t.Config = config
	t.metrics.Initialize(config.Context.Log, config.Params.Namespace, config.Params.Metrics)

	t.onFinished = t.finishBootstrapping
	t.bootstrapper.Initialize(config.BootstrapConfig)

	t.polls.log = config.Context.Log
	t.polls.numPolls = t.numPolls
	t.polls.alpha = t.Params.Alpha
	t.polls.m = make(map[uint32]poll)
}

func (t *Transitive) finishBootstrapping() {
	tail := t.Config.VM.LastAccepted()
	t.Config.VM.SetPreference(tail)
	t.Consensus.Initialize(t.Config.Context, t.Params, tail)
	t.bootstrapped = true
}

// Shutdown implements the Engine interface
func (t *Transitive) Shutdown() {
	t.Config.Context.Log.Info("Shutting down Snowman consensus")
	t.Config.VM.Shutdown()
}

// Context implements the Engine interface
func (t *Transitive) Context() *snow.Context { return t.Config.Context }

// Get implements the Engine interface
func (t *Transitive) Get(vdr ids.ShortID, requestID uint32, blkID ids.ID) {
	if blk, err := t.Config.VM.GetBlock(blkID); err == nil {
		t.Config.Sender.Put(vdr, requestID, blkID, blk.Bytes())
	}
}

// Put implements the Engine interface
func (t *Transitive) Put(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) {
	t.Config.Context.Log.Verbo("Put called for blockID %s", blkID)

	if !t.bootstrapped {
		t.bootstrapper.Put(vdr, requestID, blkID, blkBytes)
		return
	}

	blk, err := t.Config.VM.ParseBlock(blkBytes)
	if err != nil {
		t.Config.Context.Log.Warn("ParseBlock failed due to %s for block:\n%s",
			err,
			formatting.DumpBytes{Bytes: blkBytes})
		t.GetFailed(vdr, requestID, blkID)
		return
	}

	t.insertFrom(vdr, blk)
}

// GetFailed implements the Engine interface
func (t *Transitive) GetFailed(vdr ids.ShortID, requestID uint32, blkID ids.ID) {
	if !t.bootstrapped {
		t.bootstrapper.GetFailed(vdr, requestID, blkID)
		return
	}

	t.pending.Remove(blkID)
	t.blocked.Abandon(blkID)
	t.blkReqs.Remove(blkID)

	// Tracks performance statistics
	t.numBlockedBlk.Set(float64(t.pending.Len()))
}

// PullQuery implements the Engine interface
func (t *Transitive) PullQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID) {
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("Dropping PullQuery for %s due to bootstrapping", blkID)
		return
	}

	c := &convincer{
		consensus: t.Consensus,
		sender:    t.Config.Sender,
		vdr:       vdr,
		requestID: requestID,
	}

	if !t.reinsertFrom(vdr, blkID) {
		c.deps.Add(blkID)
	}

	t.blocked.Register(c)
}

// PushQuery implements the Engine interface
func (t *Transitive) PushQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID, blk []byte) {
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("Dropping PushQuery for %s due to bootstrapping", blkID)
		return
	}

	t.Put(vdr, requestID, blkID, blk)
	t.PullQuery(vdr, requestID, blkID)
}

// Chits implements the Engine interface
func (t *Transitive) Chits(vdr ids.ShortID, requestID uint32, votes ids.Set) {
	if !t.bootstrapped {
		t.Config.Context.Log.Warn("Dropping Chits due to bootstrapping")
		return
	}

	// Since this is snowman, there should only be one ID in the vote set
	if votes.Len() != 1 {
		t.Config.Context.Log.Warn("Chits was called with the wrong number of votes %d. ValidatorID: %s, RequestID: %d", votes.Len(), vdr, requestID)
		t.QueryFailed(vdr, requestID)
		return
	}
	vote := votes.List()[0]

	t.Config.Context.Log.Verbo("Chit was called. RequestID: %v. Vote: %s", requestID, vote)

	v := &voter{
		t:         t,
		vdr:       vdr,
		requestID: requestID,
		response:  vote,
	}

	if !t.reinsertFrom(vdr, vote) {
		v.deps.Add(vote)
	}

	t.blocked.Register(v)
}

// QueryFailed implements the Engine interface
func (t *Transitive) QueryFailed(vdr ids.ShortID, requestID uint32) {
	if !t.bootstrapped {
		t.Config.Context.Log.Warn("Dropping QueryFailed due to bootstrapping")
		return
	}

	t.blocked.Register(&voter{
		t:         t,
		vdr:       vdr,
		requestID: requestID,
	})
}

// Notify implements the Engine interface
func (t *Transitive) Notify(msg common.Message) {
	if !t.bootstrapped {
		t.Config.Context.Log.Warn("Dropping Notify due to bootstrapping")
		return
	}

	t.Config.Context.Log.Verbo("Snowman engine notified of %s from the vm", msg)
	switch msg {
	case common.PendingTxs:
		if blk, err := t.Config.VM.BuildBlock(); err == nil {
			if status := blk.Status(); status != choices.Processing {
				t.Config.Context.Log.Warn("Attempting to issue a block with status: %s, expected Processing", status)
			}
			parentID := blk.Parent().ID()
			if pref := t.Consensus.Preference(); !parentID.Equals(pref) {
				t.Config.Context.Log.Warn("Built block with parent: %s, expected %s", parentID, pref)
			}
			if t.insertAll(blk) {
				t.Config.Context.Log.Verbo("Successfully issued new block from the VM")
			} else {
				t.Config.Context.Log.Warn("VM.BuildBlock returned a block that is pending for ancestors")
			}
		} else {
			t.Config.Context.Log.Verbo("VM.BuildBlock errored with %s", err)
		}
	default:
		t.Config.Context.Log.Warn("Unexpected message from the VM: %s", msg)
	}
}

func (t *Transitive) repoll() {
	prefID := t.Consensus.Preference()
	t.pullSample(prefID)
}

func (t *Transitive) reinsertFrom(vdr ids.ShortID, blkID ids.ID) bool {
	blk, err := t.Config.VM.GetBlock(blkID)
	if err != nil {
		t.sendRequest(vdr, blkID)
		return false
	}
	return t.insertFrom(vdr, blk)
}

func (t *Transitive) insertFrom(vdr ids.ShortID, blk snowman.Block) bool {
	blkID := blk.ID()
	for !t.Consensus.Issued(blk) && !t.pending.Contains(blkID) {
		t.insert(blk)

		parent := blk.Parent()
		parentID := parent.ID()
		if parentStatus := parent.Status(); !parentStatus.Fetched() {
			t.sendRequest(vdr, parentID)
			return false
		}

		blk = parent
		blkID = parentID
	}
	return !t.pending.Contains(blkID)
}

func (t *Transitive) insertAll(blk snowman.Block) bool {
	blkID := blk.ID()
	for blk.Status().Fetched() && !t.Consensus.Issued(blk) && !t.pending.Contains(blkID) {
		t.insert(blk)
		blk = blk.Parent()
	}
	return !t.pending.Contains(blkID)
}

func (t *Transitive) insert(blk snowman.Block) {
	blkID := blk.ID()

	t.pending.Add(blkID)
	t.blkReqs.Remove(blkID)

	i := &issuer{
		t:   t,
		blk: blk,
	}

	if parent := blk.Parent(); !t.Consensus.Issued(parent) {
		parentID := parent.ID()
		t.Config.Context.Log.Verbo("Block waiting for parent %s", parentID)
		i.deps.Add(parentID)
	}

	t.blocked.Register(i)

	// Tracks performance statistics
	t.numBlkRequests.Set(float64(t.blkReqs.Len()))
	t.numBlockedBlk.Set(float64(t.pending.Len()))
}

func (t *Transitive) sendRequest(vdr ids.ShortID, blkID ids.ID) {
	if !t.blkReqs.Contains(blkID) {
		t.blkReqs.Add(blkID)

		t.numBlkRequests.Set(float64(t.blkReqs.Len())) // Tracks performance statistics

		t.RequestID++
		t.Config.Context.Log.Verbo("Sending Get message for %s", blkID)
		t.Config.Sender.Get(vdr, t.RequestID, blkID)
	}
}

func (t *Transitive) pullSample(blkID ids.ID) {
	t.Config.Context.Log.Verbo("About to sample from: %s", t.Config.Validators)
	p := t.Consensus.Parameters()
	vdrs := t.Config.Validators.Sample(p.K)
	vdrSet := ids.ShortSet{}
	for _, vdr := range vdrs {
		vdrSet.Add(vdr.ID())
	}

	t.RequestID++
	if numVdrs := len(vdrs); numVdrs == p.K && t.polls.Add(t.RequestID, vdrSet.Len()) {
		t.Config.Sender.PullQuery(vdrSet, t.RequestID, blkID)
	} else if numVdrs < p.K {
		t.Config.Context.Log.Error("Query for %s was dropped due to an insufficient number of validators", blkID)
	}
}

func (t *Transitive) pushSample(blk snowman.Block) bool {
	t.Config.Context.Log.Verbo("About to sample from: %s", t.Config.Validators)
	p := t.Consensus.Parameters()
	vdrs := t.Config.Validators.Sample(p.K)
	vdrSet := ids.ShortSet{}
	for _, vdr := range vdrs {
		vdrSet.Add(vdr.ID())
	}

	t.RequestID++
	queryIssued := false
	if numVdrs := len(vdrs); numVdrs == p.K && t.polls.Add(t.RequestID, vdrSet.Len()) {
		t.Config.Sender.PushQuery(vdrSet, t.RequestID, blk.ID(), blk.Bytes())
		queryIssued = true
	} else if numVdrs < p.K {
		t.Config.Context.Log.Error("Query for %s was dropped due to an insufficient number of validators", blk.ID())
	}
	return queryIssued
}

func (t *Transitive) deliver(blk snowman.Block) {
	if t.Consensus.Issued(blk) {
		return
	}

	blkID := blk.ID()
	t.pending.Remove(blkID)

	if err := blk.Verify(); err != nil {
		t.Config.Context.Log.Debug("Block failed verification due to %s, dropping block", err)
		t.blocked.Abandon(blkID)
		t.numBlockedBlk.Set(float64(t.pending.Len())) // Tracks performance statistics
		return
	}

	t.Config.Context.Log.Verbo("Adding block to consensus: %s", blkID)
	t.Consensus.Add(blk)
	polled := t.pushSample(blk)

	added := []snowman.Block{}
	dropped := []snowman.Block{}
	switch blk := blk.(type) {
	case OracleBlock:
		for _, blk := range blk.Options() {
			if err := blk.Verify(); err != nil {
				t.Config.Context.Log.Debug("Block failed verification due to %s, dropping block", err)
				t.blocked.Abandon(blk.ID())
				dropped = append(dropped, blk)
			} else {
				t.Consensus.Add(blk)
				t.pushSample(blk)
				added = append(added, blk)
			}
		}
	}

	t.Config.VM.SetPreference(t.Consensus.Preference())
	t.blocked.Fulfill(blkID)

	for _, blk := range added {
		blkID := blk.ID()
		t.pending.Remove(blkID)
		t.blocked.Fulfill(blkID)
	}
	for _, blk := range dropped {
		blkID := blk.ID()
		t.pending.Remove(blkID)
		t.blocked.Abandon(blkID)
	}

	if polled && len(t.polls.m) < t.Params.ConcurrentRepolls {
		t.repoll()
	}

	// Tracks performance statistics
	t.numBlkRequests.Set(float64(t.blkReqs.Len()))
	t.numBlockedBlk.Set(float64(t.pending.Len()))
}
