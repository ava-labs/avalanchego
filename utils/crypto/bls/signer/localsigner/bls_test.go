// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package localsigner

import (
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

func NewKeyPair(require *require.Assertions) (*LocalSigner, *bls.PublicKey) {
	sk, err := New()
	require.NoError(err)
	pk := sk.PublicKey()
	return sk, pk
}

func TestVerifyValidSignature(t *testing.T) {
	require := require.New(t)
	signer, pk := NewKeyPair(require)

	msg := []byte("TestVerifyValidSignature local signer")

	sig := signer.Sign(msg)

	isValid := bls.Verify(pk, sig, msg)
	require.True(isValid)
}

func TestVerifyWrongMessageSignature(t *testing.T) {
	require := require.New(t)
	signer, pk := NewKeyPair(require)

	msg := []byte("TestVerifyWrongMessageSignature local signer")
	wrongMsg := []byte("TestVerifyWrongMessageSignature local signer with wrong message")

	sig := signer.Sign(msg)

	isValid := bls.Verify(pk, sig, wrongMsg)
	require.False(isValid)
}

func TestVerifyWrongPubkeySignature(t *testing.T) {
	require := require.New(t)
	signer, _ := NewKeyPair(require)
	_, wrongPk := NewKeyPair(require)

	msg := []byte("TestVerifyWrongPubkeySignature local signer")

	sig := signer.Sign(msg)

	isValid := bls.Verify(wrongPk, sig, msg)
	require.False(isValid)
}

func TestVerifyWrongMessageSignedSignature(t *testing.T) {
	require := require.New(t)
	signer, pk := NewKeyPair(require)

	msg := []byte("TestVerifyWrongMessageSignedSignature local signer")
	wrongMsg := []byte("TestVerifyWrongMessageSignedSignaturelocal local signer with wrong signature")

	wrongSig := signer.Sign(wrongMsg)

	isValid := bls.Verify(pk, wrongSig, msg)
	require.False(isValid)
}

func TestValidAggregation(t *testing.T) {
	require := require.New(t)
	sk1, pk1 := NewKeyPair(require)
	sk2, pk2 := NewKeyPair(require)
	sk3, pk3 := NewKeyPair(require)

	pks := []*bls.PublicKey{pk1, pk2, pk3}

	msg := []byte("TestValidAggregation local signer")

	sigs := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(msg),
	}

	isValid, err := AggregateAndVerify(pks, sigs, msg)
	require.NoError(err)
	require.True(isValid)
}

func TestSingleKeyAggregation(t *testing.T) {
	require := require.New(t)
	signer, pk := NewKeyPair(require)

	pks := []*bls.PublicKey{pk}

	msg := []byte("TestSingleKeyAggregation local signer")

	sig := signer.Sign(msg)

	isValid, err := AggregateAndVerify(pks, []*bls.Signature{sig}, msg)
	require.NoError(err)
	require.True(isValid)
}

func TestIncorrectMessageAggregation(t *testing.T) {
	require := require.New(t)
	sk1, pk1 := NewKeyPair(require)
	sk2, pk2 := NewKeyPair(require)
	sk3, pk3 := NewKeyPair(require)

	pks := []*bls.PublicKey{pk1, pk2, pk3}

	msg := []byte("TestIncorrectMessageAggregation local signer")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(msg),
	}

	isValid, err := AggregateAndVerify(pks, signatures, []byte("a different message"))
	require.NoError(err)
	require.False(isValid)
}

func TestDifferentMessageAggregation(t *testing.T) {
	require := require.New(t)
	sk1, pk1 := NewKeyPair(require)
	sk2, pk2 := NewKeyPair(require)
	sk3, pk3 := NewKeyPair(require)

	pks := []*bls.PublicKey{pk1, pk2, pk3}

	msg := []byte("TestDifferentMessagesAggregation local signer")
	differentMsg := []byte("TestDifferentMessagesAggregation local signer with different message")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(differentMsg),
	}

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestOneIncorrectPubKeyAggregation(t *testing.T) {
	require := require.New(t)
	sk1, pk1 := NewKeyPair(require)
	sk2, pk2 := NewKeyPair(require)
	sk3, _ := NewKeyPair(require)
	_, wrongPk := NewKeyPair(require)

	pks := []*bls.PublicKey{pk1, pk2, wrongPk}

	msg := []byte("TestOneIncorrectPubKeyAggregation local signer")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(msg),
	}

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestMorePubkeysThanSignaturesAggregation(t *testing.T) {
	require := require.New(t)
	sk1, pk1 := NewKeyPair(require)
	sk2, pk2 := NewKeyPair(require)
	_, pk3 := NewKeyPair(require)

	pks := []*bls.PublicKey{pk1, pk2, pk3}

	msg := []byte("TestMorePubkeysThanSignatures local signer")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
	}

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestMoreSignaturesThanPubkeysAggregation(t *testing.T) {
	require := require.New(t)
	sk1, pk1 := NewKeyPair(require)
	sk2, pk2 := NewKeyPair(require)
	sk3, _ := NewKeyPair(require)

	pks := []*bls.PublicKey{pk1, pk2}

	msg := []byte("TestMoreSignaturesThanPubkeys local signer")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(msg),
	}

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestNoPubkeysAggregation(t *testing.T) {
	require := require.New(t)
	sk1, _ := NewKeyPair(require)
	sk2, _ := NewKeyPair(require)
	sk3, _ := NewKeyPair(require)

	msg := []byte("TestNoPubkeysAggregation local signer")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(msg),
	}

	isValid, err := AggregateAndVerify(nil, signatures, msg)
	require.ErrorIs(err, bls.ErrNoPublicKeys)
	require.False(isValid)
}

func TestNoSignaturesAggregation(t *testing.T) {
	require := require.New(t)
	_, pk1 := NewKeyPair(require)
	_, pk2 := NewKeyPair(require)
	_, pk3 := NewKeyPair(require)

	pks := []*bls.PublicKey{pk1, pk2, pk3}

	msg := []byte("TestNoSignaturesAggregation local signer")

	isValid, err := AggregateAndVerify(pks, nil, msg)
	require.ErrorIs(err, bls.ErrNoSignatures)
	require.False(isValid)
}

func TestVerifyValidProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer, pk := NewKeyPair(require)

	msg := []byte("TestVerifyValidProofOfPossession local signer")

	sig := signer.SignProofOfPossession(msg)

	isValid := bls.VerifyProofOfPossession(pk, sig, msg)
	require.True(isValid)
}

func TestVerifyWrongMessageProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer, pk := NewKeyPair(require)

	msg := []byte("TestVerifyWrongMessageProofOfPossession local signer")
	wrongMsg := []byte("TestVerifyWrongMessageProofOfPossession local signer with wrong message")

	sig := signer.SignProofOfPossession(msg)

	isValid := bls.VerifyProofOfPossession(pk, sig, wrongMsg)
	require.False(isValid)
}

func TestVerifyWrongPubkeyProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer, _ := NewKeyPair(require)
	_, wrongPk := NewKeyPair(require)

	msg := []byte("TestVerifyWrongPubkeyProofOfPossession local signer")

	sig := signer.SignProofOfPossession(msg)

	isValid := bls.VerifyProofOfPossession(wrongPk, sig, msg)
	require.False(isValid)
}

func TestVerifyWrongMessageSignedProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer, pk := NewKeyPair(require)

	msg := []byte("TestVerifyWrongMessageSignedProofOfPossession local signer")
	wrongMsg := []byte("TestVerifyWrongMessageSignedProofOfPossession local signer with wrong signature")

	wrongSig := signer.SignProofOfPossession(wrongMsg)

	isValid := bls.VerifyProofOfPossession(pk, wrongSig, msg)
	require.False(isValid)
}
