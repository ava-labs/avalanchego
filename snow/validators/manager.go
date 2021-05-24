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

	// MaskValidator hides the named validator from future samplings
	MaskValidator(ids.ShortID) error

	// RevealValidator ensures the named validator is not hidden from future
	// samplings
	RevealValidator(ids.ShortID) error
}

// NewManager returns a new, empty manager
func NewManager() Manager {
	return &manager{
		subnetToVdrs: make(map[ids.ID]Set),
	}
}

// manager implements Manager
type manager struct {
	lock sync.Mutex

	// Key: Subnet ID
	// Value: The validators that validate the subnet
	subnetToVdrs map[ids.ID]Set

	maskedVdrs ids.ShortSet
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

// AddWeight implements the Manager interface.
func (m *manager) AddWeight(subnetID ids.ID, vdrID ids.ShortID, weight uint64) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	vdrs, ok := m.subnetToVdrs[subnetID]
	if !ok {
		vdrs = NewSet()
		for _, maskedVdrID := range m.maskedVdrs.List() {
			if err := vdrs.MaskValidator(maskedVdrID); err != nil {
				return err
			}
		}
		m.subnetToVdrs[subnetID] = vdrs
	}
	return vdrs.AddWeight(vdrID, weight)
}

// RemoveValidatorSet implements the Manager interface.
func (m *manager) RemoveWeight(subnetID ids.ID, vdrID ids.ShortID, weight uint64) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if vdrs, ok := m.subnetToVdrs[subnetID]; ok {
		return vdrs.RemoveWeight(vdrID, weight)
	}
	return nil
}

// GetValidatorSet implements the Manager interface.
func (m *manager) GetValidators(subnetID ids.ID) (Set, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	vdrs, ok := m.subnetToVdrs[subnetID]
	return vdrs, ok
}

// MaskValidator implements the Manager interface.
func (m *manager) MaskValidator(vdrID ids.ShortID) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.maskedVdrs.Contains(vdrID) {
		return nil
	}
	m.maskedVdrs.Add(vdrID)

	for _, vdrs := range m.subnetToVdrs {
		if err := vdrs.MaskValidator(vdrID); err != nil {
			return err
		}
	}
	return nil
}

// RevealValidator implements the Manager interface.
func (m *manager) RevealValidator(vdrID ids.ShortID) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if !m.maskedVdrs.Contains(vdrID) {
		return nil
	}
	m.maskedVdrs.Remove(vdrID)

	for _, vdrs := range m.subnetToVdrs {
		if err := vdrs.RevealValidator(vdrID); err != nil {
			return err
		}
	}
	return nil
}
