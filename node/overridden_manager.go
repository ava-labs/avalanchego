// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ validators.Manager = (*overriddenManager)(nil)

// newOverriddenManager returns a Manager that overrides of all calls to the
// underlying Manager to only operate on the validators in [subnetID].
func newOverriddenManager(subnetID ids.ID, manager validators.Manager) *overriddenManager {
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
	manager  validators.Manager
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

func (o *overriddenManager) GetValidator(_ ids.ID, nodeID ids.NodeID) (*validators.Validator, bool) {
	return o.manager.GetValidator(o.subnetID, nodeID)
}

func (o *overriddenManager) SubsetWeight(_ ids.ID, nodeIDs set.Set[ids.NodeID]) (uint64, error) {
	return o.manager.SubsetWeight(o.subnetID, nodeIDs)
}

func (o *overriddenManager) RemoveWeight(_ ids.ID, nodeID ids.NodeID, weight uint64) error {
	return o.manager.RemoveWeight(o.subnetID, nodeID, weight)
}

func (o *overriddenManager) NumSubnets() int {
	if o.manager.NumValidators(o.subnetID) == 0 {
		return 0
	}
	return 1
}

func (o *overriddenManager) NumValidators(ids.ID) int {
	return o.manager.NumValidators(o.subnetID)
}

func (o *overriddenManager) TotalWeight(ids.ID) (uint64, error) {
	return o.manager.TotalWeight(o.subnetID)
}

func (o *overriddenManager) Sample(_ ids.ID, size int) ([]ids.NodeID, error) {
	return o.manager.Sample(o.subnetID, size)
}

func (o *overriddenManager) GetAllMaps() map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput {
	return o.manager.GetAllMaps()
}

func (o *overriddenManager) GetMap(ids.ID) map[ids.NodeID]*validators.GetValidatorOutput {
	return o.manager.GetMap(o.subnetID)
}

func (o *overriddenManager) RegisterCallbackListener(listener validators.ManagerCallbackListener) {
	o.manager.RegisterCallbackListener(listener)
}

func (o *overriddenManager) RegisterSetCallbackListener(_ ids.ID, listener validators.SetCallbackListener) {
	o.manager.RegisterSetCallbackListener(o.subnetID, listener)
}

func (o *overriddenManager) String() string {
	return fmt.Sprintf("Overridden Validator Manager (SubnetID = %s): %s", o.subnetID, o.manager)
}

func (o *overriddenManager) GetValidatorIDs(ids.ID) []ids.NodeID {
	return o.manager.GetValidatorIDs(o.subnetID)
}
