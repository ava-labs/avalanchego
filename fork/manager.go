// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork

import (
	"fmt"
	"maps"
	"slices"
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
// switch it mirrors [private], which the P-chain populates. Switch replaces
// that view with the fork validator set and tells every listener: first it
// removes everything they were told, then it adds the fork validators.
type Manager struct {
	private validators.Manager
	static  validators.Manager

	switched atomic.Bool

	// lock serializes event delivery, listener registration, and Switch.
	lock         sync.Mutex
	mirror       map[ids.ID]map[ids.NodeID]*mirrored
	listeners    []validators.ManagerCallbackListener
	setListeners map[ids.ID][]validators.SetCallbackListener
	onSwitch     []func()
}

type mirrored struct {
	pk     *bls.PublicKey
	txID   ids.ID
	weight uint64
}

// NewManager returns a Manager over [private], which must be empty.
func NewManager(private validators.Manager, c *Config, startSwitched bool) (*Manager, error) {
	static := validators.NewManager()
	for _, v := range c.Validators {
		if err := static.AddStaker(constants.PrimaryNetworkID, v.NodeID, v.Signer.Key(), ids.Empty, v.Weight); err != nil {
			return nil, fmt.Errorf("adding fork validator %s: %w", v.NodeID, err)
		}
	}
	m := &Manager{
		private:      private,
		static:       static,
		mirror:       make(map[ids.ID]map[ids.NodeID]*mirrored),
		setListeners: make(map[ids.ID][]validators.SetCallbackListener),
	}
	m.switched.Store(startSwitched)
	private.RegisterCallbackListener(forwarder{m: m})
	return m, nil
}

// Switched reports whether the fork validator set is in effect.
func (m *Manager) Switched() bool {
	return m.switched.Load()
}

// OnSwitch registers [f] to run once the switch has happened. If it already
// has, [f] runs immediately. [f] never runs while the Manager's lock is held.
func (m *Manager) OnSwitch(f func()) {
	m.lock.Lock()
	if !m.switched.Load() {
		m.onSwitch = append(m.onSwitch, f)
		m.lock.Unlock()
		return
	}
	m.lock.Unlock()
	f()
}

// Switch replaces the source validator view with the fork validator set. It is
// idempotent.
func (m *Manager) Switch() {
	m.lock.Lock()
	if m.switched.Load() {
		m.lock.Unlock()
		return
	}
	m.switched.Store(true)

	for _, subnetID := range sortedKeys(m.mirror) {
		vdrs := m.mirror[subnetID]
		for _, nodeID := range sortedKeys(vdrs) {
			m.notifyRemoved(subnetID, nodeID, vdrs[nodeID].weight)
		}
	}
	m.mirror = nil

	forkSet := m.static.GetMap(constants.PrimaryNetworkID)
	for _, nodeID := range sortedKeys(forkSet) {
		v := forkSet[nodeID]
		m.notifyAdded(constants.PrimaryNetworkID, nodeID, v.PublicKey, ids.Empty, v.Weight)
	}

	hooks := m.onSwitch
	m.onSwitch = nil
	m.lock.Unlock()

	for _, f := range hooks {
		f()
	}
}

func (m *Manager) view() validators.Manager {
	if m.switched.Load() {
		return m.static
	}
	return m.private
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

	m.listeners = append(m.listeners, listener)
	if m.switched.Load() {
		forkSet := m.static.GetMap(constants.PrimaryNetworkID)
		for _, nodeID := range sortedKeys(forkSet) {
			v := forkSet[nodeID]
			listener.OnValidatorAdded(constants.PrimaryNetworkID, nodeID, v.PublicKey, ids.Empty, v.Weight)
		}
		return
	}
	for _, subnetID := range sortedKeys(m.mirror) {
		vdrs := m.mirror[subnetID]
		for _, nodeID := range sortedKeys(vdrs) {
			v := vdrs[nodeID]
			listener.OnValidatorAdded(subnetID, nodeID, v.pk, v.txID, v.weight)
		}
	}
}

func (m *Manager) RegisterSetCallbackListener(subnetID ids.ID, listener validators.SetCallbackListener) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.setListeners[subnetID] = append(m.setListeners[subnetID], listener)
	if m.switched.Load() {
		if subnetID != constants.PrimaryNetworkID {
			return
		}
		forkSet := m.static.GetMap(constants.PrimaryNetworkID)
		for _, nodeID := range sortedKeys(forkSet) {
			v := forkSet[nodeID]
			listener.OnValidatorAdded(nodeID, v.PublicKey, ids.Empty, v.Weight)
		}
		return
	}
	vdrs := m.mirror[subnetID]
	for _, nodeID := range sortedKeys(vdrs) {
		v := vdrs[nodeID]
		listener.OnValidatorAdded(nodeID, v.pk, v.txID, v.weight)
	}
}

// notify* must be called with [m.lock] held.

func (m *Manager) notifyAdded(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	for _, l := range m.listeners {
		l.OnValidatorAdded(subnetID, nodeID, pk, txID, weight)
	}
	for _, l := range m.setListeners[subnetID] {
		l.OnValidatorAdded(nodeID, pk, txID, weight)
	}
}

func (m *Manager) notifyRemoved(subnetID ids.ID, nodeID ids.NodeID, weight uint64) {
	for _, l := range m.listeners {
		l.OnValidatorRemoved(subnetID, nodeID, weight)
	}
	for _, l := range m.setListeners[subnetID] {
		l.OnValidatorRemoved(nodeID, weight)
	}
}

func (m *Manager) notifyWeightChanged(subnetID ids.ID, nodeID ids.NodeID, oldWeight, newWeight uint64) {
	for _, l := range m.listeners {
		l.OnValidatorWeightChanged(subnetID, nodeID, oldWeight, newWeight)
	}
	for _, l := range m.setListeners[subnetID] {
		l.OnValidatorWeightChanged(nodeID, oldWeight, newWeight)
	}
}

// forwarder relays the private manager's events to the Manager's listeners
// until the switch.
type forwarder struct {
	m *Manager
}

func (f forwarder) OnValidatorAdded(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	m := f.m
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.switched.Load() {
		return
	}
	vdrs, ok := m.mirror[subnetID]
	if !ok {
		vdrs = make(map[ids.NodeID]*mirrored)
		m.mirror[subnetID] = vdrs
	}
	vdrs[nodeID] = &mirrored{pk: pk, txID: txID, weight: weight}
	m.notifyAdded(subnetID, nodeID, pk, txID, weight)
}

func (f forwarder) OnValidatorRemoved(subnetID ids.ID, nodeID ids.NodeID, weight uint64) {
	m := f.m
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.switched.Load() {
		return
	}
	vdrs := m.mirror[subnetID]
	delete(vdrs, nodeID)
	if len(vdrs) == 0 {
		delete(m.mirror, subnetID)
	}
	m.notifyRemoved(subnetID, nodeID, weight)
}

func (f forwarder) OnValidatorWeightChanged(subnetID ids.ID, nodeID ids.NodeID, oldWeight, newWeight uint64) {
	m := f.m
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.switched.Load() {
		return
	}
	if v, ok := m.mirror[subnetID][nodeID]; ok {
		v.weight = newWeight
	}
	m.notifyWeightChanged(subnetID, nodeID, oldWeight, newWeight)
}

// orderedKey is satisfied by ids.ID and ids.NodeID.
type orderedKey[T any] interface {
	comparable
	Compare(T) int
}

func sortedKeys[K orderedKey[K], V any](m map[K]V) []K {
	keys := slices.Collect(maps.Keys(m))
	slices.SortFunc(keys, func(a, b K) int { return a.Compare(b) })
	return keys
}
