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
	_ Gossiper = (*PullGossiper[*testTx])(nil)
	_ Gossiper = (*NoOpGossiper)(nil)
	_ Gossiper = (*TestGossiper)(nil)

	_ Accumulator[*testTx] = (*PushGossiper[*testTx])(nil)
	_ Accumulator[*testTx] = (*NoOpAccumulator[*testTx])(nil)
	_ Accumulator[*testTx] = (*TestAccumulator[*testTx])(nil)

	metricLabels = []string{typeLabel}
	pushLabels   = prometheus.Labels{
		typeLabel: pushType,
	}
	pullLabels = prometheus.Labels{
		typeLabel: pullType,
	}
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
	bloom, salt := p.set.GetFilter()
	request := &sdk.PullGossipRequest{
		Filter: bloom,
		Salt:   salt,
	}
	msgBytes, err := proto.Marshal(request)
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

	response := &sdk.PullGossipResponse{}
	if err := proto.Unmarshal(responseBytes, response); err != nil {
		p.log.Debug("failed to unmarshal gossip response", zap.Error(err))
		return
	}

	receivedBytes := 0
	for _, bytes := range response.Gossip {
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

		hash := gossipable.GossipID()
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

	receivedCountMetric.Add(float64(len(response.Gossip)))
	receivedBytesMetric.Add(float64(receivedBytes))
}

// NewPushGossiper returns an instance of PushGossiper
func NewPushGossiper[T Gossipable](marshaller Marshaller[T], client *p2p.Client, metrics Metrics, targetGossipSize int) *PushGossiper[T] {
	return &PushGossiper[T]{
		marshaller:       marshaller,
		client:           client,
		metrics:          metrics,
		targetGossipSize: targetGossipSize,
		pending:          buffer.NewUnboundedDeque[T](0),
	}
}

// PushGossiper broadcasts gossip to peers randomly in the network
type PushGossiper[T Gossipable] struct {
	marshaller       Marshaller[T]
	client           *p2p.Client
	metrics          Metrics
	targetGossipSize int

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

		bytes, err := p.marshaller.MarshalGossip(gossipable)
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

	sentCountMetric, err := p.metrics.sentCount.GetMetricWith(pushLabels)
	if err != nil {
		return fmt.Errorf("failed to get sent count metric: %w", err)
	}

	sentBytesMetric, err := p.metrics.sentBytes.GetMetricWith(pushLabels)
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
