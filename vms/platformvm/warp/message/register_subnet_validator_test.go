// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func newBLSPublicKey(t *testing.T) [bls.PublicKeyLen]byte {
	sk, err := bls.NewSecretKey()
	require.NoError(t, err)

	pk := bls.PublicFromSecretKey(sk)
	pkBytes := bls.PublicKeyToCompressedBytes(pk)
	return [bls.PublicKeyLen]byte(pkBytes)
}

func TestRegisterSubnetValidator(t *testing.T) {
	require := require.New(t)

	msg, err := NewRegisterSubnetValidator(
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

	parsed, err := ParseRegisterSubnetValidator(bytes)
	require.NoError(err)
	require.Equal(msg, parsed)
}

func TestRegisterSubnetValidator_Verify(t *testing.T) {
	mustCreate := func(msg *RegisterSubnetValidator, err error) *RegisterSubnetValidator {
		require.NoError(t, err)
		return msg
	}
	tests := []struct {
		name     string
		msg      *RegisterSubnetValidator
		expected error
	}{
		{
			name: "PrimaryNetworkID",
			msg: mustCreate(NewRegisterSubnetValidator(
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
			msg: mustCreate(NewRegisterSubnetValidator(
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
			msg: &RegisterSubnetValidator{
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
			msg: mustCreate(NewRegisterSubnetValidator(
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
			msg: mustCreate(NewRegisterSubnetValidator(
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
			msg: mustCreate(NewRegisterSubnetValidator(
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
