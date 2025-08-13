// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestSignatureBytes(t *testing.T) {
	require := require.New(t)

	msg := utils.RandomBytes(1234)

	sk := newKey(require)
	sig := sign(sk, msg)
	sigBytes := SignatureToBytes(sig)

	sig2, err := SignatureFromBytes(sigBytes)
	require.NoError(err)
	sig2Bytes := SignatureToBytes(sig2)

	require.Equal(sig, sig2)
	require.Equal(sigBytes, sig2Bytes)
}

func TestAggregateSignaturesNoop(t *testing.T) {
	require := require.New(t)

	msg := utils.RandomBytes(1234)

	sk := newKey(require)
	sig := sign(sk, msg)
	sigBytes := SignatureToBytes(sig)

	aggSig, err := AggregateSignatures([]*Signature{sig})
	require.NoError(err)

	aggSigBytes := SignatureToBytes(aggSig)
	require.NoError(err)

	require.Equal(sig, aggSig)
	require.Equal(sigBytes, aggSigBytes)
}
