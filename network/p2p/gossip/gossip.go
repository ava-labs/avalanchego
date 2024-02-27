// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	typeLabel = "type"
	pushType  = "push"
	pullType  = "pull"

	defaultGossipableCount = 64
)

var (
	_ Gossiper = (*ValidatorGossiper)(nil)
	_ Gossiper = (*PullGossiper[*testTx])(nil)
	_ Gossiper = (*NoOpGossiper)(nil)

	_ Set[*testTx] = (*EmptySet[*testTx])(nil)
	_ Set[*testTx] = (*FullSet[*testTx])(nil)

	metricLabels = []string{typeLabel}
	pushLabels   = prometheus.Labels{
		typeLabel: pushType,
	}
	pullLabels = prometheus.Labels{
		typeLabel: pullType,
	}

	errEmptySetCantAdd = errors.New("empty set can not add")
)

// Gossiper gossips Gossipables to other nodes
type Gossiper interface {
	// Gossip runs a cycle of gossip. Returns an error if we failed to gossip.
	Gossip(ctx context.Context) error
}

// ValidatorGossiper only calls [Gossip] if the given node is a validator
type ValidatorGossiper struct {
	Gossiper

	NodeID     ids.NodeID
	Validators p2p.ValidatorSet
}

// Metrics that are tracked across a gossip protocol. A given protocol should
// only use a single instance of Metrics.
type Metrics struct {
	sentCount     *prometheus.CounterVec
	sentBytes     *prometheus.CounterVec
	receivedCount *prometheus.CounterVec
	receivedBytes *prometheus.CounterVec
	tracking      prometheus.Gauge
}

// NewMetrics returns a common set of metrics
func NewMetrics(
	metrics prometheus.Registerer,
	namespace string,
) (Metrics, error) {
	m := Metrics{
		sentCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "gossip_sent_count",
			Help:      "amount of gossip sent (n)",
		}, metricLabels),
		sentBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "gossip_sent_bytes",
			Help:      "amount of gossip sent (bytes)",
		}, metricLabels),
		receivedCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "gossip_received_count",
			Help:      "amount of gossip received (n)",
		}, metricLabels),
		receivedBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "gossip_received_bytes",
			Help:      "amount of gossip received (bytes)",
		}, metricLabels),
		tracking: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "gossip_tracking",
			Help:      "number of gossipables being tracked",
		}),
	}
	err := utils.Err(
		metrics.Register(m.sentCount),
		metrics.Register(m.sentBytes),
		metrics.Register(m.receivedCount),
		metrics.Register(m.receivedBytes),
	)
	return m, err
}

func (v ValidatorGossiper) Gossip(ctx context.Context) error {
	if !v.Validators.Has(ctx, v.NodeID) {
		return nil
	}

	return v.Gossiper.Gossip(ctx)
}

func NewPullGossiper[T Gossipable](
	log logging.Logger,
	marshaller Marshaller[T],
	set Set[T],
	client *p2p.Client,
	metrics Metrics,
	pollSize int,
) *PullGossiper[T] {
	return &PullGossiper[T]{
		log:        log,
		marshaller: marshaller,
		set:        set,
		client:     client,
		metrics:    metrics,
		pollSize:   pollSize,
	}
}

type PullGossiper[T Gossipable] struct {
	log        logging.Logger
	marshaller Marshaller[T]
	set        Set[T]
	client     *p2p.Client
	metrics    Metrics
	pollSize   int
}

func (p *PullGossiper[_]) Gossip(ctx context.Context) error {
	msgBytes, err := MarshalAppRequest(p.set.GetFilter())
	if err != nil {
		return err
	}

	for i := 0; i < p.pollSize; i++ {
		err := p.client.AppRequestAny(ctx, msgBytes, p.handleResponse)
		if err != nil && !errors.Is(err, p2p.ErrNoPeers) {
			return err
		}
	}

	return nil
}

func (p *PullGossiper[_]) handleResponse(
	_ context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
) {
	if err != nil {
		p.log.Debug(
			"failed gossip request",
			zap.Stringer("nodeID", nodeID),
			zap.Error(err),
		)
		return
	}

	gossip, err := ParseAppResponse(responseBytes)
	if err != nil {
		p.log.Debug("failed to unmarshal gossip response", zap.Error(err))
		return
	}

	receivedBytes := 0
	for _, bytes := range gossip {
		receivedBytes += len(bytes)

		gossipable, err := p.marshaller.UnmarshalGossip(bytes)
		if err != nil {
			p.log.Debug(
				"failed to unmarshal gossip",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			continue
		}

		gossipID := gossipable.GossipID()
		p.log.Debug(
			"received gossip",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("id", gossipID),
		)
		if err := p.set.Add(gossipable); err != nil {
			p.log.Debug(
				"failed to add gossip to the known set",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("id", gossipID),
				zap.Error(err),
			)
			continue
		}
	}

	receivedCountMetric, err := p.metrics.receivedCount.GetMetricWith(pullLabels)
	if err != nil {
		p.log.Error("failed to get received count metric", zap.Error(err))
		return
	}

	receivedBytesMetric, err := p.metrics.receivedBytes.GetMetricWith(pullLabels)
	if err != nil {
		p.log.Error("failed to get received bytes metric", zap.Error(err))
		return
	}

	receivedCountMetric.Add(float64(len(gossip)))
	receivedBytesMetric.Add(float64(receivedBytes))
}

// NewPushGossiper returns an instance of PushGossiper
func NewPushGossiper[T Gossipable](
	marshaller Marshaller[T],
	mempool Set[T],
	client *p2p.Client,
	metrics Metrics,
	discardedSize int,
	targetGossipSize int,
	maxRegossipFrequency time.Duration,
) *PushGossiper[T] {
	return &PushGossiper[T]{
		marshaller:           marshaller,
		set:                  mempool,
		client:               client,
		metrics:              metrics,
		targetGossipSize:     targetGossipSize,
		maxRegossipFrequency: maxRegossipFrequency,

		tracking:  make(map[ids.ID]time.Time),
		pending:   buffer.NewUnboundedDeque[T](0),
		issued:    buffer.NewUnboundedDeque[T](0),
		discarded: &cache.LRU[ids.ID, interface{}]{Size: discardedSize},
	}
}

// PushGossiper broadcasts gossip to peers randomly in the network
type PushGossiper[T Gossipable] struct {
	marshaller Marshaller[T]
	set        Set[T]
	client     *p2p.Client
	metrics    Metrics

	targetGossipSize     int
	maxRegossipFrequency time.Duration

	lock      sync.Mutex
	tracking  map[ids.ID]time.Time
	pending   buffer.Deque[T]
	issued    buffer.Deque[T]
	discarded *cache.LRU[ids.ID, interface{}] // discarded attempts to avoid overgossiping transactions that are frequently dropped
}

// Gossip flushes any queued gossipables.
func (p *PushGossiper[T]) Gossip(ctx context.Context) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	if len(p.tracking) == 0 {
		return nil
	}

	var (
		sentBytes = 0
		gossip    = make([][]byte, 0, defaultGossipableCount)
		now       = time.Now()
	)

	// Iterate over all pending gossipables (never been sent before).
	for sentBytes < p.targetGossipSize {
		gossipable, ok := p.pending.PopLeft()
		if !ok {
			break
		}

		// Ensure item is still in the set before we gossip.
		gossipID := gossipable.GossipID()
		if !p.set.Has(gossipID) {
			delete(p.tracking, gossipID)
			continue
		}

		bytes, err := p.marshaller.MarshalGossip(gossipable)
		if err != nil {
			delete(p.tracking, gossipID)
			return err
		}

		gossip = append(gossip, bytes)
		sentBytes += len(bytes)
		p.issued.PushRight(gossipable)
		p.tracking[gossipID] = now
	}

	// Invariant: maxRegossipFrequency must be non-negative to prevent gossiping
	// the same transaction multiple times in the same message.
	maxLastGossipTimeToRegossip := now.Add(-p.maxRegossipFrequency)

	// Iterate over all issued gossipables (have been sent before) to fill any
	// remaining space in gossip batch.
	for sentBytes < p.targetGossipSize {
		gossipable, ok := p.issued.PopLeft()
		if !ok {
			break
		}

		// Ensure item is still in the set before we gossip.
		gossipID := gossipable.GossipID()
		if !p.set.Has(gossipID) {
			delete(p.tracking, gossipID)
			p.discarded.Put(gossipID, nil) // only add to discarded if issued once
			continue
		}

		// Ensure we don't attempt to send a gossipable too frequently.
		lastGossipTime := p.tracking[gossipID]
		if maxLastGossipTimeToRegossip.Before(lastGossipTime) {
			// Put the gossipable on the front of the queue to keep items sorted
			// by last issuance time.
			p.issued.PushLeft(gossipable)
			break
		}

		bytes, err := p.marshaller.MarshalGossip(gossipable)
		if err != nil {
			// Should never happen because we've already issued this once.
			delete(p.tracking, gossipID)
			return err
		}

		gossip = append(gossip, bytes)
		sentBytes += len(bytes)
		p.issued.PushRight(gossipable)
		p.tracking[gossipID] = now
	}
	p.metrics.tracking.Set(float64(len(p.tracking)))

	// Send gossipables to peers
	msgBytes, err := MarshalAppGossip(gossip)
	if err != nil {
		return err
	}
	sentCountMetric, err := p.metrics.sentCount.GetMetricWith(pushLabels)
	if err != nil {
		return fmt.Errorf("failed to get sent count metric: %w", err)
	}
	sentBytesMetric, err := p.metrics.sentBytes.GetMetricWith(pushLabels)
	if err != nil {
		return fmt.Errorf("failed to get sent bytes metric: %w", err)
	}
	sentCountMetric.Add(float64(len(gossip)))
	sentBytesMetric.Add(float64(sentBytes))
	if err := p.client.AppGossip(ctx, msgBytes); err != nil {
		return fmt.Errorf("failed to gossip: %w", err)
	}
	return nil
}

// Add enqueues new gossipables to be pushed. If a gossiable is already tracked,
// it is not added again.
func (p *PushGossiper[T]) Add(gossipables ...T) {
	p.lock.Lock()
	defer p.lock.Unlock()

	// Add new gossipables to the pending queue.
	now := time.Now()
	for _, gossipable := range gossipables {
		gossipID := gossipable.GossipID()
		if _, contains := p.tracking[gossipID]; contains {
			continue
		}
		if _, contains := p.discarded.Get(gossipID); !contains {
			p.tracking[gossipID] = time.Time{}
			p.pending.PushRight(gossipable)
		} else {
			// Pretend that recently discarded transactions were just gossiped.
			p.tracking[gossipID] = now
			p.issued.PushRight(gossipable)
		}
	}
	p.metrics.tracking.Set(float64(len(p.tracking)))
}

// Every calls [Gossip] every [frequency] amount of time.
func Every(ctx context.Context, log logging.Logger, gossiper Gossiper, frequency time.Duration) {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := gossiper.Gossip(ctx); err != nil {
				log.Warn("failed to gossip", zap.Error(err))
			}
		case <-ctx.Done():
			log.Debug("shutting down gossip")
			return
		}
	}
}

type NoOpGossiper struct{}

func (NoOpGossiper) Gossip(context.Context) error {
	return nil
}

type TestGossiper struct {
	GossipF func(ctx context.Context) error
}

func (t *TestGossiper) Gossip(ctx context.Context) error {
	return t.GossipF(ctx)
}

type EmptySet[T Gossipable] struct{}

func (EmptySet[_]) Gossip(context.Context) error {
	return nil
}

func (EmptySet[T]) Add(T) error {
	return errEmptySetCantAdd
}

func (EmptySet[T]) Has(ids.ID) bool {
	return false
}

func (EmptySet[T]) Iterate(func(gossipable T) bool) {}

func (EmptySet[_]) GetFilter() ([]byte, []byte) {
	return bloom.EmptyFilter.Marshal(), ids.Empty[:]
}

type FullSet[T Gossipable] struct{}

func (FullSet[_]) Gossip(context.Context) error {
	return nil
}

func (FullSet[T]) Add(T) error {
	return nil
}

func (FullSet[T]) Has(ids.ID) bool {
	return true
}

func (FullSet[T]) Iterate(func(gossipable T) bool) {}

func (FullSet[_]) GetFilter() ([]byte, []byte) {
	return bloom.FullFilter.Marshal(), ids.Empty[:]
}
