// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func newBLSPublicKey(t *testing.T) [bls.PublicKeyLen]byte {
	sk, err := localsigner.New()
	require.NoError(t, err)

	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToCompressedBytes(pk)
	return [bls.PublicKeyLen]byte(pkBytes)
}

func TestRegisterL1Validator(t *testing.T) {
	require := require.New(t)

	msg, err := NewRegisterL1Validator(
		ids.GenerateTestID(),
		ids.GenerateTestNodeID(),
		newBLSPublicKey(t),
		rand.Uint64(), //#nosec G404
		PChainOwner{
			Threshold: rand.Uint32(), //#nosec G404
			Addresses: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
		PChainOwner{
			Threshold: rand.Uint32(), //#nosec G404
			Addresses: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
		rand.Uint64(), //#nosec G404
	)
	require.NoError(err)

	bytes := msg.Bytes()
	var expectedValidationID ids.ID = hashing.ComputeHash256Array(bytes)
	require.Equal(expectedValidationID, msg.ValidationID())

	parsed, err := ParseRegisterL1Validator(bytes)
	require.NoError(err)
	require.Equal(msg, parsed)
}

func TestRegisterL1Validator_Verify(t *testing.T) {
	mustCreate := func(msg *RegisterL1Validator, err error) *RegisterL1Validator {
		require.NoError(t, err)
		return msg
	}
	tests := []struct {
		name     string
		msg      *RegisterL1Validator
		expected error
	}{
		{
			name: "PrimaryNetworkID",
			msg: mustCreate(NewRegisterL1Validator(
				constants.PrimaryNetworkID,
				ids.GenerateTestNodeID(),
				newBLSPublicKey(t),
				rand.Uint64(), //#nosec G404
				PChainOwner{
					Threshold: 1,
					Addresses: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				},
				PChainOwner{
					Threshold: 0,
				},
				1,
			)),
			expected: ErrInvalidSubnetID,
		},
		{
			name: "Weight = 0",
			msg: mustCreate(NewRegisterL1Validator(
				ids.GenerateTestID(),
				ids.GenerateTestNodeID(),
				newBLSPublicKey(t),
				rand.Uint64(), //#nosec G404
				PChainOwner{
					Threshold: 1,
					Addresses: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				},
				PChainOwner{
					Threshold: 0,
				},
				0,
			)),
			expected: ErrInvalidWeight,
		},
		{
			name: "Invalid NodeID Length",
			msg: &RegisterL1Validator{
				SubnetID:     ids.GenerateTestID(),
				NodeID:       nil,
				BLSPublicKey: newBLSPublicKey(t),
				Expiry:       rand.Uint64(), //#nosec G404
				RemainingBalanceOwner: PChainOwner{
					Threshold: 1,
					Addresses: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				},
				DisableOwner: PChainOwner{
					Threshold: 0,
				},
				Weight: 1,
			},
			expected: ErrInvalidNodeID,
		},
		{
			name: "Invalid NodeID",
			msg: mustCreate(NewRegisterL1Validator(
				ids.GenerateTestID(),
				ids.EmptyNodeID,
				newBLSPublicKey(t),
				rand.Uint64(), //#nosec G404
				PChainOwner{
					Threshold: 1,
					Addresses: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				},
				PChainOwner{
					Threshold: 0,
				},
				1,
			)),
			expected: ErrInvalidNodeID,
		},
		{
			name: "Invalid Owner",
			msg: mustCreate(NewRegisterL1Validator(
				ids.GenerateTestID(),
				ids.GenerateTestNodeID(),
				newBLSPublicKey(t),
				rand.Uint64(), //#nosec G404
				PChainOwner{
					Threshold: 0,
					Addresses: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				},
				PChainOwner{
					Threshold: 0,
				},
				1,
			)),
			expected: ErrInvalidOwner,
		},
		{
			name: "Valid",
			msg: mustCreate(NewRegisterL1Validator(
				ids.GenerateTestID(),
				ids.GenerateTestNodeID(),
				newBLSPublicKey(t),
				rand.Uint64(), //#nosec G404
				PChainOwner{
					Threshold: 1,
					Addresses: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				},
				PChainOwner{
					Threshold: 0,
				},
				1,
			)),
			expected: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.msg.Verify()
			require.ErrorIs(t, err, test.expected)
		})
	}
}
