// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"sync"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var _ Manager = (*manager)(nil)

// Manager provides an interface for a benchlist to register whether
// queries have been successful or unsuccessful and place validators with
// consistently failing queries on a benchlist to prevent waiting up to
// the full network timeout for their responses.
type Manager interface {
	// RegisterResponse registers that we receive a request response from
	// [nodeID] regarding [chainID] within the timeout
	RegisterResponse(chainID ids.ID, nodeID ids.NodeID)
	// RegisterFailure registers that a request to [nodeID] regarding
	// [chainID] timed out
	RegisterFailure(chainID ids.ID, nodeID ids.NodeID)

	// RegisterChain registers a new chain with metrics under [namespace]
	RegisterChain(ctx *snow.ConsensusContext) error
	// IsBenched returns true if messages to [nodeID] regarding chain [chainID]
	// should not be sent over the network and should immediately fail.
	// Returns false if such messages should be sent, or if the chain is unknown.
	IsBenched(chainID ids.ID, nodeID ids.NodeID) bool
	// GetBenched returns an array of chainIDs where the specified
	// [nodeID] is benched. If called on an id.ShortID that does
	// not map to a validator, it will return an empty array.
	GetBenched(nodeID ids.NodeID) []ids.ID
}

type manager struct {
	benchable Benchable
	vdrs      validators.Manager
	reg       metrics.MultiGatherer
	config    Config

	lock   sync.RWMutex
	chains map[ids.ID]*benchlist
}

// NewManager returns a manager for chain-specific query benchlisting
func NewManager(
	benchable Benchable,
	vdrs validators.Manager,
	reg metrics.MultiGatherer,
	config Config,
) Manager {
	if config.MaxPortion <= 0 {
		return NewNoBenchlist()
	}

	return &manager{
		benchable: benchable,
		vdrs:      vdrs,
		reg:       reg,
		config:    config,
		chains:    make(map[ids.ID]*benchlist),
	}
}

// IsBenched returns true if messages to [nodeID] regarding [chainID]
// should not be sent over the network and should immediately fail.
func (m *manager) IsBenched(chainID ids.ID, nodeID ids.NodeID) bool {
	m.lock.RLock()
	benchlist, ok := m.chains[chainID]
	m.lock.RUnlock()
	return ok && benchlist.IsBenched(nodeID)
}

// GetBenched returns an array of chainIDs where the specified
// [nodeID] is benched. If called on an id.ShortID that does
// not map to a validator, it will return an empty array.
func (m *manager) GetBenched(nodeID ids.NodeID) []ids.ID {
	m.lock.RLock()
	defer m.lock.RUnlock()

	chainIDs := []ids.ID{}
	for chainID, benchlist := range m.chains {
		if benchlist.IsBenched(nodeID) {
			chainIDs = append(chainIDs, chainID)
		}
	}
	return chainIDs
}

func (m *manager) RegisterChain(ctx *snow.ConsensusContext) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, exists := m.chains[ctx.ChainID]; exists {
		return nil
	}

	reg, err := metrics.MakeAndRegister(m.reg, ctx.PrimaryAlias)
	if err != nil {
		return err
	}

	benchlist, err := newBenchlist(ctx, m.benchable, m.vdrs, m.config, reg)
	if err != nil {
		return err
	}

	m.chains[ctx.ChainID] = benchlist
	return nil
}

func (m *manager) RegisterResponse(chainID ids.ID, nodeID ids.NodeID) {
	m.lock.RLock()
	benchlist, ok := m.chains[chainID]
	m.lock.RUnlock()
	if ok {
		benchlist.RegisterResponse(nodeID)
	}
}

func (m *manager) RegisterFailure(chainID ids.ID, nodeID ids.NodeID) {
	m.lock.RLock()
	benchlist, ok := m.chains[chainID]
	m.lock.RUnlock()
	if ok {
		benchlist.RegisterFailure(nodeID)
	}
}

type noBenchlist struct{}

// NewNoBenchlist returns an empty benchlist that will never stop any queries
func NewNoBenchlist() Manager {
	return &noBenchlist{}
}

func (noBenchlist) RegisterChain(*snow.ConsensusContext) error {
	return nil
}

func (noBenchlist) RegisterResponse(ids.ID, ids.NodeID) {}

func (noBenchlist) RegisterFailure(ids.ID, ids.NodeID) {}

func (noBenchlist) IsBenched(ids.ID, ids.NodeID) bool {
	return false
}

func (noBenchlist) GetBenched(ids.NodeID) []ids.ID {
	return []ids.ID{}
}
