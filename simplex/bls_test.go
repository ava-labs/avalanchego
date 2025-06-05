// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestBLSSignVerify(t *testing.T) {
	config, _ := newTestEngineConfig(t, 0)

	signer, verifier := NewBLSAuth(config)

	msg := "Begin at the beginning, and go on till you come to the end: then stop"

	sig, err := signer.Sign([]byte(msg))
	require.NoError(t, err)

	err = verifier.Verify([]byte(msg), sig, config.Ctx.NodeID[:])
	require.NoError(t, err)
}

func TestSignerNotInMemberSet(t *testing.T) {
	config, _ := newTestEngineConfig(t, 0)
	signer, verifier := NewBLSAuth(config)

	msg := "Begin at the beginning, and go on till you come to the end: then stop"

	sig, err := signer.Sign([]byte(msg))
	require.NoError(t, err)

	notInMembershipSet := ids.GenerateTestNodeID()
	err = verifier.Verify([]byte(msg), sig, notInMembershipSet[:])
	require.ErrorIs(t, err, errSignerNotFound)
}

func TestSignerInvalidMessageEncoding(t *testing.T) {
	config, ls := newTestEngineConfig(t, 0)

	// sign a message with invalid encoding
	dummyMsg := []byte("dummy message")
	sig, err := ls.Sign(dummyMsg)
	require.NoError(t, err)

	sigBytes := bls.SignatureToBytes(sig)

	_, verifier := NewBLSAuth(config)
	err = verifier.Verify(dummyMsg, sigBytes, config.Ctx.NodeID[:])
	require.ErrorIs(t, err, errSignatureVerificationFailed)
}
