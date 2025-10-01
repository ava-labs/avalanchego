// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func TestParseBlocks(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	pChainEpoch := Epoch{}
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	signedBlock, err := Build(
		parentID,
		timestamp,
		pChainHeight,
		pChainEpoch,
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	require.NoError(t, err)

	signedBlockBytes := signedBlock.Bytes()
	malformedBlockBytes := make([]byte, len(signedBlockBytes)-1)
	copy(malformedBlockBytes, signedBlockBytes)

	for _, testCase := range []struct {
		name   string
		input  [][]byte
		output []ParseResult
	}{
		{
			name:  "ValidThenInvalid",
			input: [][]byte{signedBlockBytes, malformedBlockBytes},
			output: []ParseResult{
				{
					Block: &statelessBlock{
						statelessBlockMetadata: statelessBlockMetadata{
							bytes: signedBlockBytes,
						},
					},
				},
				{
					Err: wrappers.ErrInsufficientLength,
				},
			},
		},
		{
			name:  "InvalidThenValid",
			input: [][]byte{malformedBlockBytes, signedBlockBytes},
			output: []ParseResult{
				{
					Err: wrappers.ErrInsufficientLength,
				},
				{
					Block: &statelessBlock{
						statelessBlockMetadata: statelessBlockMetadata{
							bytes: signedBlockBytes,
						},
					},
				},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			results := ParseBlocks(testCase.input, chainID)
			for i := range testCase.output {
				if testCase.output[i].Block == nil {
					require.Nil(t, results[i].Block)
					require.ErrorIs(t, results[i].Err, testCase.output[i].Err)
				} else {
					require.Equal(t, testCase.output[i].Block.Bytes(), results[i].Block.Bytes())
					require.NoError(t, results[i].Err)
				}
			}
		})
	}
}

func TestParse(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	pChainEpoch := Epoch{
		PChainHeight: pChainHeight,
		Number:       1,
		StartTime:    timestamp.Unix(),
	}
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	signedBlock, err := Build(
		parentID,
		timestamp,
		pChainHeight,
		pChainEpoch,
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	require.NoError(t, err)
	require.IsType(t, &statelessGraniteBlock{}, signedBlock)

	signedZeroEpochBlock, err := Build(
		parentID,
		timestamp,
		pChainHeight,
		Epoch{},
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	require.NoError(t, err)
	require.IsType(t, &statelessBlock{}, signedZeroEpochBlock)

	unsignedBlock, err := BuildUnsigned(parentID, timestamp, pChainHeight, pChainEpoch, innerBlockBytes)
	require.IsType(t, &statelessGraniteBlock{}, unsignedBlock)
	require.NoError(t, err)

	unsignedZeroEpochBlock, err := BuildUnsigned(parentID, timestamp, pChainHeight, Epoch{}, innerBlockBytes)
	require.IsType(t, &statelessBlock{}, unsignedZeroEpochBlock)
	require.NoError(t, err)

	signedWithoutCertBlockIntf, err := BuildUnsigned(parentID, timestamp, pChainHeight, Epoch{}, innerBlockBytes)
	require.IsType(t, &statelessBlock{}, signedWithoutCertBlockIntf)
	require.NoError(t, err)

	signedWithoutCertBlock := signedWithoutCertBlockIntf.(*statelessBlock)
	signedWithoutCertBlock.Signature = []byte{5}

	signedWithoutCertBlock.bytes, err = Codec.Marshal(CodecVersion, &signedWithoutCertBlockIntf)
	require.NoError(t, err)

	optionBlock, err := BuildOption(parentID, innerBlockBytes)
	require.NoError(t, err)

	tests := []struct {
		name        string
		block       Block
		chainID     ids.ID
		expectedErr error
	}{
		{
			name:        "correct chainID",
			block:       signedBlock,
			chainID:     chainID,
			expectedErr: nil,
		},
		{
			name:        "invalid chainID",
			block:       signedBlock,
			chainID:     ids.ID{5},
			expectedErr: staking.ErrECDSAVerificationFailure,
		},
		{
			name:        "unsigned block",
			block:       unsignedBlock,
			chainID:     chainID,
			expectedErr: nil,
		},
		{
			name:        "invalid signature",
			block:       signedWithoutCertBlockIntf,
			chainID:     chainID,
			expectedErr: errUnexpectedSignature,
		},
		{
			name:        "option block",
			block:       optionBlock,
			chainID:     chainID,
			expectedErr: nil,
		},
		{
			name:        "signed zero epoch block",
			block:       signedZeroEpochBlock,
			chainID:     chainID,
			expectedErr: nil,
		},
		{
			name:        "unsigned zero epoch block",
			block:       unsignedZeroEpochBlock,
			chainID:     chainID,
			expectedErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			blockBytes := test.block.Bytes()
			parsedBlockWithoutVerification, err := ParseWithoutVerification(blockBytes)
			require.NoError(err)
			equal(require, test.block, parsedBlockWithoutVerification)

			parsedBlock, err := Parse(blockBytes, test.chainID)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr == nil {
				equal(require, test.block, parsedBlock)
			}
		})
	}
}

func TestParseBytes(t *testing.T) {
	chainID := ids.ID{4}
	tests := []struct {
		name         string
		hex          string
		expectedType interface{}
		expectedErr  error
	}{
		{
			name:         "duplicate extensions in certificate",
			hex:          "0000000000000100000000000000000000000000000000000000000000000000000000000000000000000000007b0000000000000002000004bd308204b9308202a1a003020102020100300d06092a864886f70d01010b050030003020170d3939313233313030303030305a180f32313232303830333233323835335a300030820222300d06092a864886f70d01010105000382020f003082020a0282020100c2b2de1c16924d9b9254a0d5b80a4bc5f9beaa4f4f40a0e4efb69eb9b55d7d37f8c82328c237d7c5b451f5427b487284fa3f365f9caa53c7fcfef8d7a461d743bd7d88129f2da62b877ebe9d6feabf1bd12923e6c12321382c782fc3bb6b6cb4986a937a1edc3814f4e621e1a62053deea8c7649e43edd97ab6b56315b00d9ab5026bb9c31fb042dc574ba83c54e720e0120fcba2e8a66b77839be3ece0d4a6383ef3f76aac952b49a15b65e18674cd1340c32cecbcbaf80ae45be001366cb56836575fb0ab51ea44bf7278817e99b6b180fdd110a49831a132968489822c56692161bbd372cf89d9b8ee5a734cff15303b3a960ee78d79e76662a701941d9ec084429f26707f767e9b1d43241c0e4f96655d95c1f4f4aa00add78eff6bf0a6982766a035bf0b465786632c5bb240788ca0fdf032d8815899353ea4bec5848fd30118711e5b356bde8a0da074cc25709623225e734ff5bd0cf65c40d9fd8fccf746d8f8f35145bcebcf378d2b086e57d78b11e84f47fa467c4d037f92bff6dd4e934e0189b58193f24c4222ffb72b5c06361cf68ca64345bc3e230cc0f40063ad5f45b1659c643662996328c2eeddcd760d6f7c9cbae081ccc065844f7ea78c858564a408979764de882793706acc67d88092790dff567ed914b03355330932616a0f26f994b963791f0b1dbd8df979db86d1ea490700a3120293c3c2b10bef10203010001a33c303a300e0603551d0f0101ff0404030204b030130603551d25040c300a06082b0601050507030230130603551d25040c300a06082b06010505070302300d06092a864886f70d01010b05000382020100a21a0d73ec9ef4eb39f810557ac70b0b775772b8bae5f42c98565bc50b5b2c57317aa9cb1da12f55d0aac7bb36a00cd4fd0d7384c4efa284b53520c5a3c4b8a65240b393eeab02c802ea146c0728c3481c9e8d3aaad9d4dd7607103dcfaa96da83460adbe18174ed5b71bde7b0a93d4fb52234a9ff54e3fd25c5b74790dfb090f2e59dc5907357f510cc3a0b70ccdb87aee214def794b316224f318b471ffa13b66e44b467670e881cb1628c99c048a503376d9b6d7b8eef2e7be47ff7d5c1d56221f4cf7fa2519b594cb5917815c64dc75d8d281bcc99b5a12899b08f2ca0f189857b64a1afc5963337f3dd6e79390e85221569f6dbbb13aadce06a3dfb5032f0cc454809627872cd7cd0cea5eba187723f07652c8abc3fc42bd62136fc66287f2cc19a7cb416923ad1862d7f820b55cacb65e43731cb6df780e2651e457a3438456aeeeb278ad9c0ad2e760f6c1cbe276eeb621c8a4e609b5f2d902beb3212e3e45df99497021ff536d0b56390c5d785a8bf7909f6b61bdc705d7d92ae22f58e7b075f164a0450d82d8286bf449072751636ab5185f59f518b845a75d112d6f7b65223479202cff67635e2ad88106bc8a0cc9352d87c5b182ac19a4680a958d814a093acf46730f87da0df6926291d02590f215041b44a0a1a32eeb3a52cddabc3d256689bace18a8d85e644cf9137cce3718f7caac1cb16ae06e874f4c701000000010300000200b8e3a4d9a4394bac714cb597f5ba1a81865185e35c782d0317e7abc0b52d49ff8e10f787bedf86f08148e3dbd2d2d478caa2a2893d31db7d5ee51339883fe84d3004440f16cb3797a7fab0f627d3ebd79217e995488e785cd6bb7b96b9d306f8109daa9cfc4162f9839f60fb965bcb3b56a5fa787549c153a4c80027398f73a617b90b7f24f437b140cd3ac832c0b75ec98b9423b275782988a9fd426937b8f82fbb0e88a622934643fb6335c1a080a4d13125544b04585d5f5295be7cd2c8be364246ea3d5df3e837b39a85074575a1fa2f4799050460110bdfb20795c8a9172a20f61b95e1c5c43eccd0c2c155b67385366142c63409cb3fb488e7aba6c8930f7f151abf1c24a54bd21c3f7a06856ea9db35beddecb30d2c61f533a3d0590bdbb438c6f2a2286dfc3c71b383354f0abad72771c2cc3687b50c2298783e53857cf26058ed78d0c1cf53786eb8d006a058ee3c85a7b2b836b5d03ef782709ce8f2725548e557b3de45a395a669a15f1d910e97015d22ac70020cab7e2531e8b1f739b023b49e742203e9e19a7fe0053826a9a2fe2e118d3b83498c2cb308573202ad41aa4a390aee4b6b5dd2164e5c5cd1b5f68b7d5632cf7dbb9a9139663c9aac53a74b2c6fc73cad80e228a186ba027f6f32f0182d62503e04fcced385f2e7d2e11c00940622ebd533b4d144689082f9777e5b16c36f9af9066e0ad6564d43",
			expectedType: &statelessBlock{},
			expectedErr:  nil,
		},
		{
			name:         "gibberish",
			hex:          "000102030405",
			expectedType: nil,
			expectedErr:  codec.ErrUnknownVersion,
		},
		{
			name:         "granite block with zero epoch",
			hex:          "0000000000020100000000000000000000000000000000000000000000000000000000000000000000000000007b000000000000000200000114308201103081b7a003020102020100300a06082a8648ce3d04030230003020170d3939313233313030303030305a180f32313235313030313139313935325a30003059301306072a8648ce3d020106082a8648ce3d03010703420004bd57e69df4a1562baa73dea9014e638aac699a9ded79850b348cd69b312c0344d09e2726f4a778ed23a63cd85c416fc2ce697f5a4fc3468d98a481aae8d396aea320301e300e0603551d0f0101ff040403020780300c0603551d130101ff04023000300a06082a8648ce3d0403020348003045022027f6aa68587db06a05e7bafd7b42bad7e98962fca1549c242c53d62721240bb3022100d0a79da9b971a825e0000350f842b941838da20a1338af79e7db5d4176fb51860000000103000000000000000000000000000000000000000000000000000000483046022100fea2be2c93b6c70d08f8c224e93f47b4547c33b19d36c5e8869627c5f47bc610022100a8d81e273a4483a97bdfa5eedcdba66a4e34e04bcf6d49f4e6bc738b650b69fa",
			expectedType: &statelessGraniteBlock{},
			expectedErr:  errZeroEpoch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			bytes, err := hex.DecodeString(test.hex)
			require.NoError(err)

			_, err = Parse(bytes, chainID)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

func TestParseHeader(t *testing.T) {
	require := require.New(t)

	chainID := ids.ID{1}
	parentID := ids.ID{2}
	bodyID := ids.ID{3}

	builtHeader, err := BuildHeader(
		chainID,
		parentID,
		bodyID,
	)
	require.NoError(err)

	builtHeaderBytes := builtHeader.Bytes()

	parsedHeader, err := ParseHeader(builtHeaderBytes)
	require.NoError(err)

	equalHeader(require, builtHeader, parsedHeader)
}
