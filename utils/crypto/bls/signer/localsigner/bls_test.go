// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package localsigner

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func AggregateAndVerify(publicKeys []*bls.PublicKey, signatures []*bls.Signature, message []byte) (bool, error) {
	aggSig, err := bls.AggregateSignatures(signatures)
	if err != nil {
		return false, err
	}
	aggPK, err := bls.AggregatePublicKeys(publicKeys)
	if err != nil {
		return false, err
	}

	return bls.Verify(aggPK, aggSig, message), nil
}

func NewSigner(require *require.Assertions) *LocalSigner {
	sk, err := New()
	require.NoError(err)
	return sk
}

func mapSlice[T any, U any](arr []T, f func(T) U) []U {
	result := make([]U, len(arr))
	for i, val := range arr {
		result[i] = f(val)
	}
	return result
}

func TestVerifyValidSignature(t *testing.T) {
	require := require.New(t)
	signer := NewSigner(require)

	msg := []byte("TestVerifyValidSignature local signer")

	sig, err := signer.Sign(msg)
	require.NoError(err)

	isValid := bls.Verify(signer.PublicKey(), sig, msg)
	require.True(isValid)
}

func TestVerifyWrongMessageSignature(t *testing.T) {
	require := require.New(t)
	signer := NewSigner(require)

	msg := []byte("TestVerifyWrongMessageSignature local signer")
	wrongMsg := []byte("TestVerifyWrongMessageSignature local signer with wrong message")

	sig, err := signer.Sign(msg)
	require.NoError(err)

	isValid := bls.Verify(signer.PublicKey(), sig, wrongMsg)
	require.False(isValid)
}

func TestVerifyWrongPubkeySignature(t *testing.T) {
	require := require.New(t)
	signer := NewSigner(require)
	wrongPk := NewSigner(require).pk

	msg := []byte("TestVerifyWrongPubkeySignature local signer")

	sig, err := signer.Sign(msg)
	require.NoError(err)

	isValid := bls.Verify(wrongPk, sig, msg)
	require.False(isValid)
}

func TestVerifyWrongMessageSignedSignature(t *testing.T) {
	require := require.New(t)
	signer := NewSigner(require)

	msg := []byte("TestVerifyWrongMessageSignedSignature local signer")
	wrongMsg := []byte("TestVerifyWrongMessageSignedSignaturelocal local signer with wrong signature")

	wrongSig, err := signer.Sign(wrongMsg)
	require.NoError(err)

	isValid := bls.Verify(signer.PublicKey(), wrongSig, msg)
	require.False(isValid)
}

func TestValidAggregation(t *testing.T) {
	require := require.New(t)
	signers := []*LocalSigner{NewSigner(require), NewSigner(require), NewSigner(require)}
	pks := mapSlice(signers, (*LocalSigner).PublicKey)

	msg := []byte("TestValidAggregation local signer")

	sigs := mapSlice(signers, func(signer *LocalSigner) *bls.Signature {
		sig, err := signer.Sign(msg)
		require.NoError(err)
		return sig
	})

	isValid, err := AggregateAndVerify(pks, sigs, msg)
	require.NoError(err)
	require.True(isValid)
}

func TestSingleKeyAggregation(t *testing.T) {
	require := require.New(t)
	signer := NewSigner(require)

	pks := []*bls.PublicKey{signer.PublicKey()}

	msg := []byte("TestSingleKeyAggregation local signer")

	sig, err := signer.Sign(msg)
	require.NoError(err)

	isValid, err := AggregateAndVerify(pks, []*bls.Signature{sig}, msg)
	require.NoError(err)
	require.True(isValid)
}

func TestIncorrectMessageAggregation(t *testing.T) {
	require := require.New(t)
	signers := []*LocalSigner{NewSigner(require), NewSigner(require), NewSigner(require)}
	pks := mapSlice(signers, (*LocalSigner).PublicKey)

	msg := []byte("TestIncorrectMessageAggregation local signer")

	signatures := mapSlice(signers, func(signer *LocalSigner) *bls.Signature {
		sig, err := signer.Sign(msg)
		require.NoError(err)
		return sig
	})

	isValid, err := AggregateAndVerify(pks, signatures, []byte("a different message"))
	require.NoError(err)
	require.False(isValid)
}

func TestDifferentMessageAggregation(t *testing.T) {
	require := require.New(t)
	signers := []*LocalSigner{NewSigner(require), NewSigner(require), NewSigner(require)}
	pks := mapSlice(signers, (*LocalSigner).PublicKey)

	msg := []byte("TestDifferentMessagesAggregation local signer")
	differentMsg := []byte("TestDifferentMessagesAggregation local signer with different message")

	diffSig, err := signers[0].Sign(differentMsg)
	require.NoError(err)

	sameSigs := mapSlice(signers[1:], func(signer *LocalSigner) *bls.Signature {
		sig, err := signer.Sign(msg)
		require.NoError(err)
		return sig
	})

	signatures := append([]*bls.Signature{diffSig}, sameSigs...)

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestOneIncorrectPubKeyAggregation(t *testing.T) {
	require := require.New(t)
	signers := []*LocalSigner{NewSigner(require), NewSigner(require), NewSigner(require)}
	pks := mapSlice(signers, (*LocalSigner).PublicKey)

	msg := []byte("TestOneIncorrectPubKeyAggregation local signer")

	signatures := mapSlice(signers, func(signer *LocalSigner) *bls.Signature {
		sig, err := signer.Sign(msg)
		require.NoError(err)
		return sig
	})

	// incorrect pubkey
	pks[0] = NewSigner(require).PublicKey()

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestMorePubkeysThanSignaturesAggregation(t *testing.T) {
	require := require.New(t)
	signers := []*LocalSigner{NewSigner(require), NewSigner(require), NewSigner(require)}
	pks := mapSlice(signers, (*LocalSigner).PublicKey)

	msg := []byte("TestMorePubkeysThanSignatures local signer")

	signatures := mapSlice(signers, func(signer *LocalSigner) *bls.Signature {
		sig, err := signer.Sign(msg)
		require.NoError(err)
		return sig
	})

	// add an extra pubkey
	pks = append(pks, NewSigner(require).PublicKey())

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestMoreSignaturesThanPubkeysAggregation(t *testing.T) {
	require := require.New(t)
	signers := []*LocalSigner{NewSigner(require), NewSigner(require), NewSigner(require)}
	pks := mapSlice(signers, (*LocalSigner).PublicKey)

	msg := []byte("TestMoreSignaturesThanPubkeys local signer")

	signatures := mapSlice(signers, func(signer *LocalSigner) *bls.Signature {
		sig, err := signer.Sign(msg)
		require.NoError(err)
		return sig
	})

	isValid, err := AggregateAndVerify(pks[1:], signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestNoPubkeysAggregation(t *testing.T) {
	require := require.New(t)
	signers := []*LocalSigner{NewSigner(require), NewSigner(require), NewSigner(require)}

	msg := []byte("TestNoPubkeysAggregation local signer")

	signatures := mapSlice(signers, func(signer *LocalSigner) *bls.Signature {
		sig, err := signer.Sign(msg)
		require.NoError(err)
		return sig
	})

	isValid, err := AggregateAndVerify(nil, signatures, msg)
	require.ErrorIs(err, bls.ErrNoPublicKeys)
	require.False(isValid)
}

func TestNoSignaturesAggregation(t *testing.T) {
	require := require.New(t)
	signers := []*LocalSigner{NewSigner(require), NewSigner(require), NewSigner(require)}
	pks := mapSlice(signers, (*LocalSigner).PublicKey)

	msg := []byte("TestNoSignaturesAggregation local signer")

	isValid, err := AggregateAndVerify(pks, nil, msg)
	require.ErrorIs(err, bls.ErrNoSignatures)
	require.False(isValid)
}

func TestVerifyValidProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer := NewSigner(require)

	msg := []byte("TestVerifyValidProofOfPossession local signer")

	sig, err := signer.SignProofOfPossession(msg)
	require.NoError(err)

	isValid := bls.VerifyProofOfPossession(signer.PublicKey(), sig, msg)
	require.True(isValid)
}

func TestVerifyWrongMessageProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer := NewSigner(require)

	msg := []byte("TestVerifyWrongMessageProofOfPossession local signer")
	wrongMsg := []byte("TestVerifyWrongMessageProofOfPossession local signer with wrong message")

	sig, err := signer.SignProofOfPossession(msg)
	require.NoError(err)

	isValid := bls.VerifyProofOfPossession(signer.PublicKey(), sig, wrongMsg)
	require.False(isValid)
}

func TestVerifyWrongPubkeyProofOfPossession(t *testing.T) {
	require := require.New(t)
	correctSigner := NewSigner(require)
	wrongSigner := NewSigner(require)

	msg := []byte("TestVerifyWrongPubkeyProofOfPossession local signer")

	sig, err := correctSigner.SignProofOfPossession(msg)
	require.NoError(err)

	isValid := bls.VerifyProofOfPossession(wrongSigner.PublicKey(), sig, msg)
	require.False(isValid)
}

func TestVerifyWrongMessageSignedProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer := NewSigner(require)

	msg := []byte("TestVerifyWrongMessageSignedProofOfPossession local signer")
	wrongMsg := []byte("TestVerifyWrongMessageSignedProofOfPossession local signer with wrong signature")

	wrongSig, err := signer.SignProofOfPossession(wrongMsg)
	require.NoError(err)

	isValid := bls.VerifyProofOfPossession(signer.PublicKey(), wrongSig, msg)
	require.False(isValid)
}

func TestIdempotentFromFileOrPersistNew(t *testing.T) {
	require := require.New(t)
	path := filepath.Join(t.TempDir(), "keyfile")

	signer1, err := FromFileOrPersistNew(path)
	require.NoError(err)
	signer2, err := FromFileOrPersistNew(path)
	require.NoError(err)

	require.Equal(signer1.PublicKey(), signer2.PublicKey())
}
