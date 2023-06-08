// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	defaultRequestKeyLimit      = maxKeyValuesLimit
	defaultRequestByteSizeLimit = maxByteSizeLimit
)

var (
	ErrAlreadyStarted             = errors.New("cannot start a StateSyncManager that has already been started")
	ErrAlreadyClosed              = errors.New("StateSyncManager is closed")
	ErrNoClientProvided           = errors.New("client is a required field of the sync config")
	ErrNoDatabaseProvided         = errors.New("sync database is a required field of the sync config")
	ErrNoLogProvided              = errors.New("log is a required field of the sync config")
	ErrZeroWorkLimit              = errors.New("simultaneous work limit must be greater than 0")
	ErrFinishedWithUnexpectedRoot = errors.New("finished syncing with an unexpected root")
)

type priority byte

// Note that [highPriority] > [medPriority] > [lowPriority].
const (
	lowPriority priority = iota + 1
	medPriority
	highPriority
)

// Signifies that we should sync the range [start, end].
// nil [start] means there is no lower bound.
// nil [end] means there is no upper bound.
// [LocalRootID] is the ID of the root of this range in our database.
// If we have no local root for this range, [LocalRootID] is ids.Empty.
type syncWorkItem struct {
	start       []byte
	end         []byte
	priority    priority
	LocalRootID ids.ID
}

// TODO danlaine look into using a sync.Pool for syncWorkItems
func newWorkItem(localRootID ids.ID, start, end []byte, priority priority) *syncWorkItem {
	return &syncWorkItem{
		LocalRootID: localRootID,
		start:       start,
		end:         end,
		priority:    priority,
	}
}

type StateSyncManager struct {
	// Must be held when accessing [config.TargetRoot].
	syncTargetLock sync.RWMutex
	config         StateSyncConfig

	workLock sync.Mutex
	// The number of work items currently being processed.
	// Namely, the number of goroutines executing [doWork].
	// [workLock] must be held when accessing [processingWorkItems].
	processingWorkItems int
	// [workLock] must be held while accessing [unprocessedWork].
	unprocessedWork *syncWorkHeap
	// Signalled when:
	// - An item is added to [unprocessedWork].
	// - An item is added to [processedWork].
	// - Close() is called.
	// [workLock] is its inner lock.
	unprocessedWorkCond sync.Cond
	// [workLock] must be held while accessing [processedWork].
	processedWork *syncWorkHeap

	// When this is closed:
	// - [closed] is true.
	// - [cancelCtx] was called.
	// - [workToBeDone] and [completedWork] are closed.
	syncDoneChan chan struct{}

	errLock sync.Mutex
	// If non-nil, there was a fatal error.
	// [errLock] must be held when accessing [fatalError].
	fatalError error

	// Cancels all currently processing work items.
	cancelCtx context.CancelFunc

	// Set to true when StartSyncing is called.
	syncing   bool
	closeOnce sync.Once
}

type StateSyncConfig struct {
	SyncDB                SyncableDB
	Client                Client
	SimultaneousWorkLimit int
	Log                   logging.Logger
	TargetRoot            ids.ID
}

func NewStateSyncManager(config StateSyncConfig) (*StateSyncManager, error) {
	switch {
	case config.Client == nil:
		return nil, ErrNoClientProvided
	case config.SyncDB == nil:
		return nil, ErrNoDatabaseProvided
	case config.Log == nil:
		return nil, ErrNoLogProvided
	case config.SimultaneousWorkLimit == 0:
		return nil, ErrZeroWorkLimit
	}

	m := &StateSyncManager{
		config:          config,
		syncDoneChan:    make(chan struct{}),
		unprocessedWork: newSyncWorkHeap(),
		processedWork:   newSyncWorkHeap(),
	}
	m.unprocessedWorkCond.L = &m.workLock

	return m, nil
}

func (m *StateSyncManager) StartSyncing(ctx context.Context) error {
	m.workLock.Lock()
	defer m.workLock.Unlock()

	if m.syncing {
		return ErrAlreadyStarted
	}

	// Add work item to fetch the entire key range.
	// Note that this will be the first work item to be processed.
	m.unprocessedWork.Insert(newWorkItem(ids.Empty, nil, nil, lowPriority))

	m.syncing = true
	ctx, m.cancelCtx = context.WithCancel(ctx)

	go m.sync(ctx)
	return nil
}

// sync awaits signal on [m.unprocessedWorkCond], which indicates that there
// is work to do or syncing completes.  If there is work, sync will dispatch a goroutine to do
// the work.
func (m *StateSyncManager) sync(ctx context.Context) {
	defer func() {
		// Invariant: [m.workLock] is held when this goroutine begins.
		m.close()
		m.workLock.Unlock()
	}()

	// Keep doing work until we're closed, done or [ctx] is canceled.
	m.workLock.Lock()
	for {
		// Invariant: [m.workLock] is held here.
		if ctx.Err() != nil { // [m] is closed.
			return // [m.workLock] released by defer.
		}
		if m.processingWorkItems >= m.config.SimultaneousWorkLimit {
			// We're already processing the maximum number of work items.
			// Wait until one of them finishes.
			m.unprocessedWorkCond.Wait()
			continue
		}
		if m.unprocessedWork.Len() == 0 {
			if m.processingWorkItems == 0 {
				// There's no work to do, and there are no work items being processed
				// which could cause work to be added, so we're done.
				return // [m.workLock] released by defer.
			}
			// There's no work to do.
			// Note that if [m].Close() is called, or [ctx] is canceled,
			// Close() will be called, which will broadcast on [m.unprocessedWorkCond],
			// which will cause Wait() to return, and this goroutine to exit.
			m.unprocessedWorkCond.Wait()
			continue
		}
		m.processingWorkItems++
		workItem := m.unprocessedWork.GetWork()
		// TODO danlaine: We won't release [m.workLock] until
		// we've started a goroutine for each available work item.
		// We can't apply proofs we receive until we release [m.workLock].
		// Is this OK? Is it possible we end up with too many goroutines?
		go m.doWork(ctx, workItem)
	}
}

// Close will stop the syncing process
func (m *StateSyncManager) Close() {
	m.workLock.Lock()
	defer m.workLock.Unlock()
	m.close()
}

// close is called when there is a fatal error or sync is complete.
// [workLock] must be held
func (m *StateSyncManager) close() {
	m.closeOnce.Do(func() {
		// Don't process any more work items.
		// Drop currently processing work items.
		if m.cancelCtx != nil {
			m.cancelCtx()
		}

		// ensure any goroutines waiting for work from the heaps gets released
		m.unprocessedWork.Close()
		m.unprocessedWorkCond.Signal()
		m.processedWork.Close()

		// signal all code waiting on the sync to complete
		close(m.syncDoneChan)
	})
}

// Processes [item] by fetching and applying a change or range proof.
// Assumes [m.workLock] is not held.
func (m *StateSyncManager) doWork(ctx context.Context, item *syncWorkItem) {
	defer func() {
		m.workLock.Lock()
		defer m.workLock.Unlock()

		m.processingWorkItems--
		m.unprocessedWorkCond.Signal()
	}()

	if item.LocalRootID == ids.Empty {
		// the keys in this range have not been downloaded, so get all key/values
		m.getAndApplyRangeProof(ctx, item)
	} else {
		// the keys in this range have already been downloaded, but the root changed, so get all changes
		m.getAndApplyChangeProof(ctx, item)
	}
}

// Fetch and apply the change proof given by [workItem].
// Assumes [m.workLock] is not held.
func (m *StateSyncManager) getAndApplyChangeProof(ctx context.Context, workItem *syncWorkItem) {
	rootID := m.getTargetRoot()

	if workItem.LocalRootID == rootID {
		// Start root is the same as the end root, so we're done.
		m.completeWorkItem(ctx, workItem, workItem.end, rootID, nil)
		return
	}

	changeProof, err := m.config.Client.GetChangeProof(
		ctx,
		&pb.SyncGetChangeProofRequest{
			StartRootHash: workItem.LocalRootID[:],
			EndRootHash:   rootID[:],
			StartKey:      workItem.start,
			EndKey:        workItem.end,
			KeyLimit:      defaultRequestKeyLimit,
			BytesLimit:    defaultRequestByteSizeLimit,
		},
		m.config.SyncDB,
	)
	if err != nil {
		m.setError(err)
		return
	}

	select {
	case <-m.syncDoneChan:
		// If we're closed, don't apply the proof.
		return
	default:
	}

	// The start or end root IDs are not present in other nodes' history.
	// Add this range as a fresh uncompleted work item to the work heap.
	// TODO danlaine send range proof instead of failure notification
	if !changeProof.HadRootsInHistory {
		workItem.LocalRootID = ids.Empty
		m.enqueueWork(workItem)
		return
	}

	largestHandledKey := workItem.end
	// if the proof wasn't empty, apply changes to the sync DB
	if len(changeProof.KeyChanges) > 0 {
		if err := m.config.SyncDB.CommitChangeProof(ctx, changeProof); err != nil {
			m.setError(err)
			return
		}
		largestHandledKey = changeProof.KeyChanges[len(changeProof.KeyChanges)-1].Key
	}

	m.completeWorkItem(ctx, workItem, largestHandledKey, rootID, changeProof.EndProof)
}

// Fetch and apply the range proof given by [workItem].
// Assumes [m.workLock] is not held.
func (m *StateSyncManager) getAndApplyRangeProof(ctx context.Context, workItem *syncWorkItem) {
	rootID := m.getTargetRoot()
	proof, err := m.config.Client.GetRangeProof(ctx,
		&pb.SyncGetRangeProofRequest{
			RootHash:   rootID[:],
			StartKey:   workItem.start,
			EndKey:     workItem.end,
			KeyLimit:   defaultRequestKeyLimit,
			BytesLimit: defaultRequestByteSizeLimit,
		},
	)
	if err != nil {
		m.setError(err)
		return
	}

	select {
	case <-m.syncDoneChan:
		// If we're closed, don't apply the proof.
		return
	default:
	}

	largestHandledKey := workItem.end
	if len(proof.KeyValues) > 0 {
		// Add all the key-value pairs we got to the database.
		if err := m.config.SyncDB.CommitRangeProof(ctx, workItem.start, proof); err != nil {
			m.setError(err)
			return
		}

		largestHandledKey = proof.KeyValues[len(proof.KeyValues)-1].Key
	}

	m.completeWorkItem(ctx, workItem, largestHandledKey, rootID, proof.EndProof)
}

// findNextKey attempts to find the first key larger than the lastReceivedKey that is different in the local merkle vs the merkle that is being synced.
// Returns the first key with a difference in the range (lastReceivedKey, rangeEnd), or nil if no difference was found in the range
func (m *StateSyncManager) findNextKey(
	ctx context.Context,
	lastReceivedKey []byte,
	rangeEnd []byte,
	receivedProofNodes []merkledb.ProofNode,
) ([]byte, error) {
	// We want the first key larger than the lastReceivedKey.
	// This is done by taking two proofs for the same key (one that was just received as part of a proof, and one from the local db)
	// and traversing them from the deepest key to the shortest key.
	// For each node in these proofs, compare if the children of that node exist or have the same id in the other proof.
	proofKeyPath := merkledb.SerializedPath{Value: lastReceivedKey, NibbleLength: 2 * len(lastReceivedKey)}

	// If the received proof is an exclusion proof, the last node may be for a key that is after the lastReceivedKey.
	// If the last received node's key is after the lastReceivedKey, it can be removed to obtain a valid proof for a prefix of the lastReceivedKey
	if !proofKeyPath.HasPrefix(receivedProofNodes[len(receivedProofNodes)-1].KeyPath) {
		receivedProofNodes = receivedProofNodes[:len(receivedProofNodes)-1]
		// update the proofKeyPath to be for the prefix
		proofKeyPath = receivedProofNodes[len(receivedProofNodes)-1].KeyPath
	}

	// get a proof for the same key as the received proof from the local db
	localProofOfKey, err := m.config.SyncDB.GetProof(ctx, proofKeyPath.Value)
	if err != nil {
		return nil, err
	}
	localProofNodes := localProofOfKey.Path

	// The local proof may also be an exclusion proof with an extra node.
	// Remove this extra node if it exists to get a proof of the same key as the received proof
	if !proofKeyPath.HasPrefix(localProofNodes[len(localProofNodes)-1].KeyPath) {
		localProofNodes = localProofNodes[:len(localProofNodes)-1]
	}

	var nextKey []byte

	localProofNodeIndex := len(localProofNodes) - 1
	receivedProofNodeIndex := len(receivedProofNodes) - 1

	// traverse the two proofs from the deepest nodes up to the root until a difference is found
	for localProofNodeIndex >= 0 && receivedProofNodeIndex >= 0 && nextKey == nil {
		localProofNode := localProofNodes[localProofNodeIndex]
		receivedProofNode := receivedProofNodes[receivedProofNodeIndex]

		// deepest node is the proof node with the longest key (deepest in the trie) in the two proofs that hasn't been handled yet.
		// deepestNodeFromOtherProof is the proof node from the other proof with the same key/depth if it exists, nil otherwise.
		var deepestNode, deepestNodeFromOtherProof *merkledb.ProofNode

		// select the deepest proof node from the two proofs
		switch {
		case receivedProofNode.KeyPath.NibbleLength > localProofNode.KeyPath.NibbleLength:
			// there was a branch node in the received proof that isn't in the local proof
			// see if the received proof node has children not present in the local proof
			deepestNode = &receivedProofNode

			// we have dealt with this received node, so move on to the next received node
			receivedProofNodeIndex--

		case localProofNode.KeyPath.NibbleLength > receivedProofNode.KeyPath.NibbleLength:
			// there was a branch node in the local proof that isn't in the received proof
			// see if the local proof node has children not present in the received proof
			deepestNode = &localProofNode

			// we have dealt with this local node, so move on to the next local node
			localProofNodeIndex--

		default:
			// the two nodes are at the same depth
			// see if any of the children present in the local proof node are different
			// from the children in the received proof node
			deepestNode = &localProofNode
			deepestNodeFromOtherProof = &receivedProofNode

			// we have dealt with this local node and received node, so move on to the next nodes
			localProofNodeIndex--
			receivedProofNodeIndex--
		}

		// We only want to look at the children with keys greater than the proofKey.
		// The proof key has the deepest node's key as a prefix,
		// so only the next nibble of the proof key needs to be considered.

		// If the deepest node has the same key as proofKeyPath,
		// then all of its children have keys greater than the proof key, so we can start at the 0 nibble
		startingChildNibble := byte(0)

		// If the deepest node has a key shorter than the key being proven,
		// we can look at the next nibble of the proof key to determine which of that node's children have keys larger than proofKeyPath.
		// Any child with a nibble greater than the proofKeyPath's nibble at that index will have a larger key
		if deepestNode.KeyPath.NibbleLength < proofKeyPath.NibbleLength {
			startingChildNibble = proofKeyPath.NibbleVal(deepestNode.KeyPath.NibbleLength) + 1
		}

		// determine if there are any differences in the children for the deepest unhandled node of the two proofs
		if childIndex, hasDifference := findChildDifference(deepestNode, deepestNodeFromOtherProof, startingChildNibble); hasDifference {
			nextKey = deepestNode.KeyPath.AppendNibble(childIndex).Value
			break
		}
	}

	// If the nextKey is before or equal to the lastReceivedKey
	// then we couldn't find a better answer than the lastReceivedKey.
	// Set the nextKey to lastReceivedKey + 0, which is the first key in the open range (lastReceivedKey, rangeEnd)
	if nextKey != nil && bytes.Compare(nextKey, lastReceivedKey) <= 0 {
		nextKey = lastReceivedKey
		nextKey = append(nextKey, 0)
	}

	// If the nextKey is larger than the end of the range, return nil to signal that there is no next key in range
	if len(rangeEnd) > 0 && bytes.Compare(nextKey, rangeEnd) >= 0 {
		return nil, nil
	}

	// the nextKey is within the open range (lastReceivedKey, rangeEnd), so return it
	return nextKey, nil
}

// findChildDifference returns the first child index that is different between node 1 and node 2 if one exists and
// a bool indicating if any difference was found
func findChildDifference(node1, node2 *merkledb.ProofNode, startIndex byte) (byte, bool) {
	var (
		child1, child2 ids.ID
		ok1, ok2       bool
	)
	for childIndex := startIndex; childIndex < merkledb.NodeBranchFactor; childIndex++ {
		if node1 != nil {
			child1, ok1 = node1.Children[childIndex]
		}
		if node2 != nil {
			child2, ok2 = node2.Children[childIndex]
		}
		// if one node has a child and the other doesn't or the children ids don't match,
		// return the current child index as the first difference
		if (ok1 || ok2) && child1 != child2 {
			return childIndex, true
		}
	}
	// there were no differences found
	return 0, false
}

func (m *StateSyncManager) Error() error {
	m.errLock.Lock()
	defer m.errLock.Unlock()

	return m.fatalError
}

// Wait blocks until one of the following occurs:
// - sync is complete.
// - sync fatally errored.
// - [ctx] is canceled.
// If [ctx] is canceled, returns [ctx].Err().
func (m *StateSyncManager) Wait(ctx context.Context) error {
	select {
	case <-m.syncDoneChan:
	case <-ctx.Done():
		return ctx.Err()
	}

	// There was a fatal error.
	if err := m.Error(); err != nil {
		return err
	}

	root, err := m.config.SyncDB.GetMerkleRoot(ctx)
	if err != nil {
		m.config.Log.Info("completed with error", zap.Error(err))
		return err
	}
	if m.getTargetRoot() != root {
		// This should never happen.
		return fmt.Errorf("%w: expected %s, got %s", ErrFinishedWithUnexpectedRoot, m.getTargetRoot(), root)
	}
	m.config.Log.Info("completed", zap.String("new root", root.String()))
	return nil
}

func (m *StateSyncManager) UpdateSyncTarget(syncTargetRoot ids.ID) error {
	m.workLock.Lock()
	defer m.workLock.Unlock()

	select {
	case <-m.syncDoneChan:
		return ErrAlreadyClosed
	default:
	}

	m.syncTargetLock.Lock()
	defer m.syncTargetLock.Unlock()

	if m.config.TargetRoot == syncTargetRoot {
		// the target hasn't changed, so there is nothing to do
		return nil
	}

	m.config.TargetRoot = syncTargetRoot

	// move all completed ranges into the work heap with high priority
	shouldSignal := m.processedWork.Len() > 0
	for m.processedWork.Len() > 0 {
		// Note that [m.processedWork].Close() hasn't
		// been called because we have [m.workLock]
		// and we checked that [m.closed] is false.
		currentItem := m.processedWork.GetWork()
		currentItem.priority = highPriority
		m.unprocessedWork.Insert(currentItem)
	}
	if shouldSignal {
		// Only signal once because we only have 1 goroutine
		// waiting on [m.unprocessedWorkCond].
		m.unprocessedWorkCond.Signal()
	}
	return nil
}

func (m *StateSyncManager) getTargetRoot() ids.ID {
	m.syncTargetLock.RLock()
	defer m.syncTargetLock.RUnlock()

	return m.config.TargetRoot
}

// Record that there was a fatal error and begin shutting down.
func (m *StateSyncManager) setError(err error) {
	m.errLock.Lock()
	defer m.errLock.Unlock()

	m.config.Log.Error("syncing failed", zap.Error(err))
	m.fatalError = err
	// Call in goroutine because we might be holding [m.workLock]
	// which [m.Close] will try to acquire.
	go m.Close()
}

// Mark the range [start, end] as synced up to [rootID].
// Assumes [m.workLock] is not held.
func (m *StateSyncManager) completeWorkItem(ctx context.Context, workItem *syncWorkItem, largestHandledKey []byte, rootID ids.ID, proofOfLargestKey []merkledb.ProofNode) {
	// if the last key is equal to the end, then the full range is completed
	if !bytes.Equal(largestHandledKey, workItem.end) {
		// find the next key to start querying by comparing the proofs for the last completed key
		nextStartKey, err := m.findNextKey(ctx, largestHandledKey, workItem.end, proofOfLargestKey)
		if err != nil {
			m.setError(err)
			return
		}

		// nextStartKey being nil indicates that the entire range has been completed
		if nextStartKey == nil {
			largestHandledKey = workItem.end
		} else {
			// the full range wasn't completed, so enqueue a new work item for the range [nextStartKey, workItem.end]
			m.enqueueWork(newWorkItem(workItem.LocalRootID, nextStartKey, workItem.end, workItem.priority))
			largestHandledKey = nextStartKey
		}
	}

	// completed the range [workItem.start, lastKey], log and record in the completed work heap
	m.config.Log.Info("completed range",
		zap.Binary("start", workItem.start),
		zap.Binary("end", largestHandledKey),
	)
	if m.getTargetRoot() == rootID {
		m.workLock.Lock()
		defer m.workLock.Unlock()

		m.processedWork.MergeInsert(newWorkItem(rootID, workItem.start, largestHandledKey, workItem.priority))
	} else {
		// the root has changed, so reinsert with high priority
		m.enqueueWork(newWorkItem(rootID, workItem.start, largestHandledKey, highPriority))
	}
}

// Queue the given key range to be fetched and applied.
// If there are sufficiently few unprocessed/processing work items,
// splits the range into two items and queues them both.
// Assumes [m.workLock] is not held.
func (m *StateSyncManager) enqueueWork(item *syncWorkItem) {
	m.workLock.Lock()
	defer func() {
		m.workLock.Unlock()
		m.unprocessedWorkCond.Signal()
	}()

	if m.processingWorkItems+m.unprocessedWork.Len() > 2*m.config.SimultaneousWorkLimit {
		// There are too many work items already, don't split the range
		m.unprocessedWork.Insert(item)
		return
	}

	// Split the remaining range into to 2.
	// Find the middle point.
	mid := midPoint(item.start, item.end)

	// first item gets higher priority than the second to encourage finished ranges to grow
	// rather than start a new range that is not contiguous with existing completed ranges
	first := newWorkItem(item.LocalRootID, item.start, mid, medPriority)
	second := newWorkItem(item.LocalRootID, mid, item.end, lowPriority)

	m.unprocessedWork.Insert(first)
	m.unprocessedWork.Insert(second)
}

// find the midpoint between two keys
// nil on start is treated as all 0's
// nil on end is treated as all 255's
func midPoint(start, end []byte) []byte {
	length := len(start)
	if len(end) > length {
		length = len(end)
	}
	if length == 0 {
		return []byte{127}
	}

	// This check deals with cases where the end has a 255(or is nil which is treated as all 255s) and the start key ends 255.
	// For example, midPoint([255], nil) should be [255, 127], not [255].
	// The result needs the extra byte added on to the end to deal with the fact that the naive midpoint between 255 and 255 would be 255
	if (len(start) > 0 && start[len(start)-1] == 255) && (len(end) == 0 || end[len(end)-1] == 255) {
		length++
	}

	leftover := 0
	midpoint := make([]byte, length+1)
	for i := 0; i < length; i++ {
		startVal := 0
		if i < len(start) {
			startVal = int(start[i])
		}

		endVal := 0
		if len(end) == 0 {
			endVal = 255
		}
		if i < len(end) {
			endVal = int(end[i])
		}

		total := startVal + endVal + leftover
		leftover = 0
		// if total is odd, when we divide, we will lose the .5,
		// record that in the leftover for the next digits
		if total%2 == 1 {
			leftover = 256
		}

		// find the midpoint between the start and the end
		total /= 2

		// larger than byte can hold, so carry over to previous byte
		if total >= 256 {
			total -= 256
			index := i - 1
			for index > 0 && midpoint[index] == 255 {
				midpoint[index] = 0
				index--
			}
			midpoint[index]++
		}
		midpoint[i] = byte(total)
	}
	if leftover > 0 {
		midpoint[length] = 127
	} else {
		midpoint = midpoint[0:length]
	}
	return midpoint
}
