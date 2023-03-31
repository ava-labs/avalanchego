// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	_ Manager = (*manager)(nil)

	errMissingValidators = errors.New("missing validators")
)

// Manager holds the validator set of each subnet
type Manager interface {
	fmt.Stringer

	// Add a subnet's validator set to the manager.
	//
	// If the subnet had previously registered a validator set, false will be
	// returned and the manager will not be modified.
	Add(subnetID ids.ID, set Set) bool

	// Get returns the validator set for the given subnet
	// Returns false if the subnet doesn't exist
	Get(ids.ID) (Set, bool)
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

func (m *manager) Add(subnetID ids.ID, set Set) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, exists := m.subnetToVdrs[subnetID]; exists {
		return false
	}

	m.subnetToVdrs[subnetID] = set
	return true
}

func (m *manager) Get(subnetID ids.ID) (Set, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	vdrs, ok := m.subnetToVdrs[subnetID]
	return vdrs, ok
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

// Add is a helper that fetches the validator set of [subnetID] from [m] and
// adds [nodeID] to the validator set.
// Returns an error if:
// - [subnetID] does not have a registered validator set in [m]
// - adding [nodeID] to the validator set returns an error
func Add(m Manager, subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
	vdrs, ok := m.Get(subnetID)
	if !ok {
		return fmt.Errorf("%w: %s", errMissingValidators, subnetID)
	}
	return vdrs.Add(nodeID, pk, txID, weight)
}

// AddWeight is a helper that fetches the validator set of [subnetID] from [m]
// and adds [weight] to [nodeID] in the validator set.
// Returns an error if:
// - [subnetID] does not have a registered validator set in [m]
// - adding [weight] to [nodeID] in the validator set returns an error
func AddWeight(m Manager, subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	vdrs, ok := m.Get(subnetID)
	if !ok {
		return fmt.Errorf("%w: %s", errMissingValidators, subnetID)
	}
	return vdrs.AddWeight(nodeID, weight)
}

// RemoveWeight is a helper that fetches the validator set of [subnetID] from
// [m] and removes [weight] from [nodeID] in the validator set.
// Returns an error if:
// - [subnetID] does not have a registered validator set in [m]
// - removing [weight] from [nodeID] in the validator set returns an error
func RemoveWeight(m Manager, subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	vdrs, ok := m.Get(subnetID)
	if !ok {
		return fmt.Errorf("%w: %s", errMissingValidators, subnetID)
	}
	return vdrs.RemoveWeight(nodeID, weight)
}

// Contains is a helper that fetches the validator set of [subnetID] from [m]
// and returns if the validator set contains [nodeID]. If [m] does not contain a
// validator set for [subnetID], false is returned.
func Contains(m Manager, subnetID ids.ID, nodeID ids.NodeID) bool {
	vdrs, ok := m.Get(subnetID)
	if !ok {
		return false
	}
	return vdrs.Contains(nodeID)
}
