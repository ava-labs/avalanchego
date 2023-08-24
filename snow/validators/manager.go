// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"golang.org/x/exp/maps"
)

var (
	_ Manager = (*manager)(nil)

	ErrMissingValidators = errors.New("missing validators")
)

type SetCallbackListener interface {
	OnValidatorAdded(validatorID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64)
	OnValidatorRemoved(validatorID ids.NodeID, weight uint64)
	OnValidatorWeightChanged(validatorID ids.NodeID, oldWeight, newWeight uint64)
}

// Manager holds the validator set of each subnet
type Manager interface {
	fmt.Stringer

	// Add a new staker to the subnet.
	// Returns an error if:
	// - [weight] is 0
	// - [nodeID] is already in the validator set
	// - the total weight of the validator set would overflow uint64
	// If an error is returned, the set will be unmodified.
	AddStaker(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error

	// AddWeight to an existing staker to the subnet.
	// Returns an error if:
	// - [weight] is 0
	// - [nodeID] is not already in the validator set
	// - the total weight of the validator set would overflow uint64
	// If an error is returned, the set will be unmodified.
	AddWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error

	// GetWeight retrieves the validator weight from the subnet.
	GetWeight(subnetID ids.ID, validatorID ids.NodeID) uint64

	// GetValidator returns the validator tied to the specified ID in subnet.
	GetValidator(subnetID ids.ID, validatorID ids.NodeID) (*Validator, bool)

	// GetValidatoIDs returns the validator IDs in the subnet.
	GetValidatorIDs(subnetID ids.ID) ([]ids.NodeID, error)

	// SubsetWeight returns the sum of the weights of the validators in the subnet.
	SubsetWeight(subnetID ids.ID, validatorIDs set.Set[ids.NodeID]) (uint64, error)

	// RemoveWeight from a staker in the subnet. If the staker's weight becomes 0, the staker
	// will be removed from the subnet set.
	// Returns an error if:
	// - [weight] is 0
	// - [nodeID] is not already in the subnet set
	// - the weight of the validator would become negative
	// If an error is returned, the set will be unmodified.
	RemoveWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error

	// Contains returns true if there is a validator with the specified ID
	// currently in the subnet.
	Contains(subnetID ids.ID, validatorID ids.NodeID) bool

	// Len returns the number of validators currently in the subnet.
	Len(subnetID ids.ID) int

	// Weight returns the cumulative weight of all validators in the subnet.
	Weight(subnetID ids.ID) (uint64, error)

	// Sample returns a collection of validatorIDs in the subnet, potentially with duplicates.
	// If sampling the requested size isn't possible, an error will be returned.
	Sample(subnetID ids.ID, size int) ([]ids.NodeID, error)

	// Map of the validators in this subnet
	GetMap(subnetID ids.ID) map[ids.NodeID]*GetValidatorOutput

	// When a validator's weight changes, or a validator is added/removed,
	// this listener is called.
	RegisterCallbackListener(subnetID ids.ID, listener SetCallbackListener)
}

// NewManager returns a new, empty manager
func NewManager() Manager {
	return &manager{
		subnetToVdrs: make(map[ids.ID]*vdrSet),
	}
}

type manager struct {
	lock sync.RWMutex

	// Key: Subnet ID
	// Value: The validators that validate the subnet
	subnetToVdrs map[ids.ID]*vdrSet
}

func (m *manager) AddStaker(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		set = newSet()
		m.subnetToVdrs[subnetID] = set
	}

	return set.Add(nodeID, pk, txID, weight)
}

func (m *manager) AddWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		return errMissingValidator
	}

	return set.AddWeight(nodeID, weight)
}

func (m *manager) GetWeight(subnetID ids.ID, validatorID ids.NodeID) uint64 {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		return 0
	}

	return set.GetWeight(validatorID)
}

func (m *manager) GetValidator(subnetID ids.ID, validatorID ids.NodeID) (*Validator, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		return nil, false
	}

	return set.Get(validatorID)
}

func (m *manager) SubsetWeight(subnetID ids.ID, validatorIDs set.Set[ids.NodeID]) (uint64, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		return 0, nil
	}

	return set.SubsetWeight(validatorIDs)
}

func (m *manager) RemoveWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		return errMissingValidator
	}

	if err := set.RemoveWeight(nodeID, weight); err != nil {
		return err
	}

	// If this was the last validator in the subnet
	// and no callback listeners are registered, remove the subnet
	if set.Len() == 0 && !set.HasCallbackRegistered() {
		delete(m.subnetToVdrs, subnetID)
	}

	return nil
}

func (m *manager) Contains(subnetID ids.ID, validatorID ids.NodeID) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		return false
	}

	return set.Contains(validatorID)
}

func (m *manager) Len(subnetID ids.ID) int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		return 0
	}

	return set.Len()
}

func (m *manager) Weight(subnetID ids.ID) (uint64, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		return 0, nil
	}

	return set.Weight()
}

func (m *manager) Sample(subnetID ids.ID, size int) ([]ids.NodeID, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		return nil, errMissingValidator
	}

	return set.Sample(size)
}

func (m *manager) GetMap(subnetID ids.ID) map[ids.NodeID]*GetValidatorOutput {
	m.lock.RLock()
	defer m.lock.RUnlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		return nil
	}

	return set.Map()
}

func (m *manager) RegisterCallbackListener(subnetID ids.ID, listener SetCallbackListener) {
	m.lock.Lock()
	defer m.lock.Unlock()

	set, exists := m.subnetToVdrs[subnetID]
	if !exists {
		set = newSet()
		m.subnetToVdrs[subnetID] = set
	}

	set.RegisterCallbackListener(listener)
}

func (m *manager) String() string {
	m.lock.RLock()
	defer m.lock.RUnlock()

	subnets := maps.Keys(m.subnetToVdrs)
	utils.Sort(subnets)

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

func (m *manager) GetValidatorIDs(subnetID ids.ID) ([]ids.NodeID, error) {
	vdrs, exist := m.subnetToVdrs[subnetID]
	if !exist {
		return nil, fmt.Errorf("%w: %s", ErrMissingValidators, subnetID)
	}

	vdrsMap := vdrs.Map()
	return maps.Keys(vdrsMap), nil
}
