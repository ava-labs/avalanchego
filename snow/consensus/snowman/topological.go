// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	errDuplicateAdd            = errors.New("duplicate block add")
	errUnknownParentBlock      = errors.New("unknown parent block")
	errTooManyProcessingBlocks = errors.New("too many processing blocks")
	errBlockProcessingTooLong  = errors.New("block processing too long")

	_ Factory   = (*TopologicalFactory)(nil)
	_ Consensus = (*Topological)(nil)
)

// TopologicalFactory implements Factory by returning a topological struct
type TopologicalFactory struct {
	factory snowball.Factory
}

func (tf TopologicalFactory) New() Consensus {
	return &Topological{Factory: tf.factory}
}

// Topological implements the Snowman interface by using a tree tracking the
// strongly preferred branch. This tree structure amortizes network polls to
// vote on more than just the next block.
type Topological struct {
	Factory snowball.Factory

	metrics *metrics

	// pollNumber is the number of times RecordPolls has been called
	pollNumber uint64

	// ctx is the context this snowman instance is executing in
	ctx *snow.ConsensusContext

	// params are the parameters that should be used to initialize snowball
	// instances
	params snowball.Parameters

	lastAcceptedID     ids.ID
	lastAcceptedHeight uint64

	// blocks stores the last accepted block and all the pending blocks
	blocks map[ids.ID]*snowmanBlock // blockID -> snowmanBlock

	// preferredIDs stores the set of IDs that are currently preferred.
	preferredIDs set.Set[ids.ID]

	// preferredHeights maps a height to the currently preferred block ID at
	// that height.
	preferredHeights map[uint64]ids.ID // height -> blockID

	// preference is the preferred block with highest height
	preference ids.ID

	// Used in [calculateInDegree] and.
	// Should only be accessed in that method.
	// We use this one instance of set.Set instead of creating a
	// new set.Set during each call to [calculateInDegree].
	leaves set.Set[ids.ID]

	// Kahn nodes used in [calculateInDegree] and [markAncestorInDegrees].
	// Should only be accessed in those methods.
	// We use this one map instead of creating a new map
	// during each call to [calculateInDegree].
	kahnNodes map[ids.ID]kahnNode
}

// Used to track the kahn topological sort status
type kahnNode struct {
	// inDegree is the number of children that haven't been processed yet. If
	// inDegree is 0, then this node is a leaf
	inDegree int
	// votes for all the children of this node, so far
	votes bag.Bag[ids.ID]
}

// Used to track which children should receive votes
type votes struct {
	// parentID is the parent of all the votes provided in the votes bag
	parentID ids.ID
	// votes for all the children of the parent
	votes bag.Bag[ids.ID]
}

func (ts *Topological) Initialize(
	ctx *snow.ConsensusContext,
	params snowball.Parameters,
	lastAcceptedID ids.ID,
	lastAcceptedHeight uint64,
	lastAcceptedTime time.Time,
) error {
	err := params.Verify()
	if err != nil {
		return err
	}

	ts.metrics, err = newMetrics(
		ctx.Log,
		ctx.Registerer,
		lastAcceptedHeight,
		lastAcceptedTime,
	)
	if err != nil {
		return err
	}

	ts.leaves = set.Set[ids.ID]{}
	ts.kahnNodes = make(map[ids.ID]kahnNode)
	ts.ctx = ctx
	ts.params = params
	ts.lastAcceptedID = lastAcceptedID
	ts.lastAcceptedHeight = lastAcceptedHeight
	ts.blocks = map[ids.ID]*snowmanBlock{
		lastAcceptedID: {t: ts},
	}
	ts.preferredHeights = make(map[uint64]ids.ID)
	ts.preference = lastAcceptedID
	return nil
}

func (ts *Topological) NumProcessing() int {
	return len(ts.blocks) - 1
}

func (ts *Topological) Add(blk Block) error {
	blkID := blk.ID()
	height := blk.Height()
	ts.ctx.Log.Verbo("adding block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", height),
	)

	// Make sure a block is not inserted twice.
	if ts.Processing(blkID) {
		return errDuplicateAdd
	}

	ts.metrics.Verified(height)
	ts.metrics.Issued(blkID, ts.pollNumber)

	parentID := blk.Parent()
	parentNode, ok := ts.blocks[parentID]
	if !ok {
		return errUnknownParentBlock
	}

	// add the block as a child of its parent, and add the block to the tree
	parentNode.AddChild(blk)
	ts.blocks[blkID] = &snowmanBlock{
		t:   ts,
		blk: blk,
	}

	// If we are extending the preference, this is the new preference
	if ts.preference == parentID {
		ts.preference = blkID
		ts.preferredIDs.Add(blkID)
		ts.preferredHeights[height] = blkID
	}

	ts.ctx.Log.Verbo("added block",
		zap.Stringer("blkID", blkID),
		zap.Uint64("height", height),
		zap.Stringer("parentID", parentID),
	)
	return nil
}

func (ts *Topological) Processing(blkID ids.ID) bool {
	// The last accepted block is in the blocks map, so we first must ensure the
	// requested block isn't the last accepted block.
	if blkID == ts.lastAcceptedID {
		return false
	}
	// If the block is in the map of current blocks and not the last accepted
	// block, then it is currently processing.
	_, ok := ts.blocks[blkID]
	return ok
}

func (ts *Topological) IsPreferred(blkID ids.ID) bool {
	return blkID == ts.lastAcceptedID || ts.preferredIDs.Contains(blkID)
}

func (ts *Topological) LastAccepted() (ids.ID, uint64) {
	return ts.lastAcceptedID, ts.lastAcceptedHeight
}

func (ts *Topological) Preference() ids.ID {
	return ts.preference
}

func (ts *Topological) PreferenceAtHeight(height uint64) (ids.ID, bool) {
	if height == ts.lastAcceptedHeight {
		return ts.lastAcceptedID, true
	}
	blkID, ok := ts.preferredHeights[height]
	return blkID, ok
}

// The votes bag contains at most K votes for blocks in the tree. If there is a
// vote for a block that isn't in the tree, the vote is dropped.
//
// Votes are propagated transitively towards the genesis. All blocks in the tree
// that result in at least Alpha votes will record the poll on their children.
// Every other block will have an unsuccessful poll registered.
//
// After collecting which blocks should be voted on, the polls are registered
// and blocks are accepted/rejected as needed. The preference is then updated to
// equal the leaf on the preferred branch.
//
// To optimize the theoretical complexity of the vote propagation, a topological
// sort is done over the blocks that are reachable from the provided votes.
// During the sort, votes are pushed towards the genesis. To prevent interating
// over all blocks that had unsuccessful polls, we set a flag on the block to
// know that any future traversal through that block should register an
// unsuccessful poll on that block and every descendant block.
//
// The complexity of this function is:
// - Runtime = 4 * |live set| + |votes|
// - Space = 2 * |live set| + |votes|
func (ts *Topological) RecordPoll(ctx context.Context, voteBag bag.Bag[ids.ID]) error {
	// Register a new poll call
	ts.pollNumber++

	var voteStack []votes
	if voteBag.Len() >= ts.params.AlphaPreference {
		// Since we received at least alpha votes, it's possible that
		// we reached an alpha majority on a processing block.
		// We must perform the traversals to calculate all block
		// that reached an alpha majority.

		// Populates [ts.kahnNodes] and [ts.leaves]
		// Runtime = |live set| + |votes| ; Space = |live set| + |votes|
		ts.calculateInDegree(voteBag)

		// Runtime = |live set| ; Space = |live set|
		voteStack = ts.pushVotes()
	}

	// Runtime = |live set| ; Space = Constant
	preferred, err := ts.vote(ctx, voteStack)
	if err != nil {
		return err
	}

	// If the set of preferred IDs already contains the preference, then the
	// preference is guaranteed to already be set correctly. This is because the
	// value returned from vote reports the next preferred block after the last
	// preferred block that was voted for. If this block was previously
	// preferred, then we know that following the preferences down the chain
	// will return the current preference.
	if ts.preferredIDs.Contains(preferred) {
		return nil
	}

	// Runtime = 2 * |live set| ; Space = Constant
	ts.preferredIDs.Clear()
	clear(ts.preferredHeights)

	ts.preference = preferred
	startBlock := ts.blocks[ts.preference]

	// Runtime = |live set| ; Space = Constant
	// Traverse from the preferred ID to the last accepted ancestor.
	//
	// It is guaranteed that the first decided block we encounter is the last
	// accepted block because the startBlock is the preferred block. The
	// preferred block is guaranteed to either be the last accepted block or
	// extend the accepted chain.
	for block := startBlock; !block.Decided(); {
		blkID := block.blk.ID()
		ts.preferredIDs.Add(blkID)
		ts.preferredHeights[block.blk.Height()] = blkID
		block = ts.blocks[block.blk.Parent()]
	}
	// Traverse from the preferred ID to the preferred child until there are no
	// children.
	for block := startBlock; block.sb != nil; {
		ts.preference = block.sb.Preference()
		ts.preferredIDs.Add(ts.preference)
		block = ts.blocks[ts.preference]
		// Invariant: Because the prior block had an initialized snowball
		// instance, it must have a processing child. This guarantees that
		// block.blk is non-nil here.
		ts.preferredHeights[block.blk.Height()] = ts.preference
	}
	return nil
}

// HealthCheck returns information about the consensus health.
func (ts *Topological) HealthCheck(context.Context) (interface{}, error) {
	var errs []error

	numProcessingBlks := ts.NumProcessing()
	if numProcessingBlks > ts.params.MaxOutstandingItems {
		err := fmt.Errorf("%w: %d > %d",
			errTooManyProcessingBlocks,
			numProcessingBlks,
			ts.params.MaxOutstandingItems,
		)
		errs = append(errs, err)
	}

	maxTimeProcessing := ts.metrics.MeasureAndGetOldestDuration()
	if maxTimeProcessing > ts.params.MaxItemProcessingTime {
		err := fmt.Errorf("%w: %s > %s",
			errBlockProcessingTooLong,
			maxTimeProcessing,
			ts.params.MaxItemProcessingTime,
		)
		errs = append(errs, err)
	}

	return map[string]interface{}{
		"processingBlocks":       numProcessingBlks,
		"longestProcessingBlock": maxTimeProcessing.String(), // .String() is needed here to ensure a human readable format
		"lastAcceptedID":         ts.lastAcceptedID,
		"lastAcceptedHeight":     ts.lastAcceptedHeight,
	}, errors.Join(errs...)
}

// takes in a list of votes and sets up the topological ordering. Returns the
// reachable section of the graph annotated with the number of inbound edges and
// the non-transitively applied votes. Also returns the list of leaf blocks.
func (ts *Topological) calculateInDegree(votes bag.Bag[ids.ID]) {
	// Clear the Kahn node set
	clear(ts.kahnNodes)
	// Clear the leaf set
	ts.leaves.Clear()

	for _, vote := range votes.List() {
		votedBlock, validVote := ts.blocks[vote]

		// If the vote is for a block that isn't in the current pending set,
		// then the vote is dropped
		if !validVote {
			continue
		}

		// If the vote is for the last accepted block, the vote is dropped
		if votedBlock.Decided() {
			continue
		}

		// The parent contains the snowball instance of its children
		parentID := votedBlock.blk.Parent()

		// Add the votes for this block to the parent's set of responses
		numVotes := votes.Count(vote)
		kahn, previouslySeen := ts.kahnNodes[parentID]
		kahn.votes.AddCount(vote, numVotes)
		ts.kahnNodes[parentID] = kahn

		// If the parent block already had registered votes, then there is no
		// need to iterate into the parents
		if previouslySeen {
			continue
		}

		// If I've never seen this parent block before, it is currently a leaf.
		ts.leaves.Add(parentID)

		// iterate through all the block's ancestors and set up the inDegrees of
		// the blocks
		for n := ts.blocks[parentID]; !n.Decided(); n = ts.blocks[parentID] {
			parentID = n.blk.Parent()

			// Increase the inDegree by one
			kahn, previouslySeen := ts.kahnNodes[parentID]
			kahn.inDegree++
			ts.kahnNodes[parentID] = kahn

			// If we have already seen this block, then we shouldn't increase
			// the inDegree of the ancestors through this block again.
			if previouslySeen {
				// Nodes are only leaves if they have no inbound edges.
				ts.leaves.Remove(parentID)
				break
			}
		}
	}
}

// convert the tree into a branch of snowball instances with at least alpha
// votes
func (ts *Topological) pushVotes() []votes {
	voteStack := make([]votes, 0, len(ts.kahnNodes))
	for ts.leaves.Len() > 0 {
		// Pop one element of [leaves]
		leafID, _ := ts.leaves.Pop()
		// Should never return false because we just
		// checked that [ts.leaves] is non-empty.

		// get the block and sort information about the block
		kahnNode := ts.kahnNodes[leafID]
		block := ts.blocks[leafID]

		// If there are at least Alpha votes, then this block needs to record
		// the poll on the snowball instance
		if kahnNode.votes.Len() >= ts.params.AlphaPreference {
			voteStack = append(voteStack, votes{
				parentID: leafID,
				votes:    kahnNode.votes,
			})
		}

		// If the block is accepted, then we don't need to push votes to the
		// parent block
		if block.Decided() {
			continue
		}

		parentID := block.blk.Parent()

		// Remove an inbound edge from the parent kahn node and push the votes.
		parentKahnNode := ts.kahnNodes[parentID]
		parentKahnNode.inDegree--
		parentKahnNode.votes.AddCount(leafID, kahnNode.votes.Len())
		ts.kahnNodes[parentID] = parentKahnNode

		// If the inDegree is zero, then the parent node is now a leaf
		if parentKahnNode.inDegree == 0 {
			ts.leaves.Add(parentID)
		}
	}
	return voteStack
}

// apply votes to the branch that received an Alpha threshold and returns the
// next preferred block after the last preferred block that received an Alpha
// threshold.
func (ts *Topological) vote(ctx context.Context, voteStack []votes) (ids.ID, error) {
	// If the voteStack is empty, then the full tree should falter. This won't
	// change the preferred branch.
	if len(voteStack) == 0 {
		lastAcceptedBlock := ts.blocks[ts.lastAcceptedID]
		lastAcceptedBlock.shouldFalter = true

		if numProcessing := len(ts.blocks) - 1; numProcessing > 0 {
			ts.ctx.Log.Verbo("no progress was made after processing pending blocks",
				zap.Int("numProcessing", numProcessing),
			)
			ts.metrics.FailedPoll()
		}
		return ts.preference, nil
	}

	// keep track of the new preferred block
	newPreferred := ts.lastAcceptedID
	onPreferredBranch := true
	pollSuccessful := false
	for len(voteStack) > 0 {
		// pop a vote off the stack
		newStackSize := len(voteStack) - 1
		vote := voteStack[newStackSize]
		voteStack = voteStack[:newStackSize]

		// get the block that we are going to vote on
		parentBlock, notRejected := ts.blocks[vote.parentID]

		// if the block we are going to vote on was already rejected, then
		// we should stop applying the votes
		if !notRejected {
			break
		}

		// keep track of transitive falters to propagate to this block's
		// children
		shouldTransitivelyFalter := parentBlock.shouldFalter

		// if the block was previously marked as needing to falter, the block
		// should falter before applying the vote
		if shouldTransitivelyFalter {
			ts.ctx.Log.Verbo("resetting confidence below parent",
				zap.Stringer("parentID", vote.parentID),
			)

			parentBlock.sb.RecordUnsuccessfulPoll()
			parentBlock.shouldFalter = false
		}

		// apply the votes for this snowball instance
		pollSuccessful = parentBlock.sb.RecordPoll(vote.votes) || pollSuccessful

		// Only accept when you are finalized and a child of the last accepted
		// block.
		if parentBlock.sb.Finalized() && ts.lastAcceptedID == vote.parentID {
			if err := ts.acceptPreferredChild(ctx, parentBlock); err != nil {
				return ids.Empty, err
			}

			// by accepting the child of parentBlock, the last accepted block is
			// no longer voteParentID, but its child. So, voteParentID can be
			// removed from the tree.
			delete(ts.blocks, vote.parentID)
		}

		// If we are on the preferred branch, then the parent's preference is
		// the next block on the preferred branch.
		parentPreference := parentBlock.sb.Preference()
		if onPreferredBranch {
			newPreferred = parentPreference
		}

		// Get the ID of the child that is having a RecordPoll called. All other
		// children will need to have their confidence reset. If there isn't a
		// child having RecordPoll called, then the nextID will default to the
		// nil ID.
		nextID := ids.Empty
		if len(voteStack) > 0 {
			nextID = voteStack[newStackSize-1].parentID
		}

		// If we are on the preferred branch and the nextID is the preference of
		// the snowball instance, then we are following the preferred branch.
		onPreferredBranch = onPreferredBranch && nextID == parentPreference

		// If there wasn't an alpha threshold on the branch (either on this vote
		// or a past transitive vote), I should falter now.
		for childID := range parentBlock.children {
			// If we don't need to transitively falter and the child is going to
			// have RecordPoll called on it, then there is no reason to reset
			// the block's confidence
			if !shouldTransitivelyFalter && childID == nextID {
				continue
			}

			// If we finalized a child of the current block, then all other
			// children will have been rejected and removed from the tree.
			// Therefore, we need to make sure the child is still in the tree.
			childBlock, notRejected := ts.blocks[childID]
			if notRejected {
				ts.ctx.Log.Verbo("deferring confidence reset of child block",
					zap.Stringer("childID", childID),
				)

				ts.ctx.Log.Verbo("voting for next block",
					zap.Stringer("nextID", nextID),
				)

				// If the child is ever voted for positively, the confidence
				// must be reset first.
				childBlock.shouldFalter = true
			}
		}
	}

	if pollSuccessful {
		ts.metrics.SuccessfulPoll()
	} else {
		ts.metrics.FailedPoll()
	}
	return newPreferred, nil
}

// Accepts the preferred child of the provided snowman block. By accepting the
// preferred child, all other children will be rejected. When these children are
// rejected, all their descendants will be rejected.
//
// We accept a block once its parent's snowball instance has finalized
// with it as the preference.
func (ts *Topological) acceptPreferredChild(ctx context.Context, n *snowmanBlock) error {
	// We are finalizing the block's child, so we need to get the preference
	pref := n.sb.Preference()

	// Get the child and accept it
	child := n.children[pref]
	// Notify anyone listening that this block was accepted.
	bytes := child.Bytes()
	// Note that BlockAcceptor.Accept must be called before child.Accept to
	// honor Acceptor.Accept's invariant.
	if err := ts.ctx.BlockAcceptor.Accept(ts.ctx, pref, bytes); err != nil {
		return err
	}

	height := child.Height()
	timestamp := child.Timestamp()
	ts.ctx.Log.Trace("accepting block",
		zap.Stringer("blkID", pref),
		zap.Uint64("height", height),
		zap.Time("timestamp", timestamp),
	)
	if err := child.Accept(ctx); err != nil {
		return err
	}

	// Update the last accepted values to the newly accepted block.
	ts.lastAcceptedID = pref
	ts.lastAcceptedHeight = height
	// Remove the decided block from the set of processing IDs, as its status
	// now implies its preferredness.
	ts.preferredIDs.Remove(pref)
	delete(ts.preferredHeights, height)

	ts.metrics.Accepted(
		pref,
		height,
		timestamp,
		ts.pollNumber,
		len(bytes),
	)

	// Because ts.blocks contains the last accepted block, we don't delete the
	// block from the blocks map here.

	rejects := make([]ids.ID, 0, len(n.children)-1)
	for childID, child := range n.children {
		if childID == pref {
			// don't reject the block we just accepted
			continue
		}

		ts.ctx.Log.Trace("rejecting block",
			zap.String("reason", "conflict with accepted block"),
			zap.Stringer("blkID", childID),
			zap.Uint64("height", child.Height()),
			zap.Stringer("conflictID", pref),
		)
		if err := child.Reject(ctx); err != nil {
			return err
		}
		ts.metrics.Rejected(childID, ts.pollNumber, len(child.Bytes()))

		// Track which blocks have been directly rejected
		rejects = append(rejects, childID)
	}

	// reject all the descendants of the blocks we just rejected
	return ts.rejectTransitively(ctx, rejects)
}

// Takes in a list of rejected ids and rejects all descendants of these IDs
func (ts *Topological) rejectTransitively(ctx context.Context, rejected []ids.ID) error {
	// the rejected array is treated as a stack, with the next element at index
	// 0 and the last element at the end of the slice.
	for len(rejected) > 0 {
		// pop the rejected ID off the stack
		newRejectedSize := len(rejected) - 1
		rejectedID := rejected[newRejectedSize]
		rejected = rejected[:newRejectedSize]

		// get the rejected node, and remove it from the tree
		rejectedNode := ts.blocks[rejectedID]
		delete(ts.blocks, rejectedID)

		for childID, child := range rejectedNode.children {
			ts.ctx.Log.Trace("rejecting block",
				zap.String("reason", "rejected ancestor"),
				zap.Stringer("blkID", childID),
				zap.Uint64("height", child.Height()),
				zap.Stringer("parentID", rejectedID),
			)
			if err := child.Reject(ctx); err != nil {
				return err
			}
			ts.metrics.Rejected(childID, ts.pollNumber, len(child.Bytes()))

			// add the newly rejected block to the end of the stack
			rejected = append(rejected, childID)
		}
	}
	return nil
}

func (ts *Topological) GetParent(id ids.ID) (ids.ID, bool) {
	block, ok := ts.blocks[id]
	if !ok || block == nil || block.blk == nil {
		return ids.Empty, false
	}
	return block.blk.Parent(), true
}
