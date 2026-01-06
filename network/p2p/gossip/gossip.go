// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	ioLabel    = "io"
	sentIO     = "sent"
	receivedIO = "received"

	typeLabel  = "type"
	pushType   = "push"
	pullType   = "pull"
	unsentType = "unsent"
	sentType   = "sent"

	defaultGossipableCount = 64
)

var (
	_ Gossiper = (*ValidatorGossiper)(nil)
	_ Gossiper = (*PullGossiper[Gossipable])(nil)
	_ Gossiper = (*PushGossiper[Gossipable])(nil)

	ioTypeLabels   = []string{ioLabel, typeLabel}
	sentPushLabels = prometheus.Labels{
		ioLabel:   sentIO,
		typeLabel: pushType,
	}
	receivedPushLabels = prometheus.Labels{
		ioLabel:   receivedIO,
		typeLabel: pushType,
	}
	sentPullLabels = prometheus.Labels{
		ioLabel:   sentIO,
		typeLabel: pullType,
	}
	receivedPullLabels = prometheus.Labels{
		ioLabel:   receivedIO,
		typeLabel: pullType,
	}
	typeLabels   = []string{typeLabel}
	unsentLabels = prometheus.Labels{
		typeLabel: unsentType,
	}
	sentLabels = prometheus.Labels{
		typeLabel: sentType,
	}

	ErrInvalidNumValidators     = errors.New("num validators cannot be negative")
	ErrInvalidNumNonValidators  = errors.New("num non-validators cannot be negative")
	ErrInvalidNumPeers          = errors.New("num peers cannot be negative")
	ErrInvalidNumToGossip       = errors.New("must gossip to at least one peer")
	ErrInvalidDiscardedSize     = errors.New("discarded size cannot be negative")
	ErrInvalidTargetGossipSize  = errors.New("target gossip size cannot be negative")
	ErrInvalidRegossipFrequency = errors.New("re-gossip frequency cannot be negative")
)

// Gossipable is an item that can be gossiped across the network
type Gossipable interface {
	GossipID() ids.ID
}

// Marshaller handles parsing logic for a concrete Gossipable type
type Marshaller[T Gossipable] interface {
	MarshalGossip(T) ([]byte, error)
	UnmarshalGossip([]byte) (T, error)
}

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
	count                   *prometheus.CounterVec
	bytes                   *prometheus.CounterVec
	tracking                *prometheus.GaugeVec
	trackingLifetimeAverage prometheus.Gauge
	topValidators           *prometheus.GaugeVec
	bloomFilterHitRate      prometheus.Histogram
}

// NewMetrics returns a common set of metrics
func NewMetrics(
	metrics prometheus.Registerer,
	namespace string,
) (Metrics, error) {
	m := Metrics{
		bloomFilterHitRate: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "bloomfilter_hit_rate",
			Help:      "Hit rate (%) of the bloom filter sent by pull gossip",
			// Buckets are (-∞, 0], (0, 25%], (25%, 50%], (50%, 75%], (75%, ∞).
			// 0% is placed into its own bucket so that useless bloom filters
			// can be inspected individually.
			Buckets: prometheus.LinearBuckets(0, 25, 4),
		}),
		count: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "count",
				Help:      "amount of gossip (n)",
			},
			ioTypeLabels,
		),
		bytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "bytes",
				Help:      "amount of gossip (bytes)",
			},
			ioTypeLabels,
		),
		tracking: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "tracking",
				Help:      "number of gossipables being tracked",
			},
			typeLabels,
		),
		trackingLifetimeAverage: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tracking_lifetime_average",
			Help:      "average duration a gossipable has been tracked (ns)",
		}),
		topValidators: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "top_validators",
				Help:      "number of validators gossipables are sent to due to stake",
			},
			typeLabels,
		),
	}
	err := errors.Join(
		metrics.Register(m.bloomFilterHitRate),
		metrics.Register(m.count),
		metrics.Register(m.bytes),
		metrics.Register(m.tracking),
		metrics.Register(m.trackingLifetimeAverage),
		metrics.Register(m.topValidators),
	)
	return m, err
}

func (m *Metrics) observeMessage(labels prometheus.Labels, count int, bytes int) error {
	countMetric, err := m.count.GetMetricWith(labels)
	if err != nil {
		return fmt.Errorf("failed to get count metric: %w", err)
	}

	bytesMetric, err := m.bytes.GetMetricWith(labels)
	if err != nil {
		return fmt.Errorf("failed to get bytes metric: %w", err)
	}

	countMetric.Add(float64(count))
	bytesMetric.Add(float64(bytes))
	return nil
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
	set PullGossiperSet[T],
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

// PullGossiperSet exposes the current bloom filter and allows adding new items
// that were not included in the filter.
//
// TODO: Consider naming this interface based on what it provides rather than
// how its used.
type PullGossiperSet[T Gossipable] interface {
	// Add adds a value to the set. Returns an error if v was not added.
	Add(v T) error
	// BloomFilter returns the bloom filter and its corresponding salt.
	BloomFilter() (bloom *bloom.Filter, salt ids.ID)
}

type PullGossiper[T Gossipable] struct {
	log        logging.Logger
	marshaller Marshaller[T]
	set        PullGossiperSet[T]
	client     *p2p.Client
	metrics    Metrics
	pollSize   int
}

func (p *PullGossiper[_]) Gossip(ctx context.Context) error {
	bf, salt := p.set.BloomFilter()
	msgBytes, err := MarshalAppRequest(bf.Marshal(), salt[:])
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

	if err := p.metrics.observeMessage(receivedPullLabels, len(gossip), receivedBytes); err != nil {
		p.log.Error("failed to update metrics",
			zap.Error(err),
		)
	}
}

// NewPushGossiper returns an instance of PushGossiper
func NewPushGossiper[T Gossipable](
	marshaller Marshaller[T],
	set PushGossiperSet,
	validators p2p.ValidatorSubset,
	client *p2p.Client,
	metrics Metrics,
	gossipParams BranchingFactor,
	regossipParams BranchingFactor,
	discardedSize int,
	targetGossipSize int,
	maxRegossipFrequency time.Duration,
) (*PushGossiper[T], error) {
	if err := gossipParams.Verify(); err != nil {
		return nil, fmt.Errorf("invalid gossip params: %w", err)
	}
	if err := regossipParams.Verify(); err != nil {
		return nil, fmt.Errorf("invalid regossip params: %w", err)
	}
	switch {
	case discardedSize < 0:
		return nil, ErrInvalidDiscardedSize
	case targetGossipSize < 0:
		return nil, ErrInvalidTargetGossipSize
	case maxRegossipFrequency < 0:
		return nil, ErrInvalidRegossipFrequency
	}

	return &PushGossiper[T]{
		marshaller:           marshaller,
		set:                  set,
		validators:           validators,
		client:               client,
		metrics:              metrics,
		gossipParams:         gossipParams,
		regossipParams:       regossipParams,
		targetGossipSize:     targetGossipSize,
		maxRegossipFrequency: maxRegossipFrequency,

		tracking:   make(map[ids.ID]*tracking),
		toGossip:   buffer.NewUnboundedDeque[T](0),
		toRegossip: buffer.NewUnboundedDeque[T](0),
		discarded:  lru.NewCache[ids.ID, struct{}](discardedSize),
	}, nil
}

// PushGossiperSet exposes whether hashes are still included in a set.
//
// TODO: Consider naming this interface based on what it provides rather than
// how its used.
type PushGossiperSet interface {
	// Has returns true if the hash is in the set.
	Has(h ids.ID) bool
}

// PushGossiper broadcasts gossip to peers randomly in the network
type PushGossiper[T Gossipable] struct {
	marshaller Marshaller[T]
	set        PushGossiperSet
	validators p2p.ValidatorSubset
	client     *p2p.Client
	metrics    Metrics

	gossipParams         BranchingFactor
	regossipParams       BranchingFactor
	targetGossipSize     int
	maxRegossipFrequency time.Duration

	lock         sync.Mutex
	tracking     map[ids.ID]*tracking
	addedTimeSum float64 // unix nanoseconds
	toGossip     buffer.Deque[T]
	toRegossip   buffer.Deque[T]
	discarded    *lru.Cache[ids.ID, struct{}] // discarded attempts to avoid overgossiping transactions that are frequently dropped
}

type BranchingFactor struct {
	// StakePercentage determines the percentage of stake that should have
	// gossip sent to based on the inverse CDF of stake weights. This value does
	// not account for the connectivity of the nodes.
	StakePercentage float64
	// Validators specifies the number of connected validators, in addition to
	// any validators sent from the StakePercentage parameter, to send gossip
	// to. These validators are sampled uniformly rather than by stake.
	Validators int
	// NonValidators specifies the number of connected non-validators to send
	// gossip to.
	NonValidators int
	// Peers specifies the number of connected validators or non-validators, in
	// addition to the number sent due to other configs, to send gossip to.
	Peers int
}

func (b *BranchingFactor) Verify() error {
	switch {
	case b.Validators < 0:
		return ErrInvalidNumValidators
	case b.NonValidators < 0:
		return ErrInvalidNumNonValidators
	case b.Peers < 0:
		return ErrInvalidNumPeers
	case max(b.Validators, b.NonValidators, b.Peers) == 0:
		return ErrInvalidNumToGossip
	default:
		return nil
	}
}

type tracking struct {
	addedTime    float64 // unix nanoseconds
	lastGossiped time.Time
}

// Gossip flushes any queued gossipables.
func (p *PushGossiper[T]) Gossip(ctx context.Context) error {
	var (
		now         = time.Now()
		nowUnixNano = float64(now.UnixNano())
	)

	p.lock.Lock()
	defer func() {
		p.updateMetrics(nowUnixNano)
		p.lock.Unlock()
	}()

	if len(p.tracking) == 0 {
		return nil
	}

	if err := p.gossip(
		ctx,
		now,
		p.gossipParams,
		p.toGossip,
		p.toRegossip,
		&cache.Empty[ids.ID, struct{}]{}, // Don't mark dropped unsent transactions as discarded
		unsentLabels,
	); err != nil {
		return fmt.Errorf("unexpected error during gossip: %w", err)
	}

	if err := p.gossip(
		ctx,
		now,
		p.regossipParams,
		p.toRegossip,
		p.toRegossip,
		p.discarded, // Mark dropped sent transactions as discarded
		sentLabels,
	); err != nil {
		return fmt.Errorf("unexpected error during regossip: %w", err)
	}
	return nil
}

func (p *PushGossiper[T]) gossip(
	ctx context.Context,
	now time.Time,
	gossipParams BranchingFactor,
	toGossip buffer.Deque[T],
	toRegossip buffer.Deque[T],
	discarded cache.Cacher[ids.ID, struct{}],
	metricsLabels prometheus.Labels,
) error {
	var (
		sentBytes                   = 0
		gossip                      = make([][]byte, 0, defaultGossipableCount)
		maxLastGossipTimeToRegossip = now.Add(-p.maxRegossipFrequency)
	)

	for sentBytes < p.targetGossipSize {
		gossipable, ok := toGossip.PopLeft()
		if !ok {
			break
		}

		// Ensure item is still in the set before we gossip.
		gossipID := gossipable.GossipID()
		tracking := p.tracking[gossipID]
		if !p.set.Has(gossipID) {
			delete(p.tracking, gossipID)
			p.addedTimeSum -= tracking.addedTime
			discarded.Put(gossipID, struct{}{}) // Cache that the item was dropped
			continue
		}

		// Ensure we don't attempt to send a gossipable too frequently.
		if maxLastGossipTimeToRegossip.Before(tracking.lastGossiped) {
			// Put the gossipable on the front of the queue to keep items sorted
			// by last issuance time.
			toGossip.PushLeft(gossipable)
			break
		}

		bytes, err := p.marshaller.MarshalGossip(gossipable)
		if err != nil {
			delete(p.tracking, gossipID)
			p.addedTimeSum -= tracking.addedTime
			return err
		}

		gossip = append(gossip, bytes)
		sentBytes += len(bytes)
		toRegossip.PushRight(gossipable)
		tracking.lastGossiped = now
	}

	// If there is nothing to gossip, we can exit early.
	if len(gossip) == 0 {
		return nil
	}

	// Send gossipables to peers
	msgBytes, err := MarshalAppGossip(gossip)
	if err != nil {
		return err
	}

	if err := p.metrics.observeMessage(sentPushLabels, len(gossip), sentBytes); err != nil {
		return err
	}

	topValidatorsMetric, err := p.metrics.topValidators.GetMetricWith(metricsLabels)
	if err != nil {
		return fmt.Errorf("failed to get top validators metric: %w", err)
	}

	validatorsByStake := p.validators.Top(ctx, gossipParams.StakePercentage)
	topValidatorsMetric.Set(float64(len(validatorsByStake)))

	return p.client.AppGossip(
		ctx,
		common.SendConfig{
			NodeIDs:       set.Of(validatorsByStake...),
			Validators:    gossipParams.Validators,
			NonValidators: gossipParams.NonValidators,
			Peers:         gossipParams.Peers,
		},
		msgBytes,
	)
}

// Add enqueues new gossipables to be pushed. If a gossipable is already tracked,
// it is not added again.
func (p *PushGossiper[T]) Add(gossipables ...T) {
	var (
		now         = time.Now()
		nowUnixNano = float64(now.UnixNano())
	)

	p.lock.Lock()
	defer func() {
		p.updateMetrics(nowUnixNano)
		p.lock.Unlock()
	}()

	// Add new gossipables to be sent.
	for _, gossipable := range gossipables {
		gossipID := gossipable.GossipID()
		if _, ok := p.tracking[gossipID]; ok {
			continue
		}

		tracking := &tracking{
			addedTime: nowUnixNano,
		}
		if _, ok := p.discarded.Get(gossipID); ok {
			// Pretend that recently discarded transactions were just gossiped.
			tracking.lastGossiped = now
			p.toRegossip.PushRight(gossipable)
		} else {
			p.toGossip.PushRight(gossipable)
		}
		p.tracking[gossipID] = tracking
		p.addedTimeSum += nowUnixNano
	}
}

func (p *PushGossiper[_]) updateMetrics(nowUnixNano float64) {
	var (
		numUnsent       = float64(p.toGossip.Len())
		numSent         = float64(p.toRegossip.Len())
		numTracking     = numUnsent + numSent
		averageLifetime float64
	)
	if numTracking != 0 {
		averageLifetime = nowUnixNano - p.addedTimeSum/numTracking
	}

	p.metrics.tracking.With(unsentLabels).Set(numUnsent)
	p.metrics.tracking.With(sentLabels).Set(numSent)
	p.metrics.trackingLifetimeAverage.Set(averageLifetime)
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
