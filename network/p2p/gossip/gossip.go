// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	typeLabel = "type"
	pushType  = "push"
	pullType  = "pull"
)

var (
	_ Gossiper = (*ValidatorGossiper)(nil)
	_ Gossiper = (*PullGossiper[testTx, *testTx])(nil)
	_ Gossiper = (*NoOpGossiper)(nil)
	_ Gossiper = (*TestGossiper)(nil)

	_ Accumulator[*testTx] = (*PushGossiper[*testTx])(nil)
	_ Accumulator[*testTx] = (*NoOpAccumulator[*testTx])(nil)
	_ Accumulator[*testTx] = (*TestAccumulator[*testTx])(nil)

	metricLabels = []string{typeLabel}
)

// Gossiper gossips Gossipables to other nodes
type Gossiper interface {
	// Gossip runs a cycle of gossip. Returns an error if we failed to gossip.
	Gossip(ctx context.Context) error
}

// Accumulator allows a caller to accumulate gossipables to be gossiped
type Accumulator[T Gossipable] interface {
	Gossiper
	// Add queues gossipables to be gossiped
	Add(gossipables ...T)
}

// GossipableAny exists to help create non-nil pointers to a concrete Gossipable
// ref: https://stackoverflow.com/questions/69573113/how-can-i-instantiate-a-non-nil-pointer-of-type-argument-with-generic-go
type GossipableAny[T any] interface {
	*T
	Gossipable
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

func NewPullGossiper[T any, U GossipableAny[T]](
	log logging.Logger,
	set Set[U],
	client *p2p.Client,
	metrics Metrics,
	pollSize int,
) *PullGossiper[T, U] {
	return &PullGossiper[T, U]{
		log:      log,
		set:      set,
		client:   client,
		metrics:  metrics,
		pollSize: pollSize,
		labels: prometheus.Labels{
			typeLabel: pullType,
		},
	}
}

type PullGossiper[T any, U GossipableAny[T]] struct {
	log      logging.Logger
	set      Set[U]
	client   *p2p.Client
	metrics  Metrics
	pollSize int
	labels   prometheus.Labels
}

func (p *PullGossiper[_, _]) Gossip(ctx context.Context) error {
	bloom, salt, err := p.set.GetFilter()
	if err != nil {
		return err
	}

	request := &sdk.PullGossipRequest{
		Filter: bloom,
		Salt:   salt,
	}
	msgBytes, err := proto.Marshal(request)
	if err != nil {
		return err
	}

	for i := 0; i < p.pollSize; i++ {
		if err := p.client.AppRequestAny(ctx, msgBytes, p.handleResponse); err != nil {
			return err
		}
	}

	return nil
}

func (p *PullGossiper[T, U]) handleResponse(
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

	response := &sdk.PullGossipResponse{}
	if err := proto.Unmarshal(responseBytes, response); err != nil {
		p.log.Debug("failed to unmarshal gossip response", zap.Error(err))
		return
	}

	receivedBytes := 0
	for _, bytes := range response.Gossip {
		receivedBytes += len(bytes)

		gossipable := U(new(T))
		if err := gossipable.Unmarshal(bytes); err != nil {
			p.log.Debug(
				"failed to unmarshal gossip",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			continue
		}

		hash := gossipable.GetID()
		p.log.Debug(
			"received gossip",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("id", hash),
		)
		if err := p.set.Add(gossipable); err != nil {
			p.log.Debug(
				"failed to add gossip to the known set",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("id", hash),
				zap.Error(err),
			)
			continue
		}
	}

	receivedCountMetric, err := p.metrics.receivedCount.GetMetricWith(p.labels)
	if err != nil {
		p.log.Error("failed to get received count metric", zap.Error(err))
		return
	}

	receivedBytesMetric, err := p.metrics.receivedBytes.GetMetricWith(p.labels)
	if err != nil {
		p.log.Error("failed to get received bytes metric", zap.Error(err))
		return
	}

	receivedCountMetric.Add(float64(len(response.Gossip)))
	receivedBytesMetric.Add(float64(receivedBytes))
}

// NewPushGossiper returns an instance of PushGossiper
func NewPushGossiper[T Gossipable](client *p2p.Client, metrics Metrics, targetGossipSize int) *PushGossiper[T] {
	return &PushGossiper[T]{
		client:           client,
		metrics:          metrics,
		targetGossipSize: targetGossipSize,
		labels: prometheus.Labels{
			typeLabel: pushType,
		},
		pending: buffer.NewUnboundedDeque[T](0),
	}
}

// PushGossiper broadcasts gossip to peers randomly in the network
type PushGossiper[T Gossipable] struct {
	client           *p2p.Client
	metrics          Metrics
	targetGossipSize int

	labels prometheus.Labels

	lock    sync.Mutex
	pending buffer.Deque[T]
}

// Gossip flushes any queued gossipables
func (p *PushGossiper[T]) Gossip(ctx context.Context) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.pending.Len() == 0 {
		return nil
	}

	msg := &sdk.PushGossip{
		Gossip: make([][]byte, 0, p.pending.Len()),
	}

	sentBytes := 0
	for sentBytes < p.targetGossipSize {
		gossipable, ok := p.pending.PeekLeft()
		if !ok {
			break
		}

		bytes, err := gossipable.Marshal()
		if err != nil {
			// remove this item so we don't get stuck in a loop
			_, _ = p.pending.PopLeft()
			return err
		}

		msg.Gossip = append(msg.Gossip, bytes)
		sentBytes += len(bytes)
		p.pending.PopLeft()
	}

	msgBytes, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	sentCountMetric, err := p.metrics.sentCount.GetMetricWith(p.labels)
	if err != nil {
		return fmt.Errorf("failed to get sent count metric: %w", err)
	}

	sentBytesMetric, err := p.metrics.sentBytes.GetMetricWith(p.labels)
	if err != nil {
		return fmt.Errorf("failed to get sent bytes metric: %w", err)
	}

	sentCountMetric.Add(float64(len(msg.Gossip)))
	sentBytesMetric.Add(float64(sentBytes))

	return p.client.AppGossip(ctx, msgBytes)
}

func (p *PushGossiper[T]) Add(gossipables ...T) {
	p.lock.Lock()
	defer p.lock.Unlock()

	for _, gossipable := range gossipables {
		p.pending.PushRight(gossipable)
	}
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

type NoOpAccumulator[T Gossipable] struct{}

func (NoOpAccumulator[_]) Gossip(context.Context) error {
	return nil
}

func (NoOpAccumulator[T]) Add(...T) {}

type TestGossiper struct {
	GossipF func(ctx context.Context) error
}

func (t *TestGossiper) Gossip(ctx context.Context) error {
	return t.GossipF(ctx)
}

type TestAccumulator[T Gossipable] struct {
	GossipF func(ctx context.Context) error
	AddF    func(...T)
}

func (t TestAccumulator[T]) Gossip(ctx context.Context) error {
	if t.GossipF == nil {
		return nil
	}

	return t.GossipF(ctx)
}

func (t TestAccumulator[T]) Add(gossipables ...T) {
	if t.AddF == nil {
		return
	}

	t.AddF(gossipables...)
}
