// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

func TestValidMutation(t *testing.T) {
	sk, err := localsigner.New()
	require.NoError(t, err)

	continuousStaker := &Staker{
		TxID:                    ids.GenerateTestID(),
		NodeID:                  ids.GenerateTestNodeID(),
		PublicKey:               sk.PublicKey(),
		SubnetID:                ids.GenerateTestID(),
		Weight:                  100,
		StartTime:               time.Unix(10, 0),
		EndTime:                 time.Unix(20, 0),
		PotentialReward:         50,
		AccruedRewards:          20,
		AccruedDelegateeRewards: 15,
		NextTime:                time.Unix(20, 0),
		Priority:                txs.PrimaryNetworkValidatorCurrentPriority,
		ContinuationPeriod:      15,
	}

	tests := []struct {
		name        string
		mutateFn    func(Staker) *Staker
		expectedErr error
	}{
		{
			name: "mutated tx id",
			mutateFn: func(staker Staker) *Staker {
				staker.TxID = ids.GenerateTestID()

				return &staker
			},
			expectedErr: errImmutableFieldsModified,
		},
		{
			name: "mutated node id",
			mutateFn: func(staker Staker) *Staker {
				staker.NodeID = ids.GenerateTestNodeID()

				return &staker
			},
			expectedErr: errImmutableFieldsModified,
		},
		{
			name: "mutated public key",
			mutateFn: func(staker Staker) *Staker {
				newSig, err := localsigner.New()
				require.NoError(t, err)

				staker.PublicKey = newSig.PublicKey()
				return &staker
			},
			expectedErr: errImmutableFieldsModified,
		},
		{
			name: "mutated subnet id",
			mutateFn: func(staker Staker) *Staker {
				staker.SubnetID = ids.GenerateTestID()
				return &staker
			},
			expectedErr: errImmutableFieldsModified,
		},
		{
			name: "mutated next time",
			mutateFn: func(staker Staker) *Staker {
				staker.NextTime = time.Unix(10, 0)
				return &staker
			},
			expectedErr: errImmutableFieldsModified,
		},
		{
			name: "mutated priority",
			mutateFn: func(staker Staker) *Staker {
				staker.Priority = txs.Priority(255)
				return &staker
			},
			expectedErr: errImmutableFieldsModified,
		},
		{
			name: "start time too early",
			mutateFn: func(staker Staker) *Staker {
				staker.StartTime = staker.EndTime.Add(-1 * time.Second)
				return &staker
			},
			expectedErr: errStartTimeTooEarly,
		},
		{
			name: "decreased accrued rewards",
			mutateFn: func(staker Staker) *Staker {
				staker.AccruedRewards -= 1
				return &staker
			},
			expectedErr: errDecreasedAccruedRewards,
		},
		{
			name: "decreased accrued delegatee rewards",
			mutateFn: func(staker Staker) *Staker {
				staker.AccruedDelegateeRewards -= 1
				return &staker
			},
			expectedErr: errDecreasedAccruedDelegateeRewards,
		},
		{
			name: "decreased weight",
			mutateFn: func(staker Staker) *Staker {
				staker.Weight -= 1
				return &staker
			},
			expectedErr: errDecreasedWeight,
		},
		{
			name: "invalid continuation period (continuous staker)",
			mutateFn: func(staker Staker) *Staker {
				staker.ContinuationPeriod = 10
				return &staker
			},
			expectedErr: errInvalidContinuationPeriod,
		},
		{
			name: "valid mutation",
			mutateFn: func(staker Staker) *Staker {
				staker.Weight = 200
				staker.StartTime = time.Unix(30, 0)
				staker.EndTime = time.Unix(40, 0)
				staker.PotentialReward = 20
				staker.AccruedRewards = 30
				staker.AccruedDelegateeRewards = 25
				staker.ContinuationPeriod = 0
				return &staker
			},
			expectedErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			require.ErrorIs(
				test.expectedErr,
				continuousStaker.ValidMutation(
					*test.mutateFn(*continuousStaker),
				),
			)
		})
	}

	// Test the invalid continuation period using a fixed staker.
	fixedStaker := continuousStaker
	fixedStaker.ContinuationPeriod = 0

	mutateFn := func(staker Staker) *Staker {
		staker.ContinuationPeriod = 5
		return &staker
	}

	t.Run("invalid continuation period (fixed staker)", func(t *testing.T) {
		require := require.New(t)

		require.ErrorIs(
			errInvalidContinuationPeriod,
			fixedStaker.ValidMutation(*mutateFn(*fixedStaker)),
		)
	})
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
