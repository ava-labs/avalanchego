// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ validators.Connector = (*Network)(nil)
	_ common.AppHandler    = (*Network)(nil)
	_ NodeSampler          = (*peerSampler)(nil)
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
	metrics prometheus.Registerer,
	namespace string,
) *Network {
	return &Network{
		Peers:     &Peers{},
		log:       log,
		sender:    sender,
		metrics:   metrics,
		namespace: namespace,
		router:    newRouter(log, sender, metrics, namespace),
	}
}

// Network exposes networking state and supports building p2p application
// protocols
type Network struct {
	Peers *Peers

	log       logging.Logger
	sender    common.AppSender
	metrics   prometheus.Registerer
	namespace string

	router *router
}

func (n *Network) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	return n.router.AppRequest(ctx, nodeID, requestID, deadline, request)
}

func (n *Network) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return n.router.AppResponse(ctx, nodeID, requestID, response)
}

func (n *Network) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	return n.router.AppRequestFailed(ctx, nodeID, requestID)
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

func (n *Network) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	return n.router.CrossChainAppRequestFailed(ctx, chainID, requestID)
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
func (n *Network) NewClient(handlerID uint64, options ...ClientOption) (*Client, error) {
	appRequestFailedTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("client_%d_app_request_failed_time", handlerID),
		"app request failed time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register app request failed metric for client %d: %w", handlerID, err)
	}

	appResponseTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("client_%d_app_response_time", handlerID),
		"app response time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register app response metric for client %d: %w", handlerID, err)
	}

	crossChainAppRequestFailedTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("client_%d_cross_chain_app_request_failed_time", handlerID),
		"app request failed time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register cross-chain app request failed metric for client %d: %w", handlerID, err)
	}

	crossChainAppResponseTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("client_%d_cross_chain_app_response_time", handlerID),
		"cross chain app response time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register cross-chain app response metric for client %d: %w", handlerID, err)
	}

	client := &Client{
		handlerID:     handlerID,
		handlerPrefix: binary.AppendUvarint(nil, handlerID),
		sender:        n.sender,
		router:        n.router,
		options: &clientOptions{
			nodeSampler: &peerSampler{
				peers: n.Peers,
			},
		},
		appRequestFailedTime:           appRequestFailedTime,
		appResponseTime:                appResponseTime,
		crossChainAppRequestFailedTime: crossChainAppRequestFailedTime,
		crossChainAppResponseTime:      crossChainAppResponseTime,
	}

	for _, option := range options {
		option.apply(client.options)
	}

	return client, nil
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
