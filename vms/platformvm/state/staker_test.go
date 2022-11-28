// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var errCustom = errors.New("custom")

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
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(1, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			less: true,
		},
		{
			name: "left time > right time",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(1, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			less: false,
		},
		{
			name: "left priority < right priority",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkDelegatorApricotPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			less: true,
		},
		{
			name: "left priority > right priority",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkDelegatorApricotPendingPriority,
			},
			less: false,
		},
		{
			name: "left txID < right txID",
			left: &Staker{
				TxID:     ids.ID([32]byte{0}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{1}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			less: true,
		},
		{
			name: "left txID > right txID",
			left: &Staker{
				TxID:     ids.ID([32]byte{1}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{0}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			less: false,
		},
		{
			name: "equal",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
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

func TestNewCurrentStaker(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	txID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	sk, err := bls.NewSecretKey()
	require.NoError(err)
	publicKey := bls.PublicFromSecretKey(sk)
	subnetID := ids.GenerateTestID()
	weight := uint64(12345)
	startTime := time.Now()
	endTime := time.Now()
	potentialReward := uint64(54321)
	currentPriority := txs.SubnetPermissionedValidatorCurrentPriority

	stakerTx := txs.NewMockStaker(ctrl)
	stakerTx.EXPECT().NodeID().Return(nodeID)
	stakerTx.EXPECT().PublicKey().Return(publicKey, true, nil)
	stakerTx.EXPECT().SubnetID().Return(subnetID)
	stakerTx.EXPECT().Weight().Return(weight)
	stakerTx.EXPECT().StartTime().Return(startTime)
	stakerTx.EXPECT().EndTime().Return(endTime)
	stakerTx.EXPECT().CurrentPriority().Return(currentPriority)

	staker, err := NewCurrentStaker(txID, stakerTx, potentialReward)
	require.NotNil(staker)
	require.NoError(err)
	require.Equal(txID, staker.TxID)
	require.Equal(nodeID, staker.NodeID)
	require.Equal(publicKey, staker.PublicKey)
	require.Equal(subnetID, staker.SubnetID)
	require.Equal(weight, staker.Weight)
	require.Equal(startTime, staker.StartTime)
	require.Equal(endTime, staker.EndTime)
	require.Equal(potentialReward, staker.PotentialReward)
	require.Equal(endTime, staker.NextTime)
	require.Equal(currentPriority, staker.Priority)

	stakerTx.EXPECT().PublicKey().Return(nil, false, errCustom)

	_, err = NewCurrentStaker(txID, stakerTx, potentialReward)
	require.ErrorIs(err, errCustom)
}

func TestNewPendingStaker(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	txID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	sk, err := bls.NewSecretKey()
	require.NoError(err)
	publicKey := bls.PublicFromSecretKey(sk)
	subnetID := ids.GenerateTestID()
	weight := uint64(12345)
	startTime := time.Now()
	endTime := time.Now()
	pendingPriority := txs.SubnetPermissionedValidatorPendingPriority

	stakerTx := txs.NewMockStaker(ctrl)
	stakerTx.EXPECT().NodeID().Return(nodeID)
	stakerTx.EXPECT().PublicKey().Return(publicKey, true, nil)
	stakerTx.EXPECT().SubnetID().Return(subnetID)
	stakerTx.EXPECT().Weight().Return(weight)
	stakerTx.EXPECT().StartTime().Return(startTime)
	stakerTx.EXPECT().EndTime().Return(endTime)
	stakerTx.EXPECT().PendingPriority().Return(pendingPriority)

	staker, err := NewPendingStaker(txID, stakerTx)
	require.NotNil(staker)
	require.NoError(err)
	require.Equal(txID, staker.TxID)
	require.Equal(nodeID, staker.NodeID)
	require.Equal(publicKey, staker.PublicKey)
	require.Equal(subnetID, staker.SubnetID)
	require.Equal(weight, staker.Weight)
	require.Equal(startTime, staker.StartTime)
	require.Equal(endTime, staker.EndTime)
	require.Zero(staker.PotentialReward)
	require.Equal(startTime, staker.NextTime)
	require.Equal(pendingPriority, staker.Priority)

	stakerTx.EXPECT().PublicKey().Return(nil, false, errCustom)

	_, err = NewPendingStaker(txID, stakerTx)
	require.ErrorIs(err, errCustom)
}
