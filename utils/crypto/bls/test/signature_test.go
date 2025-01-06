// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signers/localsigner"
)

func TestSignatureBytes(t *testing.T) {
	require := require.New(t)

	msg := utils.RandomBytes(1234)

	sk, err := localsigner.NewSigner()
	require.NoError(err)
	sig := sk.Sign(msg)
	sigBytes := bls.SignatureToBytes(sig)

	sig2, err := bls.SignatureFromBytes(sigBytes)
	require.NoError(err)
	sig2Bytes := bls.SignatureToBytes(sig2)

	require.Equal(sig, sig2)
	require.Equal(sigBytes, sig2Bytes)
}

func TestAggregateSignaturesNoop(t *testing.T) {
	require := require.New(t)

	msg := utils.RandomBytes(1234)

	sk, err := localsigner.NewSigner()
	require.NoError(err)

	sig := sk.Sign(msg)
	sigBytes := bls.SignatureToBytes(sig)

	aggSig, err := bls.AggregateSignatures([]*bls.Signature{sig})
	require.NoError(err)

	aggSigBytes := bls.SignatureToBytes(aggSig)
	require.NoError(err)

	require.Equal(sig, aggSig)
	require.Equal(sigBytes, aggSigBytes)
}
