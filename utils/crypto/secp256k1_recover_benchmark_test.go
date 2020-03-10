package crypto

import (
	"testing"

	"github.com/ava-labs/gecko/utils/hashing"
)

// NumRecoveries is the number of recoveries to run per operation
const NumRecoveries = 1

var (
	secpSigs [][]byte
)

func init() {
	factory := FactorySECP256K1R{}

	hash := hashing.ComputeHash256(nil)
	for i := byte(0); i < NumRecoveries; i++ {
		key, err := factory.NewPrivateKey()
		if err != nil {
			panic(err)
		}
		sig, err := key.SignHash(hash)
		if err != nil {
			panic(err)
		}
		secpSigs = append(secpSigs, sig)
	}
}

func recover() {
	factory := FactorySECP256K1R{}
	hash := hashing.ComputeHash256(nil)
	for _, sig := range secpSigs {
		if _, err := factory.RecoverHashPublicKey(hash, sig); err != nil {
			panic(err)
		}
	}
}

// BenchmarkSecp256k1RecoverVerify runs the benchmark with secp sig
func BenchmarkSecp256k1RecoverVerify(b *testing.B) {
	for n := 0; n < b.N; n++ {
		recover()
	}
}
