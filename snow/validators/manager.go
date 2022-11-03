// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Manager = (*manager)(nil)

// Manager holds the validator set of each subnet
type Manager interface {
	fmt.Stringer

	// Set a subnet's validator set
	Set(ids.ID, Set) error

	// AddWeight adds weight to a given validator on the given subnet
	AddWeight(ids.ID, ids.NodeID, uint64) error

	// RemoveWeight removes weight from a given validator on a given subnet
	RemoveWeight(ids.ID, ids.NodeID, uint64) error

	// GetValidators returns the validator set for the given subnet
	// Returns false if the subnet doesn't exist
	GetValidators(ids.ID) (Set, bool)

	// Contains returns true if there is a validator with the specified ID
	// currently in the set.
	Contains(ids.ID, ids.NodeID) bool
}

// NewManager returns a new, empty manager
func NewManager() Manager {
	return &manager{
		subnetToVdrs: make(map[ids.ID]Set),
	}
}

type manager struct {
	lock sync.RWMutex

	// Key: Subnet ID
	// Value: The validators that validate the subnet
	subnetToVdrs map[ids.ID]Set
}

func (m *manager) Set(subnetID ids.ID, newSet Set) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	oldSet, exists := m.subnetToVdrs[subnetID]
	if !exists {
		m.subnetToVdrs[subnetID] = newSet
		return nil
	}
	return oldSet.Set(newSet.List())
}

func (m *manager) AddWeight(subnetID ids.ID, vdrID ids.NodeID, weight uint64) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	vdrs, ok := m.subnetToVdrs[subnetID]
	if !ok {
		vdrs = NewSet()
		m.subnetToVdrs[subnetID] = vdrs
	}
	return vdrs.AddWeight(vdrID, weight)
}

func (m *manager) RemoveWeight(subnetID ids.ID, vdrID ids.NodeID, weight uint64) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if vdrs, ok := m.subnetToVdrs[subnetID]; ok {
		return vdrs.RemoveWeight(vdrID, weight)
	}
	return nil
}

func (m *manager) GetValidators(subnetID ids.ID) (Set, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	vdrs, ok := m.subnetToVdrs[subnetID]
	return vdrs, ok
}

func (m *manager) Contains(subnetID ids.ID, vdrID ids.NodeID) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()

	vdrs, ok := m.subnetToVdrs[subnetID]
	if ok {
		return vdrs.Contains(vdrID)
	}
	return false
}

func (m *manager) String() string {
	m.lock.RLock()
	defer m.lock.RUnlock()

	subnets := make([]ids.ID, 0, len(m.subnetToVdrs))
	for subnetID := range m.subnetToVdrs {
		subnets = append(subnets, subnetID)
	}
	ids.SortIDs(subnets)

	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("Validator Manager: (Size = %d)",
		len(subnets),
	))
	for _, subnetID := range subnets {
		vdrs := m.subnetToVdrs[subnetID]
		sb.WriteString(fmt.Sprintf(
			"\n    Subnet[%s]: %s",
			subnetID,
			vdrs.PrefixedString("    "),
		))
	}

	return sb.String()
}
