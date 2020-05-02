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

	// track outstanding preference requests
	polls polls

	// blocks that have outstanding get requests
	blkReqs common.Requests

	// blocks that are fetched but haven't been issued due to missing
	// dependencies
	pending ids.Set

	// operations that are blocked on a block being issued. This could be
	// issuing another block, responding to a query, or applying votes to
	// consensus
	blocked events.Blocker

	// mark for if the engine has been bootstrapped or not
	bootstrapped bool
}

// Initialize implements the Engine interface
func (t *Transitive) Initialize(config Config) {
	config.Context.Log.Info("Initializing Snowman consensus")

	t.Config = config
	t.metrics.Initialize(
		config.Context.Log,
		config.Params.Namespace,
		config.Params.Metrics,
	)

	t.onFinished = t.finishBootstrapping
	t.bootstrapper.Initialize(config.BootstrapConfig)

	t.polls.log = config.Context.Log
	t.polls.numPolls = t.numPolls
	t.polls.alpha = t.Params.Alpha
	t.polls.m = make(map[uint32]poll)
}

// when bootstrapping is finished, this will be called. This initializes the
// consensus engine with the last accepted block.
func (t *Transitive) finishBootstrapping() {
	// set the bootstrapped mark to switch consensus modes
	t.bootstrapped = true

	// initialize consensus to the last accepted blockID
	tailID := t.Config.VM.LastAccepted()
	t.Consensus.Initialize(t.Config.Context, t.Params, tailID)

	// to maintain the invariant that oracle blocks are issued in the correct
	// preferences, we need to handle the case that we are bootstrapping into an
	// oracle block
	tail, err := t.Config.VM.GetBlock(tailID)
	if err != nil {
		t.Config.Context.Log.Error("Failed to get last accepted block due to: %s", err)
		return
	}

	switch blk := tail.(type) {
	case OracleBlock:
		for _, blk := range blk.Options() {
			// note that deliver will set the VM's preference
			t.deliver(blk)
		}
	default:
		// if there aren't blocks we need to deliver on startup, we need to set
		// the preference to the last accepted block
		t.Config.VM.SetPreference(tailID)
	}
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
	blk, err := t.Config.VM.GetBlock(blkID)
	if err != nil {
		// If we failed to get the block, that means either an unexpected error
		// has occurred, the validator is not following the protocol, or the
		// block has been pruned.
		t.Config.Context.Log.Warn("Get called for blockID %s errored with %s",
			blkID,
			err)
		return
	}

	// Respond to the validator with the fetched block and the same requestID.
	t.Config.Sender.Put(vdr, requestID, blkID, blk.Bytes())
}

// Put implements the Engine interface
func (t *Transitive) Put(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) {
	t.Config.Context.Log.Verbo("Put called for blockID %s", blkID)

	// if the engine hasn't been bootstrapped, forward the request to the
	// bootstrapper
	if !t.bootstrapped {
		t.bootstrapper.Put(vdr, requestID, blkID, blkBytes)
		return
	}

	blk, err := t.Config.VM.ParseBlock(blkBytes)
	if err != nil {
		t.Config.Context.Log.Warn("ParseBlock failed due to %s for block:\n%s",
			err,
			formatting.DumpBytes{Bytes: blkBytes})

		// because GetFailed doesn't utilize the assumption that we actually
		// sent a Get message, we can safely call GetFailed here to potentially
		// abandon the request.
		t.GetFailed(vdr, requestID)
		return
	}

	// insert the block into consensus. If the block has already been issued,
	// this will be a noop. If this block has missing dependencies, vdr will
	// receive requests to fill the ancestry. dependencies that have already
	// been fetched, but with missing dependencies themselves won't be requested
	// from the vdr.
	t.insertFrom(vdr, blk)
}

// GetFailed implements the Engine interface
func (t *Transitive) GetFailed(vdr ids.ShortID, requestID uint32) {
	// if the engine hasn't been bootstrapped, forward the request to the
	// bootstrapper
	if !t.bootstrapped {
		t.bootstrapper.GetFailed(vdr, requestID)
		return
	}

	// we don't use the assumption that this function is called after a failed
	// Get message. So we first check to see if we have an outsanding request
	// and also get what the request was for if it exists
	blkID, ok := t.blkReqs.Remove(vdr, requestID)
	if !ok {
		t.Config.Context.Log.Warn("GetFailed called without sending the corresponding Get message from %s",
			vdr)
		return
	}

	// because the get request was dropped, we no longer are expected blkID to
	// be issued.
	t.blocked.Abandon(blkID)
}

// PullQuery implements the Engine interface
func (t *Transitive) PullQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID) {
	// if the engine hasn't been bootstrapped, we aren't ready to respond to
	// queries
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("Dropping PullQuery for %s due to bootstrapping",
			blkID)
		return
	}

	c := &convincer{
		consensus: t.Consensus,
		sender:    t.Config.Sender,
		vdr:       vdr,
		requestID: requestID,
	}

	// if we aren't able to have issued this block, then it is a dependency for
	// this reply
	if !t.reinsertFrom(vdr, blkID) {
		c.deps.Add(blkID)
	}

	t.blocked.Register(c)
}

// PushQuery implements the Engine interface
func (t *Transitive) PushQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) {
	// if the engine hasn't been bootstrapped, we aren't ready to respond to
	// queries
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("Dropping PushQuery for %s due to bootstrapping", blkID)
		return
	}

	blk, err := t.Config.VM.ParseBlock(blkBytes)
	// If the parsing fails, we just drop the request, as we didn't ask for it
	if err != nil {
		t.Config.Context.Log.Warn("ParseBlock failed due to %s for block:\n%s",
			err,
			formatting.DumpBytes{Bytes: blkBytes})
		return
	}

	// insert the block into consensus. If the block has already been issued,
	// this will be a noop. If this block has missing dependencies, vdr will
	// receive requests to fill the ancestry. dependencies that have already
	// been fetched, but with missing dependencies themselves won't be requested
	// from the vdr.
	t.insertFrom(vdr, blk)

	// register the chit request
	t.PullQuery(vdr, requestID, blk.ID())
}

// Chits implements the Engine interface
func (t *Transitive) Chits(vdr ids.ShortID, requestID uint32, votes ids.Set) {
	// if the engine hasn't been bootstrapped, we shouldn't be receiving chits
	if !t.bootstrapped {
		t.Config.Context.Log.Warn("Dropping Chits due to bootstrapping")
		return
	}

	// Since this is snowman, there should only be one ID in the vote set
	if votes.Len() != 1 {
		t.Config.Context.Log.Warn("Chits was called with the wrong number of votes %d. ValidatorID: %s, RequestID: %d",
			votes.Len(),
			vdr,
			requestID)

		// because QueryFailed doesn't utilize the assumption that we actually
		// sent a Query message, we can safely call QueryFailed here to
		// potentially abandon the request.
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

	// if we aren't able to have issued the vote's block, then it is a
	// dependency for applying the vote
	if !t.reinsertFrom(vdr, vote) {
		v.deps.Add(vote)
	}

	t.blocked.Register(v)
}

// QueryFailed implements the Engine interface
func (t *Transitive) QueryFailed(vdr ids.ShortID, requestID uint32) {
	// if the engine hasn't been bootstrapped, we won't have sent a query
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
	// if the engine hasn't been bootstrapped, we shouldn't issuing blocks
	if !t.bootstrapped {
		t.Config.Context.Log.Warn("Dropping Notify due to bootstrapping")
		return
	}

	t.Config.Context.Log.Verbo("Snowman engine notified of %s from the vm", msg)
	switch msg {
	case common.PendingTxs:
		// the pending txs message means we should attempt to build a block.
		blk, err := t.Config.VM.BuildBlock()
		if err != nil {
			t.Config.Context.Log.Verbo("VM.BuildBlock errored with %s", err)
			return
		}

		// a newly created block is expected to be processing. If this check
		// fails, there is potentially an error in the VM this engine is running
		if status := blk.Status(); status != choices.Processing {
			t.Config.Context.Log.Warn("Attempting to issue a block with status: %s, expected Processing", status)
		}

		// the newly created block should be built on top of the preferred
		// block. Otherwise, the new block doesn't have the best chance of being
		// confirmed.
		parentID := blk.Parent().ID()
		if pref := t.Consensus.Preference(); !parentID.Equals(pref) {
			t.Config.Context.Log.Warn("Built block with parent: %s, expected %s", parentID, pref)
		}

		// inserting the block shouldn't have any missing dependencies
		if t.insertAll(blk) {
			t.Config.Context.Log.Verbo("Successfully issued new block from the VM")
		} else {
			t.Config.Context.Log.Warn("VM.BuildBlock returned a block that is pending for ancestors")
		}
	default:
		t.Config.Context.Log.Warn("Unexpected message from the VM: %s", msg)
	}
}

func (t *Transitive) repoll() {
	// if we are issuing a repoll, we should gossip our current preferences to
	// propagate the most likely branch as quickly as possible
	prefID := t.Consensus.Preference()

	for i := len(t.polls.m); i < t.Params.ConcurrentRepolls; i++ {
		t.pullSample(prefID)
	}
}

// reinsertFrom attempts to issue the branch ending with a block, from only its
// ID, to consensus. Returns true if the block was added, or was previously
// added, to consensus. This is useful to check the local DB before requesting a
// block in case we have the block for some reason. If the block or a dependency
// is missing, the validator will be sent a Get message.
func (t *Transitive) reinsertFrom(vdr ids.ShortID, blkID ids.ID) bool {
	blk, err := t.Config.VM.GetBlock(blkID)
	if err != nil {
		t.sendRequest(vdr, blkID)
		return false
	}
	return t.insertFrom(vdr, blk)
}

// insertFrom attempts to issue the branch ending with a block to consensus.
// Returns true if the block was added, or was previously added, to consensus.
// This is useful to check the local DB before requesting a block in case we
// have the block for some reason. If a dependency is missing, the validator
// will be sent a Get message.
func (t *Transitive) insertFrom(vdr ids.ShortID, blk snowman.Block) bool {
	blkID := blk.ID()
	// if the block has been issued, we don't need to insert it. if the block is
	// already pending, we shouldn't attempt to insert it again yet
	for !t.Consensus.Issued(blk) && !t.pending.Contains(blkID) {
		t.insert(blk)

		blk = blk.Parent()
		blkID = blk.ID()

		// if the parent hasn't been fetched, we need to request it to issue the
		// newly inserted block
		if !blk.Status().Fetched() {
			t.sendRequest(vdr, blkID)
			return false
		}
	}
	return t.Consensus.Issued(blk)
}

// insertAll attempts to issue the branch ending with a block to consensus.
// Returns true if the block was added, or was previously added, to consensus.
// This is useful to check the local DB before requesting a block in case we
// have the block for some reason. If a dependency is missing and the dependency
// hasn't been requested, the issuance will be abandoned.
func (t *Transitive) insertAll(blk snowman.Block) bool {
	blkID := blk.ID()
	for blk.Status().Fetched() && !t.Consensus.Issued(blk) && !t.pending.Contains(blkID) {
		t.insert(blk)

		blk = blk.Parent()
		blkID = blk.ID()
	}

	// if issuance the block was successful, this is the happy path
	if t.Consensus.Issued(blk) {
		return true
	}

	// if this branch is waiting on a block that we supposedly have a source of,
	// we can just wait for that request to succeed or fail
	if t.blkReqs.Contains(blkID) {
		return false
	}

	// if we have no reason to expect that this block will be inserted, we
	// should abandon the block to avoid a memory leak
	t.blocked.Abandon(blkID)
	return false
}

// attempt to insert the block to consensus. If the block's parent hasn't been
// issued, the insertion will block until the parent's issuance is abandoned or
// fulfilled
func (t *Transitive) insert(blk snowman.Block) {
	blkID := blk.ID()

	// mark that the block has been fetched but is pending
	t.pending.Add(blkID)

	// if we have any outstanding requests for this block, remove the pending
	// requests
	t.blkReqs.RemoveAny(blkID)

	i := &issuer{
		t:   t,
		blk: blk,
	}

	// block on the parent if needed
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
	// only send one request at a time for a block
	if t.blkReqs.Contains(blkID) {
		return
	}

	t.Config.Context.Log.Verbo("Sending Get message for %s", blkID)

	t.RequestID++
	t.blkReqs.Add(vdr, t.RequestID, blkID)
	t.Config.Sender.Get(vdr, t.RequestID, blkID)

	// Tracks performance statistics
	t.numBlkRequests.Set(float64(t.blkReqs.Len()))
}

// send a pull request for this block ID
func (t *Transitive) pullSample(blkID ids.ID) {
	t.Config.Context.Log.Verbo("About to sample from: %s", t.Config.Validators)
	p := t.Consensus.Parameters()
	vdrs := t.Config.Validators.Sample(p.K)
	vdrSet := ids.ShortSet{}
	for _, vdr := range vdrs {
		vdrSet.Add(vdr.ID())
	}

	if numVdrs := len(vdrs); numVdrs != p.K {
		t.Config.Context.Log.Error("Query for %s was dropped due to an insufficient number of validators", blkID)
		return
	}

	t.RequestID++
	if !t.polls.Add(t.RequestID, vdrSet.Len()) {
		t.Config.Context.Log.Error("Query for %s was dropped due to use of a duplicated requestID", blkID)
		return
	}

	t.Config.Sender.PullQuery(vdrSet, t.RequestID, blkID)
}

// send a push request for this block
func (t *Transitive) pushSample(blk snowman.Block) {
	t.Config.Context.Log.Verbo("About to sample from: %s", t.Config.Validators)
	p := t.Consensus.Parameters()
	vdrs := t.Config.Validators.Sample(p.K)
	vdrSet := ids.ShortSet{}
	for _, vdr := range vdrs {
		vdrSet.Add(vdr.ID())
	}

	blkID := blk.ID()
	if numVdrs := len(vdrs); numVdrs != p.K {
		t.Config.Context.Log.Error("Query for %s was dropped due to an insufficient number of validators", blkID)
		return
	}

	t.RequestID++
	if !t.polls.Add(t.RequestID, vdrSet.Len()) {
		t.Config.Context.Log.Error("Query for %s was dropped due to use of a duplicated requestID", blkID)
		return
	}

	t.Config.Sender.PushQuery(vdrSet, t.RequestID, blkID, blk.Bytes())
	return
}

func (t *Transitive) deliver(blk snowman.Block) {
	if t.Consensus.Issued(blk) {
		return
	}

	// we are adding the block to consensus, so it is no longer pending
	blkID := blk.ID()
	t.pending.Remove(blkID)

	if err := blk.Verify(); err != nil {
		t.Config.Context.Log.Debug("Block failed verification due to %s, dropping block", err)

		// if verify fails, then all decedents are also invalid
		t.blocked.Abandon(blkID)
		t.numBlockedBlk.Set(float64(t.pending.Len())) // Tracks performance statistics
		return
	}

	t.Config.Context.Log.Verbo("Adding block to consensus: %s", blkID)
	t.Consensus.Add(blk)

	// Add all the oracle blocks if they exist. We call verify on all the blocks
	// and add them to consensus before marking anything as fulfilled to avoid
	// any potential reentrant bugs.
	added := []snowman.Block{}
	dropped := []snowman.Block{}
	switch blk := blk.(type) {
	case OracleBlock:
		for _, blk := range blk.Options() {
			if err := blk.Verify(); err != nil {
				t.Config.Context.Log.Debug("Block failed verification due to %s, dropping block", err)
				dropped = append(dropped, blk)
			} else {
				t.Consensus.Add(blk)
				added = append(added, blk)
			}
		}
	}

	t.Config.VM.SetPreference(t.Consensus.Preference())

	// launch a query for the newly added block
	t.pushSample(blk)

	t.blocked.Fulfill(blkID)
	for _, blk := range added {
		t.pushSample(blk)

		blkID := blk.ID()
		t.pending.Remove(blkID)
		t.blocked.Fulfill(blkID)
	}
	for _, blk := range dropped {
		blkID := blk.ID()
		t.pending.Remove(blkID)
		t.blocked.Abandon(blkID)
	}

	// If we should issue multiple queries at the same time, we need to repoll
	t.repoll()

	// Tracks performance statistics
	t.numBlkRequests.Set(float64(t.blkReqs.Len()))
	t.numBlockedBlk.Set(float64(t.pending.Len()))
}
