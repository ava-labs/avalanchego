// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"

	blst "github.com/supranational/blst/bindings/go"
)

func newKey(require *require.Assertions) *blst.SecretKey {
	var ikm [32]byte
	_, err := rand.Read(ikm[:])
	require.NoError(err)
	sk := blst.KeyGen(ikm[:])
	ikm = [32]byte{} // zero out the ikm

	return sk
}

func publicKey(sk *blst.SecretKey) *PublicKey {
	return new(PublicKey).From(sk)
}

func sign(sk *blst.SecretKey, msg []byte) *Signature {
	return new(Signature).Sign(sk, msg, CiphersuiteSignature.Bytes())
}

func TestAggregationThreshold(t *testing.T) {
	require := require.New(t)

	// People in the network would privately generate their secret keys
	sk0 := newKey(require)
	sk1 := newKey(require)
	sk2 := newKey(require)

	// All the public keys would be registered on chain
	pks := []*PublicKey{
		publicKey(sk0),
		publicKey(sk1),
		publicKey(sk2),
	}

	// The transaction's unsigned bytes are publicly known.
	msg := utils.RandomBytes(1234)

	// People may attempt time sign the transaction.
	sigs := []*Signature{
		sign(sk0, msg),
		sign(sk1, msg),
		sign(sk2, msg),
	}

	// The signed transaction would specify which of the public keys have been
	// used to sign it. The aggregator should verify each individual signature,
	// until it has found a sufficient threshold of valid signatures.
	var (
		indices      = []int{0, 2}
		filteredPKs  = make([]*PublicKey, len(indices))
		filteredSigs = make([]*Signature, len(indices))
	)
	for i, index := range indices {
		pk := pks[index]
		filteredPKs[i] = pk
		sig := sigs[index]
		filteredSigs[i] = sig

		valid := Verify(pk, sig, msg)
		require.True(valid)
	}

	// Once the aggregator has the required threshold of signatures, it can
	// aggregate the signatures.
	aggregatedSig, err := AggregateSignatures(filteredSigs)
	require.NoError(err)

	// For anyone looking for a proof of the aggregated signature's correctness,
	// they can aggregate the public keys and verify the aggregated signature.
	aggregatedPK, err := AggregatePublicKeys(filteredPKs)
	require.NoError(err)

	valid := Verify(aggregatedPK, aggregatedSig, msg)
	require.True(valid)
}
