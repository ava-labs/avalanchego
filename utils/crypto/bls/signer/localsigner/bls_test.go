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

func NewSigner(require *require.Assertions) *LocalSigner {
	sk, err := New()
	require.NoError(err)
	return sk
}

func collectN[T any](n int, fn func() T) []T {
	result := make([]T, n)
	for i := 0; i < n; i++ {
		result[i] = fn()
	}
	return result
}

func mapWithError[T any, U any](arr []T, fn func(T) (U, error)) ([]U, error) {
	result := make([]U, len(arr))
	for i, val := range arr {
		newVal, err := fn(val)
		if err != nil {
			return nil, err
		}
		result[i] = newVal
	}
	return result, nil
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
	signers := collectN(3, func() *LocalSigner {
		return NewSigner(require)
	})

	msg := []byte("TestValidAggregation local signer")

	sigs, err := mapWithError(signers, func(signer *LocalSigner) (*bls.Signature, error) {
		return signer.Sign(msg)
	})
	require.NoError(err)

	pks, _ := mapWithError(signers, func(signer *LocalSigner) (*bls.PublicKey, error) {
		return signer.PublicKey(), nil
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
	signers := collectN(3, func() *LocalSigner {
		return NewSigner(require)
	})

	msg := []byte("TestIncorrectMessageAggregation local signer")

	signatures, err := mapWithError(signers, func(signer *LocalSigner) (*bls.Signature, error) {
		return signer.Sign(msg)
	})
	require.NoError(err)

	pks, _ := mapWithError(signers, func(signer *LocalSigner) (*bls.PublicKey, error) {
		return signer.PublicKey(), nil
	})

	isValid, err := AggregateAndVerify(pks, signatures, []byte("a different message"))
	require.NoError(err)
	require.False(isValid)
}

func TestDifferentMessageAggregation(t *testing.T) {
	require := require.New(t)
	signers := collectN(3, func() *LocalSigner {
		return NewSigner(require)
	})

	msg := []byte("TestDifferentMessagesAggregation local signer")
	differentMsg := []byte("TestDifferentMessagesAggregation local signer with different message")

	signatures, err := mapWithError(signers, func(signer *LocalSigner) (*bls.Signature, error) {
		return signer.Sign(msg)
	})
	require.NoError(err)

	wrongSig, err := signers[0].Sign(differentMsg)
	require.NoError(err)

	signatures[0] = wrongSig

	pks, _ := mapWithError(signers, func(pair *LocalSigner) (*bls.PublicKey, error) {
		return pair.pk, nil
	})

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestOneIncorrectPubKeyAggregation(t *testing.T) {
	require := require.New(t)
	signers := collectN(3, func() *LocalSigner {
		return NewSigner(require)
	})

	pks, _ := mapWithError(signers[1:], func(signer *LocalSigner) (*bls.PublicKey, error) {
		return signer.pk, nil
	})

	msg := []byte("TestOneIncorrectPubKeyAggregation local signer")

	signatures, err := mapWithError(signers[:len(signers)-1], func(signer *LocalSigner) (*bls.Signature, error) {
		return signer.Sign(msg)
	})
	require.NoError(err)

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestMorePubkeysThanSignaturesAggregation(t *testing.T) {
	require := require.New(t)
	signers := collectN(3, func() *LocalSigner {
		return NewSigner(require)
	})

	msg := []byte("TestMorePubkeysThanSignatures local signer")

	signatures, err := mapWithError(signers[1:], func(signer *LocalSigner) (*bls.Signature, error) {
		return signer.Sign(msg)
	})
	require.NoError(err)

	pks, _ := mapWithError(signers, func(signer *LocalSigner) (*bls.PublicKey, error) {
		return signer.PublicKey(), nil
	})

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestMoreSignaturesThanPubkeysAggregation(t *testing.T) {
	require := require.New(t)
	signers := collectN(3, func() *LocalSigner {
		return NewSigner(require)
	})

	msg := []byte("TestMoreSignaturesThanPubkeys local signer")

	signatures, err := mapWithError(signers, func(signer *LocalSigner) (*bls.Signature, error) {
		return signer.Sign(msg)
	})
	require.NoError(err)

	pks, _ := mapWithError(signers[1:], func(signer *LocalSigner) (*bls.PublicKey, error) {
		return signer.PublicKey(), nil
	})

	isValid, err := AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestNoPubkeysAggregation(t *testing.T) {
	require := require.New(t)
	sks := collectN(3, func() *LocalSigner {
		return NewSigner(require)
	})

	msg := []byte("TestNoPubkeysAggregation local signer")

	signatures, err := mapWithError(sks, func(sk *LocalSigner) (*bls.Signature, error) {
		return sk.Sign(msg)
	})
	require.NoError(err)

	isValid, err := AggregateAndVerify(nil, signatures, msg)
	require.ErrorIs(err, bls.ErrNoPublicKeys)
	require.False(isValid)
}

func TestNoSignaturesAggregation(t *testing.T) {
	require := require.New(t)
	pks := collectN(3, func() *bls.PublicKey {
		return NewSigner(require).pk
	})

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
