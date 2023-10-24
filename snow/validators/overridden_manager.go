// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Manager = (*overriddenManager)(nil)

// NewOverriddenManager returns a Manager that overrides of all calls to the
// underlying Manager to only operate on the validators in [subnetID].
func NewOverriddenManager(subnetID ids.ID, manager Manager) Manager {
	return &overriddenManager{
		subnetID: subnetID,
		manager:  manager,
	}
}

// overriddenManager is a wrapper around a Manager that overrides of all calls
// to the underlying Manager to only operate on the validators in [subnetID].
// subnetID here is typically the primary network ID, as it has the superset of
// all subnet validators.
type overriddenManager struct {
	manager  Manager
	subnetID ids.ID
}

func (o *overriddenManager) AddStaker(_ ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
	return o.manager.AddStaker(o.subnetID, nodeID, pk, txID, weight)
}

func (o *overriddenManager) AddWeight(_ ids.ID, nodeID ids.NodeID, weight uint64) error {
	return o.manager.AddWeight(o.subnetID, nodeID, weight)
}

func (o *overriddenManager) GetWeight(_ ids.ID, nodeID ids.NodeID) uint64 {
	return o.manager.GetWeight(o.subnetID, nodeID)
}

func (o *overriddenManager) GetValidator(_ ids.ID, nodeID ids.NodeID) (*Validator, bool) {
	return o.manager.GetValidator(o.subnetID, nodeID)
}

func (o *overriddenManager) SubsetWeight(_ ids.ID, nodeIDs set.Set[ids.NodeID]) (uint64, error) {
	return o.manager.SubsetWeight(o.subnetID, nodeIDs)
}

func (o *overriddenManager) RemoveWeight(_ ids.ID, nodeID ids.NodeID, weight uint64) error {
	return o.manager.RemoveWeight(o.subnetID, nodeID, weight)
}

func (o *overriddenManager) Count(ids.ID) int {
	return o.manager.Count(o.subnetID)
}

func (o *overriddenManager) TotalWeight(ids.ID) (uint64, error) {
	return o.manager.TotalWeight(o.subnetID)
}

func (o *overriddenManager) Sample(_ ids.ID, size int) ([]ids.NodeID, error) {
	return o.manager.Sample(o.subnetID, size)
}

func (o *overriddenManager) GetMap(ids.ID) map[ids.NodeID]*GetValidatorOutput {
	return o.manager.GetMap(o.subnetID)
}

func (o *overriddenManager) RegisterCallbackListener(_ ids.ID, listener SetCallbackListener) {
	o.manager.RegisterCallbackListener(o.subnetID, listener)
}

func (o *overriddenManager) String() string {
	return fmt.Sprintf("Overridden Validator Manager (SubnetID = %s): %s", o.subnetID, o.manager)
}

func (o *overriddenManager) GetValidatorIDs(ids.ID) []ids.NodeID {
	return o.manager.GetValidatorIDs(o.subnetID)
}
