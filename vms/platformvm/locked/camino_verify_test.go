// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
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
				unlockedIn(),
				stakeableIn(),
			},
			outs: []*avax.TransferableOutput{
				unlockedOut(),
				stakeableOut(),
			},
			lockModeDepositBonding: false,
			expectedErr:            nil,
		},
		"OK (lockModeDepositBonding true)": {
			ins: []*avax.TransferableInput{
				unlockedIn(),
				lockedIn(),
			},
			outs: []*avax.TransferableOutput{
				unlockedOut(),
				lockedOut(),
			},
			lockModeDepositBonding: true,
			expectedErr:            nil,
		},
		"fail (lockModeDepositBonding false): wrong input type": {
			ins: []*avax.TransferableInput{
				lockedIn(),
			},
			outs:                   []*avax.TransferableOutput{},
			lockModeDepositBonding: false,
			expectedErr:            ErrWrongInType,
		},
		"fail (lockModeDepositBonding false): wrong output type": {
			ins: []*avax.TransferableInput{},
			outs: []*avax.TransferableOutput{
				lockedOut(),
			},
			lockModeDepositBonding: false,
			expectedErr:            ErrWrongOutType,
		},
		"fail (lockModeDepositBonding true): wrong input type": {
			ins: []*avax.TransferableInput{
				stakeableIn(),
			},
			outs:                   []*avax.TransferableOutput{},
			lockModeDepositBonding: true,
			expectedErr:            ErrWrongInType,
		},
		"fail (lockModeDepositBonding true): wrong output type": {
			ins: []*avax.TransferableInput{},
			outs: []*avax.TransferableOutput{
				stakeableOut(),
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
				unlockedIn(),
			},
			outs: []*avax.TransferableOutput{
				unlockedOut(),
			},
			expectedErr: nil,
		},
		"fail: locked.In": {
			ins: []*avax.TransferableInput{
				lockedIn(),
			},
			outs:        []*avax.TransferableOutput{},
			expectedErr: ErrWrongInType,
		},
		"fail: locked.Out": {
			ins: []*avax.TransferableInput{},
			outs: []*avax.TransferableOutput{
				lockedOut(),
			},
			expectedErr: ErrWrongOutType,
		},
		"fail: stakeable.LockIn": {
			ins: []*avax.TransferableInput{
				stakeableIn(),
			},
			outs:        []*avax.TransferableOutput{},
			expectedErr: ErrWrongInType,
		},
		"fail: stakeable.LockOut": {
			ins: []*avax.TransferableInput{},
			outs: []*avax.TransferableOutput{
				stakeableOut(),
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

func unlockedIn() *avax.TransferableInput {
	return &avax.TransferableInput{
		In: &secp256k1fx.TransferInput{},
	}
}

func lockedIn() *avax.TransferableInput {
	return &avax.TransferableInput{
		In: &In{TransferableIn: &secp256k1fx.TransferInput{}},
	}
}

func stakeableIn() *avax.TransferableInput {
	return &avax.TransferableInput{
		In: &stakeable.LockIn{TransferableIn: &secp256k1fx.TransferInput{}},
	}
}

func unlockedOut() *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Out: &secp256k1fx.TransferOutput{},
	}
}

func lockedOut() *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Out: &Out{TransferableOut: &secp256k1fx.TransferOutput{}},
	}
}

func stakeableOut() *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Out: &stakeable.LockOut{TransferableOut: &secp256k1fx.TransferOutput{}},
	}
}
