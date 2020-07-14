// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"sync/atomic"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/network"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/snowman/poll"
	"github.com/ava-labs/gecko/snow/events"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	// TODO define this constant in one place rather than here and in snowman
	// Max containers size in a MultiPut message
	maxContainersLen = int(4 * network.DefaultMaxMessageSize / 5)
)

// Transitive implements the Engine interface by attempting to fetch all
// transitive dependencies.
type Transitive struct {
	Config
	bootstrapper

	// track outstanding preference requests
	polls poll.Set

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
	bootstrapped       bool
	atomicBootstrapped *uint32

	// errs tracks if an error has occurred in a callback
	errs wrappers.Errs
}

// Initialize implements the Engine interface
func (t *Transitive) Initialize(config Config) error {
	config.Context.Log.Info("initializing consensus engine")

	t.Config = config
	t.metrics.Initialize(
		config.Context.Log,
		config.Params.Namespace,
		config.Params.Metrics,
	)

	t.onFinished = t.finishBootstrapping
	t.atomicBootstrapped = new(uint32)

	factory := poll.NewEarlyTermNoTraversalFactory(int(config.Params.Alpha))
	t.polls = poll.NewSet(factory,
		config.Context.Log,
		config.Params.Namespace,
		config.Params.Metrics,
	)

	return t.bootstrapper.Initialize(config.BootstrapConfig)
}

// when bootstrapping is finished, this will be called. This initializes the
// consensus engine with the last accepted block.
func (t *Transitive) finishBootstrapping() error {
	// set the bootstrapped mark to switch consensus modes
	t.bootstrapped = true
	atomic.StoreUint32(t.atomicBootstrapped, 1)

	// initialize consensus to the last accepted blockID
	tailID := t.Config.VM.LastAccepted()
	t.Consensus.Initialize(t.Config.Context, t.Params, tailID)

	// to maintain the invariant that oracle blocks are issued in the correct
	// preferences, we need to handle the case that we are bootstrapping into an
	// oracle block
	tail, err := t.Config.VM.GetBlock(tailID)
	if err != nil {
		t.Config.Context.Log.Error("failed to get last accepted block due to: %s", err)
		return err
	}

	switch blk := tail.(type) {
	case OracleBlock:
		for _, blk := range blk.Options() {
			// note that deliver will set the VM's preference
			if err := t.deliver(blk); err != nil {
				return err
			}
		}
	default:
		// if there aren't blocks we need to deliver on startup, we need to set
		// the preference to the last accepted block
		t.Config.VM.SetPreference(tailID)
	}

	t.Config.Context.Log.Info("bootstrapping finished with %s as the last accepted block", tailID)
	return nil
}

// Gossip implements the Engine interface
func (t *Transitive) Gossip() error {
	blkID := t.Config.VM.LastAccepted()
	blk, err := t.Config.VM.GetBlock(blkID)
	if err != nil {
		t.Config.Context.Log.Warn("dropping gossip request as %s couldn't be loaded due to %s", blkID, err)
		return nil
	}

	t.Config.Context.Log.Verbo("gossiping %s as accepted to the network", blkID)
	t.Config.Sender.Gossip(blkID, blk.Bytes())
	return nil
}

// Shutdown implements the Engine interface
func (t *Transitive) Shutdown() error {
	t.Config.Context.Log.Info("shutting down consensus engine")
	return t.Config.VM.Shutdown()
}

// Context implements the Engine interface
func (t *Transitive) Context() *snow.Context { return t.Config.Context }

// Get implements the Engine interface
func (t *Transitive) Get(vdr ids.ShortID, requestID uint32, blkID ids.ID) error {
	blk, err := t.Config.VM.GetBlock(blkID)
	if err != nil {
		// If we failed to get the block, that means either an unexpected error
		// has occurred, the validator is not following the protocol, or the
		// block has been pruned.
		t.Config.Context.Log.Debug("Get(%s, %d, %s) failed with: %s", vdr, requestID, blkID, err)
		return nil
	}

	// Respond to the validator with the fetched block and the same requestID.
	t.Config.Sender.Put(vdr, requestID, blkID, blk.Bytes())
	return nil
}

// GetAncestors implements the Engine interface
func (t *Transitive) GetAncestors(vdr ids.ShortID, requestID uint32, blkID ids.ID) error {
	startTime := time.Now()
	blk, err := t.Config.VM.GetBlock(blkID)
	if err != nil { // Don't have the block. Drop this request.
		t.Config.Context.Log.Verbo("couldn't get block %s. dropping GetAncestors(%s, %d, %s)", blkID, vdr, requestID, blkID)
		return nil
	}

	ancestorsBytes := make([][]byte, 1, common.MaxContainersPerMultiPut) // First elt is byte repr. of blk, then its parents, then grandparent, etc.
	ancestorsBytes[0] = blk.Bytes()
	ancestorsBytesLen := len(blk.Bytes()) + wrappers.IntLen // length, in bytes, of all elements of ancestors

	for numFetched := 1; numFetched < common.MaxContainersPerMultiPut && time.Since(startTime) < common.MaxTimeFetchingAncestors; numFetched++ {
		blk = blk.Parent()
		if blk.Status() == choices.Unknown {
			break
		}
		blkBytes := blk.Bytes()
		// Ensure response size isn't too large. Include wrappers.IntLen because the size of the message
		// is included with each container, and the size is repr. by an int.
		if newLen := wrappers.IntLen + ancestorsBytesLen + len(blkBytes); newLen < maxContainersLen {
			ancestorsBytes = append(ancestorsBytes, blkBytes)
			ancestorsBytesLen = newLen
		} else { // reached maximum response size
			break
		}
	}

	t.Config.Sender.MultiPut(vdr, requestID, ancestorsBytes)
	return nil
}

// Put implements the Engine interface
func (t *Transitive) Put(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	// bootstrapping isn't done --> we didn't send any gets --> this put is invalid
	if !t.bootstrapped {
		if requestID == network.GossipMsgRequestID {
			t.Config.Context.Log.Verbo("dropping gossip Put(%s, %d, %s) due to bootstrapping", vdr, requestID, blkID)
		} else {
			t.Config.Context.Log.Debug("dropping Put(%s, %d, %s) due to bootstrapping", vdr, requestID, blkID)
		}
		return nil
	}

	blk, err := t.Config.VM.ParseBlock(blkBytes)
	if err != nil {
		t.Config.Context.Log.Debug("failed to parse block %s: %s", blkID, err)
		t.Config.Context.Log.Verbo("block:\n%s", formatting.DumpBytes{Bytes: blkBytes})
		// because GetFailed doesn't utilize the assumption that we actually
		// sent a Get message, we can safely call GetFailed here to potentially
		// abandon the request.
		return t.GetFailed(vdr, requestID)
	}

	// insert the block into consensus. If the block has already been issued,
	// this will be a noop. If this block has missing dependencies, vdr will
	// receive requests to fill the ancestry. dependencies that have already
	// been fetched, but with missing dependencies themselves won't be requested
	// from the vdr.
	_, err = t.insertFrom(vdr, blk)
	return err
}

// GetFailed implements the Engine interface
func (t *Transitive) GetFailed(vdr ids.ShortID, requestID uint32) error {
	// not done bootstrapping --> didn't send a get --> this message is invalid
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("dropping GetFailed(%s, %d) due to bootstrapping")
		return nil
	}

	// we don't use the assumption that this function is called after a failed
	// Get message. So we first check to see if we have an outsanding request
	// and also get what the request was for if it exists
	blkID, ok := t.blkReqs.Remove(vdr, requestID)
	if !ok {
		t.Config.Context.Log.Debug("getFailed(%s, %d) called without having sent corresponding Get", vdr, requestID)
		return nil
	}

	// because the get request was dropped, we no longer are expected blkID to
	// be issued.
	t.blocked.Abandon(blkID)
	return t.errs.Err
}

// PullQuery implements the Engine interface
func (t *Transitive) PullQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID) error {
	// if the engine hasn't been bootstrapped, we aren't ready to respond to
	// queries
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("dropping PullQuery(%s, %d, %s) due to bootstrapping", vdr, requestID, blkID)
		return nil
	}

	c := &convincer{
		consensus: t.Consensus,
		sender:    t.Config.Sender,
		vdr:       vdr,
		requestID: requestID,
		errs:      &t.errs,
	}

	added, err := t.reinsertFrom(vdr, blkID)
	if err != nil {
		return err
	}

	// if we aren't able to have issued this block, then it is a dependency for
	// this reply
	if !added {
		c.deps.Add(blkID)
	}

	t.blocked.Register(c)
	return t.errs.Err
}

// PushQuery implements the Engine interface
func (t *Transitive) PushQuery(vdr ids.ShortID, requestID uint32, blkID ids.ID, blkBytes []byte) error {
	// if the engine hasn't been bootstrapped, we aren't ready to respond to
	// queries
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("dropping PushQuery(%s, %d, %s) due to bootstrapping", vdr, requestID, blkID)
		return nil
	}

	blk, err := t.Config.VM.ParseBlock(blkBytes)
	// If the parsing fails, we just drop the request, as we didn't ask for it
	if err != nil {
		t.Config.Context.Log.Debug("failed to parse block %s: %s", blkID, err)
		t.Config.Context.Log.Verbo("block:\n%s", formatting.DumpBytes{Bytes: blkBytes})
		return nil
	}

	// insert the block into consensus. If the block has already been issued,
	// this will be a noop. If this block has missing dependencies, vdr will
	// receive requests to fill the ancestry. dependencies that have already
	// been fetched, but with missing dependencies themselves won't be requested
	// from the vdr.
	if _, err := t.insertFrom(vdr, blk); err != nil {
		return err
	}

	// register the chit request
	return t.PullQuery(vdr, requestID, blk.ID())
}

// Chits implements the Engine interface
func (t *Transitive) Chits(vdr ids.ShortID, requestID uint32, votes ids.Set) error {
	// if the engine hasn't been bootstrapped, we shouldn't be receiving chits
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("dropping Chits(%s, %d) due to bootstrapping", vdr, requestID)
		return nil
	}

	// Since this is snowman, there should only be one ID in the vote set
	if votes.Len() != 1 {
		t.Config.Context.Log.Debug("Chits(%s, %d) was called with %d votes (expected 1)", vdr, requestID, votes.Len())
		// because QueryFailed doesn't utilize the assumption that we actually
		// sent a Query message, we can safely call QueryFailed here to
		// potentially abandon the request.
		return t.QueryFailed(vdr, requestID)
	}
	vote := votes.List()[0]

	t.Config.Context.Log.Verbo("Chits(%s, %d) contains vote for %s", vdr, requestID, vote)

	v := &voter{
		t:         t,
		vdr:       vdr,
		requestID: requestID,
		response:  vote,
	}

	added, err := t.reinsertFrom(vdr, vote)
	if err != nil {
		return err
	}

	// if we aren't able to have issued the vote's block, then it is a
	// dependency for applying the vote
	if !added {
		v.deps.Add(vote)
	}

	t.blocked.Register(v)
	return t.errs.Err
}

// QueryFailed implements the Engine interface
func (t *Transitive) QueryFailed(vdr ids.ShortID, requestID uint32) error {
	// if the engine hasn't been bootstrapped, we won't have sent a query
	if !t.bootstrapped {
		t.Config.Context.Log.Warn("dropping QueryFailed(%s, %d) due to bootstrapping", vdr, requestID)
		return nil
	}

	t.blocked.Register(&voter{
		t:         t,
		vdr:       vdr,
		requestID: requestID,
	})
	return t.errs.Err
}

// Notify implements the Engine interface
func (t *Transitive) Notify(msg common.Message) error {
	// if the engine hasn't been bootstrapped, we shouldn't issuing blocks
	if !t.bootstrapped {
		t.Config.Context.Log.Debug("dropping Notify due to bootstrapping")
		return nil
	}

	t.Config.Context.Log.Verbo("snowman engine notified of %s from the vm", msg)
	switch msg {
	case common.PendingTxs:
		// the pending txs message means we should attempt to build a block.
		blk, err := t.Config.VM.BuildBlock()
		if err != nil {
			t.Config.Context.Log.Debug("VM.BuildBlock errored with: %s", err)
			return nil
		}

		// a newly created block is expected to be processing. If this check
		// fails, there is potentially an error in the VM this engine is running
		if status := blk.Status(); status != choices.Processing {
			t.Config.Context.Log.Warn("attempting to issue a block with status: %s, expected Processing", status)
		}

		// the newly created block should be built on top of the preferred
		// block. Otherwise, the new block doesn't have the best chance of being
		// confirmed.
		parentID := blk.Parent().ID()
		if pref := t.Consensus.Preference(); !parentID.Equals(pref) {
			t.Config.Context.Log.Warn("built block with parent: %s, expected %s", parentID, pref)
		}

		added, err := t.insertAll(blk)
		if err != nil {
			return err
		}

		// inserting the block shouldn't have any missing dependencies
		if added {
			t.Config.Context.Log.Verbo("successfully issued new block from the VM")
		} else {
			t.Config.Context.Log.Warn("VM.BuildBlock returned a block that is pending for ancestors")
		}
	default:
		t.Config.Context.Log.Warn("unexpected message from the VM: %s", msg)
	}
	return nil
}

func (t *Transitive) repoll() {
	// if we are issuing a repoll, we should gossip our current preferences to
	// propagate the most likely branch as quickly as possible
	prefID := t.Consensus.Preference()

	for i := t.polls.Len(); i < t.Params.ConcurrentRepolls; i++ {
		t.pullSample(prefID)
	}
}

// reinsertFrom attempts to issue the branch ending with a block, from only its
// ID, to consensus. Returns true if the block was added, or was previously
// added, to consensus. This is useful to check the local DB before requesting a
// block in case we have the block for some reason. If the block or a dependency
// is missing, the validator will be sent a Get message.
func (t *Transitive) reinsertFrom(vdr ids.ShortID, blkID ids.ID) (bool, error) {
	blk, err := t.Config.VM.GetBlock(blkID)
	if err != nil {
		t.sendRequest(vdr, blkID)
		return false, nil
	}
	return t.insertFrom(vdr, blk)
}

// insertFrom attempts to issue the branch ending with a block to consensus.
// Returns true if the block was added, or was previously added, to consensus.
// This is useful to check the local DB before requesting a block in case we
// have the block for some reason. If a dependency is missing, the validator
// will be sent a Get message.
func (t *Transitive) insertFrom(vdr ids.ShortID, blk snowman.Block) (bool, error) {
	blkID := blk.ID()
	// if the block has been issued, we don't need to insert it. if the block is
	// already pending, we shouldn't attempt to insert it again yet
	for !t.Consensus.Issued(blk) && !t.pending.Contains(blkID) {
		if err := t.insert(blk); err != nil {
			return false, err
		}

		blk = blk.Parent()
		blkID = blk.ID()

		// if the parent hasn't been fetched, we need to request it to issue the
		// newly inserted block
		if !blk.Status().Fetched() {
			t.sendRequest(vdr, blkID)
			return false, nil
		}
	}
	return t.Consensus.Issued(blk), nil
}

// insertAll attempts to issue the branch ending with a block to consensus.
// Returns true if the block was added, or was previously added, to consensus.
// This is useful to check the local DB before requesting a block in case we
// have the block for some reason. If a dependency is missing and the dependency
// hasn't been requested, the issuance will be abandoned.
func (t *Transitive) insertAll(blk snowman.Block) (bool, error) {
	blkID := blk.ID()
	for blk.Status().Fetched() && !t.Consensus.Issued(blk) && !t.pending.Contains(blkID) {
		if err := t.insert(blk); err != nil {
			return false, err
		}

		blk = blk.Parent()
		blkID = blk.ID()
	}

	// if issuance the block was successful, this is the happy path
	if t.Consensus.Issued(blk) {
		return true, nil
	}

	// if this branch is waiting on a block that we supposedly have a source of,
	// we can just wait for that request to succeed or fail
	if t.blkReqs.Contains(blkID) {
		return false, nil
	}

	// if we have no reason to expect that this block will be inserted, we
	// should abandon the block to avoid a memory leak
	t.blocked.Abandon(blkID)
	return false, t.errs.Err
}

// attempt to insert the block to consensus. If the block's parent hasn't been
// issued, the insertion will block until the parent's issuance is abandoned or
// fulfilled
func (t *Transitive) insert(blk snowman.Block) error {
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
		t.Config.Context.Log.Verbo("block %s waiting for parent %s", blkID, parentID)
		i.deps.Add(parentID)
	}

	t.blocked.Register(i)

	// Tracks performance statistics
	t.numBlkRequests.Set(float64(t.blkReqs.Len()))
	t.numBlockedBlk.Set(float64(t.pending.Len()))
	return t.errs.Err
}

func (t *Transitive) sendRequest(vdr ids.ShortID, blkID ids.ID) {
	// only send one request at a time for a block
	if t.blkReqs.Contains(blkID) {
		return
	}

	t.RequestID++
	t.blkReqs.Add(vdr, t.RequestID, blkID)
	t.Config.Context.Log.Verbo("sending Get(%s, %d, %s)", vdr, t.RequestID, blkID)
	t.Config.Sender.Get(vdr, t.RequestID, blkID)

	// Tracks performance statistics
	t.numBlkRequests.Set(float64(t.blkReqs.Len()))
}

// send a pull request for this block ID
func (t *Transitive) pullSample(blkID ids.ID) {
	t.Config.Context.Log.Verbo("about to sample from: %s", t.Config.Validators)
	p := t.Consensus.Parameters()
	vdrs := t.Config.Validators.Sample(p.K)
	vdrSet := ids.ShortSet{}
	for _, vdr := range vdrs {
		vdrSet.Add(vdr.ID())
	}

	toSample := ids.ShortSet{}
	toSample.Union(vdrSet)

	t.RequestID++
	if numVdrs := len(vdrs); numVdrs == p.K && t.polls.Add(t.RequestID, vdrSet) {
		t.Config.Sender.PullQuery(toSample, t.RequestID, blkID)
	} else if numVdrs < p.K {
		t.Config.Context.Log.Error("query for %s was dropped due to an insufficient number of validators", blkID)
	}
}

// send a push request for this block
func (t *Transitive) pushSample(blk snowman.Block) {
	t.Config.Context.Log.Verbo("about to sample from: %s", t.Config.Validators)
	p := t.Consensus.Parameters()
	vdrs := t.Config.Validators.Sample(p.K)
	vdrSet := ids.ShortSet{}
	for _, vdr := range vdrs {
		vdrSet.Add(vdr.ID())
	}

	toSample := ids.ShortSet{}
	toSample.Union(vdrSet)

	t.RequestID++
	if numVdrs := len(vdrs); numVdrs == p.K && t.polls.Add(t.RequestID, vdrSet) {
		t.Config.Sender.PushQuery(toSample, t.RequestID, blk.ID(), blk.Bytes())
	} else if numVdrs < p.K {
		t.Config.Context.Log.Error("query for %s was dropped due to an insufficient number of validators", blk.ID())
	}
}

func (t *Transitive) deliver(blk snowman.Block) error {
	if t.Consensus.Issued(blk) {
		return nil
	}

	// we are adding the block to consensus, so it is no longer pending
	blkID := blk.ID()
	t.pending.Remove(blkID)

	if err := blk.Verify(); err != nil {
		t.Config.Context.Log.Debug("block failed verification due to %s, dropping block", err)

		// if verify fails, then all decedents are also invalid
		t.blocked.Abandon(blkID)
		t.numBlockedBlk.Set(float64(t.pending.Len())) // Tracks performance statistics
		return t.errs.Err
	}

	t.Config.Context.Log.Verbo("adding block to consensus: %s", blkID)
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
				t.Config.Context.Log.Debug("block failed verification due to %s, dropping block", err)
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
	return t.errs.Err
}

// IsBootstrapped returns true iff this chain is done bootstrapping
func (t *Transitive) IsBootstrapped() bool {
	return atomic.LoadUint32(t.atomicBootstrapped) > 0
}
