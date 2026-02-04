// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database/merkle/sync/protoutils"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	DefaultRequestKeyLimit       = MaxKeyValuesLimit
	DefaultRequestByteSizeLimit  = maxByteSizeLimit
	defaultSimultaneousWorkLimit = 8
	initialRetryWait             = 10 * time.Millisecond
	maxRetryWait                 = time.Second
	retryWaitFactor              = 1.5 // Larger --> timeout grows more quickly
)

var (
	ErrAlreadyStarted                 = errors.New("cannot start a Syncer that has already been started")
	ErrAlreadyClosed                  = errors.New("Syncer is closed")
	ErrNoRangeProofMarshalerProvided  = errors.New("range proof marshaler is a required field of the sync config")
	ErrNoChangeProofMarshalerProvided = errors.New("change proof marshaler is a required field of the sync config")
	ErrNoRangeProofClientProvided     = errors.New("range proof client is a required field of the sync config")
	ErrNoChangeProofClientProvided    = errors.New("change proof client is a required field of the sync config")
	ErrNoDatabaseProvided             = errors.New("sync database is a required field of the sync config")
	ErrFinishedWithUnexpectedRoot     = errors.New("finished syncing with an unexpected root")
	errInvalidRangeProof              = errors.New("failed to verify range proof")
	errInvalidChangeProof             = errors.New("failed to verify change proof")
	errTooManyBytes                   = errors.New("response contains more than requested bytes")
	errUnexpectedChangeProofResponse  = errors.New("unexpected response type")
)

type priority byte

// Note that [highPriority] > [medPriority] > [lowPriority].
const (
	lowPriority priority = iota + 1
	medPriority
	highPriority
	retryPriority
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
	attempt     int
	queueTime   time.Time
}

func (w *workItem) requestFailed() {
	attempt := w.attempt + 1

	// Overflow check
	if attempt > w.attempt {
		w.attempt = attempt
	}
}

func newWorkItem(localRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], priority priority, queueTime time.Time) *workItem {
	return &workItem{
		localRootID: localRootID,
		start:       start,
		end:         end,
		priority:    priority,
		queueTime:   queueTime,
	}
}

type Syncer[R any, C any] struct {
	// The database to sync.
	db DB[R, C]

	target     ids.ID
	targetLock sync.RWMutex

	config Config

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

	stateSyncNodeIdx uint32
	metrics          SyncMetrics

	rangeProofClient     *p2p.Client
	changeProofClient    *p2p.Client
	rangeProofMarshaler  Marshaler[R]
	changeProofMarshaler Marshaler[C]
}

type Config struct {
	Registerer            prometheus.Registerer
	SimultaneousWorkLimit int // defaults to 8
	Log                   logging.Logger
	EmptyRoot             ids.ID
	StateSyncNodes        []ids.NodeID
}

// NewSyncer returns a Syncer that syncs to the trie to root `target`.
// All values in [Config] are optional.
func NewSyncer[R any, C any](
	db DB[R, C],
	target ids.ID,
	config Config,
	rangeProofClient, changeProofClient *p2p.Client,
	rangeProofMarshaler Marshaler[R], changeProofMarshaler Marshaler[C],
) (*Syncer[R, C], error) {
	switch {
	case db == nil:
		return nil, ErrNoDatabaseProvided
	case rangeProofMarshaler == nil:
		return nil, ErrNoRangeProofMarshalerProvided
	case changeProofMarshaler == nil:
		return nil, ErrNoChangeProofMarshalerProvided
	case rangeProofClient == nil:
		return nil, ErrNoRangeProofClientProvided
	case changeProofClient == nil:
		return nil, ErrNoChangeProofClientProvided
	case config.Log == nil:
		config.Log = logging.NoLog{}
	case config.SimultaneousWorkLimit == 0:
		config.SimultaneousWorkLimit = defaultSimultaneousWorkLimit
	}

	metrics, err := NewMetrics("sync", config.Registerer)
	if err != nil {
		return nil, err
	}

	s := &Syncer[R, C]{
		db:                   db,
		target:               target,
		config:               config,
		doneChan:             make(chan struct{}),
		unprocessedWork:      newWorkHeap(),
		processedWork:        newWorkHeap(),
		metrics:              metrics,
		rangeProofClient:     rangeProofClient,
		changeProofClient:    changeProofClient,
		rangeProofMarshaler:  rangeProofMarshaler,
		changeProofMarshaler: changeProofMarshaler,
	}
	s.unprocessedWorkCond.L = &s.workLock

	return s, nil
}

// Sync initiates the trie syncing process and blocks until one of the following occurs:
//   - [Syncer.Sync] is complete.
//   - [Syncer.Sync] fatally errored.
//   - `ctx` is canceled.
//
// If `ctx` is canceled, returns [context.Context.Err].
func (s *Syncer[_, _]) Sync(ctx context.Context) error {
	ctx, err := s.setup(ctx)
	if err != nil {
		return err
	}

	// Blocks until syncing is done or canceled.
	s.workLoop(ctx)

	// There was a fatal error.
	if err := s.error(); err != nil {
		return err
	}

	root, err := s.db.GetMerkleRoot(ctx)
	if err != nil {
		return err
	}

	if targetRootID := s.getTargetRoot(); targetRootID != root {
		// This should never happen.
		return fmt.Errorf("%w: expected %s, got %s", ErrFinishedWithUnexpectedRoot, targetRootID, root)
	}

	s.config.Log.Info("completed", zap.Stringer("root", root))
	return nil
}

// setup initiates the work queue and enables cancellation through a new context.
func (s *Syncer[_, _]) setup(ctx context.Context) (context.Context, error) {
	s.workLock.Lock()
	defer s.workLock.Unlock()

	if s.syncing {
		return ctx, ErrAlreadyStarted
	}

	s.config.Log.Info("starting sync", zap.Stringer("target root", s.target))

	// Add work item to fetch the entire key range.
	// Note that this will be the first work item to be processed.
	s.unprocessedWork.Insert(newWorkItem(ids.Empty, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), lowPriority, time.Now()))

	s.syncing = true
	ctx, s.cancelCtx = context.WithCancel(ctx)
	return ctx, nil
}

// workLoop awaits signal on [s.unprocessedWorkCond], which indicates that there
// is work to do or syncing completes.  If there is work, workLoop will dispatch a goroutine to do
// the work.
// Assumes [s.workLock] is not held.
func (s *Syncer[_, _]) workLoop(ctx context.Context) {
	defer func() {
		// Invariant: [s.workLock] is held when this goroutine begins.
		s.close()
		s.workLock.Unlock()
	}()

	// Keep doing work until we're closed, done or [ctx] is canceled.
	s.workLock.Lock()
	for {
		// Invariant: [s.workLock] is held here.
		switch {
		case ctx.Err() != nil:
			s.setError(ctx.Err())
			return // [s.workLock] released by defer.
		case s.processingWorkItems >= s.config.SimultaneousWorkLimit:
			// We're already processing the maximum number of work items.
			// Wait until one of them finishes.
			s.unprocessedWorkCond.Wait()
		case s.unprocessedWork.Len() == 0:
			if s.processingWorkItems == 0 {
				// There's no work to do, and there are no work items being processed
				// which could cause work to be added, so we're done.
				return // [s.workLock] released by defer.
			}
			// There's no work to do.
			// Note that if [s].Close() is called, or [ctx] is canceled,
			// Close() will be called, which will broadcast on [s.unprocessedWorkCond],
			// which will cause Wait() to return, and this goroutine to exit.
			s.unprocessedWorkCond.Wait()
		default:
			s.processingWorkItems++
			work := s.unprocessedWork.GetWork()
			go s.doWork(ctx, work)
		}
	}
}

// close is called when there is a fatal error or sync is complete.
// [workLock] must be held
func (s *Syncer[_, _]) close() {
	s.closeOnce.Do(func() {
		s.cancelCtx()

		// ensure any goroutines waiting for work from the heaps gets released
		s.unprocessedWork.Close()
		s.unprocessedWorkCond.Signal()
		s.processedWork.Close()

		// signal all code waiting on the sync to complete
		close(s.doneChan)
	})
}

func (s *Syncer[_, _]) finishWorkItem() {
	s.workLock.Lock()
	defer s.workLock.Unlock()

	s.processingWorkItems--
	s.unprocessedWorkCond.Signal()
}

// Processes [item] by fetching a change or range proof.
func (s *Syncer[_, _]) doWork(ctx context.Context, work *workItem) {
	// Backoff for failed requests accounting for time this job has already
	// spent waiting in the unprocessed queue
	now := time.Now()
	waitTime := max(0, calculateBackoff(work.attempt)-now.Sub(work.queueTime))

	// Check if we can start this work item before the context deadline
	deadline, ok := ctx.Deadline()
	if ok && now.Add(waitTime).After(deadline) {
		s.finishWorkItem()
		return
	}

	select {
	case <-ctx.Done():
		s.finishWorkItem()
		return
	case <-time.After(waitTime):
	}

	if work.localRootID == ids.Empty {
		// the keys in this range have not been downloaded, so get all key/values
		s.requestRangeProof(ctx, work)
	} else {
		// the keys in this range have already been downloaded, but the root changed, so get all changes
		s.requestChangeProof(ctx, work)
	}
}

// Fetch and apply the change proof given by [work].
// Assumes [s.workLock] is not held.
func (s *Syncer[_, _]) requestChangeProof(ctx context.Context, work *workItem) {
	targetRootID := s.getTargetRoot()

	if work.localRootID == targetRootID {
		// Start root is the same as the end root, so we're done.
		s.completeWorkItem(work, work.end, targetRootID)
		s.finishWorkItem()
		return
	}

	if targetRootID == s.config.EmptyRoot {
		defer s.finishWorkItem()

		// The trie is empty after this change.
		// Delete all the key-value pairs in the range.
		if err := s.db.Clear(); err != nil {
			s.setError(err)
			return
		}
		work.start = maybe.Nothing[[]byte]()
		s.completeWorkItem(work, maybe.Nothing[[]byte](), targetRootID)
		return
	}

	request := &pb.GetChangeProofRequest{
		StartRootHash: work.localRootID[:],
		EndRootHash:   targetRootID[:],
		StartKey:      protoutils.MaybeToProto(work.start),
		EndKey:        protoutils.MaybeToProto(work.end),
		KeyLimit:      DefaultRequestKeyLimit,
		BytesLimit:    DefaultRequestByteSizeLimit,
	}

	requestBytes, err := proto.Marshal(request)
	if err != nil {
		s.finishWorkItem()
		s.setError(err)
		return
	}

	onResponse := func(ctx context.Context, _ ids.NodeID, responseBytes []byte, err error) {
		defer s.finishWorkItem()

		if err := s.handleChangeProofResponse(ctx, targetRootID, work, request, responseBytes, err); err != nil {
			// TODO log responses
			s.config.Log.Debug("dropping response", zap.Error(err), zap.Stringer("request", request))
			s.retryWork(work)
			return
		}
	}

	if err := s.sendRequest(ctx, s.changeProofClient, requestBytes, onResponse); err != nil {
		s.finishWorkItem()
		s.setError(err)
		return
	}

	s.metrics.RequestMade()
}

// Fetch and apply the range proof given by [work].
// Assumes [s.workLock] is not held.
func (s *Syncer[_, _]) requestRangeProof(ctx context.Context, work *workItem) {
	targetRootID := s.getTargetRoot()

	if targetRootID == s.config.EmptyRoot {
		defer s.finishWorkItem()

		if err := s.db.Clear(); err != nil {
			s.setError(err)
			return
		}
		work.start = maybe.Nothing[[]byte]()
		s.completeWorkItem(work, maybe.Nothing[[]byte](), targetRootID)
		return
	}

	request := &pb.GetRangeProofRequest{
		RootHash:   targetRootID[:],
		StartKey:   protoutils.MaybeToProto(work.start),
		EndKey:     protoutils.MaybeToProto(work.end),
		KeyLimit:   DefaultRequestKeyLimit,
		BytesLimit: DefaultRequestByteSizeLimit,
	}

	requestBytes, err := proto.Marshal(request)
	if err != nil {
		s.finishWorkItem()
		s.setError(err)
		return
	}

	onResponse := func(ctx context.Context, _ ids.NodeID, responseBytes []byte, appErr error) {
		defer s.finishWorkItem()

		if err := s.handleRangeProofResponse(ctx, targetRootID, work, request, responseBytes, appErr); err != nil {
			// TODO log responses
			s.config.Log.Debug("dropping response", zap.Error(err), zap.Stringer("request", request))
			s.retryWork(work)
			return
		}
	}

	if err := s.sendRequest(ctx, s.rangeProofClient, requestBytes, onResponse); err != nil {
		s.finishWorkItem()
		s.setError(err)
		return
	}

	s.metrics.RequestMade()
}

func (s *Syncer[_, _]) sendRequest(
	ctx context.Context,
	client *p2p.Client,
	requestBytes []byte,
	onResponse p2p.AppResponseCallback,
) error {
	if len(s.config.StateSyncNodes) == 0 {
		return client.AppRequestAny(ctx, requestBytes, onResponse)
	}

	// Get the next nodeID to query using the [nodeIdx] offset.
	// If we're out of nodes, loop back to 0.
	// We do this try to query a different node each time if possible.
	nodeIdx := atomic.AddUint32(&s.stateSyncNodeIdx, 1)
	nodeID := s.config.StateSyncNodes[nodeIdx%uint32(len(s.config.StateSyncNodes))]
	return client.AppRequest(ctx, set.Of(nodeID), requestBytes, onResponse)
}

func (s *Syncer[_, _]) retryWork(work *workItem) {
	work.priority = retryPriority
	work.queueTime = time.Now()
	work.requestFailed()

	s.workLock.Lock()
	s.unprocessedWork.Insert(work)
	s.workLock.Unlock()
	s.unprocessedWorkCond.Signal()
}

// Returns an error if we should drop the response
func (s *Syncer[_, _]) shouldHandleResponse(
	bytesLimit uint32,
	responseBytes []byte,
	err error,
) error {
	if err != nil {
		s.metrics.RequestFailed()
		return err
	}

	s.metrics.RequestSucceeded()

	// TODO can we remove this?
	select {
	case <-s.doneChan:
		// If we're closed, don't apply the proof.
		return ErrAlreadyClosed
	default:
	}

	if len(responseBytes) > int(bytesLimit) {
		return fmt.Errorf("%w: (%d) > %d)", errTooManyBytes, len(responseBytes), bytesLimit)
	}

	return nil
}

func (s *Syncer[R, _]) handleRangeProofResponse(
	ctx context.Context,
	targetRootID ids.ID,
	work *workItem,
	request *pb.GetRangeProofRequest,
	responseBytes []byte,
	err error,
) error {
	if err := s.shouldHandleResponse(request.BytesLimit, responseBytes, err); err != nil {
		return err
	}

	rangeProof, err := s.rangeProofMarshaler.Unmarshal(responseBytes)
	if err != nil {
		return err
	}

	root, err := ids.ToID(request.RootHash)
	if err != nil {
		return err
	}

	if err := s.db.VerifyRangeProof(
		ctx,
		rangeProof,
		protoutils.ProtoToMaybe(request.StartKey),
		protoutils.ProtoToMaybe(request.EndKey),
		root,
		int(request.KeyLimit),
	); err != nil {
		return fmt.Errorf("%w: %w", errInvalidRangeProof, err)
	}

	// Replace all the key-value pairs in the DB from start to end with values from the response.
	nextKey, err := s.db.CommitRangeProof(ctx, work.start, work.end, rangeProof)
	if err != nil {
		s.setError(err)
		return nil
	}

	s.completeWorkItem(work, nextKey, targetRootID)
	return nil
}

func (s *Syncer[R, C]) handleChangeProofResponse(
	ctx context.Context,
	targetRootID ids.ID,
	work *workItem,
	request *pb.GetChangeProofRequest,
	responseBytes []byte,
	err error,
) error {
	if err := s.shouldHandleResponse(request.BytesLimit, responseBytes, err); err != nil {
		return err
	}

	var changeProofResp pb.GetChangeProofResponse
	if err := proto.Unmarshal(responseBytes, &changeProofResp); err != nil {
		return err
	}

	startKey := protoutils.ProtoToMaybe(request.StartKey)
	endKey := protoutils.ProtoToMaybe(request.EndKey)
	endRoot, err := ids.ToID(request.EndRootHash)
	if err != nil {
		return err
	}

	switch changeProofResp := changeProofResp.Response.(type) {
	case *pb.GetChangeProofResponse_ChangeProof:
		// The server had enough history to send us a change proof
		changeProof, err := s.changeProofMarshaler.Unmarshal(changeProofResp.ChangeProof)
		if err != nil {
			return err
		}
		if err := s.db.VerifyChangeProof(
			ctx,
			changeProof,
			startKey,
			endKey,
			endRoot,
			int(request.KeyLimit),
		); err != nil {
			return fmt.Errorf("%w due to %w", errInvalidChangeProof, err)
		}

		// if the proof wasn't empty, apply changes to the sync DB
		nextKey, err := s.db.CommitChangeProof(ctx, endKey, changeProof)
		if err != nil {
			s.setError(err)
			return nil
		}

		s.completeWorkItem(work, nextKey, targetRootID)
	case *pb.GetChangeProofResponse_RangeProof:
		rangeProof, err := s.rangeProofMarshaler.Unmarshal(changeProofResp.RangeProof)
		if err != nil {
			return err
		}

		// The server did not have enough history to send us a change proof
		// so they sent a range proof instead.
		if err := s.db.VerifyRangeProof(
			ctx,
			rangeProof,
			startKey,
			endKey,
			endRoot,
			int(request.KeyLimit),
		); err != nil {
			return err
		}

		// Add all the key-value pairs we got to the database.
		nextKey, err := s.db.CommitRangeProof(ctx, work.start, work.end, rangeProof)
		if err != nil {
			s.setError(err)
			return nil
		}

		s.completeWorkItem(work, nextKey, targetRootID)
	default:
		return fmt.Errorf(
			"%w: %T",
			errUnexpectedChangeProofResponse, changeProofResp,
		)
	}

	return nil
}

func (s *Syncer[_, _]) error() error {
	s.errLock.Lock()
	defer s.errLock.Unlock()

	return s.fatalError
}

func (s *Syncer[_, _]) UpdateSyncTarget(syncTargetRoot ids.ID) error {
	s.targetLock.Lock()
	defer s.targetLock.Unlock()

	s.workLock.Lock()
	defer s.workLock.Unlock()

	select {
	case <-s.doneChan:
		return ErrAlreadyClosed
	default:
	}

	if s.target == syncTargetRoot {
		// the target hasn't changed, so there is nothing to do
		return nil
	}

	s.config.Log.Debug("updated sync target", zap.Stringer("target", syncTargetRoot))
	s.target = syncTargetRoot

	// move all completed ranges into the work heap with high priority
	shouldSignal := s.processedWork.Len() > 0
	for s.processedWork.Len() > 0 {
		// Note that [s.processedWork].Close() hasn't
		// been called because we have [s.workLock]
		// and we checked that [s.closed] is false.
		currentItem := s.processedWork.GetWork()
		currentItem.priority = highPriority
		s.unprocessedWork.Insert(currentItem)
	}
	if shouldSignal {
		// Only signal once because we only have 1 goroutine
		// waiting on [s.unprocessedWorkCond].
		s.unprocessedWorkCond.Signal()
	}
	return nil
}

func (s *Syncer[_, _]) getTargetRoot() ids.ID {
	s.targetLock.RLock()
	defer s.targetLock.RUnlock()

	return s.target
}

// Record that there was a fatal error and begin shutting down.
func (s *Syncer[_, _]) setError(err error) {
	s.errLock.Lock()
	defer s.errLock.Unlock()

	s.config.Log.Error("sync errored", zap.Error(err))
	s.fatalError = err
	// Call in goroutine because we might be holding [s.workLock]
	go func() {
		s.workLock.Lock()
		defer s.workLock.Unlock()
		s.close()
	}()
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
// Assumes [s.workLock] is not held.
func (s *Syncer[_, _]) completeWorkItem(
	work *workItem,
	largestHandledKey maybe.Maybe[[]byte],
	rootID ids.ID,
) {
	// largestHandledKey being Nothing indicates that the entire range has been completed
	if largestHandledKey.IsNothing() {
		largestHandledKey = work.end
	} else {
		// the full range wasn't completed, so enqueue a new work item for the range [nextStartKey, workItem.end]
		s.enqueueWork(newWorkItem(work.localRootID, largestHandledKey, work.end, work.priority, time.Now()))
	}

	// Process [work] while holding [syncTargetLock] to ensure that object
	// is added to the right queue, even if a target update is triggered
	s.targetLock.RLock()
	defer s.targetLock.RUnlock()

	stale := s.target != rootID
	if stale {
		// the root has changed, so reinsert with high priority
		s.enqueueWork(newWorkItem(rootID, work.start, largestHandledKey, highPriority, time.Now()))
	} else {
		s.workLock.Lock()
		defer s.workLock.Unlock()

		s.processedWork.MergeInsert(newWorkItem(rootID, work.start, largestHandledKey, work.priority, time.Now()))
	}

	// completed the range [work.start, lastKey], log and record in the completed work heap
	s.config.Log.Debug("completed range",
		zap.Stringer("start", work.start),
		zap.Stringer("end", largestHandledKey),
		zap.Stringer("rootID", rootID),
		zap.Bool("stale", stale),
	)
}

// Queue the given key range to be fetched and applied.
// If there are sufficiently few unprocessed/processing work items,
// splits the range into two items and queues them both.
// Assumes [s.workLock] is not held.
func (s *Syncer[_, _]) enqueueWork(work *workItem) {
	s.workLock.Lock()
	defer func() {
		s.workLock.Unlock()
		s.unprocessedWorkCond.Signal()
	}()

	if s.processingWorkItems+s.unprocessedWork.Len() > 2*s.config.SimultaneousWorkLimit {
		// There are too many work items already, don't split the range
		s.unprocessedWork.Insert(work)
		return
	}

	// Split the remaining range into to 2.
	// Find the middle point.
	mid := midPoint(work.start, work.end)

	if maybe.Equal(work.start, mid, bytes.Equal) || maybe.Equal(mid, work.end, bytes.Equal) {
		// The range is too small to split.
		// If we didn't have this check we would add work items
		// [start, start] and [start, end]. Since start <= end, this would
		// violate the invariant of [s.unprocessedWork] and [s.processedWork]
		// that there are no overlapping ranges.
		s.unprocessedWork.Insert(work)
		return
	}

	// first item gets higher priority than the second to encourage finished ranges to grow
	// rather than start a new range that is not contiguous with existing completed ranges
	first := newWorkItem(work.localRootID, work.start, mid, medPriority, time.Now())
	second := newWorkItem(work.localRootID, mid, work.end, lowPriority, time.Now())

	s.unprocessedWork.Insert(first)
	s.unprocessedWork.Insert(second)
}

// find the midpoint between two keys
// start is expected to be less than end
// Nothing/nil [start] is treated as all 0's
// Nothing/nil [end] is treated as all 255's
func midPoint(startMaybe, endMaybe maybe.Maybe[[]byte]) maybe.Maybe[[]byte] {
	start := startMaybe.Value()
	end := endMaybe.Value()
	length := max(len(end), len(start))

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

func calculateBackoff(attempt int) time.Duration {
	if attempt == 0 {
		return 0
	}

	return min(
		initialRetryWait*time.Duration(math.Pow(retryWaitFactor, float64(attempt))),
		maxRetryWait,
	)
}
