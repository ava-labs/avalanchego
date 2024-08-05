// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/fee"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestTxComplexity(t *testing.T) {
	for _, test := range txTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			txBytes, err := hex.DecodeString(test.tx)
			require.NoError(err)

			tx, err := txs.Parse(txs.Codec, txBytes)
			require.NoError(err)

			// If the test fails, logging the transaction can be helpful for
			// debugging.
			txJSON, err := json.MarshalIndent(tx, "", "\t")
			require.NoError(err)
			t.Log(string(txJSON))

			actual, err := TxComplexity(tx.Unsigned)
			require.Equal(test.expectedComplexity, actual)
			require.ErrorIs(err, test.expectedComplexityErr)
			if err != nil {
				return
			}

			require.Len(txBytes, int(actual[fee.Bandwidth]))
		})
	}
}

func BenchmarkTxComplexity(b *testing.B) {
	for _, test := range txTests {
		b.Run(test.name, func(b *testing.B) {
			require := require.New(b)

			txBytes, err := hex.DecodeString(test.tx)
			require.NoError(err)

			tx, err := txs.Parse(txs.Codec, txBytes)
			require.NoError(err)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = TxComplexity(tx.Unsigned)
			}
		})
	}
}

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

			numBytesWithoutCodecVersion := uint64(len(bytes) - codec.VersionSize)
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

			numBytesWithoutCodecVersion := uint64(len(inputBytes) + len(credentialBytes) - 2*codec.VersionSize)
			require.Equal(numBytesWithoutCodecVersion, actual[fee.Bandwidth])
		})
	}
}

func TestOwnerComplexity(t *testing.T) {
	tests := []struct {
		name        string
		owner       fx.Owner
		expected    fee.Dimensions
		expectedErr error
	}{
		{
			name: "any can spend",
			owner: &secp256k1fx.OutputOwners{
				Addrs: make([]ids.ShortID, 0),
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 16,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0,
			},
			expectedErr: nil,
		},
		{
			name: "one owner",
			owner: &secp256k1fx.OutputOwners{
				Addrs: make([]ids.ShortID, 1),
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 36,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0,
			},
			expectedErr: nil,
		},
		{
			name: "three owners",
			owner: &secp256k1fx.OutputOwners{
				Addrs: make([]ids.ShortID, 3),
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 76,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0,
			},
			expectedErr: nil,
		},
		{
			name:        "invalid owner type",
			owner:       nil,
			expected:    fee.Dimensions{},
			expectedErr: errUnsupportedOwner,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := OwnerComplexity(test.owner)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)

			if err != nil {
				return
			}

			ownerBytes, err := txs.Codec.Marshal(txs.CodecVersion, test.owner)
			require.NoError(err)

			numBytesWithoutCodecVersion := uint64(len(ownerBytes) - codec.VersionSize)
			require.Equal(numBytesWithoutCodecVersion, actual[fee.Bandwidth])
		})
	}
}

func TestAuthComplexity(t *testing.T) {
	tests := []struct {
		name        string
		auth        verify.Verifiable
		cred        verify.Verifiable
		expected    fee.Dimensions
		expectedErr error
	}{
		{
			name: "any can spend",
			auth: &secp256k1fx.Input{
				SigIndices: make([]uint32, 0),
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 0),
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 8,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0, // TODO: implement
			},
			expectedErr: nil,
		},
		{
			name: "one owner",
			auth: &secp256k1fx.Input{
				SigIndices: make([]uint32, 1),
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 1),
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 77,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0, // TODO: implement
			},
			expectedErr: nil,
		},
		{
			name: "three owners",
			auth: &secp256k1fx.Input{
				SigIndices: make([]uint32, 3),
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 3),
			},
			expected: fee.Dimensions{
				fee.Bandwidth: 215,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0, // TODO: implement
			},
			expectedErr: nil,
		},
		{
			name: "invalid auth type",
			auth: nil,
			cred: nil,
			expected: fee.Dimensions{
				fee.Bandwidth: 0,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0, // TODO: implement
			},
			expectedErr: errUnsupportedAuth,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := AuthComplexity(test.auth)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)

			if err != nil {
				return
			}

			authBytes, err := txs.Codec.Marshal(txs.CodecVersion, test.auth)
			require.NoError(err)

			credentialBytes, err := txs.Codec.Marshal(txs.CodecVersion, test.cred)
			require.NoError(err)

			numBytesWithoutCodecVersion := uint64(len(authBytes) + len(credentialBytes) - 2*codec.VersionSize)
			require.Equal(numBytesWithoutCodecVersion, actual[fee.Bandwidth])
		})
	}
}

func TestSignerComplexity(t *testing.T) {
	tests := []struct {
		name        string
		signer      signer.Signer
		expected    fee.Dimensions
		expectedErr error
	}{
		{
			name:   "empty",
			signer: &signer.Empty{},
			expected: fee.Dimensions{
				fee.Bandwidth: 0,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0,
			},
			expectedErr: nil,
		},
		{
			name:   "bls pop",
			signer: &signer.ProofOfPossession{},
			expected: fee.Dimensions{
				fee.Bandwidth: 144,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0, // TODO: implement
			},
			expectedErr: nil,
		},
		{
			name:   "invalid signer type",
			signer: nil,
			expected: fee.Dimensions{
				fee.Bandwidth: 0,
				fee.DBRead:    0,
				fee.DBWrite:   0,
				fee.Compute:   0,
			},
			expectedErr: errUnsupportedSigner,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := SignerComplexity(test.signer)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)

			if err != nil {
				return
			}

			signerBytes, err := txs.Codec.Marshal(txs.CodecVersion, test.signer)
			require.NoError(err)

			numBytesWithoutCodecVersion := uint64(len(signerBytes) - codec.VersionSize)
			require.Equal(numBytesWithoutCodecVersion, actual[fee.Bandwidth])
		})
	}
}
