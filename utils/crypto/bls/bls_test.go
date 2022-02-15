// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/utils"
)

func TestT(t *testing.T) {
	assert := assert.New(t)

	// People in the network would privately generate their secret keys
	sk0, err := NewSecretKey()
	assert.NoError(err)
	sk1, err := NewSecretKey()
	assert.NoError(err)
	sk2, err := NewSecretKey()
	assert.NoError(err)

	// All the public keys would be registered on chain
	pks := []*PublicKey{
		PublicFromSecretKey(sk0),
		PublicFromSecretKey(sk1),
		PublicFromSecretKey(sk2),
	}

	// The transaction's unsigned bytes are publicly known.
	msg := utils.RandomBytes(1234)

	// People may attempt time sign the transaction.
	sigs := []*Signature{
		Sign(sk0, msg),
		Sign(sk1, msg),
		Sign(sk2, msg),
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
		assert.True(valid)
	}

	// Once the aggregator has the required threshold of signatures, it can
	// aggregate the signatures.
	aggregatedSig, ok := AggregateSignatures(filteredSigs)
	assert.True(ok)

	// For anyone looking for a proof of the aggregated signature's correctness,
	// they can aggregate the public keys and verify the aggregated signature.
	aggregatedPK, ok := AggregatePublicKeys(filteredPKs)
	assert.True(ok)

	valid := Verify(aggregatedPK, aggregatedSig, msg)
	assert.True(valid)

	pkBytes := PublicKeyToBytes(aggregatedPK)
	sigBytes := SignatureToBytes(aggregatedSig)

	t.Fatalf(
		"\n%d: %v"+
			"\n%d: %v",
		len(pkBytes), pkBytes,
		len(sigBytes), sigBytes,
	)
}
