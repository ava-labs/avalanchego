// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestBuild(t *testing.T) {
	require := require.New(t)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}
	networkID := uint32(5)
	parentBlockSig := []byte{}
	blsSignKey, err := bls.NewSecretKey()
	require.NoError(err)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	key := tlsCert.PrivateKey.(crypto.Signer)
	nodeID := ids.NodeIDFromCert(cert)

	vrfSig := NextBlockVRFSig(parentBlockSig, blsSignKey, chainID, networkID)
	builtBlock, err := Build(
		parentID,
		timestamp,
		pChainHeight,
		cert,
		innerBlockBytes,
		chainID,
		key,
		vrfSig,
	)
	require.NoError(err)

	require.Equal(parentID, builtBlock.ParentID())
	require.Equal(pChainHeight, builtBlock.PChainHeight())
	require.Equal(timestamp, builtBlock.Timestamp())
	require.Equal(innerBlockBytes, builtBlock.Block())
	require.Equal(nodeID, builtBlock.Proposer())
}

func TestBuildUnsigned(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	parentBlockSig := []byte{}

	require := require.New(t)

	builtBlock, err := BuildUnsigned(parentID, timestamp, pChainHeight, innerBlockBytes, parentBlockSig)
	require.NoError(err)

	require.Equal(parentID, builtBlock.ParentID())
	require.Equal(pChainHeight, builtBlock.PChainHeight())
	require.Equal(timestamp, builtBlock.Timestamp())
	require.Equal(innerBlockBytes, builtBlock.Block())
	require.Equal(ids.EmptyNodeID, builtBlock.Proposer())
}

func TestBuildHeader(t *testing.T) {
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

	require.Equal(chainID, builtHeader.ChainID())
	require.Equal(parentID, builtHeader.ParentID())
	require.Equal(bodyID, builtHeader.BodyID())
}

func TestBuildOption(t *testing.T) {
	require := require.New(t)

	parentID := ids.ID{1}
	innerBlockBytes := []byte{3}

	builtOption, err := BuildOption(parentID, innerBlockBytes)
	require.NoError(err)

	require.Equal(parentID, builtOption.ParentID())
	require.Equal(innerBlockBytes, builtOption.Block())
}

func TestCalculateVRFOut(t *testing.T) {
	testCases := []struct {
		name        string
		vrfSig      []byte
		expectedOut [32]byte
	}{
		{"nil input", nil, [32]byte{0}},
		{"empty input", []byte{}, [32]byte{0}},
		{"one byte input", []byte{1}, [32]byte{0}},
		{"three bytes input", []byte{1, 2, 3}, [32]byte{0}},
		{"70 bytes input", make([]byte, 70), [32]byte{0}},
		{"too long input", make([]byte, bls.SignatureLen+1), [32]byte{0}},
		{"valid empty input", make([]byte, bls.SignatureLen), [32]byte{0xc9, 0x28, 0x64, 0xd4, 0x5a, 0x33, 0xac, 0xd4, 0x42, 0x10, 0xfa, 0x67, 0xda, 0x71, 0x9d, 0xa3, 0x48, 0x2b, 0xf8, 0xab, 0xb0, 0xbc, 0xbb, 0x9e, 0x48, 0x94, 0x4e, 0x2c, 0xd, 0x72, 0xd7, 0x9b}},
		{"valid common input", []byte{0xc9, 0x28, 0x64, 0xd4, 0x5a, 0x33, 0xac, 0xd4, 0x42, 0x10, 0xfa, 0x67, 0xda, 0x71, 0x9d, 0xa3, 0x48, 0x2b, 0xf8, 0xab, 0xb0, 0xbc, 0xbb, 0x9e, 0x48, 0x94, 0x4e, 0x2c, 0xd, 0x72, 0xd7, 0x9b, 0xc9, 0x28, 0x64, 0xd4, 0x5a, 0x33, 0xac, 0xd4, 0x42, 0x10, 0xfa, 0x67, 0xda, 0x71, 0x9d, 0xa3, 0x48, 0x2b, 0xf8, 0xab, 0xb0, 0xbc, 0xbb, 0x9e, 0x48, 0x94, 0x4e, 0x2c, 0xd, 0x72, 0xd7, 0x9b, 0xc9, 0x28, 0x64, 0xd4, 0x5a, 0x33, 0xac, 0xd4, 0x42, 0x10, 0xfa, 0x67, 0xda, 0x71, 0x9d, 0xa3, 0x48, 0x2b, 0xf8, 0xab, 0xb0, 0xbc, 0xbb, 0x9e, 0x48, 0x94, 0x4e, 0x2c, 0xd, 0x72, 0xd7, 0x9b}, [32]byte{0x47, 0x32, 0x0, 0x92, 0x9, 0x6c, 0x5e, 0x32, 0x3b, 0x6, 0x38, 0xec, 0xef, 0x5b, 0xa5, 0x80, 0x65, 0xff, 0xea, 0x2e, 0xd4, 0x7e, 0x76, 0xb, 0x5c, 0x2e, 0x25, 0xd1, 0xd6, 0x40, 0xcb, 0x0}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require := require.New(t)
			require.Equal(testCase.expectedOut, CalculateVRFOut(testCase.vrfSig))
		})
	}
}

func TestCalculateBootstrappingBlockSig(t *testing.T) {
	testCases := []struct {
		name        string
		chainID     ids.ID
		networkID   uint32
		expectedOut [hashing.HashLen]byte
	}{
		{"empty chainID zero network id", ids.ID(make([]byte, hashing.HashLen)), 0, [hashing.HashLen]byte{0x77, 0x0, 0xf5, 0x5d, 0x4e, 0x85, 0xe8, 0xe6, 0x69, 0xb6, 0x4a, 0xbb, 0x56, 0x17, 0x4d, 0x59, 0xdd, 0x11, 0x2e, 0xd5, 0x79, 0x66, 0x47, 0x13, 0x1f, 0xcf, 0xb7, 0x46, 0x5f, 0x5f, 0x57, 0xe0}},
		{"valid chainID zero network id", ids.ID(hashing.ComputeHash256Array([]byte{1, 2, 3})), 0, [hashing.HashLen]byte{0x77, 0x65, 0xa9, 0x88, 0x83, 0x40, 0x82, 0x5f, 0x58, 0xf1, 0xf9, 0xb, 0x73, 0xab, 0xe8, 0xff, 0x8b, 0x7c, 0xf2, 0xa0, 0x86, 0xe5, 0xd5, 0x4a, 0x1e, 0x11, 0xe6, 0x9e, 0x35, 0x9c, 0x37, 0xb2}},
		{"valid chainID valid network id", ids.ID(make([]byte, hashing.HashLen)), 2, [hashing.HashLen]byte{0x49, 0xe4, 0xcb, 0xbf, 0xf5, 0x1d, 0x8c, 0x12, 0xf7, 0x4d, 0xb6, 0x2, 0xf2, 0xdd, 0x7b, 0x4c, 0xe5, 0xcd, 0x97, 0xbd, 0x3c, 0x49, 0x7f, 0xb0, 0x94, 0x9c, 0x88, 0x30, 0xd0, 0x5c, 0xc5, 0x35}},
		{"valid chainID zero network id", ids.ID(hashing.ComputeHash256Array([]byte{0x77, 0x65, 0xa9, 0x88, 0x83, 0x40, 0x82, 0x5f, 0x58, 0xf1, 0xf9, 0xb, 0x73, 0xab, 0xe8, 0xff, 0x8b, 0x7c, 0xf2, 0xa0, 0x86, 0xe5, 0xd5, 0x4a, 0x1e, 0x11, 0xe6, 0x9e, 0x35, 0x9c, 0x37, 0xb2})), 0, [hashing.HashLen]byte{0xf7, 0x18, 0x86, 0x80, 0xb5, 0xb3, 0x5e, 0xe6, 0xd1, 0x58, 0xf4, 0x66, 0x9, 0x73, 0x67, 0x35, 0x87, 0x4d, 0x5b, 0x8d, 0xac, 0x16, 0x5d, 0xf8, 0x5e, 0x46, 0x3a, 0x61, 0x0, 0x74, 0xf8, 0x97}},
		{"valid chainID valid network id", ids.ID(hashing.ComputeHash256Array([]byte{0x77, 0x65, 0xa9, 0x88, 0x83, 0x40, 0x82, 0x5f, 0x58, 0xf1, 0xf9, 0xb, 0x73, 0xab, 0xe8, 0xff, 0x8b, 0x7c, 0xf2, 0xa0, 0x86, 0xe5, 0xd5, 0x4a, 0x1e, 0x11, 0xe6, 0x9e, 0x35, 0x9c, 0x37, 0xb2})), 4, [hashing.HashLen]byte{0xc9, 0xbb, 0x15, 0xbd, 0x5b, 0x7e, 0xee, 0xa, 0xde, 0x8a, 0x6f, 0xd0, 0x65, 0x71, 0x73, 0xf8, 0x58, 0xd9, 0xa8, 0x32, 0x7d, 0x90, 0xb0, 0xf4, 0x96, 0x74, 0x7e, 0x42, 0x7e, 0xd9, 0x4f, 0x34}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require := require.New(t)
			require.Equal(testCase.expectedOut, calculateBootstrappingBlockSig(testCase.chainID, testCase.networkID))
		})
	}
}

func TestNextHashBlockSignature(t *testing.T) {
	testCases := []struct {
		name        string
		vrfSig      []byte
		expectedOut []byte
	}{
		{"nil input", nil, nil},
		{"empty input", []byte{}, nil},
		{"one byte input", []byte{1}, []byte{0x4b, 0xf5, 0x12, 0x2f, 0x34, 0x45, 0x54, 0xc5, 0x3b, 0xde, 0x2e, 0xbb, 0x8c, 0xd2, 0xb7, 0xe3, 0xd1, 0x60, 0xa, 0xd6, 0x31, 0xc3, 0x85, 0xa5, 0xd7, 0xcc, 0xe2, 0x3c, 0x77, 0x85, 0x45, 0x9a}},
		{"three byte input", []byte{1, 2, 3}, []byte{0x3, 0x90, 0x58, 0xc6, 0xf2, 0xc0, 0xcb, 0x49, 0x2c, 0x53, 0x3b, 0xa, 0x4d, 0x14, 0xef, 0x77, 0xcc, 0xf, 0x78, 0xab, 0xcc, 0xce, 0xd5, 0x28, 0x7d, 0x84, 0xa1, 0xa2, 0x1, 0x1c, 0xfb, 0x81}},
		{"valid input", []byte{0xc9, 0x28, 0x64, 0xd4, 0x5a, 0x33, 0xac, 0xd4, 0x42, 0x10, 0xfa, 0x67, 0xda, 0x71, 0x9d, 0xa3, 0x48, 0x2b, 0xf8, 0xab, 0xb0, 0xbc, 0xbb, 0x9e, 0x48, 0x94, 0x4e, 0x2c, 0xd, 0x72, 0xd7, 0x9b, 0xc9, 0x28, 0x64, 0xd4, 0x5a, 0x33, 0xac, 0xd4, 0x42, 0x10, 0xfa, 0x67, 0xda, 0x71, 0x9d, 0xa3, 0x48, 0x2b, 0xf8, 0xab, 0xb0, 0xbc, 0xbb, 0x9e, 0x48, 0x94, 0x4e, 0x2c, 0xd, 0x72, 0xd7, 0x9b, 0xc9, 0x28, 0x64, 0xd4, 0x5a, 0x33, 0xac, 0xd4, 0x42, 0x10, 0xfa, 0x67, 0xda, 0x71, 0x9d, 0xa3, 0x48, 0x2b, 0xf8, 0xab, 0xb0, 0xbc, 0xbb, 0x9e, 0x48, 0x94, 0x4e, 0x2c, 0xd, 0x72, 0xd7, 0x9b}, []byte{0x69, 0x0, 0xec, 0x74, 0x7c, 0x77, 0xba, 0x38, 0x30, 0x38, 0xe8, 0x93, 0xae, 0x49, 0x17, 0x4, 0x32, 0x4, 0x1d, 0x7, 0xd0, 0x31, 0xc1, 0x1a, 0x7b, 0xc8, 0xdb, 0x89, 0x3f, 0xd5, 0x9e, 0x31}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require := require.New(t)
			require.Equal(testCase.expectedOut, NextHashBlockSignature(testCase.vrfSig))
		})
	}
}

func TestNextBlockVRFSig(t *testing.T) {
	validTestKey, err := bls.SecretKeyFromBytes([]byte{82, 18, 255, 50, 56, 29, 176, 229, 124, 84, 244, 171, 82, 160, 181, 170, 123, 16, 24, 243, 159, 86, 48, 53, 180, 156, 62, 64, 253, 211, 156, 198})
	require.NoError(t, err)

	networkID := uint32(4)
	chainID := hashing.ComputeHash256Array([]byte{5})

	parentHashSig := hashing.ComputeHash256Array([]byte{6})

	parentSig := bls.SignatureToBytes(bls.Sign(validTestKey, parentHashSig[:]))

	testCases := []struct {
		name              string
		parentBlockVRFSig []byte
		blsSignKey        *bls.SecretKey
		chainID           ids.ID
		networkID         uint32
		expectedOut       []byte
	}{
		{"nil parentBlockVRFSig and nil blsSignKey", nil, nil, ids.ID(make([]byte, hashing.HashLen)), 0, []byte{}},
		{"empty parentBlockVRFSig and nil blsSignKey", []byte{}, nil, ids.ID(make([]byte, hashing.HashLen)), 0, []byte{}},
		{"one byte length parentBlockVRFSig and nil blsSignKey", []byte{1}, nil, ids.ID(make([]byte, hashing.HashLen)), 0, []byte{0x4b, 0xf5, 0x12, 0x2f, 0x34, 0x45, 0x54, 0xc5, 0x3b, 0xde, 0x2e, 0xbb, 0x8c, 0xd2, 0xb7, 0xe3, 0xd1, 0x60, 0xa, 0xd6, 0x31, 0xc3, 0x85, 0xa5, 0xd7, 0xcc, 0xe2, 0x3c, 0x77, 0x85, 0x45, 0x9a}},

		{"nil parentBlockVRFSig valid blsSignKey and empty chainID and network ID", nil, validTestKey, ids.ID(make([]byte, hashing.HashLen)), 0, []byte{0x8f, 0x61, 0x6, 0x95, 0x7c, 0x8a, 0xa4, 0x30, 0xce, 0x27, 0x52, 0xbc, 0x1, 0x99, 0x48, 0x46, 0x5f, 0xf, 0x31, 0xa8, 0x54, 0xd8, 0x48, 0xf4, 0x88, 0xf2, 0xbc, 0xfa, 0xff, 0x2e, 0x1e, 0x69, 0x18, 0xf6, 0x6d, 0xf4, 0x55, 0xea, 0x10, 0x6d, 0x32, 0x28, 0xd8, 0x16, 0x77, 0xed, 0x94, 0x99, 0x10, 0xd, 0x9d, 0xc1, 0x19, 0x8, 0x7a, 0x41, 0xb1, 0x7d, 0x9f, 0xab, 0xc9, 0x31, 0x22, 0x9f, 0x4c, 0xa5, 0xd5, 0xea, 0xb6, 0xc, 0xcd, 0xd7, 0x92, 0xb7, 0x29, 0x7, 0x10, 0xfb, 0x51, 0x4a, 0x8e, 0xb0, 0xed, 0x55, 0xf3, 0xf3, 0xb2, 0x1a, 0xa3, 0x85, 0x44, 0x34, 0xf3, 0xc3, 0x51, 0x56}},
		{"nil parentBlockVRFSig valid blsSignKey and empty chainID and valid network ID", nil, validTestKey, ids.ID(make([]byte, hashing.HashLen)), networkID, []byte{0xb7, 0xb5, 0x7a, 0x73, 0x65, 0xfe, 0xf2, 0xb0, 0x44, 0x57, 0xfa, 0x77, 0x45, 0xe6, 0xeb, 0xf2, 0x5d, 0x68, 0x2f, 0xee, 0x3d, 0x3a, 0x37, 0xc, 0x3b, 0x58, 0x33, 0x46, 0x40, 0x3b, 0x10, 0xa0, 0x5, 0x9, 0x51, 0x6, 0x1c, 0x68, 0x11, 0xdd, 0xdd, 0xe2, 0xf9, 0xe2, 0xd, 0xba, 0x4b, 0x10, 0x5, 0x64, 0x97, 0x9e, 0xf1, 0x19, 0x78, 0xd3, 0x68, 0x42, 0xeb, 0xed, 0xde, 0x87, 0x68, 0xfb, 0x5c, 0xa2, 0x13, 0x2d, 0xa, 0x4a, 0x80, 0x33, 0xdb, 0xd9, 0xac, 0xd0, 0xda, 0xdd, 0xc7, 0xda, 0x24, 0xd, 0x14, 0x5e, 0xac, 0xc, 0xe2, 0xfe, 0x59, 0x99, 0x16, 0x68, 0x89, 0x2d, 0x0, 0x8f}},
		{"nil parentBlockVRFSig valid blsSignKey and valid chainID and empty network ID", nil, validTestKey, chainID, 0, []byte{0x90, 0xf1, 0xd, 0xbb, 0x3c, 0x11, 0xa5, 0x0, 0x14, 0xa0, 0xd2, 0xa2, 0x90, 0x9b, 0xbb, 0x1c, 0x73, 0x47, 0xe1, 0xb6, 0x18, 0x37, 0x60, 0xd8, 0x10, 0xc0, 0xd4, 0x8b, 0xb8, 0xda, 0x71, 0x20, 0x11, 0x4d, 0x4c, 0xba, 0xb3, 0xc8, 0xf0, 0x77, 0x20, 0x78, 0x46, 0xec, 0xf2, 0xd5, 0xa6, 0xe3, 0x5, 0xa6, 0x78, 0x8b, 0x44, 0x3d, 0x4e, 0x35, 0xb7, 0x1, 0xd8, 0xb7, 0x19, 0x26, 0x9f, 0x12, 0x58, 0x3e, 0x36, 0x6a, 0x42, 0xeb, 0xd7, 0x1c, 0xf9, 0x5e, 0x62, 0xed, 0xce, 0x4c, 0x5e, 0x5, 0x37, 0x6d, 0xc0, 0x8f, 0x78, 0x92, 0x32, 0xd9, 0x24, 0x9a, 0x81, 0x49, 0xa2, 0x54, 0xcd, 0xb}},
		{"nil parentBlockVRFSig valid blsSignKey and valid chainID and valid network ID", nil, validTestKey, chainID, networkID, []byte{0xad, 0x77, 0x9e, 0xac, 0x67, 0x69, 0xa9, 0x82, 0xb3, 0xf6, 0x2d, 0x7d, 0xc4, 0xb8, 0xe2, 0xd, 0xf9, 0x19, 0x31, 0xfb, 0x3f, 0x17, 0x6b, 0x89, 0x39, 0x57, 0xda, 0xca, 0x75, 0x8, 0x53, 0xc6, 0xe9, 0x2c, 0xab, 0xe0, 0x4a, 0x17, 0x7c, 0xdc, 0x7c, 0x1, 0x9b, 0x26, 0x6c, 0x8d, 0xf3, 0x6a, 0x0, 0x62, 0xcd, 0x50, 0xbe, 0x6f, 0x1e, 0xd2, 0x18, 0x7b, 0x79, 0xed, 0xf5, 0x9e, 0x55, 0xbf, 0x2f, 0x58, 0xff, 0xcd, 0xa0, 0xf8, 0x69, 0x17, 0xe1, 0xb3, 0x2a, 0x13, 0xef, 0x8e, 0x39, 0x93, 0x4b, 0x63, 0xc5, 0x9f, 0xf3, 0xef, 0x36, 0x2, 0xf6, 0x65, 0x9, 0x25, 0xc8, 0xbe, 0x2f, 0x66}},

		{"valid parentBlockVRFSig hash valid blsSignKey and empty chainID and empty network ID", parentHashSig[:], validTestKey, ids.ID(make([]byte, hashing.HashLen)), 0, []byte{0x95, 0xf1, 0xc6, 0x48, 0xbf, 0x47, 0xed, 0x63, 0x4f, 0xd3, 0xb8, 0xd4, 0xad, 0xa9, 0x74, 0x69, 0xdf, 0x64, 0x3a, 0x26, 0xb, 0xed, 0xfa, 0xb6, 0x98, 0xa8, 0xd7, 0xcb, 0xf3, 0x1d, 0xbd, 0xe7, 0x4, 0x95, 0xbb, 0x65, 0x85, 0x6d, 0x28, 0xac, 0xa5, 0xce, 0x2e, 0x4f, 0xc0, 0x2b, 0x2f, 0x52, 0x16, 0xbd, 0xef, 0xcd, 0x7f, 0xdb, 0x66, 0x90, 0x39, 0xa9, 0x23, 0x10, 0x98, 0x74, 0x10, 0x15, 0xab, 0xfb, 0xb1, 0x37, 0x50, 0xf0, 0xf1, 0x2f, 0xb9, 0xc5, 0x9a, 0xbc, 0xba, 0x52, 0x35, 0xe4, 0x8, 0xd3, 0xf9, 0x90, 0x31, 0xc1, 0xda, 0x27, 0xe2, 0x3b, 0x62, 0x94, 0xa7, 0x8, 0x49, 0x9e}},
		{"valid parentBlockVRFSig hash valid blsSignKey and empty chainID and valid network ID", parentHashSig[:], validTestKey, ids.ID(make([]byte, hashing.HashLen)), networkID, []byte{0x95, 0xf1, 0xc6, 0x48, 0xbf, 0x47, 0xed, 0x63, 0x4f, 0xd3, 0xb8, 0xd4, 0xad, 0xa9, 0x74, 0x69, 0xdf, 0x64, 0x3a, 0x26, 0xb, 0xed, 0xfa, 0xb6, 0x98, 0xa8, 0xd7, 0xcb, 0xf3, 0x1d, 0xbd, 0xe7, 0x4, 0x95, 0xbb, 0x65, 0x85, 0x6d, 0x28, 0xac, 0xa5, 0xce, 0x2e, 0x4f, 0xc0, 0x2b, 0x2f, 0x52, 0x16, 0xbd, 0xef, 0xcd, 0x7f, 0xdb, 0x66, 0x90, 0x39, 0xa9, 0x23, 0x10, 0x98, 0x74, 0x10, 0x15, 0xab, 0xfb, 0xb1, 0x37, 0x50, 0xf0, 0xf1, 0x2f, 0xb9, 0xc5, 0x9a, 0xbc, 0xba, 0x52, 0x35, 0xe4, 0x8, 0xd3, 0xf9, 0x90, 0x31, 0xc1, 0xda, 0x27, 0xe2, 0x3b, 0x62, 0x94, 0xa7, 0x8, 0x49, 0x9e}},
		{"valid parentBlockVRFSig hash valid blsSignKey valid chainID and empty network ID", parentHashSig[:], validTestKey, chainID, 0, []byte{0x95, 0xf1, 0xc6, 0x48, 0xbf, 0x47, 0xed, 0x63, 0x4f, 0xd3, 0xb8, 0xd4, 0xad, 0xa9, 0x74, 0x69, 0xdf, 0x64, 0x3a, 0x26, 0xb, 0xed, 0xfa, 0xb6, 0x98, 0xa8, 0xd7, 0xcb, 0xf3, 0x1d, 0xbd, 0xe7, 0x4, 0x95, 0xbb, 0x65, 0x85, 0x6d, 0x28, 0xac, 0xa5, 0xce, 0x2e, 0x4f, 0xc0, 0x2b, 0x2f, 0x52, 0x16, 0xbd, 0xef, 0xcd, 0x7f, 0xdb, 0x66, 0x90, 0x39, 0xa9, 0x23, 0x10, 0x98, 0x74, 0x10, 0x15, 0xab, 0xfb, 0xb1, 0x37, 0x50, 0xf0, 0xf1, 0x2f, 0xb9, 0xc5, 0x9a, 0xbc, 0xba, 0x52, 0x35, 0xe4, 0x8, 0xd3, 0xf9, 0x90, 0x31, 0xc1, 0xda, 0x27, 0xe2, 0x3b, 0x62, 0x94, 0xa7, 0x8, 0x49, 0x9e}},
		{"valid parentBlockVRFSig hash valid blsSignKey valid chainID and valid network ID", parentHashSig[:], validTestKey, chainID, networkID, []byte{0x95, 0xf1, 0xc6, 0x48, 0xbf, 0x47, 0xed, 0x63, 0x4f, 0xd3, 0xb8, 0xd4, 0xad, 0xa9, 0x74, 0x69, 0xdf, 0x64, 0x3a, 0x26, 0xb, 0xed, 0xfa, 0xb6, 0x98, 0xa8, 0xd7, 0xcb, 0xf3, 0x1d, 0xbd, 0xe7, 0x4, 0x95, 0xbb, 0x65, 0x85, 0x6d, 0x28, 0xac, 0xa5, 0xce, 0x2e, 0x4f, 0xc0, 0x2b, 0x2f, 0x52, 0x16, 0xbd, 0xef, 0xcd, 0x7f, 0xdb, 0x66, 0x90, 0x39, 0xa9, 0x23, 0x10, 0x98, 0x74, 0x10, 0x15, 0xab, 0xfb, 0xb1, 0x37, 0x50, 0xf0, 0xf1, 0x2f, 0xb9, 0xc5, 0x9a, 0xbc, 0xba, 0x52, 0x35, 0xe4, 0x8, 0xd3, 0xf9, 0x90, 0x31, 0xc1, 0xda, 0x27, 0xe2, 0x3b, 0x62, 0x94, 0xa7, 0x8, 0x49, 0x9e}},

		{"valid parentBlockVRFSig signature valid blsSignKey and empty chainID and empty network ID", parentSig, validTestKey, ids.ID(make([]byte, hashing.HashLen)), 0, []byte{0xab, 0xde, 0x89, 0xda, 0x59, 0x46, 0xcd, 0xfa, 0xbc, 0xf0, 0x9e, 0x0, 0x1a, 0xf9, 0xd2, 0x24, 0x17, 0x6b, 0x4e, 0x2d, 0x93, 0xd0, 0x90, 0x3, 0x41, 0x95, 0x97, 0x38, 0xba, 0x9f, 0x3c, 0x7, 0x9c, 0x8e, 0xcf, 0x58, 0x1a, 0x56, 0x10, 0xe6, 0x54, 0xe2, 0x5, 0x36, 0xc1, 0x40, 0x68, 0x9b, 0x17, 0xdd, 0x3d, 0x7f, 0xe4, 0x39, 0xd1, 0xa6, 0x61, 0x7a, 0xc8, 0x75, 0x5c, 0xd5, 0x3b, 0x16, 0xee, 0xfc, 0x9b, 0xf0, 0x1f, 0xa1, 0xaf, 0x7d, 0xdc, 0xa3, 0x64, 0xe0, 0xc1, 0xf0, 0x76, 0xa2, 0xb8, 0xd0, 0x9e, 0x19, 0x13, 0x87, 0x36, 0x76, 0x5a, 0x60, 0x28, 0xce, 0x6d, 0xb7, 0xe5, 0x77}},
		{"valid parentBlockVRFSig signature valid blsSignKey and empty chainID and valid network ID", parentSig, validTestKey, ids.ID(make([]byte, hashing.HashLen)), networkID, []byte{0xab, 0xde, 0x89, 0xda, 0x59, 0x46, 0xcd, 0xfa, 0xbc, 0xf0, 0x9e, 0x0, 0x1a, 0xf9, 0xd2, 0x24, 0x17, 0x6b, 0x4e, 0x2d, 0x93, 0xd0, 0x90, 0x3, 0x41, 0x95, 0x97, 0x38, 0xba, 0x9f, 0x3c, 0x7, 0x9c, 0x8e, 0xcf, 0x58, 0x1a, 0x56, 0x10, 0xe6, 0x54, 0xe2, 0x5, 0x36, 0xc1, 0x40, 0x68, 0x9b, 0x17, 0xdd, 0x3d, 0x7f, 0xe4, 0x39, 0xd1, 0xa6, 0x61, 0x7a, 0xc8, 0x75, 0x5c, 0xd5, 0x3b, 0x16, 0xee, 0xfc, 0x9b, 0xf0, 0x1f, 0xa1, 0xaf, 0x7d, 0xdc, 0xa3, 0x64, 0xe0, 0xc1, 0xf0, 0x76, 0xa2, 0xb8, 0xd0, 0x9e, 0x19, 0x13, 0x87, 0x36, 0x76, 0x5a, 0x60, 0x28, 0xce, 0x6d, 0xb7, 0xe5, 0x77}},
		{"valid parentBlockVRFSig signature valid blsSignKey valid chainID and empty network ID", parentSig, validTestKey, chainID, 0, []byte{0xab, 0xde, 0x89, 0xda, 0x59, 0x46, 0xcd, 0xfa, 0xbc, 0xf0, 0x9e, 0x0, 0x1a, 0xf9, 0xd2, 0x24, 0x17, 0x6b, 0x4e, 0x2d, 0x93, 0xd0, 0x90, 0x3, 0x41, 0x95, 0x97, 0x38, 0xba, 0x9f, 0x3c, 0x7, 0x9c, 0x8e, 0xcf, 0x58, 0x1a, 0x56, 0x10, 0xe6, 0x54, 0xe2, 0x5, 0x36, 0xc1, 0x40, 0x68, 0x9b, 0x17, 0xdd, 0x3d, 0x7f, 0xe4, 0x39, 0xd1, 0xa6, 0x61, 0x7a, 0xc8, 0x75, 0x5c, 0xd5, 0x3b, 0x16, 0xee, 0xfc, 0x9b, 0xf0, 0x1f, 0xa1, 0xaf, 0x7d, 0xdc, 0xa3, 0x64, 0xe0, 0xc1, 0xf0, 0x76, 0xa2, 0xb8, 0xd0, 0x9e, 0x19, 0x13, 0x87, 0x36, 0x76, 0x5a, 0x60, 0x28, 0xce, 0x6d, 0xb7, 0xe5, 0x77}},
		{"valid parentBlockVRFSig signature valid blsSignKey valid chainID and valid network ID", parentSig, validTestKey, chainID, networkID, []byte{0xab, 0xde, 0x89, 0xda, 0x59, 0x46, 0xcd, 0xfa, 0xbc, 0xf0, 0x9e, 0x0, 0x1a, 0xf9, 0xd2, 0x24, 0x17, 0x6b, 0x4e, 0x2d, 0x93, 0xd0, 0x90, 0x3, 0x41, 0x95, 0x97, 0x38, 0xba, 0x9f, 0x3c, 0x7, 0x9c, 0x8e, 0xcf, 0x58, 0x1a, 0x56, 0x10, 0xe6, 0x54, 0xe2, 0x5, 0x36, 0xc1, 0x40, 0x68, 0x9b, 0x17, 0xdd, 0x3d, 0x7f, 0xe4, 0x39, 0xd1, 0xa6, 0x61, 0x7a, 0xc8, 0x75, 0x5c, 0xd5, 0x3b, 0x16, 0xee, 0xfc, 0x9b, 0xf0, 0x1f, 0xa1, 0xaf, 0x7d, 0xdc, 0xa3, 0x64, 0xe0, 0xc1, 0xf0, 0x76, 0xa2, 0xb8, 0xd0, 0x9e, 0x19, 0x13, 0x87, 0x36, 0x76, 0x5a, 0x60, 0x28, 0xce, 0x6d, 0xb7, 0xe5, 0x77}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require := require.New(t)
			require.Equal(testCase.expectedOut, NextBlockVRFSig(testCase.parentBlockVRFSig, testCase.blsSignKey, testCase.chainID, testCase.networkID))
		})
	}
}

func TestVerifySignature(t *testing.T) {
	signKey, err := bls.NewSecretKey()
	require.NoError(t, err)
	var block statelessBlock
	block.StatelessBlock.VRFSig = NextBlockVRFSig(nil, signKey, [32]byte{0}, 8888)
	block.vrfSig, err = bls.SignatureFromBytes(block.StatelessBlock.VRFSig)
	require.NoError(t, err)
	require.True(t, block.VerifySignature(bls.PublicFromSecretKey(signKey), nil, [32]byte{0}, 8888))
}
