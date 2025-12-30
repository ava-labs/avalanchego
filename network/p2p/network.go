// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"encoding/binary"
	"errors"
	"strconv"
	"sync"
	"time"

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
	_ NodeSampler          = (*PeerSampler)(nil)
	_ ConnectionHandler    = (*Peers)(nil)

	opLabel      = "op"
	handlerLabel = "handlerID"
	labelNames   = []string{opLabel, handlerLabel}
)

// ConnectionHandler handles peer connection events
type ConnectionHandler interface {
	// Connected is called when we connect to nodeID
	Connected(nodeID ids.NodeID)
	// Disconnected is called when we disconnect from nodeID
	Disconnected(nodeID ids.NodeID)
}

// NewNetwork returns an instance of Network
func NewNetwork(
	log logging.Logger,
	sender common.AppSender,
	registerer prometheus.Registerer,
	namespace string,
	connectionHandlers ...ConnectionHandler,
) (*Network, error) {
	metrics := metrics{
		msgTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "msg_time",
				Help:      "message handling time (ns)",
			},
			labelNames,
		),
		msgCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "msg_count",
				Help:      "message count (n)",
			},
			labelNames,
		),
	}

	err := errors.Join(
		registerer.Register(metrics.msgTime),
		registerer.Register(metrics.msgCount),
	)
	if err != nil {
		return nil, err
	}

	return &Network{
		sender:             sender,
		connectionHandlers: connectionHandlers,
		router:             newRouter(log, sender, metrics),
	}, nil
}

// Network exposes networking state and supports building p2p application
// protocols
type Network struct {
	sender             common.AppSender
	connectionHandlers []ConnectionHandler

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

func (n *Network) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	for _, c := range n.connectionHandlers {
		c.Connected(nodeID)
	}

	return nil
}

func (n *Network) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	for _, c := range n.connectionHandlers {
		c.Disconnected(nodeID)
	}

	return nil
}

// NewClient returns a Client that can be used to send messages for the
// corresponding protocol.
func (n *Network) NewClient(handlerID uint64, nodeSampler NodeSampler) *Client {
	return &Client{
		handlerIDStr:  strconv.FormatUint(handlerID, 10),
		handlerPrefix: ProtocolPrefix(handlerID),
		sender:        n.sender,
		router:        n.router,
		nodeSampler:   nodeSampler,
	}
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

func (p *Peers) Connected(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.set.Add(nodeID)
}

func (p *Peers) Disconnected(nodeID ids.NodeID) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.set.Remove(nodeID)
}

func (p *Peers) Has(nodeID ids.NodeID) bool {
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

// PeerSampler implements NodeSampler
type PeerSampler struct {
	Peers *Peers
}

func (p PeerSampler) Sample(_ context.Context, limit int) []ids.NodeID {
	return p.Peers.Sample(limit)
}

func ProtocolPrefix(handlerID uint64) []byte {
	return binary.AppendUvarint(nil, handlerID)
}
