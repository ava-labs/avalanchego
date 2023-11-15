// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	snowvalidators "github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ snowvalidators.Connector = (*Network)(nil)
	_ common.AppHandler        = (*Network)(nil)
	_ NodeSampler              = (*peers)(nil)
)

// ClientOption configures Client
type ClientOption interface {
	apply(options *ClientOptions)
}

type clientOptionFunc func(options *ClientOptions)

func (o clientOptionFunc) apply(options *ClientOptions) {
	o(options)
}

// WithPeerSampling configures Client.AppRequestAny to sample peers
func WithPeerSampling(network Network) ClientOption {
	return clientOptionFunc(func(options *ClientOptions) {
		options.NodeSampler = network.peers
	})
}

// WithValidatorSampling configures Client.AppRequestAny to sample validators
func WithValidatorSampling(validators *Validators) ClientOption {
	return clientOptionFunc(func(options *ClientOptions) {
		options.NodeSampler = validators
	})
}

// ClientOptions holds client-configurable values
type ClientOptions struct {
	// NodeSampler is used to select nodes to route Client.AppRequestAny to
	NodeSampler NodeSampler
}

// NewNetwork returns an instance of Network
func NewNetwork(
	log logging.Logger,
	sender common.AppSender,
	metrics prometheus.Registerer,
	namespace string,
) *Network {
	return &Network{
		log:       log,
		sender:    sender,
		metrics:   metrics,
		namespace: namespace,
		router:    newRouter(log),
		peers:     &peers{},
	}
}

// Network maintains state of the peer-to-peer network and any in-flight
// requests
type Network struct {
	log       logging.Logger
	sender    common.AppSender
	metrics   prometheus.Registerer
	namespace string

	*router
	peers *peers
}

func (n *Network) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.peers.set.Add(nodeID)
	return nil
}

func (n *Network) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	n.lock.Lock()
	defer n.lock.Unlock()

	n.peers.set.Remove(nodeID)
	return nil
}

// RegisterAppProtocol reserves an identifier for an application protocol and
// returns a Client that can be used to send messages for the corresponding
// protocol.
func (n *Network) RegisterAppProtocol(handlerID uint64, handler Handler, options ...ClientOption) (*Client, error) {
	// TODO refactor router
	n.router.lock.Lock()
	defer n.router.lock.Unlock()

	if _, ok := n.router.handlers[handlerID]; ok {
		return nil, fmt.Errorf("failed to register handler id %d: %w", handlerID, ErrExistingAppProtocol)
	}

	appRequestTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("handler_%d_app_request", handlerID),
		"app request time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register app request metric for handler_%d: %w", handlerID, err)
	}

	appRequestFailedTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("handler_%d_app_request_failed", handlerID),
		"app request failed time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register app request failed metric for handler_%d: %w", handlerID, err)
	}

	appResponseTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("handler_%d_app_response", handlerID),
		"app response time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register app response metric for handler_%d: %w", handlerID, err)
	}

	appGossipTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("handler_%d_app_gossip", handlerID),
		"app gossip time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register app gossip metric for handler_%d: %w", handlerID, err)
	}

	crossChainAppRequestTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("handler_%d_cross_chain_app_request", handlerID),
		"cross chain app request time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register cross-chain app request metric for handler_%d: %w", handlerID, err)
	}

	crossChainAppRequestFailedTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("handler_%d_cross_chain_app_request_failed", handlerID),
		"app request failed time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register cross-chain app request failed metric for handler_%d: %w", handlerID, err)
	}

	crossChainAppResponseTime, err := metric.NewAverager(
		n.namespace,
		fmt.Sprintf("handler_%d_cross_chain_app_response", handlerID),
		"cross chain app response time (ns)",
		n.metrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register cross-chain app response metric for handler_%d: %w", handlerID, err)
	}

	n.router.handlers[handlerID] = &meteredHandler{
		responder: &responder{
			handlerID: handlerID,
			handler:   handler,
			log:       n.log,
			sender:    n.sender,
		},
		metrics: &metrics{
			appRequestTime:                 appRequestTime,
			appRequestFailedTime:           appRequestFailedTime,
			appResponseTime:                appResponseTime,
			appGossipTime:                  appGossipTime,
			crossChainAppRequestTime:       crossChainAppRequestTime,
			crossChainAppRequestFailedTime: crossChainAppRequestFailedTime,
			crossChainAppResponseTime:      crossChainAppResponseTime,
		},
	}

	clientOptions := &ClientOptions{
		NodeSampler: n.peers,
	}

	for _, option := range options {
		option.apply(clientOptions)
	}

	return &Client{
		handlerID:     handlerID,
		handlerPrefix: binary.AppendUvarint(nil, handlerID),
		sender:        n.sender,
		router:        n.router,
		options:       clientOptions,
	}, nil
}

type peers struct {
	lock sync.RWMutex
	set  set.SampleableSet[ids.NodeID]
}

func (p *peers) has(nodeID ids.NodeID) bool {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.set.Contains(nodeID)
}

// Sample returns a pseudo-random sample of up to limit peers
func (p *peers) Sample(_ context.Context, limit int) []ids.NodeID {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.set.Sample(limit)
}
