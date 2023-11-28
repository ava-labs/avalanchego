// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var (
	_ validators.Connector = (*Network)(nil)
	_ common.AppHandler    = (*Network)(nil)
	_ NodeSampler          = (*peers)(nil)
)

// ClientOption configures Client
type ClientOption interface {
	apply(options *clientOptions)
}

type clientOptionFunc func(options *clientOptions)

func (o clientOptionFunc) apply(options *clientOptions) {
	o(options)
}

// WithPeerSampling configures Client.AppRequestAny to sample peers
func WithPeerSampling(network *Network) ClientOption {
	return clientOptionFunc(func(options *clientOptions) {
		options.nodeSampler = network.peers
	})
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
	peers := &peers{}
	clientDefaults := &clientOptions{
		nodeSampler: peers,
	}

	return &Network{
		log:       log,
		sender:    sender,
		metrics:   metrics,
		namespace: namespace,
		router:    newRouter(log, sender, metrics, namespace, clientDefaults),
		peers:     peers,
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
	//TODO dont need this lock
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

func (n *Network) NewAppProtocol(handlerID uint64, handler Handler, options ...ClientOption) (*Client, error) {
	client, err := n.router.registerAppProtocol(handlerID, handler)
	if err != nil {
		return nil, err
	}

	for _, option := range options {
		option.apply(client.options)
	}

	return client, nil
}

// peers contains the set of nodes we are connected to
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
