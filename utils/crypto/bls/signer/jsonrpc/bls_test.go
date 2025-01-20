// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package jsonrpc

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/blstest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

func NewLocalPair(require *require.Assertions) (*localsigner.LocalSigner, *bls.PublicKey) {
	sk, err := localsigner.NewSigner()
	require.NoError(err)
	pk := sk.PublicKey()
	return sk, pk
}

func NewSigner(require *require.Assertions) (*Server, *Client, *bls.PublicKey) {
	service, err := NewSignerService()
	require.NoError(err)
	server, err := Serve(service)
	require.NoError(err)

	url := url.URL{
		Scheme: "http",
		Host:   server.Addr().String(),
	}

	client := NewClient(url)
	pk := client.PublicKey()

	return server, client, pk
}

func TestVerifyValidSignature(t *testing.T) {
	require := require.New(t)
	server, signer, pk := NewSigner(require)
	defer server.Close()

	msg := []byte("TestVerifyValidSignature json-rpc")

	sig := signer.Sign(msg)

	isValid := bls.Verify(pk, sig, msg)
	require.True(isValid)
}

func TestVerifyWrongMessageSignature(t *testing.T) {
	require := require.New(t)
	server, signer, pk := NewSigner(require)
	defer server.Close()

	msg := []byte("TestVerifyWrongMessageSignature json-rpc")
	wrongMsg := []byte("TestVerifyWrongMessageSignature json-rpc with wrong message")

	sig := signer.Sign(msg)

	isValid := bls.Verify(pk, sig, wrongMsg)
	require.False(isValid)
}

func TestVerifyWrongPubkeySignature(t *testing.T) {
	require := require.New(t)
	server, signer, _ := NewSigner(require)
	defer server.Close()
	_, wrongPk := NewLocalPair(require)

	msg := []byte("TestVerifyWrongPubkeySignature json-rpc")

	sig := signer.Sign(msg)

	isValid := bls.Verify(wrongPk, sig, msg)
	require.False(isValid)
}

func TestVerifyWrongMessageSignedSignature(t *testing.T) {
	require := require.New(t)
	server, signer, pk := NewSigner(require)
	defer server.Close()

	msg := []byte("TestVerifyWrongMessageSignedSignature json-rpc")
	wrongMsg := []byte("TestVerifyWrongMessageSignedSignaturelocal json-rpc with wrong signature")

	wrongSig := signer.Sign(wrongMsg)

	isValid := bls.Verify(pk, wrongSig, msg)
	require.False(isValid)
}

func TestValidAggregation(t *testing.T) {
	require := require.New(t)
	server, signer1, pk1 := NewSigner(require)
	defer server.Close()

	signer2, pk2 := NewLocalPair(require)
	signer3, pk3 := NewLocalPair(require)

	pks := []*bls.PublicKey{pk1, pk2, pk3}

	msg := []byte("TestValidAggregation json-rpc")

	sigs := []*bls.Signature{
		signer1.Sign(msg),
		signer2.Sign(msg),
		signer3.Sign(msg),
	}

	isValid, err := blstest.AggregateAndVerify(pks, sigs, msg)
	require.NoError(err)
	require.True(isValid)
}

func TestSingleKeyAggregation(t *testing.T) {
	require := require.New(t)
	server, signer, pk := NewSigner(require)
	defer server.Close()

	pks := []*bls.PublicKey{pk}

	msg := []byte("TestSingleKeyAggregation json-rpc")

	sig := signer.Sign(msg)

	isValid, err := blstest.AggregateAndVerify(pks, []*bls.Signature{sig}, msg)
	require.NoError(err)
	require.True(isValid)
}

func TestIncorrectMessageAggregation(t *testing.T) {
	require := require.New(t)
	server, sk1, pk1 := NewSigner(require)
	defer server.Close()

	sk2, pk2 := NewLocalPair(require)
	sk3, pk3 := NewLocalPair(require)

	pks := []*bls.PublicKey{pk1, pk2, pk3}

	msg := []byte("TestIncorrectMessageAggregation json-rpc")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(msg),
	}

	isValid, err := blstest.AggregateAndVerify(pks, signatures, []byte("a different message"))
	require.NoError(err)
	require.False(isValid)
}

func TestOneDifferentMessageAggregation(t *testing.T) {
	require := require.New(t)
	server, sk1, pk1 := NewSigner(require)
	defer server.Close()

	sk2, pk2 := NewLocalPair(require)
	sk3, pk3 := NewLocalPair(require)

	pks := []*bls.PublicKey{pk1, pk2, pk3}

	msg := []byte("TestDifferentMessagesAggregation json-rpc")
	differentMsg := []byte("TestDifferentMessagesAggregation json-rpc with different message")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(differentMsg),
	}

	isValid, err := blstest.AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestOneIncorrectPubKeyAggregation(t *testing.T) {
	require := require.New(t)
	server, sk1, pk1 := NewSigner(require)
	defer server.Close()

	sk2, pk2 := NewLocalPair(require)
	sk3, _ := NewLocalPair(require)
	_, wrongPk := NewLocalPair(require)

	pks := []*bls.PublicKey{pk1, pk2, wrongPk}

	msg := []byte("TestOneIncorrectPubKeyAggregation json-rpc")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(msg),
	}

	isValid, err := blstest.AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestMorePubkeysThanSignaturesAggregation(t *testing.T) {
	require := require.New(t)
	server, sk1, pk1 := NewSigner(require)
	defer server.Close()
	sk2, pk2 := NewLocalPair(require)
	_, pk3 := NewLocalPair(require)

	pks := []*bls.PublicKey{pk1, pk2, pk3}

	msg := []byte("TestMorePubkeysThanSignatures json-rpc")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
	}

	isValid, err := blstest.AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestMoreSignaturesThanPubkeysAggregation(t *testing.T) {
	require := require.New(t)
	server, sk1, pk1 := NewSigner(require)
	defer server.Close()
	sk2, pk2 := NewLocalPair(require)
	sk3, _ := NewLocalPair(require)

	pks := []*bls.PublicKey{pk1, pk2}

	msg := []byte("TestMoreSignaturesThanPubkeys json-rpc")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(msg),
	}

	isValid, err := blstest.AggregateAndVerify(pks, signatures, msg)
	require.NoError(err)
	require.False(isValid)
}

func TestNoPubkeysAggregation(t *testing.T) {
	require := require.New(t)
	server, sk1, _ := NewSigner(require)
	defer server.Close()
	sk2, _ := NewLocalPair(require)
	sk3, _ := NewLocalPair(require)

	msg := []byte("TestNoPubkeysAggregation json-rpc")

	signatures := []*bls.Signature{
		sk1.Sign(msg),
		sk2.Sign(msg),
		sk3.Sign(msg),
	}

	isValid, err := blstest.AggregateAndVerify(nil, signatures, msg)
	require.ErrorIs(err, bls.ErrNoPublicKeys)
	require.False(isValid)
}

func TestNoSignaturesAggregation(t *testing.T) {
	require := require.New(t)
	server, _, pk1 := NewSigner(require)
	defer server.Close()
	_, pk2 := NewLocalPair(require)
	_, pk3 := NewLocalPair(require)

	pks := []*bls.PublicKey{pk1, pk2, pk3}

	msg := []byte("TestNoSignaturesAggregation json-rpc")

	isValid, err := blstest.AggregateAndVerify(pks, nil, msg)
	require.ErrorIs(err, bls.ErrNoSignatures)
	require.False(isValid)
}

func TestVerifyValidProofOfPossession(t *testing.T) {
	require := require.New(t)
	server, signer, pk := NewSigner(require)
	defer server.Close()

	msg := []byte("TestVerifyValidProofOfPossession json-rpc")

	sig := signer.SignProofOfPossession(msg)

	isValid := bls.VerifyProofOfPossession(pk, sig, msg)
	require.True(isValid)
}

func TestVerifyWrongMessageProofOfPossession(t *testing.T) {
	require := require.New(t)
	server, signer, pk := NewSigner(require)
	defer server.Close()

	msg := []byte("TestVerifyWrongMessageProofOfPossession json-rpc")
	wrongMsg := []byte("TestVerifyWrongMessageProofOfPossession json-rpc with wrong message")

	sig := signer.SignProofOfPossession(msg)

	isValid := bls.VerifyProofOfPossession(pk, sig, wrongMsg)
	require.False(isValid)
}

func TestVerifyWrongPubkeyProofOfPossession(t *testing.T) {
	require := require.New(t)
	server, signer, _ := NewSigner(require)
	defer server.Close()
	_, wrongPk := NewLocalPair(require)

	msg := []byte("TestVerifyWrongPubkeyProofOfPossession json-rpc")

	sig := signer.SignProofOfPossession(msg)

	isValid := bls.VerifyProofOfPossession(wrongPk, sig, msg)
	require.False(isValid)
}

func TestVerifyWrongMessageSignedProofOfPossession(t *testing.T) {
	require := require.New(t)
	server, signer, pk := NewSigner(require)
	defer server.Close()

	msg := []byte("TestVerifyWrongMessageSignedProofOfPossession json-rpc")
	wrongMsg := []byte("TestVerifyWrongMessageSignedProofOfPossession json-rpc with wrong signature")

	wrongSig := signer.SignProofOfPossession(wrongMsg)

	isValid := bls.VerifyProofOfPossession(pk, wrongSig, msg)
	require.False(isValid)
}
