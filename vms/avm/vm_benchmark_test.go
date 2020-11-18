// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/ava-labs/avalanchego/utils/codec"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/vms/components/avax"

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

// GetAllUTXOsBenchmark is a helper func to benchmark the GetAllUTXOs depending on the size
func GetAllUTXOsBenchmark(b *testing.B, utxoCount int) {

	vm := VM{}
	vm.genesisCodec = codec.New(math.MaxUint32, 1<<20)
	_ = vm.genesisCodec.RegisterType(&avax.TestAddressable{})
	c := codec.New(math.MaxUint32, 1<<20)
	_ = c.RegisterType(&secp256k1fx.TransferOutput{})

	vm.codec = &codecRegistry{
		genesisCodec:  vm.genesisCodec,
		codec:         c,
		index:         0,
		typeToFxIndex: vm.typeToFxIndex,
	}
	vm.state = &prefixedState{
		state: &state{State: avax.State{
			Cache:        &cache.LRU{Size: stateCacheSize},
			DB:           prefixdb.New([]byte{1}, memdb.New()),
			GenesisCodec: vm.genesisCodec,
			Codec:        c,
		}},

		tx:       &cache.LRU{Size: idCacheSize},
		utxo:     &cache.LRU{Size: idCacheSize},
		txStatus: &cache.LRU{Size: idCacheSize},

		uniqueTx: &cache.EvictableLRU{Size: txCacheSize},
	}

	// #nosec G404
	for i := 0; i < utxoCount; i++ {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: rand.Uint32(),
			},
			Asset: avax.Asset{ID: ids.ID{'y', 'e', 'e', 't'}},
			Out: &secp256k1fx.TransferOutput{
				Amt: 100000,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{addrs[0]},
					Threshold: 1,
				},
			},
		}

		if err := vm.state.FundUTXO(utxo); err != nil {
			b.Fatal(err)
		}
	}

	addrsSet := ids.ShortSet{}
	addrsSet.Add(addrs[0])

	var (
		err               error
		notPaginatedUTXOs []*avax.UTXO
	)

	// Fetch all UTXOs older version
	notPaginatedUTXOs, _, _, err = vm.getAllUTXOs(addrsSet, ids.ShortEmpty, ids.Empty)
	if err != nil {
		b.Fatal(err)
	}

	if len(notPaginatedUTXOs) != utxoCount {
		b.Fatalf("Wrong number of utxos. Expected (%d) returned (%d)", utxoCount, len(notPaginatedUTXOs))
	}

}

func Benchmark100GetUTXosExistingVersion(b *testing.B) {
	GetAllUTXOsBenchmark(b, 100)
}

func Benchmark10000GetUTXosExistingVersion(b *testing.B) {
	GetAllUTXOsBenchmark(b, 10000)
}

func Benchmark100000GetUTXosExistingVersion(b *testing.B) {
	GetAllUTXOsBenchmark(b, 100000)
}
