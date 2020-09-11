// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

// Manager holds the validator set of each subnet
type Manager interface {
	// Set a subnet's validator set
	Set(ids.ID, Set) error

	// AddWeight adds weight to a given validator on the given subnet
	AddWeight(ids.ID, ids.ShortID, uint64) error

	// RemoveWeight removes weight from a given validator on a given subnet
	RemoveWeight(ids.ID, ids.ShortID, uint64) error

	// GetValidators returns the validator set for the given subnet
	// Returns false if the subnet doesn't exist
	GetValidators(ids.ID) (Set, bool)
}

// NewManager returns a new, empty manager
func NewManager() Manager {
	return &manager{
		subnetToVdrs: make(map[[32]byte]Set),
	}
}

// manager implements Manager
type manager struct {
	lock sync.Mutex
	// Key: Subnet ID
	// Value: The validators that validate the subnet
	subnetToVdrs map[[32]byte]Set
}

func (m *manager) Set(subnetID ids.ID, newSet Set) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	subnetKey := subnetID.Key()

	oldSet, exists := m.subnetToVdrs[subnetKey]
	if !exists {
		m.subnetToVdrs[subnetKey] = newSet
		return nil
	}
	return oldSet.Set(newSet.List())
}

// AddWeight implements the Manager interface.
func (m *manager) AddWeight(subnetID ids.ID, vdrID ids.ShortID, weight uint64) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	subnetIDKey := subnetID.Key()

	vdrs, ok := m.subnetToVdrs[subnetIDKey]
	if !ok {
		vdrs = NewSet()
		m.subnetToVdrs[subnetIDKey] = vdrs
	}
	return vdrs.AddWeight(vdrID, weight)
}

// RemoveValidatorSet implements the Manager interface.
func (m *manager) RemoveWeight(subnetID ids.ID, vdrID ids.ShortID, weight uint64) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if vdrs, ok := m.subnetToVdrs[subnetID.Key()]; ok {
		return vdrs.RemoveWeight(vdrID, weight)
	}
	return nil
}

// GetValidatorSet implements the Manager interface.
func (m *manager) GetValidators(subnetID ids.ID) (Set, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	vdrs, ok := m.subnetToVdrs[subnetID.Key()]
	return vdrs, ok
}
