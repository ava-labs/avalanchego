// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"sync"

	"github.com/ava-labs/gecko/ids"
)

// Manager holds the validator set of each subnet
type Manager interface {
	// PutValidatorSet puts associaties the given subnet ID with the given validator set
	PutValidatorSet(ids.ID, Set)

	// RemoveValidatorSet removes the specified validator set
	RemoveValidatorSet(ids.ID)

	// GetGroup returns:
	// 1) the validator set of the subnet with the specified ID
	// 2) false if there is no subnet with the specified ID
	GetValidatorSet(ids.ID) (Set, bool)
}

// NewManager returns a new, empty manager
func NewManager() Manager {
	return &manager{
		validatorSets: make(map[[32]byte]Set),
	}
}

// manager implements Manager
type manager struct {
	lock          sync.Mutex
	validatorSets map[[32]byte]Set
}

// PutValidatorSet implements the Manager interface.
func (m *manager) PutValidatorSet(subnetID ids.ID, set Set) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.validatorSets[subnetID.Key()] = set
}

// RemoveValidatorSet implements the Manager interface.
func (m *manager) RemoveValidatorSet(subnetID ids.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	delete(m.validatorSets, subnetID.Key())
}

// GetValidatorSet implements the Manager interface.
func (m *manager) GetValidatorSet(subnetID ids.ID) (Set, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	set, exists := m.validatorSets[subnetID.Key()]
	return set, exists
}
