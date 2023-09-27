// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Manager = (*manager)(nil)

// NewOverriddenManager returns a Manager that overrides of all calls to the underlying Manager
// to only operate on the validators in [subnetID].
func NewOverriddenManager(subnetID ids.ID, manager Manager) Manager {
	return &overriddenManager{
		subnetID: subnetID,
		manager:  manager,
	}
}

// overriddenManager is a wrapper around a Manager that overrides of all calls to the underlying Manager
// to only operate on the validators in [subnetID].
// subnetID here is typically the primary network ID, as it has the superset of all subnet validators.
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

func (o *overriddenManager) GetWeight(_ ids.ID, validatorID ids.NodeID) uint64 {
	return o.manager.GetWeight(o.subnetID, validatorID)
}

func (o *overriddenManager) GetValidator(_ ids.ID, validatorID ids.NodeID) (*Validator, bool) {
	return o.manager.GetValidator(o.subnetID, validatorID)
}

func (o *overriddenManager) SubsetWeight(_ ids.ID, validatorIDs set.Set[ids.NodeID]) (uint64, error) {
	return o.manager.SubsetWeight(o.subnetID, validatorIDs)
}

func (o *overriddenManager) RemoveWeight(_ ids.ID, nodeID ids.NodeID, weight uint64) error {
	return o.manager.RemoveWeight(o.subnetID, nodeID, weight)
}

func (o *overriddenManager) Contains(_ ids.ID, validatorID ids.NodeID) bool {
	return o.manager.Contains(o.subnetID, validatorID)
}

func (o *overriddenManager) Count(_ ids.ID) int {
	return o.manager.Count(o.subnetID)
}

func (o *overriddenManager) TotalWeight(_ ids.ID) (uint64, error) {
	return o.manager.TotalWeight(o.subnetID)
}

func (o *overriddenManager) Sample(_ ids.ID, size int) ([]ids.NodeID, error) {
	return o.manager.Sample(o.subnetID, size)
}

func (o *overriddenManager) GetMap(_ ids.ID) map[ids.NodeID]*GetValidatorOutput {
	return o.manager.GetMap(o.subnetID)
}

func (o *overriddenManager) RegisterCallbackListener(_ ids.ID, listener SetCallbackListener) {
	o.manager.RegisterCallbackListener(o.subnetID, listener)
}

func (o *overriddenManager) String() string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("Overridden Validator Manager: (Size = 1, SubnetID = %s)", o.subnetID))

	vdrMap := o.manager.GetMap(o.subnetID)
	weight, err := o.manager.TotalWeight(o.subnetID)
	if err != nil {
		sb.WriteString(fmt.Sprintf("Validator Set: (Size = %d, Weight: Error: %s)", len(vdrMap), err))
	} else {
		sb.WriteString(fmt.Sprintf("Validator Set: (Size = %d, Weight: %d)", len(vdrMap), weight))
	}

	format := fmt.Sprintf("\n    Validator[%s]: %%33s, %%d", formatting.IntFormat(len(vdrMap)-1))
	for i, vdr := range vdrMap {
		sb.WriteString(fmt.Sprintf(
			format,
			i,
			vdr.NodeID,
			vdr.Weight,
		))
	}

	return sb.String()
}

func (o *overriddenManager) GetValidatorIDs(_ ids.ID) []ids.NodeID {
	return o.manager.GetValidatorIDs(o.subnetID)
}
