// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"encoding/binary"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ validators.Connector = (*Network)(nil)
	_ common.AppHandler    = (*Network)(nil)
	_ NodeSampler          = (*peerSampler)(nil)

	handlerLabel = "handlerID"
	labelNames   = []string{handlerLabel}
)

// ClientOption configures Client
type ClientOption interface {
	apply(options *clientOptions)
}

type clientOptionFunc func(options *clientOptions)

func (o clientOptionFunc) apply(options *clientOptions) {
	o(options)
}

// WithValidatorSampling configures Client.AppRequestAny to sample validators
func WithValidatorSampling(validators *Validators) ClientOption {
	return clientOptionFunc(func(options *clientOptions) {
		options.nodeSampler = validators
	})
}

// clientOptions holds client-configurable values
type clientOptions struct {
	// nodeSampler is used to select nodes to route Client.AppRequestAny to
	nodeSampler NodeSampler
}

// NewNetwork returns an instance of Network
func NewNetwork(
	log logging.Logger,
	sender common.AppSender,
	registerer prometheus.Registerer,
	namespace string,
) (*Network, error) {
	metrics := metrics{
		appRequestTime: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "app_request_time",
			Help:      "app request time (ns)",
		}, labelNames),
		appRequestCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "app_request_count",
			Help:      "app request count (n)",
		}, labelNames),
		appResponseTime: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "app_response_time",
			Help:      "app response time (ns)",
		}, labelNames),
		appResponseCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "app_response_count",
			Help:      "app response count (n)",
		}, labelNames),
		appRequestFailedTime: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "app_request_failed_time",
			Help:      "app request failed time (ns)",
		}, labelNames),
		appRequestFailedCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "app_request_failed_count",
			Help:      "app request failed count (ns)",
		}, labelNames),
		appGossipTime: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "app_gossip_time",
			Help:      "app gossip time (ns)",
		}, labelNames),
		appGossipCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "app_gossip_count",
			Help:      "app gossip count (n)",
		}, labelNames),
		crossChainAppRequestTime: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cross_chain_app_request_time",
			Help:      "cross chain app request time (ns)",
		}, labelNames),
		crossChainAppRequestCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cross_chain_app_request_count",
			Help:      "cross chain app request count (n)",
		}, labelNames),
		crossChainAppResponseTime: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cross_chain_app_response_time",
			Help:      "cross chain app response time (ns)",
		}, labelNames),
		crossChainAppResponseCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cross_chain_app_response_count",
			Help:      "cross chain app response count (n)",
		}, labelNames),
		crossChainAppRequestFailedTime: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cross_chain_app_request_failed_time",
			Help:      "cross chain app request failed time (ns)",
		}, labelNames),
		crossChainAppRequestFailedCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cross_chain_app_request_failed_count",
			Help:      "cross chain app request failed count (n)",
		}, labelNames),
	}

	err := utils.Err(
		registerer.Register(metrics.appRequestTime),
		registerer.Register(metrics.appRequestCount),
		registerer.Register(metrics.appResponseTime),
		registerer.Register(metrics.appResponseCount),
		registerer.Register(metrics.appRequestFailedTime),
		registerer.Register(metrics.appRequestFailedCount),
		registerer.Register(metrics.appGossipTime),
		registerer.Register(metrics.appGossipCount),
		registerer.Register(metrics.crossChainAppRequestTime),
		registerer.Register(metrics.crossChainAppRequestCount),
		registerer.Register(metrics.crossChainAppResponseTime),
		registerer.Register(metrics.crossChainAppResponseCount),
		registerer.Register(metrics.crossChainAppRequestFailedTime),
		registerer.Register(metrics.crossChainAppRequestFailedCount),
	)
	if err != nil {
		return nil, err
	}

	return &Network{
		Peers:  &Peers{},
		log:    log,
		sender: sender,
		router: newRouter(log, sender, metrics),
	}, nil
}

// Network exposes networking state and supports building p2p application
// protocols
type Network struct {
	Peers *Peers

	log    logging.Logger
	sender common.AppSender

	router *router
}

func (n *Network) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	return n.router.AppRequest(ctx, nodeID, requestID, deadline, request)
}

func (n *Network) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return n.router.AppResponse(ctx, nodeID, requestID, response)
}

func (n *Network) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	return n.router.AppRequestFailed(ctx, nodeID, requestID, appErr)
}

func (n *Network) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return n.router.AppGossip(ctx, nodeID, msg)
}

func (n *Network) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	return n.router.CrossChainAppRequest(ctx, chainID, requestID, deadline, request)
}

func (n *Network) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	return n.router.CrossChainAppResponse(ctx, chainID, requestID, response)
}

func (n *Network) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32, appErr *common.AppError) error {
	return n.router.CrossChainAppRequestFailed(ctx, chainID, requestID, appErr)
}

func (n *Network) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	n.Peers.add(nodeID)
	return nil
}

func (n *Network) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	n.Peers.remove(nodeID)
	return nil
}

// NewClient returns a Client that can be used to send messages for the
// corresponding protocol.
func (n *Network) NewClient(handlerID uint64, options ...ClientOption) *Client {
	client := &Client{
		handlerID:     handlerID,
		handlerIDStr:  strconv.FormatUint(handlerID, 10),
		handlerPrefix: binary.AppendUvarint(nil, handlerID),
		sender:        n.sender,
		router:        n.router,
		options: &clientOptions{
			nodeSampler: &peerSampler{
				peers: n.Peers,
			},
		},
	}

	for _, option := range options {
		option.apply(client.options)
	}

	return client
}

// AddHandler reserves an identifier for an application protocol
func (n *Network) AddHandler(handlerID uint64, handler Handler) error {
	return n.router.addHandler(handlerID, handler)
}

// Peers contains metadata about the current set of connected peers
type Peers struct {
	lock sync.RWMutex
	set  set.SampleableSet[ids.NodeID]
}

func (p *Peers) add(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.set.Add(nodeID)
}

func (p *Peers) remove(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.set.Remove(nodeID)
}

func (p *Peers) has(nodeID ids.NodeID) bool {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.set.Contains(nodeID)
}

// Sample returns a pseudo-random sample of up to limit Peers
func (p *Peers) Sample(limit int) []ids.NodeID {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.set.Sample(limit)
}

type peerSampler struct {
	peers *Peers
}

func (p peerSampler) Sample(_ context.Context, limit int) []ids.NodeID {
	return p.peers.Sample(limit)
}
