// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

const (
	defaultLeafRequestLimit = 1024
	maxTokenWaitTime        = 5 * time.Second
)

var (
	token                         = struct{}{}
	ErrAlreadyStarted             = errors.New("cannot start a StateSyncManager that has already been started")
	ErrAlreadyClosed              = errors.New("StateSyncManager is closed")
	ErrNotEnoughBytes             = errors.New("less bytes read than the specified length")
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
	// - There are no more items in [unprocessedWork] and [processingWorkItems] is 0.
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

	// Rate-limits the number of concurrently processing work items.
	workTokens chan struct{}

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
	SyncDB                *merkledb.Database
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
		unprocessedWork: newSyncWorkHeap(2 * config.SimultaneousWorkLimit),
		processedWork:   newSyncWorkHeap(2 * config.SimultaneousWorkLimit),
		workTokens:      make(chan struct{}, config.SimultaneousWorkLimit),
	}
	m.unprocessedWorkCond.L = &m.workLock

	// fill the work tokens channel with work tokens
	for i := 0; i < config.SimultaneousWorkLimit; i++ {
		m.workTokens <- token
	}
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
	m.enqueueWork(newWorkItem(ids.Empty, nil, nil, lowPriority))

	m.syncing = true
	ctx, m.cancelCtx = context.WithCancel(ctx)

	go m.sync(ctx)
	return nil
}

// Repeatedly awaits signal on [m.unprocessedWorkCond] that there
// is work to do or we're done, and dispatches a goroutine to do
// the work.
func (m *StateSyncManager) sync(ctx context.Context) {
	defer func() {
		// Note we release [m.workLock] before calling Close()
		// because Close() will try to acquire [m.workLock].
		// Invariant: [m.workLock] is held when we return from this goroutine.
		m.workLock.Unlock()
		m.Close()
	}()

	// Keep doing work until we're closed, done or [ctx] is canceled.
	m.workLock.Lock()
	for {
		// Invariant: [m.workLock] is held here.
		if ctx.Err() != nil { // [m] is closed.
			return // [m.workLock] released by defer.
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

// Called when there is a fatal error or sync is complete.
func (m *StateSyncManager) Close() {
	m.closeOnce.Do(func() {
		m.workLock.Lock()
		defer m.workLock.Unlock()

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
	// Wait until we get a work token or we close.
	select {
	case <-m.workTokens:
	case <-ctx.Done():
		// [m] is closed and sync() is returning so don't care about cleanup.
		return
	}

	defer func() {
		m.workTokens <- token
		m.workLock.Lock()
		m.processingWorkItems--
		if m.processingWorkItems == 0 && m.unprocessedWork.Len() == 0 {
			// There are no processing or unprocessed work items so we're done.
			m.unprocessedWorkCond.Signal()
		}
		m.workLock.Unlock()
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

	changeproof, err := m.config.Client.GetChangeProof(ctx,
		&ChangeProofRequest{
			StartingRoot: workItem.LocalRootID,
			EndingRoot:   rootID,
			Start:        workItem.start,
			End:          workItem.end,
			Limit:        defaultLeafRequestLimit,
		},
		m.config.SyncDB,
	)
	if err != nil {
		m.setError(err)
		return
	}

	m.workLock.Lock()
	defer m.workLock.Unlock()

	select {
	case <-m.syncDoneChan:
		// If we're closed, don't apply the proof.
		return
	default:
	}

	// The start or end root IDs are not present in other nodes' history.
	// Add this range as a fresh uncompleted work item to the work heap.
	// TODO danlaine send range proof instead of failure notification
	if !changeproof.HadRootsInHistory {
		workItem.LocalRootID = ids.Empty
		m.enqueueWork(workItem)
		return
	}

	largestHandledKey := workItem.end
	// if the proof wasn't empty, apply changes to the sync DB
	if len(changeproof.KeyValues)+len(changeproof.DeletedKeys) > 0 {
		if err := m.config.SyncDB.CommitChangeProof(ctx, changeproof); err != nil {
			m.setError(err)
			return
		}

		if len(changeproof.KeyValues) > 0 {
			largestHandledKey = changeproof.KeyValues[len(changeproof.KeyValues)-1].Key
		}
		if len(changeproof.DeletedKeys) > 0 {
			lastDeletedKey := changeproof.DeletedKeys[len(changeproof.DeletedKeys)-1]
			if bytes.Compare(lastDeletedKey, largestHandledKey) == 1 {
				largestHandledKey = lastDeletedKey
			}
		}
	}

	m.completeWorkItem(ctx, workItem, largestHandledKey, rootID, changeproof.EndProof)
}

// Fetch and apply the range proof given by [workItem].
// Assumes [m.workLock] is not held.
func (m *StateSyncManager) getAndApplyRangeProof(ctx context.Context, workItem *syncWorkItem) {
	rootID := m.getTargetRoot()
	proof, err := m.config.Client.GetRangeProof(ctx,
		&RangeProofRequest{
			Root:  rootID,
			Start: workItem.start,
			End:   workItem.end,
			Limit: defaultLeafRequestLimit,
		},
	)
	if err != nil {
		m.setError(err)
		return
	}

	// TODO danlaine: Do we need this or can we
	// grab just before touching [m.unprocessedWork]?
	m.workLock.Lock()
	defer m.workLock.Unlock()

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

// Attempt to find what key to query next based on the differences between
// the local trie path to a node and the path recently received.
func (m *StateSyncManager) findNextKey(
	ctx context.Context,
	start []byte,
	end []byte,
	receivedProofNodes []merkledb.ProofNode,
) ([]byte, error) {
	proofOfStart, err := m.config.SyncDB.GetProof(ctx, start)
	if err != nil {
		return nil, err
	}
	localProofNodes := proofOfStart.Path

	var result []byte
	localIndex := len(localProofNodes) - 1
	receivedIndex := len(receivedProofNodes) - 1
	startKeyPath := merkledb.SerializedPath{Value: start, NibbleLength: 2 * len(start)}

	// Just return the start key when the proof nodes contain keys that are not prefixes of the start key
	// this occurs mostly in change proofs where the largest returned key was a deleted key.
	// Since the key was deleted, it no longer shows up in the proof nodes
	// for now, just fallback to using the start key, which is always correct.
	// TODO: determine a more accurate nextKey in this scenario
	if !startKeyPath.HasPrefix(localProofNodes[localIndex].KeyPath) || !startKeyPath.HasPrefix(receivedProofNodes[receivedIndex].KeyPath) {
		return start, nil
	}

	// walk up the node paths until a difference is found
	for receivedIndex >= 0 && result == nil {
		localNode := localProofNodes[localIndex]
		receivedNode := receivedProofNodes[receivedIndex]
		// the two nodes have the same key
		if localNode.KeyPath.Equal(receivedNode.KeyPath) {
			startingChildIndex := byte(0)
			if localNode.KeyPath.NibbleLength < startKeyPath.NibbleLength {
				startingChildIndex = startKeyPath.NibbleVal(localNode.KeyPath.NibbleLength) + 1
			}
			// the two nodes have the same path, so ensure that all children have matching ids
			for childIndex := startingChildIndex; childIndex < 16; childIndex++ {
				receivedChildID, receiveOk := receivedNode.Children[childIndex]
				localChildID, localOk := localNode.Children[childIndex]
				// if they both don't have a child or have matching children, continue
				if (receiveOk || localOk) && receivedChildID != localChildID {
					result = localNode.KeyPath.AppendNibble(childIndex).Value
					break
				}
			}
			if result != nil {
				break
			}
			// only want to move both indexes when they have equal keys
			localIndex--
			receivedIndex--
			continue
		}

		var branchNode merkledb.ProofNode

		if receivedNode.KeyPath.NibbleLength > localNode.KeyPath.NibbleLength {
			// the received proof has an extra node due to a branch that is not present locally
			branchNode = receivedNode
			receivedIndex--
		} else {
			// the local proof has an extra node due to a branch that was not present in the received proof
			branchNode = localNode
			localIndex--
		}

		// the two nodes have different paths, so find where they branched
		for nextKeyNibble := startKeyPath.NibbleVal(branchNode.KeyPath.NibbleLength) + 1; nextKeyNibble < 16; nextKeyNibble++ {
			if _, ok := branchNode.Children[nextKeyNibble]; ok {
				result = branchNode.KeyPath.AppendNibble(nextKeyNibble).Value
				break
			}
		}
	}

	if result == nil || (len(end) > 0 && bytes.Compare(result, end) >= 0) {
		return nil, nil
	}

	return result, nil
}

func (m *StateSyncManager) Error() error {
	m.errLock.Lock()
	defer m.errLock.Unlock()

	return m.fatalError
}

// Blocks until either:
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
// Assumes [m.workLock] is held.
func (m *StateSyncManager) completeWorkItem(ctx context.Context, workItem *syncWorkItem, largestHandledKey []byte, rootID ids.ID, proofOfLargestKey []merkledb.ProofNode) {
	// if the last key is equal to the end, then the full range is completed
	if !bytes.Equal(largestHandledKey, workItem.end) {
		// find the next key to start querying by comparing the proofs for the last completed key
		nextStartKey, err := m.findNextKey(ctx, largestHandledKey, workItem.end, proofOfLargestKey)
		if err != nil {
			m.setError(err)
			return
		}

		largestHandledKey = workItem.end

		// nextStartKey being nil indicates that the entire range has been completed
		if nextStartKey != nil {
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
		m.processedWork.MergeInsert(newWorkItem(rootID, workItem.start, largestHandledKey, workItem.priority))
	} else {
		// the root has changed, so reinsert with high priority
		m.enqueueWork(newWorkItem(rootID, workItem.start, largestHandledKey, highPriority))
	}
}

// Queue the given key range to be fetched and applied.
// If there are sufficiently few unprocessed/processing work items,
// splits the range into two items and queues them both.
// Assumes [m.workLock] is held.
func (m *StateSyncManager) enqueueWork(item *syncWorkItem) {
	defer m.unprocessedWorkCond.Signal()

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
			for index >= 0 {
				if midpoint[index] != 255 {
					midpoint[index]++
					break
				}

				midpoint[index] = 0
				index--
			}
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
