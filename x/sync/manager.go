// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	defaultRequestKeyLimit      = maxKeyValuesLimit
	defaultRequestByteSizeLimit = maxByteSizeLimit
	initialRetryWait            = 10 * time.Millisecond
	maxRetryWait                = time.Second
	retryWaitFactor             = 1.5 // Larger --> timeout grows more quickly
)

var (
	_ Proof       = (*rangeProof)(nil)
	_ Proof       = (*changeProof)(nil)
	_ ProofParser = (*proofParser)(nil)

	ErrAlreadyStarted                = errors.New("cannot start a Manager that has already been started")
	ErrAlreadyClosed                 = errors.New("Manager is closed")
	ErrNoRangeProofClientProvided    = errors.New("range proof client is a required field of the sync config")
	ErrNoChangeProofClientProvided   = errors.New("change proof client is a required field of the sync config")
	ErrNoParserProvided              = errors.New("proof parser is a required field of the sync config")
	ErrNoLogProvided                 = errors.New("log is a required field of the sync config")
	ErrZeroWorkLimit                 = errors.New("simultaneous work limit must be greater than 0")
	errInvalidRangeProof             = errors.New("failed to verify range proof")
	errInvalidChangeProof            = errors.New("failed to verify change proof")
	errTooManyKeys                   = errors.New("response contains more than requested keys")
	errTooManyBytes                  = errors.New("response contains more than requested bytes")
	errUnexpectedChangeProofResponse = errors.New("unexpected response type")
	ErrNoDBProvided                  = errors.New("database is required to create a parser")
)

type priority byte

// Note that [highPriority] > [medPriority] > [lowPriority].
const (
	lowPriority priority = iota + 1
	medPriority
	highPriority
	retryPriority
)

type proofParser struct {
	db        DB
	hasher    merkledb.Hasher
	tokenSize int
}

func newParser(db DB, hasher merkledb.Hasher, branchFactor merkledb.BranchFactor) (*proofParser, error) {
	if db == nil {
		return nil, ErrNoDBProvided
	}
	if err := branchFactor.Valid(); err != nil {
		return nil, err
	}

	if hasher == nil {
		hasher = merkledb.DefaultHasher
	}

	return &proofParser{
		db:        db,
		hasher:    hasher,
		tokenSize: merkledb.BranchFactorToTokenSize[branchFactor],
	}, nil
}

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

	stateSyncNodeIdx uint32
	metrics          SyncMetrics
}

// TODO remove non-config values out of this struct
type ManagerConfig struct {
	Parser                ProofParser
	RangeProofClient      *p2p.Client
	ChangeProofClient     *p2p.Client
	SimultaneousWorkLimit int
	Log                   logging.Logger
	TargetRoot            ids.ID
	StateSyncNodes        []ids.NodeID
}

func NewManager(config ManagerConfig, registerer prometheus.Registerer) (*Manager, error) {
	switch {
	case config.RangeProofClient == nil:
		return nil, ErrNoRangeProofClientProvided
	case config.ChangeProofClient == nil:
		return nil, ErrNoChangeProofClientProvided
	case config.Parser == nil:
		return nil, ErrNoParserProvided
	case config.Log == nil:
		return nil, ErrNoLogProvided
	case config.SimultaneousWorkLimit == 0:
		return nil, ErrZeroWorkLimit
	}

	metrics, err := NewMetrics("sync", registerer)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		config:          config,
		doneChan:        make(chan struct{}),
		unprocessedWork: newWorkHeap(),
		processedWork:   newWorkHeap(),
		metrics:         metrics,
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
	m.unprocessedWork.Insert(newWorkItem(ids.Empty, maybe.Nothing[[]byte](), maybe.Nothing[[]byte](), lowPriority, time.Now()))

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

func (m *Manager) finishWorkItem() {
	m.workLock.Lock()
	defer m.workLock.Unlock()

	m.processingWorkItems--
	m.unprocessedWorkCond.Signal()
}

// Processes [item] by fetching a change or range proof.
func (m *Manager) doWork(ctx context.Context, work *workItem) {
	// Backoff for failed requests accounting for time this job has already
	// spent waiting in the unprocessed queue
	now := time.Now()
	waitTime := max(0, calculateBackoff(work.attempt)-now.Sub(work.queueTime))

	// Check if we can start this work item before the context deadline
	deadline, ok := ctx.Deadline()
	if ok && now.Add(waitTime).After(deadline) {
		m.finishWorkItem()
		return
	}

	select {
	case <-ctx.Done():
		m.finishWorkItem()
		return
	case <-time.After(waitTime):
	}

	if work.localRootID == ids.Empty {
		// the keys in this range have not been downloaded, so get all key/values
		m.requestRangeProof(ctx, work)
	} else {
		// the keys in this range have already been downloaded, but the root changed, so get all changes
		m.requestChangeProof(ctx, work)
	}
}

// Fetch and apply the change proof given by [work].
// Assumes [m.workLock] is not held.
func (m *Manager) requestChangeProof(ctx context.Context, work *workItem) {
	targetRootID := m.getTargetRoot()

	if work.localRootID == targetRootID {
		// Start root is the same as the end root, so we're done.
		m.completeWorkItem(work, work.end, targetRootID)
		m.finishWorkItem()
		return
	}

	request := &pb.SyncGetChangeProofRequest{
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
	}

	requestBytes, err := proto.Marshal(request)
	if err != nil {
		m.finishWorkItem()
		m.setError(err)
		return
	}

	onResponse := func(ctx context.Context, _ ids.NodeID, responseBytes []byte, err error) {
		defer m.finishWorkItem()

		if err := m.handleChangeProofResponse(ctx, targetRootID, work, request, responseBytes, err); err != nil {
			// TODO log responses
			m.config.Log.Debug("dropping response", zap.Error(err), zap.Stringer("request", request))
			m.retryWork(work)
			return
		}
	}

	if err := m.sendRequest(ctx, m.config.ChangeProofClient, requestBytes, onResponse); err != nil {
		m.finishWorkItem()
		m.setError(err)
		return
	}

	m.metrics.RequestMade()
}

// Fetch and apply the range proof given by [work].
// Assumes [m.workLock] is not held.
func (m *Manager) requestRangeProof(ctx context.Context, work *workItem) {
	targetRootID := m.getTargetRoot()

	request := &pb.SyncGetRangeProofRequest{
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
	}

	requestBytes, err := proto.Marshal(request)
	if err != nil {
		m.finishWorkItem()
		m.setError(err)
		return
	}

	onResponse := func(ctx context.Context, _ ids.NodeID, responseBytes []byte, appErr error) {
		defer m.finishWorkItem()

		if err := m.handleRangeProofResponse(ctx, targetRootID, work, request, responseBytes, appErr); err != nil {
			// TODO log responses
			m.config.Log.Debug("dropping response", zap.Error(err), zap.Stringer("request", request))
			m.retryWork(work)
			return
		}
	}

	if err := m.sendRequest(ctx, m.config.RangeProofClient, requestBytes, onResponse); err != nil {
		m.finishWorkItem()
		m.setError(err)
		return
	}

	m.metrics.RequestMade()
}

func (m *Manager) sendRequest(ctx context.Context, client *p2p.Client, requestBytes []byte, onResponse p2p.AppResponseCallback) error {
	if len(m.config.StateSyncNodes) == 0 {
		return client.AppRequestAny(ctx, requestBytes, onResponse)
	}

	// Get the next nodeID to query using the [nodeIdx] offset.
	// If we're out of nodes, loop back to 0.
	// We do this try to query a different node each time if possible.
	nodeIdx := atomic.AddUint32(&m.stateSyncNodeIdx, 1)
	nodeID := m.config.StateSyncNodes[nodeIdx%uint32(len(m.config.StateSyncNodes))]
	return client.AppRequest(ctx, set.Of(nodeID), requestBytes, onResponse)
}

func (m *Manager) retryWork(work *workItem) {
	work.priority = retryPriority
	work.queueTime = time.Now()
	work.requestFailed()

	m.workLock.Lock()
	m.unprocessedWork.Insert(work)
	m.workLock.Unlock()
	m.unprocessedWorkCond.Signal()
}

// Returns an error if we should drop the response
func (m *Manager) shouldHandleResponse(
	bytesLimit uint32,
	responseBytes []byte,
	err error,
) error {
	if err != nil {
		m.metrics.RequestFailed()
		return err
	}

	m.metrics.RequestSucceeded()

	// TODO can we remove this?
	select {
	case <-m.doneChan:
		// If we're closed, don't apply the proof.
		return ErrAlreadyClosed
	default:
	}

	if len(responseBytes) > int(bytesLimit) {
		return fmt.Errorf("%w: (%d) > %d)", errTooManyBytes, len(responseBytes), bytesLimit)
	}

	return nil
}

func (m *Manager) handleRangeProofResponse(
	ctx context.Context,
	targetRootID ids.ID,
	work *workItem,
	request *pb.SyncGetRangeProofRequest,
	responseBytes []byte,
	err error,
) error {
	if err := m.shouldHandleResponse(request.BytesLimit, responseBytes, err); err != nil {
		return err
	}

	rangeProof, err := m.config.Parser.ParseRangeProof(
		ctx,
		responseBytes,
		request.RootHash,
		maybeBytesToMaybe(request.StartKey),
		maybeBytesToMaybe(request.EndKey),
		request.KeyLimit,
	)
	if err != nil {
		return err
	}
	// Replace all the key-value pairs in the DB from start to end with values from the response.
	nextKey, err := rangeProof.Commit(ctx)
	if err != nil {
		m.setError(err)
		return nil
	}

	m.completeWorkItem(work, nextKey, targetRootID)
	return nil
}

// Stores the request information for the proof.
type proofRequest struct {
	rootHash []byte
	startKey maybe.Maybe[[]byte]
	endKey   maybe.Maybe[[]byte]
	keyLimit int
}

func (p *proofParser) ParseRangeProof(ctx context.Context, responseBytes, rootHash []byte, startKey, endKey maybe.Maybe[[]byte], keyLimit uint32) (Proof, error) {
	var rangeProofProto pb.RangeProof
	if err := proto.Unmarshal(responseBytes, &rangeProofProto); err != nil {
		return nil, err
	}

	var merkleProof merkledb.RangeProof
	if err := merkleProof.UnmarshalProto(&rangeProofProto); err != nil {
		return nil, err
	}

	proof := &rangeProof{
		merkleProof: &merkleProof,
		request: &proofRequest{
			rootHash: rootHash,
			startKey: startKey,
			endKey:   endKey,
			keyLimit: int(keyLimit),
		},
		db:        p.db,
		tokenSize: p.tokenSize,
		hasher:    p.hasher,
	}

	if err := proof.verify(ctx); err != nil {
		return nil, err
	}
	return proof, nil
}

type rangeProof struct {
	merkleProof *merkledb.RangeProof
	request     *proofRequest
	db          DB
	tokenSize   int
	hasher      merkledb.Hasher
}

func (r *rangeProof) verify(ctx context.Context) error {
	// Database is empty, no proof is needed.
	if bytes.Equal(r.request.rootHash, ids.Empty[:]) {
		return nil
	}

	return verifyRangeProof(
		ctx,
		r.merkleProof,
		r.request.keyLimit,
		r.request.startKey,
		r.request.endKey,
		r.request.rootHash,
		r.tokenSize,
		r.hasher,
	)
}

func (r *rangeProof) Commit(ctx context.Context) (maybe.Maybe[[]byte], error) {
	// If the root is empty, we clear the database of all values.
	if bytes.Equal(r.request.rootHash, ids.Empty[:]) {
		return maybe.Nothing[[]byte](), r.db.Clear()
	}

	// Range proofs must always be committed, even if no key changes are present.
	// This is because it may have been provided instead of a change proof, and the keys were deleted.
	if err := r.db.CommitRangeProof(ctx, r.request.startKey, r.request.endKey, r.merkleProof); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// Find the next key to fetch.
	// This will return Nothing if there are no more keys to fetch in the range `largestHandledKey` to `r.request.endKey`.
	nextKey, err := findNextKey(ctx, r.db, r.merkleProof.KeyChanges, r.request.endKey, r.merkleProof.EndProof, r.tokenSize)
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	return nextKey, nil
}

func (m *Manager) handleChangeProofResponse(
	ctx context.Context,
	targetRootID ids.ID,
	work *workItem,
	request *pb.SyncGetChangeProofRequest,
	responseBytes []byte,
	err error,
) error {
	if err := m.shouldHandleResponse(request.BytesLimit, responseBytes, err); err != nil {
		return err
	}

	proof, err := m.config.Parser.ParseChangeProof(
		ctx,
		responseBytes,
		request.EndRootHash,
		maybeBytesToMaybe(request.StartKey),
		maybeBytesToMaybe(request.EndKey),
		request.KeyLimit,
	)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidChangeProof, err)
	}

	nextKey, err := proof.Commit(ctx)
	if err != nil {
		m.setError(err)
		return nil
	}

	m.completeWorkItem(work, nextKey, targetRootID)
	return nil
}

func (p *proofParser) ParseChangeProof(ctx context.Context, responseBytes, endRootHash []byte, startKey, endKey maybe.Maybe[[]byte], keyLimit uint32) (Proof, error) {
	var changeProofResp pb.SyncGetChangeProofResponse
	if err := proto.Unmarshal(responseBytes, &changeProofResp); err != nil {
		return nil, err
	}

	switch changeProofResp := changeProofResp.Response.(type) {
	case *pb.SyncGetChangeProofResponse_ChangeProof:
		// The server had enough history to send us a change proof
		var merkleChangeProof merkledb.ChangeProof
		if err := merkleChangeProof.UnmarshalProto(changeProofResp.ChangeProof); err != nil {
			return nil, err
		}

		proof := &changeProof{
			merkleProof: &merkleChangeProof,
			request: &proofRequest{
				rootHash: endRootHash,
				startKey: startKey,
				endKey:   endKey,
				keyLimit: int(keyLimit),
			},
			db:        p.db,
			tokenSize: p.tokenSize,
		}
		if err := proof.verify(ctx); err != nil {
			return nil, err
		}
		return proof, nil

	case *pb.SyncGetChangeProofResponse_RangeProof:
		var merkleRangeProof merkledb.RangeProof
		if err := merkleRangeProof.UnmarshalProto(changeProofResp.RangeProof); err != nil {
			return nil, err
		}

		proof := &rangeProof{
			merkleProof: &merkleRangeProof,
			request: &proofRequest{
				rootHash: endRootHash,
				startKey: startKey,
				endKey:   endKey,
				keyLimit: int(keyLimit),
			},
			db:        p.db,
			tokenSize: p.tokenSize,
			hasher:    p.hasher,
		}
		if err := proof.verify(ctx); err != nil {
			return nil, err
		}
		return proof, nil

	default:
		return nil, fmt.Errorf(
			"%w: %T",
			errUnexpectedChangeProofResponse, changeProofResp,
		)
	}
}

type changeProof struct {
	merkleProof *merkledb.ChangeProof
	request     *proofRequest
	db          DB
	tokenSize   int
}

func (c *changeProof) verify(ctx context.Context) error {
	// If the database is empty, we don't need to verify a change proof.
	if bytes.Equal(c.request.rootHash, ids.Empty[:]) {
		return nil
	}

	// Ensure the response does not contain more than the requested number of leaves
	// and the start and end roots match the requested roots.
	if len(c.merkleProof.KeyChanges) > c.request.keyLimit {
		return fmt.Errorf(
			"%w: (%d) > %d)",
			errTooManyKeys, len(c.merkleProof.KeyChanges), c.request.keyLimit,
		)
	}

	endRoot, err := ids.ToID(c.request.rootHash)
	if err != nil {
		return err
	}
	return c.db.VerifyChangeProof(
		ctx,
		c.merkleProof,
		c.request.startKey,
		c.request.endKey,
		endRoot,
	)
}

func (c *changeProof) Commit(ctx context.Context) (maybe.Maybe[[]byte], error) {
	// If the root is empty, we clear the database.
	if bytes.Equal(c.request.rootHash, ids.Empty[:]) {
		return maybe.Nothing[[]byte](), c.db.Clear()
	}

	// We only need to apply changes if there are any key changes to commit.
	if len(c.merkleProof.KeyChanges) != 0 {
		if err := c.db.CommitChangeProof(ctx, c.merkleProof); err != nil {
			return maybe.Nothing[[]byte](), err
		}
	}

	// This will return Nothing if there are no more keys to fetch in the range `largestHandledKey` to `c.request.endKey`.
	nextKey, err := findNextKey(ctx, c.db, c.merkleProof.KeyChanges, c.request.endKey, c.merkleProof.EndProof, c.tokenSize)
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	return nextKey, nil
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
// If [rangeEnd] is Nothing it's considered > [lastReceivedKey].
func findNextKey(
	ctx context.Context,
	db DB,
	keyChanges []merkledb.KeyChange,
	rangeEnd maybe.Maybe[[]byte],
	endProof []merkledb.ProofNode,
	tokenSize int,
) (maybe.Maybe[[]byte], error) {
	lastReceivedKey := rangeEnd.Value()
	if len(keyChanges) > 0 {
		lastReceivedKey = keyChanges[len(keyChanges)-1].Key
	}

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
	localProofOfKey, err := db.GetProof(ctx, proofKeyPath.Bytes())
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
				localProofNodes[0].Key.Token(0, tokenSize): ids.Empty,
			},
		}
		localProofNodes = append([]merkledb.ProofNode{sentinel}, localProofNodes...)
	}

	// Add sentinel node back into the endProof, if it is missing.
	// Required to ensure that a common node exists in both proofs
	if len(endProof) > 0 && endProof[0].Key.Length() != 0 {
		sentinel := merkledb.ProofNode{
			Children: map[byte]ids.ID{
				endProof[0].Key.Token(0, tokenSize): ids.Empty,
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
			startingChildToken = int(proofKeyPath.Token(deepestNode.Key.Length(), tokenSize)) + 1
		}

		// determine if there are any differences in the children for the deepest unhandled node of the two proofs
		if childIndex, hasDifference := findChildDifference(deepestNode, deepestNodeFromOtherProof, startingChildToken); hasDifference {
			nextKey = maybe.Some(deepestNode.Key.Extend(merkledb.ToToken(childIndex, tokenSize)).Bytes())
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

	m.config.Log.Info("completed", zap.Stringer("root", m.getTargetRoot()))
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
// [work.start, nextKey) for the trie with root [rootID].
//
// If [work.start] is Nothing, then we've fetched all the key-value
// pairs up to and including [nextKey].
//
// If [nextKey] is Nothing, then we've fetched all the key-value
// pairs in the trie with root [rootID] until [work.end].
//
// TODO: add some assertions that `nextKey` work changes don't violate
// the invariants of the work queue.
//
// Assumes [m.workLock] is not held.
func (m *Manager) completeWorkItem(work *workItem, nextKey maybe.Maybe[[]byte], rootID ids.ID) {
	if !nextKey.IsNothing() {
		// the full range wasn't completed, so enqueue a new work item for the range [nextStartKey, workItem.end]
		m.enqueueWork(newWorkItem(work.localRootID, nextKey, work.end, work.priority, time.Now()))
	}

	// Process [work] while holding [syncTargetLock] to ensure that object
	// is added to the right queue, even if a target update is triggered
	m.syncTargetLock.RLock()
	defer m.syncTargetLock.RUnlock()

	stale := m.config.TargetRoot != rootID
	if stale {
		// the root has changed, so reinsert with high priority
		// Use root from proof as old root.
		m.enqueueWork(newWorkItem(rootID, work.start, nextKey, highPriority, time.Now()))
	} else {
		m.workLock.Lock()
		defer m.workLock.Unlock()

		m.processedWork.MergeInsert(newWorkItem(rootID, work.start, nextKey, work.priority, time.Now()))
	}

	// completed the range [work.start, lastKey], log and record in the completed work heap
	m.config.Log.Debug("completed range",
		zap.Stringer("start", work.start),
		zap.Stringer("end", nextKey),
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
	first := newWorkItem(work.localRootID, work.start, mid, medPriority, time.Now())
	second := newWorkItem(work.localRootID, mid, work.end, lowPriority, time.Now())

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

// Verify [rangeProof] is a valid range proof for keys in [start, end] for
// root [rootBytes]. Returns [errTooManyKeys] if the response contains more
// than [keyLimit] keys.
func verifyRangeProof(
	ctx context.Context,
	rangeProof *merkledb.RangeProof,
	keyLimit int,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	rootBytes []byte,
	tokenSize int,
	hasher merkledb.Hasher,
) error {
	root, err := ids.ToID(rootBytes)
	if err != nil {
		return err
	}

	// Ensure the response does not contain more than the maximum requested number of leaves.
	if len(rangeProof.KeyChanges) > keyLimit {
		return fmt.Errorf(
			"%w: (%d) > %d)",
			errTooManyKeys, len(rangeProof.KeyChanges), keyLimit,
		)
	}

	if err := rangeProof.Verify(
		ctx,
		start,
		end,
		root,
		tokenSize,
		hasher,
	); err != nil {
		return fmt.Errorf("%w due to %w", errInvalidRangeProof, err)
	}
	return nil
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
