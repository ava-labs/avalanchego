// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestTxComplexity_Individual(t *testing.T) {
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

			require.Len(txBytes, int(actual[gas.Bandwidth]))
		})
	}
}

func TestTxComplexity_Batch(t *testing.T) {
	require := require.New(t)

	var (
		unsignedTxs        = make([]txs.UnsignedTx, 0, len(txTests))
		expectedComplexity gas.Dimensions
	)
	for _, test := range txTests {
		if test.expectedComplexityErr != nil {
			continue
		}

		var err error
		expectedComplexity, err = test.expectedComplexity.Add(&expectedComplexity)
		require.NoError(err)

		txBytes, err := hex.DecodeString(test.tx)
		require.NoError(err)

		tx, err := txs.Parse(txs.Codec, txBytes)
		require.NoError(err)

		unsignedTxs = append(unsignedTxs, tx.Unsigned)
	}

	complexity, err := TxComplexity(unsignedTxs...)
	require.NoError(err)
	require.Equal(expectedComplexity, complexity)
}

func BenchmarkTxComplexity_Individual(b *testing.B) {
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

func BenchmarkTxComplexity_Batch(b *testing.B) {
	require := require.New(b)

	unsignedTxs := make([]txs.UnsignedTx, 0, len(txTests))
	for _, test := range txTests {
		if test.expectedComplexityErr != nil {
			continue
		}

		txBytes, err := hex.DecodeString(test.tx)
		require.NoError(err)

		tx, err := txs.Parse(txs.Codec, txBytes)
		require.NoError(err)

		unsignedTxs = append(unsignedTxs, tx.Unsigned)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = TxComplexity(unsignedTxs...)
	}
}

func TestOutputComplexity(t *testing.T) {
	tests := []struct {
		name        string
		out         *avax.TransferableOutput
		expected    gas.Dimensions
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
			expected: gas.Dimensions{
				gas.Bandwidth: 60,
				gas.DBWrite:   1,
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
			expected: gas.Dimensions{
				gas.Bandwidth: 80,
				gas.DBWrite:   1,
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
			expected: gas.Dimensions{
				gas.Bandwidth: 120,
				gas.DBWrite:   1,
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
			expected: gas.Dimensions{
				gas.Bandwidth: 132,
				gas.DBWrite:   1,
			},
			expectedErr: nil,
		},
		{
			name: "invalid output type",
			out: &avax.TransferableOutput{
				Out: nil,
			},
			expected:    gas.Dimensions{},
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
			require.Equal(numBytesWithoutCodecVersion, actual[gas.Bandwidth])
		})
	}
}

func TestInputComplexity(t *testing.T) {
	tests := []struct {
		name        string
		in          *avax.TransferableInput
		cred        verify.Verifiable
		expected    gas.Dimensions
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
			expected: gas.Dimensions{
				gas.Bandwidth: 92,
				gas.DBRead:    1,
				gas.DBWrite:   1,
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
			expected: gas.Dimensions{
				gas.Bandwidth: 161,
				gas.DBRead:    1,
				gas.DBWrite:   1,
				gas.Compute:   200,
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
			expected: gas.Dimensions{
				gas.Bandwidth: 299,
				gas.DBRead:    1,
				gas.DBWrite:   1,
				gas.Compute:   600,
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
			expected: gas.Dimensions{
				gas.Bandwidth: 311,
				gas.DBRead:    1,
				gas.DBWrite:   1,
				gas.Compute:   600,
			},
			expectedErr: nil,
		},
		{
			name: "invalid input type",
			in: &avax.TransferableInput{
				In: nil,
			},
			cred:        nil,
			expected:    gas.Dimensions{},
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
			require.Equal(numBytesWithoutCodecVersion, actual[gas.Bandwidth])
		})
	}
}

func TestConvertSubnetToL1ValidatorComplexity(t *testing.T) {
	tests := []struct {
		name     string
		vdr      txs.ConvertSubnetToL1Validator
		expected gas.Dimensions
	}{
		{
			name: "any can spend",
			vdr: txs.ConvertSubnetToL1Validator{
				NodeID:                make([]byte, ids.NodeIDLen),
				Signer:                signer.ProofOfPossession{},
				RemainingBalanceOwner: message.PChainOwner{},
				DeactivationOwner:     message.PChainOwner{},
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 200,
				gas.DBWrite:   4,
				gas.Compute:   1050,
			},
		},
		{
			name: "single remaining balance owner",
			vdr: txs.ConvertSubnetToL1Validator{
				NodeID: make([]byte, ids.NodeIDLen),
				Signer: signer.ProofOfPossession{},
				RemainingBalanceOwner: message.PChainOwner{
					Threshold: 1,
					Addresses: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				},
				DeactivationOwner: message.PChainOwner{},
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 220,
				gas.DBWrite:   4,
				gas.Compute:   1050,
			},
		},
		{
			name: "single deactivation owner",
			vdr: txs.ConvertSubnetToL1Validator{
				NodeID:                make([]byte, ids.NodeIDLen),
				Signer:                signer.ProofOfPossession{},
				RemainingBalanceOwner: message.PChainOwner{},
				DeactivationOwner: message.PChainOwner{
					Threshold: 1,
					Addresses: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				},
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 220,
				gas.DBWrite:   4,
				gas.Compute:   1050,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := ConvertSubnetToL1ValidatorComplexity(&test.vdr)
			require.NoError(err)
			require.Equal(test.expected, actual)

			vdrBytes, err := txs.Codec.Marshal(txs.CodecVersion, test.vdr)
			require.NoError(err)

			numBytesWithoutCodecVersion := uint64(len(vdrBytes) - codec.VersionSize)
			require.Equal(numBytesWithoutCodecVersion, actual[gas.Bandwidth])
		})
	}
}

func TestOwnerComplexity(t *testing.T) {
	tests := []struct {
		name        string
		owner       fx.Owner
		expected    gas.Dimensions
		expectedErr error
	}{
		{
			name: "any can spend",
			owner: &secp256k1fx.OutputOwners{
				Addrs: make([]ids.ShortID, 0),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 16,
			},
			expectedErr: nil,
		},
		{
			name: "one owner",
			owner: &secp256k1fx.OutputOwners{
				Addrs: make([]ids.ShortID, 1),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 36,
			},
			expectedErr: nil,
		},
		{
			name: "three owners",
			owner: &secp256k1fx.OutputOwners{
				Addrs: make([]ids.ShortID, 3),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 76,
			},
			expectedErr: nil,
		},
		{
			name:        "invalid owner type",
			owner:       nil,
			expected:    gas.Dimensions{},
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
			require.Equal(numBytesWithoutCodecVersion, actual[gas.Bandwidth])
		})
	}
}

func TestAuthComplexity(t *testing.T) {
	tests := []struct {
		name        string
		auth        verify.Verifiable
		cred        verify.Verifiable
		expected    gas.Dimensions
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
			expected: gas.Dimensions{
				gas.Bandwidth: 8,
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
			expected: gas.Dimensions{
				gas.Bandwidth: 77,
				gas.Compute:   200,
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
			expected: gas.Dimensions{
				gas.Bandwidth: 215,
				gas.Compute:   600,
			},
			expectedErr: nil,
		},
		{
			name:        "invalid auth type",
			auth:        nil,
			cred:        nil,
			expected:    gas.Dimensions{},
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
			require.Equal(numBytesWithoutCodecVersion, actual[gas.Bandwidth])
		})
	}
}

func TestSignerComplexity(t *testing.T) {
	tests := []struct {
		name        string
		signer      signer.Signer
		expected    gas.Dimensions
		expectedErr error
	}{
		{
			name:        "empty",
			signer:      &signer.Empty{},
			expected:    gas.Dimensions{},
			expectedErr: nil,
		},
		{
			name:   "bls pop",
			signer: &signer.ProofOfPossession{},
			expected: gas.Dimensions{
				gas.Bandwidth: 144,
				gas.Compute:   1050,
			},
			expectedErr: nil,
		},
		{
			name:        "invalid signer type",
			signer:      nil,
			expected:    gas.Dimensions{},
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
			require.Equal(numBytesWithoutCodecVersion, actual[gas.Bandwidth])
		})
	}
}
