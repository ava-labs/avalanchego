// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"

	"github.com/ava-labs/avalanchego/graft/evm/sync/session"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
)

var (
	_ types.CodeRequestQueue = (*SessionedQueue)(nil)

	ErrFailedToAddCodeHashesToQueue = errors.New("failed to add code hashes to queue")
	errFailedToFinalizeCodeQueue    = errors.New("failed to finalize code queue")
	errSessionedQueueNotStarted     = errors.New("sessioned code queue not started")
	errSessionedQueueAlreadyStarted = errors.New("sessioned code queue already started")
	errSessionBoundarySendTimeout   = errors.New("session boundary event send timed out")
)

type EventType uint8

const (
	EventSessionEnd EventType = iota
	EventSessionStart
	EventCodeHash
)

type Event struct {
	Type      EventType
	SessionID session.ID
	Hash      common.Hash // only set for EventCodeHash
}

type sessionedQueueConfig struct {
	capacity                 int
	sessionBoundarySendLimit time.Duration
}

type SessionedQueueOption = options.Option[sessionedQueueConfig]

// WithSessionedCapacity overrides the sessioned queue buffer capacity.
func WithSessionedCapacity(n int) SessionedQueueOption {
	return options.Func[sessionedQueueConfig](func(c *sessionedQueueConfig) {
		if n > 0 {
			c.capacity = n
		}
	})
}

// WithSessionBoundarySendTimeout bounds Start/Pivot boundary event sends.
// Set <= 0 to disable the timeout and block until send/quit.
func WithSessionBoundarySendTimeout(d time.Duration) SessionedQueueOption {
	return options.Func[sessionedQueueConfig](func(c *sessionedQueueConfig) {
		c.sessionBoundarySendLimit = d
	})
}

// SessionedQueue is a session-boundary aware code queue that emits tagged events.
//
// It is intentionally optional and does not alter static queue behavior.
type SessionedQueue struct {
	db   ethdb.Database
	quit <-chan struct{}

	in            chan<- Event // in is nil after close to avoid send-after-close
	out           <-chan Event // out is always non-nil
	chanLock      sync.RWMutex
	closeChanOnce sync.Once

	mu sync.Mutex

	started          bool
	finalized        bool
	currentSessionID session.ID
	manager          *session.Manager[common.Hash]

	sessionBoundarySendLimit time.Duration
}

func NewSessionedQueue(db ethdb.Database, quit <-chan struct{}, opts ...SessionedQueueOption) (*SessionedQueue, error) {
	cfg := sessionedQueueConfig{
		capacity:                 defaultQueueCapacity,
		sessionBoundarySendLimit: 5 * time.Second,
	}
	options.ApplyTo(&cfg, opts...)

	ch := make(chan Event, cfg.capacity)
	q := &SessionedQueue{
		db:   db,
		quit: quit,
		in:   ch,
		out:  ch,

		sessionBoundarySendLimit: cfg.sessionBoundarySendLimit,
	}

	if quit != nil {
		go func() {
			<-quit
			q.closeEventChannelOnce()
		}()
	}

	return q, nil
}

// Events returns the receive-only stream of queue events.
func (q *SessionedQueue) Events() <-chan Event {
	return q.out
}

// Start begins the first session for [root].
func (q *SessionedQueue) Start(root common.Hash) (session.ID, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.finalized {
		return 0, errFailedToFinalizeCodeQueue
	}
	if q.started {
		return 0, errSessionedQueueAlreadyStarted
	}

	// Recover durable markers before we expose a started session.
	dbCodeHashes, err := recoverUnfetchedCodeHashes(q.db)
	if err != nil {
		return 0, fmt.Errorf("unable to recover previous sync state: %w", err)
	}

	manager := session.NewManager(root, session.WithComparableEqual[common.Hash]())
	s, _ := manager.Start(context.Background())

	if err := q.sendBoundaryEventLocked(Event{Type: EventSessionStart, SessionID: s.ID}); err != nil {
		return 0, err
	}
	for _, h := range dbCodeHashes {
		if err := q.sendBoundaryEventLocked(Event{Type: EventCodeHash, SessionID: s.ID, Hash: h}); err != nil {
			return 0, fmt.Errorf("unable to resume previous sync: %w", err)
		}
	}

	q.manager = manager
	q.currentSessionID = s.ID
	q.started = true
	return s.ID, nil
}

// PivotTo requests a new session for [root]. If [root] is unchanged, it is a no-op.
func (q *SessionedQueue) PivotTo(root common.Hash) (session.ID, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.finalized {
		return 0, false, errFailedToFinalizeCodeQueue
	}
	if !q.started {
		return 0, false, errSessionedQueueNotStarted
	}

	cur, ok := q.manager.Current()
	if !ok {
		return 0, false, errSessionedQueueNotStarted
	}

	if root == cur.Target {
		return cur.ID, false, nil
	}

	// Send the session-end boundary event before any irreversible mutations
	// (DB marker cleanup, session-manager pivot) so that a timeout or send
	// failure leaves durable state completely unchanged.
	if err := q.sendBoundaryEventLocked(Event{Type: EventSessionEnd, SessionID: cur.ID}); err != nil {
		return 0, false, err
	}

	if err := clearAllCodeToFetch(q.db); err != nil {
		return 0, false, fmt.Errorf("failed to clear code-to-fetch markers on pivot: %w", err)
	}

	if !q.manager.RequestPivot(root) {
		return cur.ID, false, nil
	}

	next, _, restarted := q.manager.RestartIfPending(context.Background())
	if !restarted {
		return cur.ID, false, nil
	}
	if err := q.sendBoundaryEventLocked(Event{Type: EventSessionStart, SessionID: next.ID}); err != nil {
		return 0, false, err
	}

	q.currentSessionID = next.ID
	return next.ID, true, nil
}

// AddCode persists and emits code hash events for the current session.
//
// Session transitions and AddCode are serialized by [mu] to avoid cross-session
// event tagging races.
func (q *SessionedQueue) AddCode(ctx context.Context, hashes []common.Hash) error {
	if len(hashes) == 0 {
		return nil
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.finalized {
		return errFailedToFinalizeCodeQueue
	}
	if !q.started {
		return errSessionedQueueNotStarted
	}

	batch := q.db.NewBatch()
	for _, h := range hashes {
		if err := customrawdb.WriteCodeToFetch(batch, h); err != nil {
			return fmt.Errorf("failed to write code to fetch marker: %w", err)
		}
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch of code to fetch markers: %w", err)
	}

	for _, h := range hashes {
		if err := q.sendEventLocked(ctx, Event{
			Type:      EventCodeHash,
			SessionID: q.currentSessionID,
			Hash:      h,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Finalize closes the event stream and signals consumers to stop.
// The channel close is the reliable termination signal and consumers
// detect it via the receive-ok flag.
func (q *SessionedQueue) Finalize() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.finalized {
		return errFailedToFinalizeCodeQueue
	}
	q.finalized = true

	// Close the event channel. If the quit goroutine already closed it,
	// that is equivalent - the consumer sees the same channel-closed signal.
	q.closeEventChannelOnce()
	return nil
}

func (q *SessionedQueue) closeEventChannelOnce() {
	q.closeChanOnce.Do(func() {
		q.chanLock.Lock()
		defer q.chanLock.Unlock()
		close(q.in)
		q.in = nil
	})
}

func (q *SessionedQueue) sendEventLocked(ctx context.Context, ev Event) error {
	q.chanLock.RLock()
	defer q.chanLock.RUnlock()

	if q.in == nil {
		return ErrFailedToAddCodeHashesToQueue
	}

	select {
	case q.in <- ev:
		return nil
	case <-q.quit:
		return ErrFailedToAddCodeHashesToQueue
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *SessionedQueue) sendBoundaryEventLocked(ev Event) error {
	ctx := context.Background()
	cancel := func() {}
	if q.sessionBoundarySendLimit > 0 {
		ctx, cancel = context.WithTimeout(ctx, q.sessionBoundarySendLimit)
	}
	defer cancel()

	if err := q.sendEventLocked(ctx, ev); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: event=%d", errSessionBoundarySendTimeout, ev.Type)
		}
		return err
	}
	return nil
}

func clearAllCodeToFetch(db ethdb.Database) error {
	it := customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()

	batch := db.NewBatch()
	for it.Next() {
		codeHash := common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):])
		if err := customrawdb.DeleteCodeToFetch(batch, codeHash); err != nil {
			return fmt.Errorf("failed to delete code to fetch marker: %w", err)
		}
		if batch.ValueSize() < ethdb.IdealBatchSize {
			continue
		}

		if err := batch.Write(); err != nil {
			return fmt.Errorf("failed to write batch removing old code markers: %w", err)
		}
		batch.Reset()
	}
	if err := it.Error(); err != nil {
		return fmt.Errorf("failed to iterate code entries to fetch: %w", err)
	}
	if batch.ValueSize() > 0 {
		if err := batch.Write(); err != nil {
			return fmt.Errorf("failed to write batch removing old code markers: %w", err)
		}
	}
	return nil
}
