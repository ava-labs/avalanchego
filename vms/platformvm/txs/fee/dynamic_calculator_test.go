// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestOutputComplexity(t *testing.T) {
	tests := []struct {
		name        string
		out         *avax.TransferableOutput
		expected    fee.Dimensions
		expectedErr error
	}{
		{
			name: "any can spend",
			out: &avax.TransferableOutput{
				Out: &secp256k1fx.TransferOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Addrs: make([]ids.ShortID, 0),
					},
				},
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 60,
				fee.DBRead:    0,
				fee.DBWrite:   1,
				fee.Compute:   0,
			},
			expectedErr: nil,
		},
		{
			name: "one owner",
			out: &avax.TransferableOutput{
				Out: &secp256k1fx.TransferOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Addrs: make([]ids.ShortID, 1),
					},
				},
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 80,
				fee.DBRead:    0,
				fee.DBWrite:   1,
				fee.Compute:   0,
			},
			expectedErr: nil,
		},
		{
			name: "three owners",
			out: &avax.TransferableOutput{
				Out: &secp256k1fx.TransferOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Addrs: make([]ids.ShortID, 3),
					},
				},
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 120,
				fee.DBRead:    0,
				fee.DBWrite:   1,
				fee.Compute:   0,
			},
			expectedErr: nil,
		},
		{
			name: "locked stakeable",
			out: &avax.TransferableOutput{
				Out: &stakeable.LockOut{
					TransferableOut: &secp256k1fx.TransferOutput{
						OutputOwners: secp256k1fx.OutputOwners{
							Addrs: make([]ids.ShortID, 3),
						},
					},
				},
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 132,
				fee.DBRead:    0,
				fee.DBWrite:   1,
				fee.Compute:   0,
			},
			expectedErr: nil,
		},
		{
			name: "invalid output type",
			out: &avax.TransferableOutput{
				Out: nil,
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 0,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0,
			},
			expectedErr: errUnsupportedOutput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := OutputComplexity(test.out)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)

			if err != nil {
				return
			}

			bytes, err := txs.Codec.Marshal(txs.CodecVersion, test.out)
			require.NoError(err)

			numBytesWithoutCodecVersion := uint64(len(bytes) - wrappers.ShortLen)
			require.Equal(numBytesWithoutCodecVersion, actual[fee.Bandwidth])
		})
	}
}

func TestInputComplexity(t *testing.T) {
	tests := []struct {
		name        string
		in          *avax.TransferableInput
		cred        verify.Verifiable
		expected    fee.Dimensions
		expectedErr error
	}{
		{
			name: "any can spend",
			in: &avax.TransferableInput{
				In: &secp256k1fx.TransferInput{
					Input: secp256k1fx.Input{
						SigIndices: make([]uint32, 0),
					},
				},
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 0),
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 92,
				fee.DBRead:    1,
				fee.DBWrite:   1,
				fee.Compute:   0, // TODO: implement
			},
			expectedErr: nil,
		},
		{
			name: "one owner",
			in: &avax.TransferableInput{
				In: &secp256k1fx.TransferInput{
					Input: secp256k1fx.Input{
						SigIndices: make([]uint32, 1),
					},
				},
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 1),
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 161,
				fee.DBRead:    1,
				fee.DBWrite:   1,
				fee.Compute:   0, // TODO: implement
			},
			expectedErr: nil,
		},
		{
			name: "three owners",
			in: &avax.TransferableInput{
				In: &secp256k1fx.TransferInput{
					Input: secp256k1fx.Input{
						SigIndices: make([]uint32, 3),
					},
				},
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 3),
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 299,
				fee.DBRead:    1,
				fee.DBWrite:   1,
				fee.Compute:   0, // TODO: implement
			},
			expectedErr: nil,
		},
		{
			name: "locked stakeable",
			in: &avax.TransferableInput{
				In: &stakeable.LockIn{
					TransferableIn: &secp256k1fx.TransferInput{
						Input: secp256k1fx.Input{
							SigIndices: make([]uint32, 3),
						},
					},
				},
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 3),
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 311,
				fee.DBRead:    1,
				fee.DBWrite:   1,
				fee.Compute:   0, // TODO: implement
			},
			expectedErr: nil,
		},
		{
			name: "invalid input type",
			in: &avax.TransferableInput{
				In: nil,
			},
			cred: nil,
			expected: fee.Dimensions{
				fee.Bandwidth: 0,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0,
			},
			expectedErr: errUnsupportedInput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := InputComplexity(test.in)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)

			if err != nil {
				return
			}

			inputBytes, err := txs.Codec.Marshal(txs.CodecVersion, test.in)
			require.NoError(err)

			cred := test.cred
			credentialBytes, err := txs.Codec.Marshal(txs.CodecVersion, &cred)
			require.NoError(err)

			numBytesWithoutCodecVersion := uint64(len(inputBytes) + len(credentialBytes) - 2*wrappers.ShortLen)
			require.Equal(numBytesWithoutCodecVersion, actual[fee.Bandwidth])
		})
	}
}
