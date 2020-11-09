// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
)

func BenchmarkLoadUser(b *testing.B) {
	runLoadUserBenchmark := func(b *testing.B, numKeys int) {
		// This will segfault instead of failing gracefully if there's an error
		_, _, vm, _ := GenesisVM(nil)
		ctx := vm.ctx
		defer func() {
			if err := vm.Shutdown(); err != nil {
				b.Fatal(err)
			}
			ctx.Lock.Unlock()
		}()

		db, err := vm.ctx.Keystore.GetDatabase(username, password)
		if err != nil {
			b.Fatalf("Failed to get user keystore db: %s", err)
		}
		defer db.Close()

		user := userState{vm: vm}
		factory := crypto.FactorySECP256K1R{}

		addresses := make([]ids.ShortID, numKeys)
		for i := 0; i < numKeys; i++ {
			skIntf, err := factory.NewPrivateKey()
			if err != nil {
				b.Fatalf("problem generating private key: %s", err)
			}
			sk := skIntf.(*crypto.PrivateKeySECP256K1R)

			if err := user.SetKey(db, sk); err != nil {
				b.Fatalf("problem saving private key: %s", err)
			}
			addresses[i] = sk.PublicKey().Address()
		}

		if err := user.SetAddresses(db, addresses); err != nil {
			b.Fatalf("problem saving address: %s", err)
		}

		b.ResetTimer()

		fromAddrs := ids.ShortSet{}
		for n := 0; n < b.N; n++ {
			addrIndex := n % numKeys
			fromAddrs.Clear()
			fromAddrs.Add(addresses[addrIndex])
			if _, _, err := vm.LoadUser(username, password, fromAddrs); err != nil {
				b.Fatalf("Failed to load user: %s", err)
			}
		}
	}

	benchmarkSize := []int{10, 100, 1000, 10000}
	for _, numKeys := range benchmarkSize {
		b.Run(fmt.Sprintf("NumKeys=%d", numKeys), func(b *testing.B) {
			runLoadUserBenchmark(b, numKeys)
		})
	}
}
