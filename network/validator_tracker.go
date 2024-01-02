// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ validators.SetCallbackListener = (*ValidatorTracker)(nil)

func NewValidatorTracker() *ValidatorTracker {
	return &ValidatorTracker{
		connected:                  make(map[ids.NodeID]*ips.ClaimedIPPort),
		connectedValidatorIndicies: make(map[ids.NodeID]int),
	}
}

type ValidatorTracker struct {
	lock                       sync.RWMutex
	connected                  map[ids.NodeID]*ips.ClaimedIPPort
	validators                 set.Set[ids.NodeID]
	connectedValidatorIndicies map[ids.NodeID]int
	connectedValidators        []*ips.ClaimedIPPort
}

func (v *ValidatorTracker) Connected(nodeID ids.NodeID, ip *ips.ClaimedIPPort) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.connected[nodeID] = ip
	if v.validators.Contains(nodeID) {
		v.addConnectedValidator(nodeID, ip)
	}
}

func (v *ValidatorTracker) Disconnected(nodeID ids.NodeID) *ips.ClaimedIPPort {
	v.lock.Lock()
	defer v.lock.Unlock()

	ip, ok := v.connected[nodeID]
	if !ok {
		return nil
	}

	delete(v.connected, nodeID)
	v.removeConnectedValidator(nodeID)
	return ip
}

func (v *ValidatorTracker) OnValidatorAdded(nodeID ids.NodeID, _ *bls.PublicKey, _ ids.ID, _ uint64) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.validators.Add(nodeID)
	if ip, connected := v.connected[nodeID]; connected {
		v.addConnectedValidator(nodeID, ip)
	}
}

func (*ValidatorTracker) OnValidatorWeightChanged(ids.NodeID, uint64, uint64) {}

func (v *ValidatorTracker) OnValidatorRemoved(nodeID ids.NodeID, _ uint64) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.validators.Remove(nodeID)
	v.removeConnectedValidator(nodeID)
}

func (v *ValidatorTracker) addConnectedValidator(nodeID ids.NodeID, ip *ips.ClaimedIPPort) {
	v.connectedValidatorIndicies[nodeID] = len(v.connectedValidators)
	v.connectedValidators = append(v.connectedValidators, ip)
}

func (v *ValidatorTracker) removeConnectedValidator(nodeID ids.NodeID) {
	connectedValidatorIndexToRemove, wasValidator := v.connectedValidatorIndicies[nodeID]
	if !wasValidator {
		return
	}

	newNumConnectedValidators := len(v.connectedValidators) - 1
	if newNumConnectedValidators != connectedValidatorIndexToRemove {
		replacementIP := v.connectedValidators[newNumConnectedValidators]
		replacementNodeID := replacementIP.NodeID()
		v.connectedValidatorIndicies[replacementNodeID] = connectedValidatorIndexToRemove
		v.connectedValidators[connectedValidatorIndexToRemove] = replacementIP
	}

	delete(v.connectedValidatorIndicies, nodeID)
	v.connectedValidators[newNumConnectedValidators] = nil
	v.connectedValidators = v.connectedValidators[:newNumConnectedValidators]
}

func (v *ValidatorTracker) GetValidatorIPs(except *gossip.BloomFilter, maxNumIPs int) []*ips.ClaimedIPPort {
	var (
		uniform = sampler.NewUniform()
		ips     = make([]*ips.ClaimedIPPort, 0, maxNumIPs)
	)

	v.lock.RLock()
	defer v.lock.RUnlock()

	uniform.Initialize(uint64(len(v.connectedValidators)))
	for len(ips) < maxNumIPs {
		index, err := uniform.Next()
		if err != nil {
			return ips
		}

		ip := v.connectedValidators[index]
		if !except.Has(ip) {
			ips = append(ips, ip)
		}
	}
	return ips
}
