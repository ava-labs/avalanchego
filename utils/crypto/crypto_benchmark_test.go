// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// NumVerifies is the number of verifications to run per operation
const NumVerifies = 1

// The different signature schemes
const (
	RSA = iota
	RSAPSS
	ED25519
	SECP256K1
)

var (
	hashes [][]byte

	keys [][]PublicKey
	sigs [][][]byte
)

func init() {
	// Setup hashes:
	bytes := ids.ID{}
	for i := uint64(0); i < NumVerifies; i++ {
		bytes[i%32]++
		hash := hashing.ComputeHash256(bytes[:])
		hashes = append(hashes, hash)
	}

	// Setup signatures:
	factories := []Factory{
		RSA:       &FactoryRSA{},
		RSAPSS:    &FactoryRSAPSS{},
		ED25519:   &FactoryED25519{},
		SECP256K1: &FactorySECP256K1R{},
	}
	for _, f := range factories {
		fKeys := []PublicKey{}
		fSigs := [][]byte{}
		for i := uint64(0); i < NumVerifies; i++ {
			privateKey, err := f.NewPrivateKey()
			if err != nil {
				panic(err)
			}

			publicKey := privateKey.PublicKey()
			sig, err := privateKey.SignHash(hashes[i])
			if err != nil {
				panic(err)
			}

			fKeys = append(fKeys, publicKey)
			fSigs = append(fSigs, sig)
		}
		keys = append(keys, fKeys)
		sigs = append(sigs, fSigs)
	}
}

func verify(algo int) {
	for i := 0; i < NumVerifies; i++ {
		if !keys[algo][i].VerifyHash(hashes[i], sigs[algo][i]) {
			panic("Verification failed")
		}
	}
}

// BenchmarkRSAVerify runs the benchmark with RSA keys
func BenchmarkRSAVerify(b *testing.B) {
	for n := 0; n < b.N; n++ {
		verify(RSA)
	}
}

// BenchmarkRSAPSSVerify runs the benchmark with RSAPSS keys
func BenchmarkRSAPSSVerify(b *testing.B) {
	for n := 0; n < b.N; n++ {
		verify(RSAPSS)
	}
}

// BenchmarkED25519Verify runs the benchmark with ED25519 keys
func BenchmarkED25519Verify(b *testing.B) {
	for n := 0; n < b.N; n++ {
		verify(ED25519)
	}
}

// BenchmarkSECP256k1Verify runs the benchmark with SECP256K1 keys
func BenchmarkSECP256k1Verify(b *testing.B) {
	for n := 0; n < b.N; n++ {
		verify(SECP256K1)
	}
}
