// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

func TestStakerLess(t *testing.T) {
	tests := []struct {
		name  string
		left  *Staker
		right *Staker
		less  bool
	}{
		{
			name: "left time < right time",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkValidatorCurrentPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(1, 0),
				Priority: PrimaryNetworkValidatorCurrentPriority,
			},
			less: true,
		},
		{
			name: "left time > right time",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(1, 0),
				Priority: PrimaryNetworkValidatorCurrentPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkValidatorCurrentPriority,
			},
			less: false,
		},
		{
			name: "left priority < right priority",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkDelegatorPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkValidatorPendingPriority,
			},
			less: true,
		},
		{
			name: "left priority > right priority",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkValidatorPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkDelegatorPendingPriority,
			},
			less: false,
		},
		{
			name: "left txID < right txID",
			left: &Staker{
				TxID:     ids.ID([32]byte{0}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkValidatorPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{1}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkValidatorPendingPriority,
			},
			less: true,
		},
		{
			name: "left txID > right txID",
			left: &Staker{
				TxID:     ids.ID([32]byte{1}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkValidatorPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{0}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkValidatorPendingPriority,
			},
			less: false,
		},
		{
			name: "equal",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkValidatorCurrentPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: PrimaryNetworkValidatorCurrentPriority,
			},
			less: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.less, test.left.Less(test.right))
		})
	}
}

func TestNewPrimaryNetworkStaker(t *testing.T) {
	require := require.New(t)
	txID := ids.GenerateTestID()
	vdr := &validator.Validator{
		NodeID: ids.GenerateTestNodeID(),
		Start:  0,
		End:    1,
		Wght:   2,
	}

	staker := NewPrimaryNetworkStaker(txID, vdr)
	require.NotNil(staker)
	require.Equal(txID, staker.TxID)
	require.Equal(vdr.NodeID, staker.NodeID)
	require.Equal(constants.PrimaryNetworkID, staker.SubnetID)
	require.Equal(vdr.Wght, staker.Weight)
	require.Equal(vdr.StartTime(), staker.StartTime)
	require.Equal(vdr.EndTime(), staker.EndTime)
	require.Zero(staker.PotentialReward)
	require.Zero(staker.NextTime)
	require.Zero(staker.Priority)
}

func TestNewSubnetStaker(t *testing.T) {
	require := require.New(t)
	txID := ids.GenerateTestID()
	vdr := &validator.SubnetValidator{
		Validator: validator.Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  0,
			End:    1,
			Wght:   2,
		},
		Subnet: ids.GenerateTestID(),
	}

	staker := NewSubnetStaker(txID, vdr)
	require.NotNil(staker)
	require.Equal(txID, staker.TxID)
	require.Equal(vdr.NodeID, staker.NodeID)
	require.Equal(vdr.Subnet, staker.SubnetID)
	require.Equal(vdr.Wght, staker.Weight)
	require.Equal(vdr.StartTime(), staker.StartTime)
	require.Equal(vdr.EndTime(), staker.EndTime)
	require.Zero(staker.PotentialReward)
	require.Zero(staker.NextTime)
	require.Zero(staker.Priority)
}
