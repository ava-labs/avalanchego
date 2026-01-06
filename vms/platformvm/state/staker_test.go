// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer/signermock"
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
	stakerTx := generateStakerTx(require)

	txID := ids.GenerateTestID()
	startTime := stakerTx.StartTime().Add(2 * time.Hour)
	potentialReward := uint64(12345)

	staker, err := NewCurrentStaker(txID, stakerTx, startTime, potentialReward)
	require.NoError(err)
	publicKey, isNil, err := stakerTx.PublicKey()
	require.NoError(err)
	require.True(isNil)
	require.Equal(&Staker{
		TxID:            txID,
		NodeID:          stakerTx.NodeID(),
		PublicKey:       publicKey,
		SubnetID:        stakerTx.SubnetID(),
		Weight:          stakerTx.Weight(),
		StartTime:       startTime,
		EndTime:         stakerTx.EndTime(),
		PotentialReward: potentialReward,
		NextTime:        stakerTx.EndTime(),
		Priority:        stakerTx.CurrentPriority(),
	}, staker)

	ctrl := gomock.NewController(t)
	signer := signermock.NewSigner(ctrl)
	signer.EXPECT().Verify().Return(errCustom)
	stakerTx.Signer = signer

	_, err = NewCurrentStaker(txID, stakerTx, startTime, potentialReward)
	require.ErrorIs(err, errCustom)
}

func TestNewPendingStaker(t *testing.T) {
	require := require.New(t)

	stakerTx := generateStakerTx(require)

	txID := ids.GenerateTestID()
	staker, err := NewPendingStaker(txID, stakerTx)
	require.NoError(err)
	publicKey, isNil, err := stakerTx.PublicKey()
	require.NoError(err)
	require.True(isNil)
	require.Equal(&Staker{
		TxID:      txID,
		NodeID:    stakerTx.NodeID(),
		PublicKey: publicKey,
		SubnetID:  stakerTx.SubnetID(),
		Weight:    stakerTx.Weight(),
		StartTime: stakerTx.StartTime(),
		EndTime:   stakerTx.EndTime(),
		NextTime:  stakerTx.StartTime(),
		Priority:  stakerTx.PendingPriority(),
	}, staker)

	ctrl := gomock.NewController(t)
	signer := signermock.NewSigner(ctrl)
	signer.EXPECT().Verify().Return(errCustom)
	stakerTx.Signer = signer

	_, err = NewPendingStaker(txID, stakerTx)
	require.ErrorIs(err, errCustom)
}

func generateStakerTx(require *require.Assertions) *txs.AddPermissionlessValidatorTx {
	nodeID := ids.GenerateTestNodeID()
	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)
	subnetID := ids.GenerateTestID()
	weight := uint64(12345)
	startTime := time.Now().Truncate(time.Second)
	endTime := startTime.Add(time.Hour)

	return &txs.AddPermissionlessValidatorTx{
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  uint64(startTime.Unix()),
			End:    uint64(endTime.Unix()),
			Wght:   weight,
		},
		Signer: pop,
		Subnet: subnetID,
	}
}
