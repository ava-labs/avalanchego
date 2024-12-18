// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

const testDynamicPrice = gas.Price(units.NanoAvax)

var (
	txFee = 1 * units.Avax
	testDynamicWeights = gas.Dimensions{
		gas.Bandwidth: 1,
		gas.DBRead:    2000,
		gas.DBWrite:   20000,
		gas.Compute:   10,
	}

	// TODO: Rather than hardcoding transactions, consider implementing and
	// using a transaction generator.
	txTests = []struct {
		name                  string
		tx                    string
		expectedStaticFee     uint64
		expectedStaticFeeErr  error
		expectedComplexity    gas.Dimensions
		expectedComplexityErr error
		expectedDynamicFee    uint64
		expectedDynamicFeeErr error
	}{
		{
			name:                  "AdvanceTimeTx",
			tx:                    "0000000000130000000066a56fe700000000",
			expectedStaticFeeErr:  ErrUnsupportedTx,
			expectedComplexityErr: ErrUnsupportedTx,
			expectedDynamicFeeErr: ErrUnsupportedTx,
		},
		{
			name:                  "RewardValidatorTx",
			tx:                    "0000000000143d0ad12b8ee8928edf248ca91ca55600fb383f07c32bff1d6dec472b25cf59a700000000",
			expectedStaticFeeErr:  ErrUnsupportedTx,
			expectedComplexityErr: ErrUnsupportedTx,
			expectedDynamicFeeErr: ErrUnsupportedTx,
		},
		{
			name:                  "AddValidatorTx",
			tx:                    "00000000000c0000000100000000000000000000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000000000f4b21e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000015000000006134088000000005000001d1a94a200000000001000000000000000400000000b3da694c70b8bee4478051313621c3f2282088b4000000005f6976d500000000614aaa19000001d1a94a20000000000121e67317cbc4be2aeb00677ad6462778a8f52274b9d605df2591b23027a87dff00000016000000006134088000000007000001d1a94a20000000000000000000000000010000000120868ed5ac611711b33d2e4f97085347415db1c40000000b0000000000000000000000010000000120868ed5ac611711b33d2e4f97085347415db1c400009c40000000010000000900000001620513952dd17c8726d52e9e621618cb38f09fd194abb4cd7b4ee35ecd10880a562ad968dc81a89beab4e87d88d5d582aa73d0d265c87892d1ffff1f6e00f0ef00",
			expectedStaticFee:     0,
			expectedComplexityErr: ErrUnsupportedTx,
			expectedDynamicFeeErr: ErrUnsupportedTx,
		},
		{
			name:                  "AddDelegatorTx",
			tx:                    "00000000000e000000050000000000000000000000000000000000000000000000000000000000000000000000013d9bdac0ed1d761330cf680efdeb1a42159eb387d6d2950c96f7d28f61bbe2aa00000007000000003b9aca0000000000000000000000000100000001f887b4c7030e95d2495603ae5d8b14cc0a66781a000000011767be999a49ca24fe705de032fa613b682493110fd6468ae7fb56bde1b9d729000000003d9bdac0ed1d761330cf680efdeb1a42159eb387d6d2950c96f7d28f61bbe2aa00000005000000012a05f20000000001000000000000000400000000c51c552c49174e2e18b392049d3e4cd48b11490f000000005f692452000000005f73b05200000000ee6b2800000000013d9bdac0ed1d761330cf680efdeb1a42159eb387d6d2950c96f7d28f61bbe2aa0000000700000000ee6b280000000000000000000000000100000001e0cfe8cae22827d032805ded484e393ce51cbedb0000000b00000000000000000000000100000001e0cfe8cae22827d032805ded484e393ce51cbedb00000001000000090000000135cd78758035ed528d230317e5d880083a86a2b68c4a95655571828fe226548f235031c8dabd1fe06366a57613c4370ac26c4c59d1a1c46287a59906ec41b88f00",
			expectedStaticFee:     0,
			expectedComplexityErr: ErrUnsupportedTx,
			expectedDynamicFeeErr: ErrUnsupportedTx,
		},
		{
			name:              "AddPermissionlessValidatorTx for primary network",
			tx:                "00000000001900003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000700238520ba8b1e00000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001043c91e9d508169329034e2a68110427a311f945efc53ed3f3493d335b393fd100000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f263d53e00000000010000000000000000c582872c37c81efa2c94ea347af49cdc23a830aa00000000669ae35f0000000066b692df000001d1a94a200000000000000000000000000000000000000000000000000000000000000000000000001ca3783a891cb41cadbfcf456da149f30e7af972677a162b984bef0779f254baac51ec042df1781d1295df80fb41c801269731fc6c25e1e5940dc3cb8509e30348fa712742cfdc83678acc9f95908eb98b89b28802fb559b4a2a6ff3216707c07f0ceb0b45a95f4f9a9540bbd3331d8ab4f233bffa4abb97fad9d59a1695f31b92a2b89e365facf7ab8c30de7c4a496d1e00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000001d1a94a2000000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000b000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000b000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0007a12000000001000000090000000135f122f90bcece0d6c43e07fed1829578a23bc1734f8a4b46203f9f192ea1aec7526f3dca8fddec7418988615e6543012452bae1544275aae435313ec006ec9000",
			expectedStaticFee: 0,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 691, // The length of the tx in bytes
				gas.DBRead:    IntrinsicAddPermissionlessValidatorTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicAddPermissionlessValidatorTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + 2*intrinsicOutputDBWrite,
				gas.Compute:   intrinsicBLSPoPVerifyCompute + intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 137_191 * units.NanoAvax,
		},
		{
			name:              "AddPermissionlessValidatorTx for subnet",
			tx:                "000000000019000030390000000000000000000000000000000000000000000000000000000000000000000000022f6399f3e626fe1e75f9daa5e726cb64b7bfec0b6e6d8930eaa9dfa336edca7a000000070000000000006091000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29cdbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000700238520ba6c9980000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000002038b42b73d3dc695c76ca12f966e97fe0681b1200f9a5e28d088720a18ea23c9000000002f6399f3e626fe1e75f9daa5e726cb64b7bfec0b6e6d8930eaa9dfa336edca7a00000005000000000000609b0000000100000000a378b74b3293a9d885bd9961f2cc2e1b3364d393c9be875964f2bd614214572c00000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500238520ba7bdbc0000000010000000000000000c582872c37c81efa2c94ea347af49cdc23a830aa0000000066a57a160000000066b7ef16000000000000000a97ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c1240000001b000000012f6399f3e626fe1e75f9daa5e726cb64b7bfec0b6e6d8930eaa9dfa336edca7a00000007000000000000000a000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000b000000000000000000000000000000000000000b00000000000000000000000000000000000f4240000000020000000900000001593fc20f88a8ce0b3470b0bb103e5f7e09f65023b6515d36660da53f9a15dedc1037ee27a8c4a27c24e20ad3b0ab4bd1ff3a02a6fcc2cbe04282bfe9902c9ae6000000000900000001593fc20f88a8ce0b3470b0bb103e5f7e09f65023b6515d36660da53f9a15dedc1037ee27a8c4a27c24e20ad3b0ab4bd1ff3a02a6fcc2cbe04282bfe9902c9ae600",
			expectedStaticFee: 0,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 748, // The length of the tx in bytes
				gas.DBRead:    IntrinsicAddPermissionlessValidatorTxComplexities[gas.DBRead] + 2*intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicAddPermissionlessValidatorTxComplexities[gas.DBWrite] + 2*intrinsicInputDBWrite + 3*intrinsicOutputDBWrite,
				gas.Compute:   2 * intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 170_748 * units.NanoAvax,
		},
		{
			name:              "AddPermissionlessDelegatorTx for primary network",
			tx:                "00000000001a00003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834f1140fe00000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c000000017d199179744b3b82d0071c83c2fb7dd6b95a2cdbe9dde295e0ae4f8c2287370300000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500238520ba8b1e00000000010000000000000000c582872c37c81efa2c94ea347af49cdc23a830aa00000000669ae6080000000066ad5b08000001d1a94a2000000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000001d1a94a2000000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000b000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000100000009000000012261556f74a29f02ffc2725a567db2c81f75d0892525dbebaa1cf8650534cc70061123533a9553184cb02d899943ff0bf0b39c77b173c133854bc7c8bc7ab9a400",
			expectedStaticFee: 0,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 499, // The length of the tx in bytes
				gas.DBRead:    IntrinsicAddPermissionlessDelegatorTxComplexities[gas.DBRead] + 1*intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicAddPermissionlessDelegatorTxComplexities[gas.DBWrite] + 1*intrinsicInputDBWrite + 2*intrinsicOutputDBWrite,
				gas.Compute:   intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 106_499 * units.NanoAvax,
		},
		{
			name:              "AddPermissionlessDelegatorTx for subnet",
			tx:                "00000000001a000030390000000000000000000000000000000000000000000000000000000000000000000000022f6399f3e626fe1e75f9daa5e726cb64b7bfec0b6e6d8930eaa9dfa336edca7a000000070000000000006087000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29cdbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000700470c1336195b80000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c000000029494c80361884942e4292c3531e8e790fcf7561e74404ded27eab8634e3fb30f000000002f6399f3e626fe1e75f9daa5e726cb64b7bfec0b6e6d8930eaa9dfa336edca7a00000005000000000000609100000001000000009494c80361884942e4292c3531e8e790fcf7561e74404ded27eab8634e3fb30f00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db0000000500470c1336289dc0000000010000000000000000c582872c37c81efa2c94ea347af49cdc23a830aa0000000066a57c1d0000000066b7f11d000000000000000a97ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c124000000012f6399f3e626fe1e75f9daa5e726cb64b7bfec0b6e6d8930eaa9dfa336edca7a00000007000000000000000a000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000b00000000000000000000000000000000000000020000000900000001764190e2405fef72fce0d355e3dcc58a9f5621e583ae718cb2c23b55957995d1206d0b5efcc3cef99815e17a4b2cccd700147a759b7279a131745b237659666a000000000900000001764190e2405fef72fce0d355e3dcc58a9f5621e583ae718cb2c23b55957995d1206d0b5efcc3cef99815e17a4b2cccd700147a759b7279a131745b237659666a00",
			expectedStaticFee: 0,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 720, // The length of the tx in bytes
				gas.DBRead:    IntrinsicAddPermissionlessDelegatorTxComplexities[gas.DBRead] + 2*intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicAddPermissionlessDelegatorTxComplexities[gas.DBWrite] + 2*intrinsicInputDBWrite + 3*intrinsicOutputDBWrite,
				gas.Compute:   2 * intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 150_720 * units.NanoAvax,
		},
		{
			name:              "AddSubnetValidatorTx",
			tx:                "00000000000d00003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834f1131bbc0000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000138f94d1a0514eaabdaf4c52cad8d62b26cee61eaa951f5b75a5e57c2ee3793c800000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050023834f1140fe00000000010000000000000000c582872c37c81efa2c94ea347af49cdc23a830aa00000000669ae7c90000000066ad5cc9000000000000c13797ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c1240000000a00000001000000000000000200000009000000012127130d37877fb1ec4b2374ef72571d49cd7b0319a3769e5da19041a138166c10b1a5c07cf5ccf0419066cbe3bab9827cf29f9fa6213ebdadf19d4849501eb60000000009000000012127130d37877fb1ec4b2374ef72571d49cd7b0319a3769e5da19041a138166c10b1a5c07cf5ccf0419066cbe3bab9827cf29f9fa6213ebdadf19d4849501eb600",
			expectedStaticFee: 0,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 460, // The length of the tx in bytes
				gas.DBRead:    IntrinsicAddSubnetValidatorTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicAddSubnetValidatorTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite,
				gas.Compute:   2 * intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 112_460 * units.NanoAvax,
		},
		{
			name:              "BaseTx",
			tx:                "00000000002200003039000000000000000000000000000000000000000000000000000000000000000000000002dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000000003b9aca00000000000000000100000002000000024a177205df5c29929d06db9d941f83d5ea985de3e902a9a86640bfdb1cd0e36c0cc982b83e5765fadbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834ed587af80000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001fa4ff39749d44f29563ed9da03193d4a19ef419da4ce326594817ca266fda5ed00000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050023834f1131bbc00000000100000000000000000000000100000009000000014a7b54c63dd25a532b5fe5045b6d0e1db876e067422f12c9c327333c2c792d9273405ac8bbbc2cce549bbd3d0f9274242085ee257adfdb859b0f8d55bdd16fb000",
			expectedStaticFee: 0,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 399, // The length of the tx in bytes
				gas.DBRead:    IntrinsicBaseTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicBaseTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + 2*intrinsicOutputDBWrite,
				gas.Compute:   intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 64_399 * units.NanoAvax,
		},
		{
			name:              "CreateChainTx",
			tx:                "00000000000f00003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007002386f263d53e00000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000197ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c12400000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f269cb1f0000000001000000000000000097ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c12400096c65742074686572657873766d00000000000000000000000000000000000000000000000000000000000000000000002a000000000000669ae21e000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29cffffffffffffffff0000000a0000000100000000000000020000000900000001cf8104877b1a59b472f4f34d360c0e4f38e92c5fa334215430d0b99cf78eae8f621b6daf0b0f5c3a58a9497601f978698a1e5545d1873db8f2f38ecb7496c2f8010000000900000001cf8104877b1a59b472f4f34d360c0e4f38e92c5fa334215430d0b99cf78eae8f621b6daf0b0f5c3a58a9497601f978698a1e5545d1873db8f2f38ecb7496c2f801",
			expectedStaticFee: 0,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 509, // The length of the tx in bytes
				gas.DBRead:    IntrinsicCreateChainTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicCreateChainTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite,
				gas.Compute:   2 * intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 72_509 * units.NanoAvax,
		},
		{
			name:              "CreateSubnetTx",
			tx:                "00000000001000003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007002386f269cb1f00000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f26fc100000000000100000000000000000000000b000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c000000010000000900000001b3c905e7227e619bd6b98c164a8b2b4a8ce89ac5142bbb1c42b139df2d17fd777c4c76eae66cef3de90800e567407945f58d918978f734f8ca4eda6923c78eb201",
			expectedStaticFee: 0,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 339, // The length of the tx in bytes
				gas.DBRead:    IntrinsicCreateSubnetTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicCreateSubnetTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite,
				gas.Compute:   intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 64_339 * units.NanoAvax,
		},
		{
			name:              "ExportTx",
			tx:                "00000000001200003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834e99dda340000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001f62c03574790b6a31a988f90c3e91c50fdd6f5d93baf200057463021ff23ec5c00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050023834ed587af800000000100000000000000009d0775f450604bd2fbc49ce0c5c1c6dfeb2dc2acb8c92c26eeae6e6df4502b1900000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000000003b9aca00000000000000000100000002000000024a177205df5c29929d06db9d941f83d5ea985de3e902a9a86640bfdb1cd0e36c0cc982b83e5765fa000000010000000900000001129a07c92045e0b9d0a203fcb5b53db7890fabce1397ff6a2ad16c98ef0151891ae72949d240122abf37b1206b95e05ff171df164a98e6bdf2384432eac2c30200",
			expectedStaticFee: txFee,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 435, // The length of the tx in bytes
				gas.DBRead:    IntrinsicExportTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicExportTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + 2*intrinsicOutputDBWrite,
				gas.Compute:   intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 64_435 * units.NanoAvax,
		},
		{
			name:              "ImportTx",
			tx:                "00000000001100003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007000000003b8b87c0000000000000000100000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000000000000d891ad56056d9c01f18f43f58b5c784ad07a4a49cf3d1f11623804b5cba2c6bf0000000163684415710a7d65f4ccb095edff59f897106b94d38937fc60e3ffc29892833b00000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005000000003b9aca00000000010000000000000001000000090000000148ea12cb0950e47d852b99765208f5a811d3c8a47fa7b23fd524bd970019d157029f973abb91c31a146752ef8178434deb331db24c8dca5e61c961e6ac2f3b6700",
			expectedStaticFee: txFee,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 335, // The length of the tx in bytes
				gas.DBRead:    IntrinsicImportTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicImportTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite,
				gas.Compute:   intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 44_335 * units.NanoAvax,
		},
		{
			name:              "RemoveSubnetValidatorTx",
			tx:                "00000000001700003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834e99ce6100000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001cd4569cfd044d50636fa597c700710403b3b52d3b75c30c542a111cc52c911ec00000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050023834e99dda340000000010000000000000000c582872c37c81efa2c94ea347af49cdc23a830aa97ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c1240000000a0000000100000000000000020000000900000001673ee3e5a3a1221935274e8ff5c45b27ebe570e9731948e393a8ebef6a15391c189a54de7d2396095492ae171103cd4bfccfc2a4dafa001d48c130694c105c2d010000000900000001673ee3e5a3a1221935274e8ff5c45b27ebe570e9731948e393a8ebef6a15391c189a54de7d2396095492ae171103cd4bfccfc2a4dafa001d48c130694c105c2d01",
			expectedStaticFee: txFee,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 436, // The length of the tx in bytes
				gas.DBRead:    IntrinsicRemoveSubnetValidatorTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicRemoveSubnetValidatorTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite,
				gas.Compute:   2 * intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 108_436 * units.NanoAvax,
		},
		{
			name:                  "TransformSubnetTx",
			tx:                    "000000000018000030390000000000000000000000000000000000000000000000000000000000000000000000022f6399f3e626fe1e75f9daa5e726cb64b7bfec0b6e6d8930eaa9dfa336edca7a00000007000000000000609b000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29cdbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007002386f263c5fbc0000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c0000000294a113f31a30ee643288277574434f9066e0cdc1d53d6eb2610805c388814134000000002f6399f3e626fe1e75f9daa5e726cb64b7bfec0b6e6d8930eaa9dfa336edca7a00000005000000000000c137000000010000000094a113f31a30ee643288277574434f9066e0cdc1d53d6eb2610805c38881413400000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f269bbdcc000000001000000000000000097ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c1242f6399f3e626fe1e75f9daa5e726cb64b7bfec0b6e6d8930eaa9dfa336edca7a000000000000609b000000000000c1370000000000000001000000000000000a0000000000000001000000000000006400127500001fa40000000001000000000000000a64000000010000000a00000001000000000000000300000009000000015c640ddd6afc7d8059ef54663654d74f0c56cc1ed0b974d401171cdae0b29be67f3223e299d3e5e7c492ef4c7110ddf44d672bd698c42947bfb15ab750f0ca820000000009000000015c640ddd6afc7d8059ef54663654d74f0c56cc1ed0b974d401171cdae0b29be67f3223e299d3e5e7c492ef4c7110ddf44d672bd698c42947bfb15ab750f0ca820000000009000000015c640ddd6afc7d8059ef54663654d74f0c56cc1ed0b974d401171cdae0b29be67f3223e299d3e5e7c492ef4c7110ddf44d672bd698c42947bfb15ab750f0ca8200",
			expectedStaticFee:     0,
			expectedComplexityErr: ErrUnsupportedTx,
			expectedDynamicFeeErr: ErrUnsupportedTx,
		},
		{
			name:              "TransferSubnetOwnershipTx",
			tx:                "00000000002100003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000070023834e99bf1ec0000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c000000018f6e5f2840e34f9a375f35627a44bb0b9974285d280dc3220aa9489f97b17ebd00000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db000000050023834e99ce610000000001000000000000000097ea88082100491617204ed70c19fc1a2fce4474bee962904359d0b59e84c1240000000a00000001000000000000000b00000000000000000000000000000000000000020000000900000001e3479034ed8134dd23e154e1ec6e61b25073a20750ebf808e50ec1aae180ef430f8151347afdf6606bc7866f7f068b01719e4dad12e2976af1159fb048f73f7f010000000900000001e3479034ed8134dd23e154e1ec6e61b25073a20750ebf808e50ec1aae180ef430f8151347afdf6606bc7866f7f068b01719e4dad12e2976af1159fb048f73f7f01",
			expectedStaticFee: 0,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 436, // The length of the tx in bytes
				gas.DBRead:    IntrinsicTransferSubnetOwnershipTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicTransferSubnetOwnershipTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite,
				gas.Compute:   2 * intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 68_436 * units.NanoAvax,
		},
		{
			name:                 "ConvertSubnetToL1Tx",
			tx:                   "00000000002300003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007002386f234262960000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001705f3d4415f990225d3df5ce437d7af2aa324b1bbce854ee34ab6f39882250d200000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f26fc0f94e000000010000000000000000a0673b4ee5ec44e57c8ab250dd7cd7b68d04421f64bd6559a4284a3ee358ff2b705f3d4415f990225d3df5ce437d7af2aa324b1bbce854ee34ab6f39882250d2000000000000000100000014c582872c37c81efa2c94ea347af49cdc23a830aa000000000000c137000000003b9aca00a3783a891cb41cadbfcf456da149f30e7af972677a162b984bef0779f254baac51ec042df1781d1295df80fb41c801269731fc6c25e1e5940dc3cb8509e30348fa712742cfdc83678acc9f95908eb98b89b28802fb559b4a2a6ff3216707c07f0ceb0b45a95f4f9a9540bbd3331d8ab4f233bffa4abb97fad9d59a1695f31b92a2b89e365facf7ab8c30de7c4a496d1e000000000000000000000000000000000000000a00000001000000000000000200000009000000011430759900fdf516cdeff6a1390dd7438585568a89c06142c44b3bf1178c4cae4bff44e955b19da08f0359d396a7a738b989bb46377e7465cd858ddd1e8dd3790100000009000000011430759900fdf516cdeff6a1390dd7438585568a89c06142c44b3bf1178c4cae4bff44e955b19da08f0359d396a7a738b989bb46377e7465cd858ddd1e8dd37901",
			expectedStaticFeeErr: ErrUnsupportedTx,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 656, // The length of the tx in bytes
				gas.DBRead:    IntrinsicConvertSubnetToL1TxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicConvertSubnetToL1TxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite + intrinsicConvertSubnetToL1ValidatorDBWrite,
				gas.Compute:   2*intrinsicSECP256k1FxSignatureCompute + intrinsicBLSPoPVerifyCompute,
			},
			expectedDynamicFee: 183_156 * units.NanoAvax,
		},
		{
			name:                 "RegisterL1ValidatorTx",
			tx:                   "00000000002400003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007002386f1f88b552a000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001ca44ad45a63381b07074be7f82005c41550c989b967f40020f3bedc4b02191f300000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f234262404000000010000000000000000000000003b9aca00ab5cb0516b7afdb13727f766185b2b8da44e2653eef63c85f196701083e649289cce1a23c39eb471b2473bc6872aa3ea190de0fe66296cbdd4132c92c3430ff22f28f0b341b15905a005bbd66cc0f4056bc4be5934e4f3a57151a60060f429190000012f000000003039705f3d4415f990225d3df5ce437d7af2aa324b1bbce854ee34ab6f39882250d20000009c000000000001000000000000008e000000000001a0673b4ee5ec44e57c8ab250dd7cd7b68d04421f64bd6559a4284a3ee358ff2b000000145efc86a11c5b12cc95b2cf527c023f9cf6e0e8f6b62034315c5d11cea4190f6ea8997821c02483d29adb5e4567843f7a44c39b2ffa20c8520dc358702fb1ec29f2746dcc000000006705af280000000000000000000000000000000000000000000000010000000000000001018e99dc6ed736089c03b9a1275e0cf801524ed341fb10111f29c0390fa2f96cf6aa78539ec767e5cd523c606c7ede50e60ba6065a3685e770d979b0df74e3541b61ed63f037463776098576e385767a695de59352b44e515831c5ee7a8cc728f9000000010000000900000001a0950b9e6e866130f0d09e2a7bfdd0246513295237258afa942b1850dab79824605c796bbfc9223cf91935fb29c66f8b927690220b9b1c24d6f078054a3e346201",
			expectedStaticFeeErr: ErrUnsupportedTx,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 710, // The length of the tx in bytes
				gas.DBRead:    IntrinsicRegisterL1ValidatorTxComplexities[gas.DBRead] + intrinsicInputDBRead + intrinsicWarpDBReads,
				gas.DBWrite:   IntrinsicRegisterL1ValidatorTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite,
				gas.Compute:   intrinsicSECP256k1FxSignatureCompute + intrinsicBLSPoPVerifyCompute + intrinsicBLSAggregateCompute + intrinsicBLSVerifyCompute,
			},
			expectedDynamicFee: 241_260 * units.NanoAvax,
		},
		{
			name:                 "SetL1ValidatorWeightTx",
			tx:                   "00000000002500003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007002386f1f88b5100000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001389c41b6ed301e4c118bd23673268fd2054b772efcf25685a117b74bab7ae5e400000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f1f88b552a000000010000000000000000000000d7000000003039705f3d4415f990225d3df5ce437d7af2aa324b1bbce854ee34ab6f39882250d200000044000000000001000000000000003600000000000338e6e9fe31c6d070a8c792dbacf6d0aefb8eac2aded49cc0aa9f422d1fdd9ecd0000000000000001000000000000000500000000000000010187f4bb2c42869c56f023a1ca81045aff034acd490b8f15b5069025f982e605e077007fc588f7d56369a65df7574df3b70ff028ea173739c789525ab7eebfcb5c115b13cca8f02b362104b700c75bc95234109f3f1360ddcb4ec3caf6b0e821cb0000000100000009000000010a29f3c86d52908bf2efbc3f918a363df704c429d66c8d6615712a2a584a2a5f264a9e7b107c07122a06f31cadc2f51285884d36fe8df909a07467417f1d64cf00",
			expectedStaticFeeErr: ErrUnsupportedTx,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 518, // The length of the tx in bytes
				gas.DBRead:    IntrinsicSetL1ValidatorWeightTxComplexities[gas.DBRead] + intrinsicInputDBRead + intrinsicWarpDBReads,
				gas.DBWrite:   IntrinsicSetL1ValidatorWeightTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite,
				gas.Compute:   intrinsicSECP256k1FxSignatureCompute + intrinsicBLSAggregateCompute + intrinsicBLSVerifyCompute,
			},
			expectedDynamicFee: 206_568 * units.NanoAvax,
		},
		{
			name:                 "IncreaseL1ValidatorBalanceTx",
			tx:                   "00000000002600003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007002386f1f88b4e52000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001f61ea7e3bb6d33da9901644f3c623e4537b7d1c276e9ef23bcc8e4150e494d6600000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f1f88b510000000001000000000000000038e6e9fe31c6d070a8c792dbacf6d0aefb8eac2aded49cc0aa9f422d1fdd9ecd0000000000000002000000010000000900000001cb56b56387be9186d86430fad5418db4d13e991b6805b6ba178b719e3f47ce001da52d6ed3173bfdd8b69940a135432abce493a10332e881f6c34cea3617595e00",
			expectedStaticFeeErr: ErrUnsupportedTx,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 339, // The length of the tx in bytes
				gas.DBRead:    IntrinsicIncreaseL1ValidatorBalanceTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicIncreaseL1ValidatorBalanceTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite,
				gas.Compute:   intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 146_339 * units.NanoAvax,
		},
		{
			name:                 "DisableL1ValidatorTx",
			tx:                   "00000000002700003039000000000000000000000000000000000000000000000000000000000000000000000001dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000007002386f1f88b4b9e000000000000000000000001000000013cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c00000001fd91c5c421468b13b09dda413bdbe1316c7c9417f2468b893071d4cb608a01da00000000dbcf890f77f49b96857648b72b77f9f82937f28a68704af05da0dc12ba53f2db00000005002386f1f88b4e5200000001000000000000000038e6e9fe31c6d070a8c792dbacf6d0aefb8eac2aded49cc0aa9f422d1fdd9ecd0000000a00000000000000020000000900000001ff99bb626d898907a660701e2febaa311b4e644fe71add2d1a3f71748102c73f54d73c8370a9ae33e09c984bb8c03da4922bf208af836ec2daaa31cb42788bee010000000900000000",
			expectedStaticFeeErr: ErrUnsupportedTx,
			expectedComplexity: gas.Dimensions{
				gas.Bandwidth: 347, // The length of the tx in bytes
				gas.DBRead:    IntrinsicDisableL1ValidatorTxComplexities[gas.DBRead] + intrinsicInputDBRead,
				gas.DBWrite:   IntrinsicDisableL1ValidatorTxComplexities[gas.DBWrite] + intrinsicInputDBWrite + intrinsicOutputDBWrite,
				gas.Compute:   intrinsicSECP256k1FxSignatureCompute,
			},
			expectedDynamicFee: 166_347 * units.NanoAvax,
		},
	}
)
