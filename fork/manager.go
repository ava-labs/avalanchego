// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ validators.Manager                 = (*Manager)(nil)
	_ validators.ManagerCallbackListener = forwarder{}
)

// Manager is the node's current validator view in fork mode. Before the
// switch it reads from [mirror], a copy of [private] (which the P-chain
// populates) that the forwarder keeps in sync. After the switch it reads from
// [static], which holds the fork validators.
//
// Listeners are registered on both views. Switch empties the mirror, which
// tells them the source validators are gone, and then fills the static view,
// which tells them the fork validators are here.
type Manager struct {
	config  *Config
	private validators.Manager
	mirror  validators.Manager
	static  validators.Manager

	switched atomic.Bool
	// lock serializes the forwarder, listener registration, and Switch.
	lock sync.Mutex
}

// NewManager returns a Manager over [private], which must be empty.
func NewManager(private validators.Manager, c *Config, startSwitched bool) (*Manager, error) {
	m := &Manager{
		config:  c,
		private: private,
		mirror:  validators.NewManager(),
		static:  validators.NewManager(),
	}
	if startSwitched {
		if err := m.addForkValidators(); err != nil {
			return nil, err
		}
		m.switched.Store(true)
	}
	private.RegisterCallbackListener(forwarder{m: m})
	return m, nil
}

func (m *Manager) addForkValidators() error {
	for _, v := range m.config.Validators {
		if err := m.static.AddStaker(constants.PrimaryNetworkID, v.NodeID, v.Signer.Key(), ids.Empty, v.Weight); err != nil {
			return fmt.Errorf("adding fork validator %s: %w", v.NodeID, err)
		}
	}
	return nil
}

// Switched reports whether the fork validator set is in effect.
func (m *Manager) Switched() bool {
	return m.switched.Load()
}

// Switch replaces the source validator view with the fork validator set. It is
// idempotent.
func (m *Manager) Switch() {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.switched.Load() {
		return
	}
	m.switched.Store(true)

	for subnetID, vdrs := range m.mirror.GetAllMaps() {
		for nodeID, v := range vdrs {
			// Cannot fail: the weight is the validator's current weight.
			_ = m.mirror.RemoveWeight(subnetID, nodeID, v.Weight)
		}
	}
	// Cannot fail: the config is validated and the static view is empty.
	_ = m.addForkValidators()
}

func (m *Manager) view() validators.Manager {
	if m.switched.Load() {
		return m.static
	}
	return m.mirror
}

func (m *Manager) String() string {
	return fmt.Sprintf("fork.Manager(switched=%t): %s", m.switched.Load(), m.view())
}

// Writes always go to the private manager, which the P-chain owns. After the
// switch they are invisible through this Manager.

func (m *Manager) AddStaker(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
	return m.private.AddStaker(subnetID, nodeID, pk, txID, weight)
}

func (m *Manager) AddWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	return m.private.AddWeight(subnetID, nodeID, weight)
}

func (m *Manager) RemoveWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	return m.private.RemoveWeight(subnetID, nodeID, weight)
}

func (m *Manager) GetWeight(subnetID ids.ID, nodeID ids.NodeID) uint64 {
	return m.view().GetWeight(subnetID, nodeID)
}

func (m *Manager) GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*validators.Validator, bool) {
	return m.view().GetValidator(subnetID, nodeID)
}

func (m *Manager) GetValidatorIDs(subnetID ids.ID) []ids.NodeID {
	return m.view().GetValidatorIDs(subnetID)
}

func (m *Manager) SubsetWeight(subnetID ids.ID, validatorIDs set.Set[ids.NodeID]) (uint64, error) {
	return m.view().SubsetWeight(subnetID, validatorIDs)
}

func (m *Manager) NumSubnets() int {
	return m.view().NumSubnets()
}

func (m *Manager) NumValidators(subnetID ids.ID) int {
	return m.view().NumValidators(subnetID)
}

func (m *Manager) TotalWeight(subnetID ids.ID) (uint64, error) {
	return m.view().TotalWeight(subnetID)
}

func (m *Manager) Sample(subnetID ids.ID, size int) ([]ids.NodeID, error) {
	return m.view().Sample(subnetID, size)
}

func (m *Manager) GetAllMaps() map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput {
	return m.view().GetAllMaps()
}

func (m *Manager) GetMap(subnetID ids.ID) map[ids.NodeID]*validators.GetValidatorOutput {
	return m.view().GetMap(subnetID)
}

func (m *Manager) RegisterCallbackListener(listener validators.ManagerCallbackListener) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.mirror.RegisterCallbackListener(listener)
	m.static.RegisterCallbackListener(listener)
}

func (m *Manager) RegisterSetCallbackListener(subnetID ids.ID, listener validators.SetCallbackListener) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.mirror.RegisterSetCallbackListener(subnetID, listener)
	m.static.RegisterSetCallbackListener(subnetID, listener)
}

// forwarder copies the private manager's events into the mirror until the
// switch. The private manager emits a consistent sequence, so the mirror
// writes cannot fail.
type forwarder struct {
	m *Manager
}

func (f forwarder) OnValidatorAdded(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	m := f.m
	m.lock.Lock()
	defer m.lock.Unlock()

	if !m.switched.Load() {
		_ = m.mirror.AddStaker(subnetID, nodeID, pk, txID, weight)
	}
}

func (f forwarder) OnValidatorRemoved(subnetID ids.ID, nodeID ids.NodeID, weight uint64) {
	m := f.m
	m.lock.Lock()
	defer m.lock.Unlock()

	if !m.switched.Load() {
		_ = m.mirror.RemoveWeight(subnetID, nodeID, weight)
	}
}

func (f forwarder) OnValidatorWeightChanged(subnetID ids.ID, nodeID ids.NodeID, oldWeight, newWeight uint64) {
	m := f.m
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.switched.Load() {
		return
	}
	if newWeight > oldWeight {
		_ = m.mirror.AddWeight(subnetID, nodeID, newWeight-oldWeight)
	} else {
		_ = m.mirror.RemoveWeight(subnetID, nodeID, oldWeight-newWeight)
	}
}
