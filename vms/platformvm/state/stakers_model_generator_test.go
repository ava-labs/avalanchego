// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"math"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/prop"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// TestGeneratedStakersValidity tests the staker generator itself.
// It documents and verifies theinvariants enforced by the staker generator.
func TestGeneratedStakersValidity(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("EndTime never before StartTime", prop.ForAll(
		func(s Staker) string {
			if s.EndTime.Before(s.StartTime) {
				return fmt.Sprintf("startTime %v not before endTime %v, staker %v",
					s.StartTime, s.EndTime, s)
			}
			return ""
		},
		stakerGenerator(anyPriority, nil, nil, math.MaxUint64),
	))

	properties.Property("NextTime coherent with priority", prop.ForAll(
		func(s Staker) string {
			switch p := s.Priority; p {
			case txs.PrimaryNetworkDelegatorApricotPendingPriority,
				txs.PrimaryNetworkDelegatorBanffPendingPriority,
				txs.SubnetPermissionlessDelegatorPendingPriority,
				txs.PrimaryNetworkValidatorPendingPriority,
				txs.SubnetPermissionlessValidatorPendingPriority,
				txs.SubnetPermissionedValidatorPendingPriority:
				if !s.NextTime.Equal(s.StartTime) {
					return fmt.Sprintf("pending staker has nextTime %v different from startTime %v, staker %v",
						s.NextTime, s.StartTime, s)
				}
				return ""

			case txs.PrimaryNetworkDelegatorCurrentPriority,
				txs.SubnetPermissionlessDelegatorCurrentPriority,
				txs.PrimaryNetworkValidatorCurrentPriority,
				txs.SubnetPermissionlessValidatorCurrentPriority,
				txs.SubnetPermissionedValidatorCurrentPriority:
				if !s.NextTime.Equal(s.EndTime) {
					return fmt.Sprintf("current staker has nextTime %v different from endTime %v, staker %v",
						s.NextTime, s.EndTime, s)
				}
				return ""

			default:
				return fmt.Sprintf("priority %v unhandled in test", p)
			}
		},
		stakerGenerator(anyPriority, nil, nil, math.MaxUint64),
	))

	subnetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	properties.Property("subnetID and nodeID set as specified", prop.ForAll(
		func(s Staker) string {
			if s.SubnetID != subnetID {
				return fmt.Sprintf("unexpected subnetID, expected %v, got %v",
					subnetID, s.SubnetID)
			}
			if s.NodeID != nodeID {
				return fmt.Sprintf("unexpected nodeID, expected %v, got %v",
					nodeID, s.NodeID)
			}
			return ""
		},
		stakerGenerator(anyPriority, &subnetID, &nodeID, math.MaxUint64),
	))

	properties.TestingRun(t)
}
