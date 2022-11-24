// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package locked

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestVerifyLockMode(t *testing.T) {
	tests := map[string]struct {
		ins                    []*avax.TransferableInput
		outs                   []*avax.TransferableOutput
		lockModeDepositBonding bool
		expectedErr            error
	}{
		"OK (lockModeDepositBonding false)": {
			ins: []*avax.TransferableInput{
				generateTestIn(),
				generateTestStakeableIn(),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(),
				generateTestStakeableOut(),
			},
			lockModeDepositBonding: false,
			expectedErr:            nil,
		},
		"OK (lockModeDepositBonding true)": {
			ins: []*avax.TransferableInput{
				generateTestIn(),
				generateTestLockedIn(),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(),
				generateTestLockedOut(),
			},
			lockModeDepositBonding: true,
			expectedErr:            nil,
		},
		"fail (lockModeDepositBonding false): wrong input type": {
			ins: []*avax.TransferableInput{
				generateTestLockedIn(),
			},
			outs:                   []*avax.TransferableOutput{},
			lockModeDepositBonding: false,
			expectedErr:            ErrWrongInType,
		},
		"fail (lockModeDepositBonding false): wrong output type": {
			ins: []*avax.TransferableInput{},
			outs: []*avax.TransferableOutput{
				generateTestLockedOut(),
			},
			lockModeDepositBonding: false,
			expectedErr:            ErrWrongOutType,
		},
		"fail (lockModeDepositBonding true): wrong input type": {
			ins: []*avax.TransferableInput{
				generateTestStakeableIn(),
			},
			outs:                   []*avax.TransferableOutput{},
			lockModeDepositBonding: true,
			expectedErr:            ErrWrongInType,
		},
		"fail (lockModeDepositBonding true): wrong output type": {
			ins: []*avax.TransferableInput{},
			outs: []*avax.TransferableOutput{
				generateTestStakeableOut(),
			},
			lockModeDepositBonding: true,
			expectedErr:            ErrWrongOutType,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := VerifyLockMode(
				test.ins,
				test.outs,
				test.lockModeDepositBonding,
			)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

func TestVerifyNoLocks(t *testing.T) {
	tests := map[string]struct {
		ins         []*avax.TransferableInput
		outs        []*avax.TransferableOutput
		expectedErr error
	}{
		"OK": {
			ins: []*avax.TransferableInput{
				generateTestIn(),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(),
			},
			expectedErr: nil,
		},
		"fail: locked.In": {
			ins: []*avax.TransferableInput{
				generateTestLockedIn(),
			},
			outs:        []*avax.TransferableOutput{},
			expectedErr: ErrWrongInType,
		},
		"fail: locked.Out": {
			ins: []*avax.TransferableInput{},
			outs: []*avax.TransferableOutput{
				generateTestLockedOut(),
			},
			expectedErr: ErrWrongOutType,
		},
		"fail: stakeable.LockIn": {
			ins: []*avax.TransferableInput{
				generateTestStakeableIn(),
			},
			outs:        []*avax.TransferableOutput{},
			expectedErr: ErrWrongInType,
		},
		"fail: stakeable.LockOut": {
			ins: []*avax.TransferableInput{},
			outs: []*avax.TransferableOutput{
				generateTestStakeableOut(),
			},
			expectedErr: ErrWrongOutType,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := VerifyNoLocks(
				test.ins,
				test.outs,
			)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

func generateTestIn() *avax.TransferableInput {
	return &avax.TransferableInput{
		In: &secp256k1fx.TransferInput{},
	}
}

func generateTestLockedIn() *avax.TransferableInput {
	return &avax.TransferableInput{
		In: &In{TransferableIn: &secp256k1fx.TransferInput{}},
	}
}

func generateTestStakeableIn() *avax.TransferableInput {
	return &avax.TransferableInput{
		In: &stakeable.LockIn{TransferableIn: &secp256k1fx.TransferInput{}},
	}
}

func generateTestOut() *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Out: &secp256k1fx.TransferOutput{},
	}
}

func generateTestLockedOut() *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Out: &Out{TransferableOut: &secp256k1fx.TransferOutput{}},
	}
}

func generateTestStakeableOut() *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Out: &stakeable.LockOut{TransferableOut: &secp256k1fx.TransferOutput{}},
	}
}
