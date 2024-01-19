// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/exp/maps"

	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	defaultRequestKeyLimit      = maxKeyValuesLimit
	defaultRequestByteSizeLimit = maxByteSizeLimit
)

var (
	ErrAlreadyStarted             = errors.New("cannot start a Manager that has already been started")
	ErrAlreadyClosed              = errors.New("Manager is closed")
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
// [localRootID] is the ID of the root of this range in our database.
// If we have no local root for this range, [localRootID] is ids.Empty.
type workItem struct {
	start       maybe.Maybe[[]byte]
	end         maybe.Maybe[[]byte]
	priority    priority
	localRootID ids.ID
}

func newWorkItem(localRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], priority priority) *workItem {
	return &workItem{
		localRootID: localRootID,
		start:       start,
		end:         end,
		priority:    priority,
	}
}

type Manager struct {
	// Must be held when accessing [config.TargetRoot].
	syncTargetLock sync.RWMutex
	config         ManagerConfig

	workLock sync.Mutex
	// The number of work items currently being processed.
	// Namely, the number of goroutines executing [doWork].
	// [workLock] must be held when accessing [processingWorkItems].
	processingWorkItems int
	// [workLock] must be held while accessing [unprocessedWork].
	unprocessedWork *workHeap
	// Signalled when:
	// - An item is added to [unprocessedWork].
	// - An item is added to [processedWork].
	// - Close() is called.
	// [workLock] is its inner lock.
	unprocessedWorkCond sync.Cond
	// [workLock] must be held while accessing [processedWork].
	processedWork *workHeap

	// When this is closed:
	// - [closed] is true.
	// - [cancelCtx] was called.
	// - [workToBeDone] and [completedWork] are closed.
	doneChan chan struct{}

	errLock sync.Mutex
	// If non-nil, there was a fatal error.
	// [errLock] must be held when accessing [fatalError].
	fatalError error

	// Cancels all currently processing work items.
	cancelCtx context.CancelFunc

	// Set to true when StartSyncing is called.
	syncing   bool
	closeOnce sync.Once
	tokenSize int
}

type ManagerConfig struct {
	DB                    DB
	Client                Client
	SimultaneousWorkLimit int
	Log                   logging.Logger
	TargetRoot            ids.ID
	BranchFactor          merkledb.BranchFactor
}

func NewManager(config ManagerConfig) (*Manager, error) {
	switch {
	case config.Client == nil:
		return nil, ErrNoClientProvided
	case config.DB == nil:
		return nil, ErrNoDatabaseProvided
	case config.Log == nil:
		return nil, ErrNoLogProvided
	case config.SimultaneousWorkLimit == 0:
		return nil, ErrZeroWorkLimit
	}
	if err := config.BranchFactor.Valid(); err != nil {
		return nil, err
	}

	m := &Manager{
		config:          config,
		doneChan:        make(chan struct{}),
		unprocessedWork: newWorkHeap(),
		processedWork:   newWorkHeap(),
		tokenSize:       merkledb.BranchFactorToTokenSize[config.BranchFactor],
	}
	m.unprocessedWorkCond.L = &m.workLock

	return m, nil
}

func (m *Manager) Start(ctx context.Context) error {
	m.workLock.Lock()
	defer m.workLock.Unlock()

	if m.syncing {
		return ErrAlreadyStarted
	}

	m.config.Log.Info("starting sync", zap.Stringer("target root", m.config.TargetRoot))

	// Add work item to fetch the entire key range.
	// Note that this will be the first work item to be processed.
	m.unprocessedWork.Insert(newWorkItem(ids.Empty, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), lowPriority))

	m.syncing = true
	ctx, m.cancelCtx = context.WithCancel(ctx)

	go m.sync(ctx)
	return nil
}

// sync awaits signal on [m.unprocessedWorkCond], which indicates that there
// is work to do or syncing completes.  If there is work, sync will dispatch a goroutine to do
// the work.
func (m *Manager) sync(ctx context.Context) {
	defer func() {
		// Invariant: [m.workLock] is held when this goroutine begins.
		m.close()
		m.workLock.Unlock()
	}()

	// Keep doing work until we're closed, done or [ctx] is canceled.
	m.workLock.Lock()
	for {
		// Invariant: [m.workLock] is held here.
		switch {
		case ctx.Err() != nil:
			return // [m.workLock] released by defer.
		case m.processingWorkItems >= m.config.SimultaneousWorkLimit:
			// We're already processing the maximum number of work items.
			// Wait until one of them finishes.
			m.unprocessedWorkCond.Wait()
		case m.unprocessedWork.Len() == 0:
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
		default:
			m.processingWorkItems++
			work := m.unprocessedWork.GetWork()
			go m.doWork(ctx, work)
		}
	}
}

// Close will stop the syncing process
func (m *Manager) Close() {
	m.workLock.Lock()
	defer m.workLock.Unlock()

	m.close()
}

// close is called when there is a fatal error or sync is complete.
// [workLock] must be held
func (m *Manager) close() {
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
		close(m.doneChan)
	})
}

// Processes [item] by fetching and applying a change or range proof.
// Assumes [m.workLock] is not held.
func (m *Manager) doWork(ctx context.Context, work *workItem) {
	defer func() {
		m.workLock.Lock()
		defer m.workLock.Unlock()

		m.processingWorkItems--
		m.unprocessedWorkCond.Signal()
	}()

	if work.localRootID == ids.Empty {
		// the keys in this range have not been downloaded, so get all key/values
		m.getAndApplyRangeProof(ctx, work)
	} else {
		// the keys in this range have already been downloaded, but the root changed, so get all changes
		m.getAndApplyChangeProof(ctx, work)
	}
}

// Fetch and apply the change proof given by [work].
// Assumes [m.workLock] is not held.
func (m *Manager) getAndApplyChangeProof(ctx context.Context, work *workItem) {
	targetRootID := m.getTargetRoot()

	if work.localRootID == targetRootID {
		// Start root is the same as the end root, so we're done.
		m.completeWorkItem(ctx, work, work.end, targetRootID, nil)
		return
	}

	if targetRootID == ids.Empty {
		// The trie is empty after this change.
		// Delete all the key-value pairs in the range.
		if err := m.config.DB.Clear(); err != nil {
			m.setError(err)
			return
		}
		work.start = maybe.Nothing[[]byte]()
		m.completeWorkItem(ctx, work, maybe.Nothing[[]byte](), targetRootID, nil)
		return
	}

	changeOrRangeProof, err := m.config.Client.GetChangeProof(
		ctx,
		&pb.SyncGetChangeProofRequest{
			StartRootHash: work.localRootID[:],
			EndRootHash:   targetRootID[:],
			StartKey: &pb.MaybeBytes{
				Value:     work.start.Value(),
				IsNothing: work.start.IsNothing(),
			},
			EndKey: &pb.MaybeBytes{
				Value:     work.end.Value(),
				IsNothing: work.end.IsNothing(),
			},
			KeyLimit:   defaultRequestKeyLimit,
			BytesLimit: defaultRequestByteSizeLimit,
		},
		m.config.DB,
	)
	if err != nil {
		m.setError(err)
		return
	}

	select {
	case <-m.doneChan:
		// If we're closed, don't apply the proof.
		return
	default:
	}

	if changeOrRangeProof.ChangeProof != nil {
		// The server had sufficient history to respond with a change proof.
		changeProof := changeOrRangeProof.ChangeProof
		largestHandledKey := work.end
		// if the proof wasn't empty, apply changes to the sync DB
		if len(changeProof.KeyChanges) > 0 {
			if err := m.config.DB.CommitChangeProof(ctx, changeProof); err != nil {
				m.setError(err)
				return
			}
			largestHandledKey = maybe.Some(changeProof.KeyChanges[len(changeProof.KeyChanges)-1].Key)
		}

		m.completeWorkItem(ctx, work, largestHandledKey, targetRootID, changeProof.EndProof)
		return
	}

	// The server responded with a range proof.
	rangeProof := changeOrRangeProof.RangeProof
	largestHandledKey := work.end
	if len(rangeProof.KeyValues) > 0 {
		// Add all the key-value pairs we got to the database.
		if err := m.config.DB.CommitRangeProof(ctx, work.start, work.end, rangeProof); err != nil {
			m.setError(err)
			return
		}
		largestHandledKey = maybe.Some(rangeProof.KeyValues[len(rangeProof.KeyValues)-1].Key)
	}

	m.completeWorkItem(ctx, work, largestHandledKey, targetRootID, rangeProof.EndProof)
}

// Fetch and apply the range proof given by [work].
// Assumes [m.workLock] is not held.
func (m *Manager) getAndApplyRangeProof(ctx context.Context, work *workItem) {
	targetRootID := m.getTargetRoot()

	if targetRootID == ids.Empty {
		if err := m.config.DB.Clear(); err != nil {
			m.setError(err)
			return
		}
		work.start = maybe.Nothing[[]byte]()
		m.completeWorkItem(ctx, work, maybe.Nothing[[]byte](), targetRootID, nil)
		return
	}

	proof, err := m.config.Client.GetRangeProof(ctx,
		&pb.SyncGetRangeProofRequest{
			RootHash: targetRootID[:],
			StartKey: &pb.MaybeBytes{
				Value:     work.start.Value(),
				IsNothing: work.start.IsNothing(),
			},
			EndKey: &pb.MaybeBytes{
				Value:     work.end.Value(),
				IsNothing: work.end.IsNothing(),
			},
			KeyLimit:   defaultRequestKeyLimit,
			BytesLimit: defaultRequestByteSizeLimit,
		},
	)
	if err != nil {
		m.setError(err)
		return
	}

	select {
	case <-m.doneChan:
		// If we're closed, don't apply the proof.
		return
	default:
	}

	largestHandledKey := work.end

	// Replace all the key-value pairs in the DB from start to end with values from the response.
	if err := m.config.DB.CommitRangeProof(ctx, work.start, work.end, proof); err != nil {
		m.setError(err)
		return
	}

	if len(proof.KeyValues) > 0 {
		largestHandledKey = maybe.Some(proof.KeyValues[len(proof.KeyValues)-1].Key)
	}

	m.completeWorkItem(ctx, work, largestHandledKey, targetRootID, proof.EndProof)
}

// findNextKey returns the start of the key range that should be fetched next
// given that we just received a range/change proof that proved a range of
// key-value pairs ending at [lastReceivedKey].
//
// [rangeEnd] is the end of the range that we want to fetch.
//
// Returns Nothing if there are no more keys to fetch in [lastReceivedKey, rangeEnd].
//
// [endProof] is the end proof of the last proof received.
//
// Invariant: [lastReceivedKey] < [rangeEnd].
// If [rangeEnd] is Nothing it's considered > [lastReceivedKey].
func (m *Manager) findNextKey(
	ctx context.Context,
	lastReceivedKey []byte,
	rangeEnd maybe.Maybe[[]byte],
	endProof []merkledb.ProofNode,
) (maybe.Maybe[[]byte], error) {
	if len(endProof) == 0 {
		// We try to find the next key to fetch by looking at the end proof.
		// If the end proof is empty, we have no information to use.
		// Start fetching from the next key after [lastReceivedKey].
		nextKey := lastReceivedKey
		nextKey = append(nextKey, 0)
		return maybe.Some(nextKey), nil
	}

	// We want the first key larger than the [lastReceivedKey].
	// This is done by taking two proofs for the same key
	// (one that was just received as part of a proof, and one from the local db)
	// and traversing them from the longest key to the shortest key.
	// For each node in these proofs, compare if the children of that node exist
	// or have the same ID in the other proof.
	proofKeyPath := merkledb.ToKey(lastReceivedKey)

	// If the received proof is an exclusion proof, the last node may be for a
	// key that is after the [lastReceivedKey].
	// If the last received node's key is after the [lastReceivedKey], it can
	// be removed to obtain a valid proof for a prefix of the [lastReceivedKey].
	if !proofKeyPath.HasPrefix(endProof[len(endProof)-1].Key) {
		endProof = endProof[:len(endProof)-1]
		// update the proofKeyPath to be for the prefix
		proofKeyPath = endProof[len(endProof)-1].Key
	}

	// get a proof for the same key as the received proof from the local db
	localProofOfKey, err := m.config.DB.GetProof(ctx, proofKeyPath.Bytes())
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}
	localProofNodes := localProofOfKey.Path

	// The local proof may also be an exclusion proof with an extra node.
	// Remove this extra node if it exists to get a proof of the same key as the received proof
	if !proofKeyPath.HasPrefix(localProofNodes[len(localProofNodes)-1].Key) {
		localProofNodes = localProofNodes[:len(localProofNodes)-1]
	}

	nextKey := maybe.Nothing[[]byte]()

	// Add sentinel node back into the localProofNodes, if it is missing.
	// Required to ensure that a common node exists in both proofs
	if len(localProofNodes) > 0 && localProofNodes[0].Key.Length() != 0 {
		sentinel := merkledb.ProofNode{
			Children: map[byte]ids.ID{
				localProofNodes[0].Key.Token(0, m.tokenSize): ids.Empty,
			},
		}
		localProofNodes = append([]merkledb.ProofNode{sentinel}, localProofNodes...)
	}

	// Add sentinel node back into the endProof, if it is missing.
	// Required to ensure that a common node exists in both proofs
	if len(endProof) > 0 && endProof[0].Key.Length() != 0 {
		sentinel := merkledb.ProofNode{
			Children: map[byte]ids.ID{
				endProof[0].Key.Token(0, m.tokenSize): ids.Empty,
			},
		}
		endProof = append([]merkledb.ProofNode{sentinel}, endProof...)
	}

	localProofNodeIndex := len(localProofNodes) - 1
	receivedProofNodeIndex := len(endProof) - 1

	// traverse the two proofs from the deepest nodes up to the sentinel node until a difference is found
	for localProofNodeIndex >= 0 && receivedProofNodeIndex >= 0 && nextKey.IsNothing() {
		localProofNode := localProofNodes[localProofNodeIndex]
		receivedProofNode := endProof[receivedProofNodeIndex]

		// [deepestNode] is the proof node with the longest key (deepest in the trie) in the
		// two proofs that hasn't been handled yet.
		// [deepestNodeFromOtherProof] is the proof node from the other proof with
		// the same key/depth if it exists, nil otherwise.
		var deepestNode, deepestNodeFromOtherProof *merkledb.ProofNode

		// select the deepest proof node from the two proofs
		switch {
		case receivedProofNode.Key.Length() > localProofNode.Key.Length():
			// there was a branch node in the received proof that isn't in the local proof
			// see if the received proof node has children not present in the local proof
			deepestNode = &receivedProofNode

			// we have dealt with this received node, so move on to the next received node
			receivedProofNodeIndex--

		case localProofNode.Key.Length() > receivedProofNode.Key.Length():
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
		// so only the next token of the proof key needs to be considered.

		// If the deepest node has the same key as [proofKeyPath],
		// then all of its children have keys greater than the proof key,
		// so we can start at the 0 token.
		startingChildToken := 0

		// If the deepest node has a key shorter than the key being proven,
		// we can look at the next token index of the proof key to determine which of that
		// node's children have keys larger than [proofKeyPath].
		// Any child with a token greater than the [proofKeyPath]'s token at that
		// index will have a larger key.
		if deepestNode.Key.Length() < proofKeyPath.Length() {
			startingChildToken = int(proofKeyPath.Token(deepestNode.Key.Length(), m.tokenSize)) + 1
		}

		// determine if there are any differences in the children for the deepest unhandled node of the two proofs
		if childIndex, hasDifference := findChildDifference(deepestNode, deepestNodeFromOtherProof, startingChildToken); hasDifference {
			nextKey = maybe.Some(deepestNode.Key.Extend(merkledb.ToToken(childIndex, m.tokenSize)).Bytes())
			break
		}
	}

	// If the nextKey is before or equal to the [lastReceivedKey]
	// then we couldn't find a better answer than the [lastReceivedKey].
	// Set the nextKey to [lastReceivedKey] + 0, which is the first key in
	// the open range (lastReceivedKey, rangeEnd).
	if nextKey.HasValue() && bytes.Compare(nextKey.Value(), lastReceivedKey) <= 0 {
		nextKeyVal := slices.Clone(lastReceivedKey)
		nextKeyVal = append(nextKeyVal, 0)
		nextKey = maybe.Some(nextKeyVal)
	}

	// If the [nextKey] is larger than the end of the range, return Nothing to signal that there is no next key in range
	if rangeEnd.HasValue() && bytes.Compare(nextKey.Value(), rangeEnd.Value()) >= 0 {
		return maybe.Nothing[[]byte](), nil
	}

	// the nextKey is within the open range (lastReceivedKey, rangeEnd), so return it
	return nextKey, nil
}

func (m *Manager) Error() error {
	m.errLock.Lock()
	defer m.errLock.Unlock()

	return m.fatalError
}

// Wait blocks until one of the following occurs:
// - sync is complete.
// - sync fatally errored.
// - [ctx] is canceled.
// If [ctx] is canceled, returns [ctx].Err().
func (m *Manager) Wait(ctx context.Context) error {
	select {
	case <-m.doneChan:
	case <-ctx.Done():
		return ctx.Err()
	}

	// There was a fatal error.
	if err := m.Error(); err != nil {
		return err
	}

	root, err := m.config.DB.GetMerkleRoot(ctx)
	if err != nil {
		return err
	}

	if targetRootID := m.getTargetRoot(); targetRootID != root {
		// This should never happen.
		return fmt.Errorf("%w: expected %s, got %s", ErrFinishedWithUnexpectedRoot, targetRootID, root)
	}

	m.config.Log.Info("completed", zap.Stringer("root", root))
	return nil
}

func (m *Manager) UpdateSyncTarget(syncTargetRoot ids.ID) error {
	m.syncTargetLock.Lock()
	defer m.syncTargetLock.Unlock()

	m.workLock.Lock()
	defer m.workLock.Unlock()

	select {
	case <-m.doneChan:
		return ErrAlreadyClosed
	default:
	}

	if m.config.TargetRoot == syncTargetRoot {
		// the target hasn't changed, so there is nothing to do
		return nil
	}

	m.config.Log.Debug("updated sync target", zap.Stringer("target", syncTargetRoot))
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

func (m *Manager) getTargetRoot() ids.ID {
	m.syncTargetLock.RLock()
	defer m.syncTargetLock.RUnlock()

	return m.config.TargetRoot
}

// Record that there was a fatal error and begin shutting down.
func (m *Manager) setError(err error) {
	m.errLock.Lock()
	defer m.errLock.Unlock()

	m.config.Log.Error("sync errored", zap.Error(err))
	m.fatalError = err
	// Call in goroutine because we might be holding [m.workLock]
	// which [m.Close] will try to acquire.
	go m.Close()
}

// Mark that we've fetched all the key-value pairs in the range
// [workItem.start, largestHandledKey] for the trie with root [rootID].
//
// If [workItem.start] is Nothing, then we've fetched all the key-value
// pairs up to and including [largestHandledKey].
//
// If [largestHandledKey] is Nothing, then we've fetched all the key-value
// pairs at and after [workItem.start].
//
// [proofOfLargestKey] is the end proof for the range/change proof
// that gave us the range up to and including [largestHandledKey].
//
// Assumes [m.workLock] is not held.
func (m *Manager) completeWorkItem(ctx context.Context, work *workItem, largestHandledKey maybe.Maybe[[]byte], rootID ids.ID, proofOfLargestKey []merkledb.ProofNode) {
	if !maybe.Equal(largestHandledKey, work.end, bytes.Equal) {
		// The largest handled key isn't equal to the end of the work item.
		// Find the start of the next key range to fetch.
		// Note that [largestHandledKey] can't be Nothing.
		// Proof: Suppose it is. That means that we got a range/change proof that proved up to the
		// greatest key-value pair in the database. That means we requested a proof with no upper
		// bound. That is, [workItem.end] is Nothing. Since we're here, [bothNothing] is false,
		// which means [workItem.end] isn't Nothing. Contradiction.
		nextStartKey, err := m.findNextKey(ctx, largestHandledKey.Value(), work.end, proofOfLargestKey)
		if err != nil {
			m.setError(err)
			return
		}

		// nextStartKey being Nothing indicates that the entire range has been completed
		if nextStartKey.IsNothing() {
			largestHandledKey = work.end
		} else {
			// the full range wasn't completed, so enqueue a new work item for the range [nextStartKey, workItem.end]
			m.enqueueWork(newWorkItem(work.localRootID, nextStartKey, work.end, work.priority))
			largestHandledKey = nextStartKey
		}
	}

	// Process [work] while holding [syncTargetLock] to ensure that object
	// is added to the right queue, even if a target update is triggered
	m.syncTargetLock.RLock()
	defer m.syncTargetLock.RUnlock()

	stale := m.config.TargetRoot != rootID
	if stale {
		// the root has changed, so reinsert with high priority
		m.enqueueWork(newWorkItem(rootID, work.start, largestHandledKey, highPriority))
	} else {
		m.workLock.Lock()
		defer m.workLock.Unlock()

		m.processedWork.MergeInsert(newWorkItem(rootID, work.start, largestHandledKey, work.priority))
	}

	// completed the range [work.start, lastKey], log and record in the completed work heap
	m.config.Log.Debug("completed range",
		zap.Stringer("start", work.start),
		zap.Stringer("end", largestHandledKey),
		zap.Stringer("rootID", rootID),
		zap.Bool("stale", stale),
	)
}

// Queue the given key range to be fetched and applied.
// If there are sufficiently few unprocessed/processing work items,
// splits the range into two items and queues them both.
// Assumes [m.workLock] is not held.
func (m *Manager) enqueueWork(work *workItem) {
	m.workLock.Lock()
	defer func() {
		m.workLock.Unlock()
		m.unprocessedWorkCond.Signal()
	}()

	if m.processingWorkItems+m.unprocessedWork.Len() > 2*m.config.SimultaneousWorkLimit {
		// There are too many work items already, don't split the range
		m.unprocessedWork.Insert(work)
		return
	}

	// Split the remaining range into to 2.
	// Find the middle point.
	mid := midPoint(work.start, work.end)

	if maybe.Equal(work.start, mid, bytes.Equal) || maybe.Equal(mid, work.end, bytes.Equal) {
		// The range is too small to split.
		// If we didn't have this check we would add work items
		// [start, start] and [start, end]. Since start <= end, this would
		// violate the invariant of [m.unprocessedWork] and [m.processedWork]
		// that there are no overlapping ranges.
		m.unprocessedWork.Insert(work)
		return
	}

	// first item gets higher priority than the second to encourage finished ranges to grow
	// rather than start a new range that is not contiguous with existing completed ranges
	first := newWorkItem(work.localRootID, work.start, mid, medPriority)
	second := newWorkItem(work.localRootID, mid, work.end, lowPriority)

	m.unprocessedWork.Insert(first)
	m.unprocessedWork.Insert(second)
}

// find the midpoint between two keys
// start is expected to be less than end
// Nothing/nil [start] is treated as all 0's
// Nothing/nil [end] is treated as all 255's
func midPoint(startMaybe, endMaybe maybe.Maybe[[]byte]) maybe.Maybe[[]byte] {
	start := startMaybe.Value()
	end := endMaybe.Value()
	length := len(start)
	if len(end) > length {
		length = len(end)
	}

	if length == 0 {
		if endMaybe.IsNothing() {
			return maybe.Some([]byte{127})
		} else if len(end) == 0 {
			return maybe.Nothing[[]byte]()
		}
	}

	// This check deals with cases where the end has a 255(or is nothing which is treated as all 255s) and the start key ends 255.
	// For example, midPoint([255], nothing) should be [255, 127], not [255].
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
		if endMaybe.IsNothing() {
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
	return maybe.Some(midpoint)
}

// findChildDifference returns the first child index that is different between node 1 and node 2 if one exists and
// a bool indicating if any difference was found
func findChildDifference(node1, node2 *merkledb.ProofNode, startIndex int) (byte, bool) {
	// Children indices >= [startIndex] present in at least one of the nodes.
	childIndices := set.Set[byte]{}
	for _, node := range []*merkledb.ProofNode{node1, node2} {
		if node == nil {
			continue
		}
		for key := range node.Children {
			if int(key) >= startIndex {
				childIndices.Add(key)
			}
		}
	}

	sortedChildIndices := maps.Keys(childIndices)
	slices.Sort(sortedChildIndices)
	var (
		child1, child2 ids.ID
		ok1, ok2       bool
	)
	for _, childIndex := range sortedChildIndices {
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
