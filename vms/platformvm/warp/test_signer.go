// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

// SignerTests is a list of all signer tests
var SignerTests = []func(t *testing.T, s Signer, sk *bls.SecretKey, networkID uint32, chainID ids.ID){
	TestSignerWrongChainID,
	TestSignerVerifies,
}

// Test that using a random SourceChainID results in an error
func TestSignerWrongChainID(t *testing.T, s Signer, _ *bls.SecretKey, _ uint32, _ ids.ID) {
	require := require.New(t)

	msg, err := NewUnsignedMessage(
		constants.UnitTestID,
		ids.GenerateTestID(),
		[]byte("payload"),
	)
	require.NoError(err)

	_, err = s.Sign(msg)
	// TODO: require error to be errWrongSourceChainID
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too
}

// Test that using a different networkID results in an error
func TestSignerWrongNetworkID(t *testing.T, s Signer, _ *bls.SecretKey, networkID uint32, blockchainID ids.ID) {
	require := require.New(t)

	msg, err := NewUnsignedMessage(
		networkID+1,
		blockchainID,
		[]byte("payload"),
	)
	require.NoError(err)

	_, err = s.Sign(msg)
	// TODO: require error to be errWrongNetworkID
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too
}

// Test that a signature generated with the signer verifies correctly
func TestSignerVerifies(t *testing.T, s Signer, sk *bls.SecretKey, networkID uint32, chainID ids.ID) {
	require := require.New(t)

	msg, err := NewUnsignedMessage(
		networkID,
		chainID,
		[]byte("payload"),
	)
	require.NoError(err)

	sigBytes, err := s.Sign(msg)
	require.NoError(err)

	sig, err := bls.SignatureFromBytes(sigBytes)
	require.NoError(err)

	pk := bls.PublicFromSecretKey(sk)
	msgBytes := msg.Bytes()
	require.True(bls.Verify(pk, sig, msgBytes))
}
