// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/metrics"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
)

// TopologicalFactory implements Factory by returning a topological struct
type TopologicalFactory struct{}

// New implements Factory
func (TopologicalFactory) New() Consensus { return &Topological{} }

// Topological implements the Snowman interface by using a tree tracking the
// strongly preferred branch. This tree structure amortizes network polls to
// vote on more than just the next block.
type Topological struct {
	metrics.Latency
	metrics.Polls

	// pollNumber is the number of times RecordPolls has been called
	pollNumber uint64

	// ctx is the context this snowman instance is executing in
	ctx *snow.ConsensusContext

	// params are the parameters that should be used to initialize snowball
	// instances
	params snowball.Parameters

	// head is the last accepted block
	head ids.ID

	// height is the height of the last accepted block
	height uint64

	// blocks stores the last accepted block and all the pending blocks
	blocks map[ids.ID]*snowmanBlock // blockID -> snowmanBlock

	// preferredIDs stores the set of IDs that are currently preferred.
	preferredIDs ids.Set

	// tail is the preferred block with no children
	tail ids.ID

	// Used in [calculateInDegree] and.
	// Should only be accessed in that method.
	// We use this one instance of ids.Set instead of creating a
	// new ids.Set during each call to [calculateInDegree].
	leaves ids.Set

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
	votes ids.Bag
}

// Used to track which children should receive votes
type votes struct {
	// parentID is the parent of all the votes provided in the votes bag
	parentID ids.ID
	// votes for all the children of the parent
	votes ids.Bag
}

// Initialize implements the Snowman interface
func (ts *Topological) Initialize(ctx *snow.ConsensusContext, params snowball.Parameters, rootID ids.ID, rootHeight uint64) error {
	if err := params.Verify(); err != nil {
		return err
	}
	if err := ts.Latency.Initialize("blks", "block(s)", ctx.Log, "", ctx.Registerer); err != nil {
		return err
	}
	if err := ts.Polls.Initialize("", ctx.Registerer); err != nil {
		return err
	}
	ts.leaves = ids.Set{}
	ts.kahnNodes = make(map[ids.ID]kahnNode)
	ts.ctx = ctx
	ts.params = params
	ts.head = rootID
	ts.height = rootHeight
	ts.blocks = map[ids.ID]*snowmanBlock{
		rootID: {sm: ts},
	}
	ts.tail = rootID
	return nil
}

// Parameters implements the Snowman interface
func (ts *Topological) Parameters() snowball.Parameters { return ts.params }

// NumProcessing implements the Snowman interface
func (ts *Topological) NumProcessing() int { return len(ts.blocks) - 1 }

// Add implements the Snowman interface
func (ts *Topological) Add(blk Block) error {
	parentID := blk.Parent()

	blkID := blk.ID()
	blkBytes := blk.Bytes()

	// Notify anyone listening that this block was issued.
	if err := ts.ctx.DecisionDispatcher.Issue(ts.ctx, blkID, blkBytes); err != nil {
		return err
	}
	if err := ts.ctx.ConsensusDispatcher.Issue(ts.ctx, blkID, blkBytes); err != nil {
		return err
	}
	ts.Latency.Issued(blkID, ts.pollNumber)

	parentNode, ok := ts.blocks[parentID]
	if !ok {
		// If the ancestor is missing, this means the ancestor must have already
		// been pruned. Therefore, the dependent should be transitively
		// rejected.
		if err := blk.Reject(); err != nil {
			return err
		}

		// Notify anyone listening that this block was rejected.
		if err := ts.ctx.DecisionDispatcher.Reject(ts.ctx, blkID, blkBytes); err != nil {
			return err
		}
		if err := ts.ctx.ConsensusDispatcher.Reject(ts.ctx, blkID, blkBytes); err != nil {
			return err
		}
		ts.Latency.Rejected(blkID, ts.pollNumber)
		return nil
	}

	// add the block as a child of its parent, and add the block to the tree
	parentNode.AddChild(blk)
	ts.blocks[blkID] = &snowmanBlock{
		sm:  ts,
		blk: blk,
	}

	// If we are extending the tail, this is the new tail
	if ts.tail == parentID {
		ts.tail = blkID
		ts.preferredIDs.Add(blkID)
	}
	return nil
}

// AcceptedOrProcessing implements the Snowman interface
func (ts *Topological) AcceptedOrProcessing(blk Block) bool {
	// If the block is accepted, then it mark it as so.
	if blk.Status() == choices.Accepted {
		return true
	}
	return ts.processing(blk.ID())
}

// DecidedOrProcessing implements the Snowman interface
func (ts *Topological) DecidedOrProcessing(blk Block) bool {
	// If the block is decided, then it must have been previously issued.
	if blk.Status().Decided() {
		return true
	}
	// If the block is marked as fetched, we can check if it has been
	// transitively rejected.
	if blk.Status() == choices.Processing && blk.Height() <= ts.height {
		return true
	}
	return ts.processing(blk.ID())
}

func (ts *Topological) processing(blkID ids.ID) bool {
	// If the block is in the map of current blocks, then the block is currently
	// processing.
	_, ok := ts.blocks[blkID]
	return ok
}

// IsPreferred implements the Snowman interface
func (ts *Topological) IsPreferred(blk Block) bool {
	// If the block is accepted, then it must be transitively preferred.
	if blk.Status() == choices.Accepted {
		return true
	}
	return ts.preferredIDs.Contains(blk.ID())
}

// Preference implements the Snowman interface
func (ts *Topological) Preference() ids.ID { return ts.tail }

// RecordPoll implements the Snowman interface
//
// The votes bag contains at most K votes for blocks in the tree. If there is a
// vote for a block that isn't in the tree, the vote is dropped.
//
// Votes are propagated transitively towards the genesis. All blocks in the tree
// that result in at least Alpha votes will record the poll on their children.
// Every other block will have an unsuccessful poll registered.
//
// After collecting which blocks should be voted on, the polls are registered
// and blocks are accepted/rejected as needed. The tail is then updated to equal
// the leaf on the preferred branch.
//
// To optimize the theoretical complexity of the vote propagation, a topological
// sort is done over the blocks that are reachable from the provided votes.
// During the sort, votes are pushed towards the genesis. To prevent interating
// over all blocks that had unsuccessful polls, we set a flag on the block to
// know that any future traversal through that block should register an
// unsuccessful poll on that block and every descendant block.
//
// The complexity of this function is:
// - Runtime = 3 * |live set| + |votes|
// - Space = 2 * |live set| + |votes|
func (ts *Topological) RecordPoll(voteBag ids.Bag) error {
	// Register a new poll call
	ts.pollNumber++

	var voteStack []votes
	if voteBag.Len() >= ts.params.Alpha {
		// If there is no way for an alpha majority to occur, there is no need
		// to perform any traversals.

		// Populates [ts.kahnNodes] and [ts.leaves]
		// Runtime = |live set| + |votes| ; Space = |live set| + |votes|
		ts.calculateInDegree(voteBag)

		// Runtime = |live set| ; Space = |live set|
		voteStack = ts.pushVotes()
	}

	// Runtime = |live set| ; Space = Constant
	preferred, err := ts.vote(voteStack)
	if err != nil {
		return err
	}

	// If the set of preferred IDs already contains the preference, then the
	// tail is guaranteed to already be set correctly. This is because the value
	// returned from vote reports the next preferred block after the last
	// preferred block that was voted for. If this block was previously
	// preferred, then we know that following the preferences down the chain
	// will return the current tail.
	if ts.preferredIDs.Contains(preferred) {
		return nil
	}

	// Runtime = |live set| ; Space = Constant
	ts.preferredIDs.Clear()

	ts.tail = preferred
	startBlock := ts.blocks[ts.tail]

	// Runtime = |live set| ; Space = Constant
	// Traverse from the preferred ID to the last accepted ancestor.
	for block := startBlock; !block.Accepted(); {
		ts.preferredIDs.Add(block.blk.ID())
		block = ts.blocks[block.blk.Parent()]
	}
	// Traverse from the preferred ID to the preferred child until there are no
	// children.
	for block := startBlock; block.sb != nil; block = ts.blocks[ts.tail] {
		ts.tail = block.sb.Preference()
		ts.preferredIDs.Add(ts.tail)
	}
	return nil
}

// Finalized implements the Snowman interface
func (ts *Topological) Finalized() bool { return len(ts.blocks) == 1 }

// HealthCheck returns information about the consensus health.
func (ts *Topological) HealthCheck() (interface{}, error) {
	numOutstandingBlks := ts.Latency.ProcessingLen()
	isOutstandingBlks := numOutstandingBlks <= ts.params.MaxOutstandingItems
	healthy := isOutstandingBlks
	details := map[string]interface{}{
		"outstandingBlocks": numOutstandingBlks,
	}

	// check for long running blocks
	timeReqRunning := ts.Latency.MeasureAndGetOldestDuration()
	isProcessingTime := timeReqRunning <= ts.params.MaxItemProcessingTime
	healthy = healthy && isProcessingTime
	details["longestRunningBlock"] = timeReqRunning.String()

	if !healthy {
		var errorReasons []string
		if !isOutstandingBlks {
			errorReasons = append(errorReasons, fmt.Sprintf("number of outstanding blocks %d > %d", numOutstandingBlks, ts.params.MaxOutstandingItems))
		}
		if !isProcessingTime {
			errorReasons = append(errorReasons, fmt.Sprintf("block processing time %s > %s", timeReqRunning, ts.params.MaxItemProcessingTime))
		}
		return details, fmt.Errorf("snowman consensus is not healthy reason: %s", strings.Join(errorReasons, ", "))
	}
	return details, nil
}

// takes in a list of votes and sets up the topological ordering. Returns the
// reachable section of the graph annotated with the number of inbound edges and
// the non-transitively applied votes. Also returns the list of leaf blocks.
func (ts *Topological) calculateInDegree(votes ids.Bag) {
	// Clear the Kahn node set
	for k := range ts.kahnNodes {
		delete(ts.kahnNodes, k)
	}
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
		if votedBlock.Accepted() {
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
		for n := ts.blocks[parentID]; !n.Accepted(); n = ts.blocks[parentID] {
			parentID = n.blk.Parent()

			// Increase the inDegree by one
			kahn := ts.kahnNodes[parentID]
			kahn.inDegree++
			ts.kahnNodes[parentID] = kahn

			// If we have already seen this block, then we shouldn't increase
			// the inDegree of the ancestors through this block again.
			if kahn.inDegree != 1 {
				break
			}

			// If I am transitively seeing this block for the first time, either
			// the block was previously unknown or it was previously a leaf.
			// Regardless, it shouldn't be tracked as a leaf.
			ts.leaves.Remove(parentID)
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
		if kahnNode.votes.Len() >= ts.params.Alpha {
			voteStack = append(voteStack, votes{
				parentID: leafID,
				votes:    kahnNode.votes,
			})
		}

		// If the block is accepted, then we don't need to push votes to the
		// parent block
		if block.Accepted() {
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
func (ts *Topological) vote(voteStack []votes) (ids.ID, error) {
	// If the voteStack is empty, then the full tree should falter. This won't
	// change the preferred branch.
	if len(voteStack) == 0 {
		headBlock := ts.blocks[ts.head]
		headBlock.shouldFalter = true

		if numProcessing := len(ts.blocks) - 1; numProcessing > 0 {
			ts.ctx.Log.Verbo("No progress was made after a vote with %d pending blocks", numProcessing)
			ts.Polls.Failed()
		}
		return ts.tail, nil
	}

	// keep track of the new preferred block
	newPreferred := ts.head
	onPreferredBranch := true
	for len(voteStack) > 0 {
		// pop a vote off the stack
		newStackSize := len(voteStack) - 1
		vote := voteStack[newStackSize]
		voteStack = voteStack[:newStackSize]

		// get the block that we are going to vote on
		parentBlock, notRejected := ts.blocks[vote.parentID]

		// if the block block we are going to vote on was already rejected, then
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
			ts.ctx.Log.Verbo("Resetting confidence below %s", vote.parentID)

			parentBlock.sb.RecordUnsuccessfulPoll()
			parentBlock.shouldFalter = false
		}

		// apply the votes for this snowball instance
		parentBlock.sb.RecordPoll(vote.votes)

		// Only accept when you are finalized and the head.
		if parentBlock.sb.Finalized() && ts.head == vote.parentID {
			if err := ts.accept(parentBlock); err != nil {
				return ids.ID{}, err
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
		nextID := ids.ID{}
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
				ts.ctx.Log.Verbo("Defering confidence reset of %s. Voting for %s", childID, nextID)

				// If the child is ever voted for positively, the confidence
				// must be reset first.
				childBlock.shouldFalter = true
			}
		}
	}

	ts.Polls.Successful()
	return newPreferred, nil
}

// accept the preferred child of the provided snowman block. By accepting the
// preferred child, all other children will be rejected. When these children are
// rejected, all their descendants will be rejected.
func (ts *Topological) accept(n *snowmanBlock) error {
	// We are finalizing the block's child, so we need to get the preference
	pref := n.sb.Preference()

	// Get the child and accept it
	child := n.children[pref]
	// Notify anyone listening that this block was accepted.
	bytes := child.Bytes()
	// Note that DecisionDispatcher.Accept / DecisionDispatcher.Accept must be
	// called before child.Accept to honor EventDispatcher.Accept's invariant.
	if err := ts.ctx.DecisionDispatcher.Accept(ts.ctx, pref, bytes); err != nil {
		return err
	}
	if err := ts.ctx.ConsensusDispatcher.Accept(ts.ctx, pref, bytes); err != nil {
		return err
	}

	ts.ctx.Log.Trace("accepting block %s", pref)
	if err := child.Accept(); err != nil {
		return err
	}

	ts.Latency.Accepted(pref, ts.pollNumber)

	// Because this is the newest accepted block, this is the new head.
	ts.head = pref
	ts.height = child.Height()
	// Remove the decided block from the set of processing IDs, as its status
	// now implies its preferredness.
	ts.preferredIDs.Remove(pref)

	// Because ts.blocks contains the last accepted block, we don't delete the
	// block from the blocks map here.

	rejects := make([]ids.ID, 0, len(n.children)-1)
	for childID, child := range n.children {
		if childID == pref {
			// don't reject the block we just accepted
			continue
		}

		ts.ctx.Log.Trace("rejecting block %s due to conflict with accepted block %s", childID, pref)
		if err := child.Reject(); err != nil {
			return err
		}

		// Notify anyone listening that this block was rejected.
		bytes := child.Bytes()
		if err := ts.ctx.DecisionDispatcher.Reject(ts.ctx, childID, bytes); err != nil {
			return err
		}
		if err := ts.ctx.ConsensusDispatcher.Reject(ts.ctx, childID, bytes); err != nil {
			return err
		}
		ts.Latency.Rejected(childID, ts.pollNumber)

		// Track which blocks have been directly rejected
		rejects = append(rejects, childID)
	}

	// reject all the descendants of the blocks we just rejected
	return ts.rejectTransitively(rejects)
}

// Takes in a list of rejected ids and rejects all descendants of these IDs
func (ts *Topological) rejectTransitively(rejected []ids.ID) error {
	// the rejected array is treated as a queue, with the next element at index
	// 0 and the last element at the end of the slice.
	for len(rejected) > 0 {
		// pop the rejected ID off the queue
		newRejectedSize := len(rejected) - 1
		rejectedID := rejected[newRejectedSize]
		rejected = rejected[:newRejectedSize]

		// get the rejected node, and remove it from the tree
		rejectedNode := ts.blocks[rejectedID]
		delete(ts.blocks, rejectedID)

		for childID, child := range rejectedNode.children {
			if err := child.Reject(); err != nil {
				return err
			}

			// Notify anyone listening that this block was rejected.
			bytes := child.Bytes()
			if err := ts.ctx.DecisionDispatcher.Reject(ts.ctx, childID, bytes); err != nil {
				return err
			}
			if err := ts.ctx.ConsensusDispatcher.Reject(ts.ctx, childID, bytes); err != nil {
				return err
			}
			ts.Latency.Rejected(childID, ts.pollNumber)

			// add the newly rejected block to the end of the queue
			rejected = append(rejected, childID)
		}
	}
	return nil
}
