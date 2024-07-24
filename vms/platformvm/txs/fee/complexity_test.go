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
	tests := []struct {
		name     string
		tx       string
		expected fee.Dimensions
	}{
		{
			name: "AddPermissionlessValidatorTx",
			tx:   "00000000001900003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000700238520ba8b1e00000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001043c91e9d508169329034e2a68110427a311f945efc53ed3f3493d335b393fd100000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f263d53e00000000010000000000000000c582872c37c81efa2c94ea347af49cdc23a830aa00000000669ae35f0000000066b692df000001d1a94a200000000000000000000000000000000000000000000000000000000000000000000000001ca3783a891cb41cadbfcf456da149f30e7af972677a162b984bef0779f254baac51ec042df1781d1295df80fb41c801269731fc6c25e1e5940dc3cb8509e30348fa712742cfdc83678acc9f95908eb98b89b28802fb559b4a2a6ff3216707c07f0ceb0b45a95f4f9a9540bbd3331d8ab4f233bffa4abb97fad9d59a1695f31b92a2b89e365facf7ab8c30de7c4a496d1e00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000001d1a94a2000000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000b000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000b000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0007a12000000001000000090000000135f122f90bcece0d6c43e07fed1829578a23bc1734f8a4b46203f9f192ea1aec7526f3dca8fddec7418988615e6543012452bae1544275aae435313ec006ec9000",
			expected: fee.Dimensions{
				fee.Bandwidth: 691,
				fee.DBRead:    2,
				fee.DBWrite:   4,
				fee.Compute:   0, // TODO: implement
			},
		},
		{
			name: "AddPermissionlessDelegatorTx",
			tx:   "00000000001a00003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834f1140fe00000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c000000017d199179744b3b82d0071c83c2fb7dd6b95a2cdbe9dde295e0ae4f8c2287370300000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500238520ba8b1e00000000010000000000000000c582872c37c81efa2c94ea347af49cdc23a830aa00000000669ae6080000000066ad5b08000001d1a94a2000000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000001d1a94a2000000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000b000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000100000009000000012261556f74a29f02ffc2725a567db2c81f75d0892525dbebaa1cf8650534cc70061123533a9553184cb02d899943ff0bf0b39c77b173c133854bc7c8bc7ab9a400",
			expected: fee.Dimensions{
				fee.Bandwidth: 499,
				fee.DBRead:    2,
				fee.DBWrite:   4,
				fee.Compute:   0, // TODO: implement
			},
		},
		{
			name: "AddSubnetValidatorTx",
			tx:   "00000000000d00003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834f1131bbc0000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000138f94d1a0514eaabdaf4c52cad8d62b26cee61eaa951f5b75a5e57c2ee3793c800000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050023834f1140fe00000000010000000000000000c582872c37c81efa2c94ea347af49cdc23a830aa00000000669ae7c90000000066ad5cc9000000000000c13797ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c1240000000a00000001000000000000000200000009000000012127130d37877fb1ec4b2374ef72571d49cd7b0319a3769e5da19041a138166c10b1a5c07cf5ccf0419066cbe3bab9827cf29f9fa6213ebdadf19d4849501eb60000000009000000012127130d37877fb1ec4b2374ef72571d49cd7b0319a3769e5da19041a138166c10b1a5c07cf5ccf0419066cbe3bab9827cf29f9fa6213ebdadf19d4849501eb600",
			expected: fee.Dimensions{
				fee.Bandwidth: 460,
				fee.DBRead:    3,
				fee.DBWrite:   3,
				fee.Compute:   0, // TODO: implement
			},
		},
		{
			name: "BaseTx",
			tx:   "00000000002200003039000000000000000000000000000000000000000000000000000000000000000000000002dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000000003b9aca00000000000000000100000002000000024a177205df5c29929d06db9d941f83d5ea985de3e902a9a86640bfdb1cd0e36c0cc982b83e5765fadbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834ed587af80000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001fa4ff39749d44f29563ed9da03193d4a19ef419da4ce326594817ca266fda5ed00000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050023834f1131bbc00000000100000000000000000000000100000009000000014a7b54c63dd25a532b5fe5045b6d0e1db876e067422f12c9c327333c2c792d9273405ac8bbbc2cce549bbd3d0f9274242085ee257adfdb859b0f8d55bdd16fb000",
			expected: fee.Dimensions{
				fee.Bandwidth: 399,
				fee.DBRead:    1,
				fee.DBWrite:   3,
				fee.Compute:   0, // TODO: implement
			},
		},
		{
			name: "CreateChainTx",
			tx:   "00000000000f00003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007002386f263d53e00000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000197ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c12400000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f269cb1f0000000001000000000000000097ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c12400096c65742074686572657873766d00000000000000000000000000000000000000000000000000000000000000000000002a000000000000669ae21e000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29cffffffffffffffff0000000a0000000100000000000000020000000900000001cf8104877b1a59b472f4f34d360c0e4f38e92c5fa334215430d0b99cf78eae8f621b6daf0b0f5c3a58a9497601f978698a1e5545d1873db8f2f38ecb7496c2f8010000000900000001cf8104877b1a59b472f4f34d360c0e4f38e92c5fa334215430d0b99cf78eae8f621b6daf0b0f5c3a58a9497601f978698a1e5545d1873db8f2f38ecb7496c2f801",
			expected: fee.Dimensions{
				fee.Bandwidth: 509,
				fee.DBRead:    2,
				fee.DBWrite:   3,
				fee.Compute:   0, // TODO: implement
			},
		},
		{
			name: "CreateSubnetTx",
			tx:   "00000000001000003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007002386f269cb1f00000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f26fc100000000000100000000000000000000000b000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c000000010000000900000001b3c905e7227e619bd6b98c164a8b2b4a8ce89ac5142bbb1c42b139df2d17fd777c4c76eae66cef3de90800e567407945f58d918978f734f8ca4eda6923c78eb201",
			expected: fee.Dimensions{
				fee.Bandwidth: 339,
				fee.DBRead:    1,
				fee.DBWrite:   3,
				fee.Compute:   0, // TODO: implement
			},
		},
		{
			name: "ExportTx",
			tx:   "00000000001200003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834e99dda340000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001f62c03574790b6a31a988f90c3e91c50fdd6f5d93baf200057463021ff23ec5c00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050023834ed587af800000000100000000000000009d0775f450604bd2fbc49ce0c5c1c6dfeb2dc2acb8c92c26eeae6e6df4502b1900000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000000003b9aca00000000000000000100000002000000024a177205df5c29929d06db9d941f83d5ea985de3e902a9a86640bfdb1cd0e36c0cc982b83e5765fa000000010000000900000001129a07c92045e0b9d0a203fcb5b53db7890fabce1397ff6a2ad16c98ef0151891ae72949d240122abf37b1206b95e05ff171df164a98e6bdf2384432eac2c30200",
			expected: fee.Dimensions{
				fee.Bandwidth: 435,
				fee.DBRead:    1,
				fee.DBWrite:   3,
				fee.Compute:   0, // TODO: implement
			},
		},
		{
			name: "ImportTx",
			tx:   "00000000001100003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000000003b8b87c0000000000000000100000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000000000000d891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf0000000163684415710a7d65f4ccb095edff59f897106b94d38937fc60e3ffc29892833b00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005000000003b9aca00000000010000000000000001000000090000000148ea12cb0950e47d852b99765208f5a811d3c8a47fa7b23fd524bd970019d157029f973abb91c31a146752ef8178434deb331db24c8dca5e61c961e6ac2f3b6700",
			expected: fee.Dimensions{
				fee.Bandwidth: 335,
				fee.DBRead:    1,
				fee.DBWrite:   2,
				fee.Compute:   0, // TODO: implement
			},
		},
		{
			name: "RemoveSubnetValidatorTx",
			tx:   "00000000001700003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834e99ce6100000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001cd4569cfd044d50636fa597c700710403b3b52d3b75c30c542a111cc52c911ec00000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050023834e99dda340000000010000000000000000c582872c37c81efa2c94ea347af49cdc23a830aa97ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c1240000000a0000000100000000000000020000000900000001673ee3e5a3a1221935274e8ff5c45b27ebe570e9731948e393a8ebef6a15391c189a54de7d2396095492ae171103cd4bfccfc2a4dafa001d48c130694c105c2d010000000900000001673ee3e5a3a1221935274e8ff5c45b27ebe570e9731948e393a8ebef6a15391c189a54de7d2396095492ae171103cd4bfccfc2a4dafa001d48c130694c105c2d01",
			expected: fee.Dimensions{
				fee.Bandwidth: 436,
				fee.DBRead:    3,
				fee.DBWrite:   3,
				fee.Compute:   0, // TODO: implement
			},
		},
		{
			name: "TransferSubnetOwnershipTx",
			tx:   "00000000002100003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834e99bf1ec0000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c000000018f6e5f2840e34f9a375f35627a44bb0b9974285d280dc3220aa9489f97b17ebd00000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050023834e99ce610000000001000000000000000097ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c1240000000a00000001000000000000000b00000000000000000000000000000000000000020000000900000001e3479034ed8134dd23e154e1ec6e61b25073a20750ebf808e50ec1aae180ef430f8151347afdf6606bc7866f7f068b01719e4dad12e2976af1159fb048f73f7f010000000900000001e3479034ed8134dd23e154e1ec6e61b25073a20750ebf808e50ec1aae180ef430f8151347afdf6606bc7866f7f068b01719e4dad12e2976af1159fb048f73f7f01",
			expected: fee.Dimensions{
				fee.Bandwidth: 436,
				fee.DBRead:    2,
				fee.DBWrite:   3,
				fee.Compute:   0, // TODO: implement
			},
		},
	}
	for _, test := range tests {
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
			require.NoError(err)
			require.Equal(test.expected, actual)

			require.Len(txBytes, int(actual[fee.Bandwidth]))
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
